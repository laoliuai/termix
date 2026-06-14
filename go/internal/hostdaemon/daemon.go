package hostdaemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/daemonipc"
	"github.com/termix/termix/go/internal/diagnostics"
	"github.com/termix/termix/go/internal/relayclient"
	"github.com/termix/termix/go/internal/session"
	"github.com/termix/termix/go/internal/tmux"
	"google.golang.org/grpc"
)

const (
	daemonOperationTimeout = 10 * time.Second
	reaperStopTimeout      = time.Second
)

func Run(ctx context.Context, paths config.HostPaths, version string) error {
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	ctx = runCtx

	socketPath := daemonipc.SocketPath(paths)
	listener, err := daemonipc.Listen(socketPath)
	if err != nil {
		return fmt.Errorf("listen on daemon socket: %w", err)
	}
	defer listener.Close()

	cfg, err := config.LoadHostConfig(paths.HostConfigFile)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	doctor := diagnostics.NewRunner(paths)
	// Surface the proxy check at boot (after policy is applied so the
	// reported state matches what HTTP clients will actually see) so a
	// proxy-related failure is visible in the daemon log without the
	// user having to run `termix doctor` separately.
	if checks, err := doctor.Checks(ctx); err == nil {
		for _, c := range checks {
			if strings.HasPrefix(c, "proxy:") {
				log.Println(c)
			}
		}
	}
	creds, err := credentials.Load(paths.CredentialsFile)
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}

	// One control client + Refresher pair for the lifetime of termixd. The
	// Refresher swaps the access token via /auth/refresh whenever it's about
	// to expire (proactive) or has been rejected by control (reactive).
	controlClient, err := controlapi.New(creds.ServerBaseURL, http.DefaultTransport)
	if err != nil {
		return fmt.Errorf("create control client: %w", err)
	}
	refresher := credentials.NewRefresher(
		paths.CredentialsFile,
		&controlRefreshAdapter{client: controlClient},
		nil, // time.Now
	)

	supervisor := relayclient.NewSupervisor(relayclient.SupervisorOptions{
		Factory: func(ctx context.Context, creds credentials.StoredCredentials) (*relayclient.Client, error) {
			c := relayclient.New(cfg.RelayWSURL, creds.AccessToken, creds.DeviceID)
			if err := c.Connect(ctx); err != nil {
				return nil, err
			}
			return c, nil
		},
		Refresher:       refresher,
		Clock:           relayclient.RealClock(),
		Rand:            rand.Float64,
		RequestShutdown: cancelRun,
	})

	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(paths.StateDir),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			refreshCtx, cancel := context.WithTimeout(ctx, daemonOperationTimeout)
			defer cancel()
			return refresher.EnsureFresh(refreshCtx)
		},
		RefreshCredentials: func(ctx context.Context) (credentials.StoredCredentials, error) {
			refreshCtx, cancel := context.WithTimeout(ctx, daemonOperationTimeout)
			defer cancel()
			return refresher.RefreshNow(refreshCtx)
		},
		IsAuthError: isControlAuthError,
		Control:     controlClient,
		NewControl: func(creds credentials.StoredCredentials) (session.ControlClient, error) {
			return controlapi.New(creds.ServerBaseURL, http.DefaultTransport)
		},
		Tmux:  tmux.NewRunner(),
		Relay: supervisor,
		Snapshot: func(ctx context.Context, sessionName string) ([]byte, error) {
			return tmux.CaptureSnapshotWithCursor(ctx, sessionName)
		},
		Input: func(ctx context.Context, sessionName string, payload []byte) error {
			return tmux.InjectInput(ctx, sessionName, payload)
		},
		Now:      time.Now,
		Hostname: os.Hostname,
		DoctorChecks: func(ctx context.Context) ([]string, error) {
			return doctor.Checks(ctx)
		},
		OutputFifoDir:   filepath.Join(paths.RunDir, "output-fifos"),
		LogDir:          paths.LogDir,
		Version:         version,
		RequestShutdown: cancelRun,
		RelayStateSource: func() session.RelayStateSnapshot {
			st := supervisor.State()
			return session.RelayStateSnapshot{
				Phase:           string(st.Phase),
				Attempt:         st.Attempt,
				LastConnectedAt: st.LastConnectedAt,
				LastError:       st.LastError,
				NextRetryAt:     st.NextRetryAt,
				AuthFailures:    st.AuthFailures,
			}
		},
		StartTime:   time.Now(),
		BaseContext: ctx,
	})

	supervisor.SetResizeHandler(func(ctx context.Context, sessionID string, cols, rows uint32) error {
		resizeCtx, cancel := context.WithTimeout(ctx, daemonOperationTimeout)
		defer cancel()
		return manager.ResizeSession(resizeCtx, sessionID, cols, rows)
	})

	supervisor.SetReconnectCallback(manager.ReannounceAllSessions)
	go func() {
		if err := supervisor.Run(ctx); err != nil {
			log.Printf("relay supervisor exited: %v", err)
		}
	}()

	reaperDone := runReaper(ctx, 30*time.Second, daemonOperationTimeout, manager.Reap)

	server := daemonipc.NewServer(manager)
	if err := serveDaemonIPC(ctx, server, listener); err != nil {
		if ctx.Err() != nil {
			if waitErr := waitForReaper(reaperDone, reaperStopTimeout); waitErr != nil {
				return waitErr
			}
		}
		return fmt.Errorf("serve daemon IPC: %w", err)
	}
	return nil
}

func serveDaemonIPC(ctx context.Context, server *grpc.Server, listener net.Listener) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	shutdownDone := make(chan struct{})
	go func() {
		<-serveCtx.Done()
		_ = listener.Close()
		server.Stop()
		close(shutdownDone)
	}()

	err := server.Serve(listener)
	if ctx.Err() != nil {
		<-shutdownDone
		return ctx.Err()
	}
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	return nil
}

func waitForReaper(done <-chan struct{}, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("host daemon reaper did not stop after context cancellation")
	}
}

func runReaper(ctx context.Context, interval, operationTimeout time.Duration, reap func(context.Context) error) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := reapWithTimeout(ctx, operationTimeout, reap); err != nil {
			log.Printf("reap: initial sweep failed: %v", err)
		}

		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := reapWithTimeout(ctx, operationTimeout, reap); err != nil {
					log.Printf("reap: periodic sweep failed: %v", err)
				}
			}
		}
	}()
	return done
}

func reapWithTimeout(ctx context.Context, timeout time.Duration, reap func(context.Context) error) error {
	reapCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return reap(reapCtx)
}

// controlRefreshAdapter bridges *controlapi.Client -> credentials.RefreshClient.
// It collapses the OpenAPI-flavoured response into the small RefreshResult the
// credentials package consumes, and translates a 401 into ErrReLoginRequired
// so the user sees a "run `termix login` again" message rather than a raw
// HTTP error.
type controlRefreshAdapter struct {
	client *controlapi.Client
}

func (a *controlRefreshAdapter) RefreshAccessToken(ctx context.Context, refreshToken string) (*credentials.RefreshResult, error) {
	res, err := a.client.RefreshAccessToken(ctx, refreshToken)
	if err != nil {
		if isControlAuthError(err) {
			return nil, credentials.ErrReLoginRequired
		}
		return nil, err
	}
	return &credentials.RefreshResult{
		AccessToken:      res.AccessToken,
		ExpiresInSeconds: res.ExpiresInSeconds,
	}, nil
}

func isControlAuthError(err error) bool {
	var ae *controlapi.APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusUnauthorized
}
