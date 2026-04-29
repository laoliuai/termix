package hostdaemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

func Run(ctx context.Context, paths config.HostPaths) error {
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	socketPath := daemonipc.SocketPath(paths)
	listener, err := daemonipc.Listen(socketPath)
	if err != nil {
		return fmt.Errorf("listen on daemon socket: %w", err)
	}
	defer listener.Close()

	doctor := diagnostics.NewRunner(paths)
	cfg, err := config.LoadHostConfig(paths.HostConfigFile)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
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

	// Refresh up front so the relay handshake never starts with a stale token
	// (relayclient does not currently re-auth after the initial Connect).
	freshCreds, err := refresher.EnsureFresh(ctx)
	if err != nil {
		return fmt.Errorf("refresh credentials: %w", err)
	}

	relayClient := relayclient.New(cfg.RelayWSURL, freshCreds.AccessToken, freshCreds.DeviceID)
	if err := relayClient.Connect(ctx); err != nil {
		return fmt.Errorf("connect relay: %w", err)
	}

	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(paths.StateDir),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return refresher.EnsureFresh(context.Background())
		},
		RefreshCredentials: refresher.RefreshNow,
		IsAuthError:        isControlAuthError,
		Control:            controlClient,
		NewControl: func(creds credentials.StoredCredentials) (session.ControlClient, error) {
			return controlapi.New(creds.ServerBaseURL, http.DefaultTransport)
		},
		Tmux:  tmux.NewRunner(),
		Relay: relayClient,
		Snapshot: func(ctx context.Context, sessionName string) ([]byte, error) {
			return tmux.CaptureSnapshot(ctx, sessionName)
		},
		Input: func(ctx context.Context, sessionName string, payload []byte) error {
			return tmux.InjectInput(ctx, sessionName, payload)
		},
		Now:      time.Now,
		Hostname: os.Hostname,
		DoctorChecks: func(ctx context.Context) ([]string, error) {
			return doctor.Checks(ctx)
		},
		OutputFifoDir: filepath.Join(paths.RunDir, "output-fifos"),
	})

	runReaper(ctx, 30*time.Second, manager.Reap)

	server := daemonipc.NewServer(manager)
	if err := serveDaemonIPC(ctx, server, listener); err != nil {
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
		server.GracefulStop()
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

func runReaper(ctx context.Context, interval time.Duration, reap func(context.Context) error) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := reap(ctx); err != nil {
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
				if err := reap(ctx); err != nil {
					log.Printf("reap: periodic sweep failed: %v", err)
				}
			}
		}
	}()
	return done
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
