package tests

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/daemonipc"
	"github.com/termix/termix/go/internal/session"
)

func TestDaemonIPCStartSessionRoundTrip(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := daemonipc.Listen(socketPath)
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer listener.Close()

	control := &fakeControlClient{
		createResponse: &openapi.CreateSessionResponse{
			SessionId:       uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
			Status:          "starting",
		},
		updateResponse: &openapi.Session{
			Id:              uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			HostDeviceId:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			UserId:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Tool:            openapi.SessionToolCodex,
			LaunchCommand:   "codex",
			Cwd:             "/tmp/project",
			CwdLabel:        "project",
			TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
			Status:          "running",
		},
	}

	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(t.TempDir()),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{
				ServerBaseURL: "https://termix.example.com",
				UserID:        "11111111-1111-1111-1111-111111111111",
				DeviceID:      "22222222-2222-2222-2222-222222222222",
				AccessToken:   "access-token",
			}, nil
		},
		Control: control,
		Tmux:    &fakeTmuxRunner{},
		Now: func() time.Time {
			return time.Date(2026, 4, 23, 15, 0, 0, 0, time.UTC)
		},
		Hostname: func() (string, error) {
			return "devbox", nil
		},
		DoctorChecks: func(context.Context) ([]string, error) {
			return []string{"tmux: ok"}, nil
		},
	})

	server := daemonipc.NewServer(manager)
	defer server.Stop()
	go func() {
		_ = server.Serve(listener)
	}()

	client, conn, err := daemonipc.Dial(context.Background(), socketPath)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close()

	healthResp, err := client.Health(context.Background(), &daemonv1.HealthRequest{})
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if healthResp.Status != "ok" {
		t.Fatalf("expected ok health status, got %q", healthResp.Status)
	}

	startResp, err := client.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool:  "codex",
		Name:  "fix auth",
		Cwd:   "/tmp/project",
		Env:   map[string]string{"LANG": "en_US.UTF-8"},
		Shell: "/bin/bash",
	})
	if err != nil {
		t.Fatalf("StartSession returned error: %v", err)
	}
	if startResp.Status != "running" {
		t.Fatalf("expected running status, got %q", startResp.Status)
	}

	listResp, err := client.ListSessions(context.Background(), &daemonv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(listResp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listResp.Sessions))
	}

	attachResp, err := client.AttachInfo(context.Background(), &daemonv1.AttachInfoRequest{SessionId: startResp.SessionId})
	if err != nil {
		t.Fatalf("AttachInfo returned error: %v", err)
	}
	if attachResp.TmuxSessionName != startResp.TmuxSessionName {
		t.Fatalf("expected attach session %q, got %q", startResp.TmuxSessionName, attachResp.TmuxSessionName)
	}
}
