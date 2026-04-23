package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/daemonipc"
	"github.com/termix/termix/go/internal/diagnostics"
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
	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(paths.StateDir),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.Load(paths.CredentialsFile)
		},
		NewControl: func(creds credentials.StoredCredentials) (session.ControlClient, error) {
			return controlapi.New(creds.ServerBaseURL, http.DefaultTransport)
		},
		Tmux:     tmux.NewRunner(),
		Now:      time.Now,
		Hostname: os.Hostname,
		DoctorChecks: func(ctx context.Context) ([]string, error) {
			return doctor.Checks(ctx)
		},
	})

	server := daemonipc.NewServer(manager)
	log.Fatal(server.Serve(listener))
}
