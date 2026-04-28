package main

import (
	"context"
	"errors"
	"log"
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
)

func main() {
	paths := config.DefaultHostPaths()
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		log.Fatal(err)
	}

	socketPath := daemonipc.SocketPath(paths)
	listener, err := daemonipc.Listen(socketPath)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	doctor := diagnostics.NewRunner(paths)
	cfg, err := config.LoadHostConfig(paths.HostConfigFile)
	if err != nil {
		log.Fatal(err)
	}
	creds, err := credentials.Load(paths.CredentialsFile)
	if err != nil {
		log.Fatal(err)
	}

	// One control client + Refresher pair for the lifetime of termixd. The
	// Refresher swaps the access token via /auth/refresh whenever it's about
	// to expire (proactive) or has been rejected by control (reactive).
	controlClient, err := controlapi.New(creds.ServerBaseURL, http.DefaultTransport)
	if err != nil {
		log.Fatal(err)
	}
	refresher := credentials.NewRefresher(
		paths.CredentialsFile,
		&controlRefreshAdapter{client: controlClient},
		nil, // time.Now
	)

	// Refresh up front so the relay handshake never starts with a stale token
	// (relayclient does not currently re-auth after the initial Connect).
	freshCreds, err := refresher.EnsureFresh(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	relayClient := relayclient.New(cfg.RelayWSURL, freshCreds.AccessToken, freshCreds.DeviceID)
	if err := relayClient.Connect(context.Background()); err != nil {
		log.Fatal(err)
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

	// Reaper: reconcile local-state sessions against tmux every 30s and on
	// startup. Any session whose tmux pane is gone gets PATCHed to exited
	// in the control DB so the SPA's session list stops showing zombies.
	go func() {
		if err := manager.Reap(context.Background()); err != nil {
			log.Printf("reap: initial sweep failed: %v", err)
		}
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			if err := manager.Reap(context.Background()); err != nil {
				log.Printf("reap: periodic sweep failed: %v", err)
			}
		}
	}()

	server := daemonipc.NewServer(manager)
	log.Fatal(server.Serve(listener))
}

// controlRefreshAdapter bridges *controlapi.Client → credentials.RefreshClient.
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
