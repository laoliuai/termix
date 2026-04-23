package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/session"
)

func TestDaemonDoctorAndHealthIntegration(t *testing.T) {
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
		Control: &fakeControlClient{
			createResponse: &openapi.CreateSessionResponse{
				SessionId:       uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
				Status:          "starting",
			},
			updateResponse: &openapi.Session{
				Id:              uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				UserId:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				HostDeviceId:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Tool:            openapi.SessionToolClaude,
				LaunchCommand:   "claude",
				Cwd:             "/tmp/project",
				CwdLabel:        "project",
				TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
				Status:          "running",
			},
		},
		Tmux: &fakeTmuxRunner{},
		Now: func() time.Time {
			return time.Date(2026, 4, 23, 16, 0, 0, 0, time.UTC)
		},
		Hostname: func() (string, error) {
			return "devbox", nil
		},
		DoctorChecks: func(context.Context) ([]string, error) {
			return []string{"tmux: ok", "socket: ok"}, nil
		},
	})

	healthResp, err := manager.Health(context.Background(), &daemonv1.HealthRequest{})
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if healthResp.Status != "ok" {
		t.Fatalf("expected ok health status, got %q", healthResp.Status)
	}

	doctorResp, err := manager.Doctor(context.Background(), &daemonv1.DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if len(doctorResp.Checks) != 2 {
		t.Fatalf("expected 2 doctor checks, got %d", len(doctorResp.Checks))
	}
}
