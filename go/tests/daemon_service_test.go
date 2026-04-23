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

func TestManagerStartSessionCreatesCloudRecordStartsTmuxAndPersistsState(t *testing.T) {
	sessionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	deviceID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	control := &fakeControlClient{
		createResponse: &openapi.CreateSessionResponse{
			SessionId:       sessionID,
			TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
			Status:          "starting",
		},
		updateResponse: &openapi.Session{
			Id:              sessionID,
			HostDeviceId:    deviceID,
			UserId:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Tool:            openapi.SessionToolCodex,
			LaunchCommand:   "codex",
			Cwd:             "/tmp/project",
			CwdLabel:        "project",
			TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
			Status:          "running",
		},
	}
	tmuxRunner := &fakeTmuxRunner{}
	store := session.NewStore(t.TempDir())
	manager := session.NewManager(session.ManagerOptions{
		Store: store,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{
				ServerBaseURL: "https://termix.example.com",
				UserID:        "11111111-1111-1111-1111-111111111111",
				DeviceID:      "22222222-2222-2222-2222-222222222222",
				AccessToken:   "access-token",
			}, nil
		},
		Control: control,
		Tmux:    tmuxRunner,
		Now: func() time.Time {
			return time.Date(2026, 4, 23, 12, 30, 0, 0, time.UTC)
		},
		Hostname: func() (string, error) {
			return "devbox", nil
		},
		DoctorChecks: func(context.Context) ([]string, error) {
			return []string{"tmux: ok"}, nil
		},
	})

	resp, err := manager.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool:     "codex",
		Name:     "fix auth",
		Cwd:      "/tmp/project",
		Shell:    "/bin/bash",
		Term:     "xterm-256color",
		Language: "en_US.UTF-8",
		Env: map[string]string{
			"LANG": "en_US.UTF-8",
			"FOO":  "bar",
		},
	})
	if err != nil {
		t.Fatalf("StartSession returned error: %v", err)
	}

	if control.accessToken != "access-token" {
		t.Fatalf("expected access token to be forwarded, got %q", control.accessToken)
	}
	if control.createRequest.DeviceId.String() != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("unexpected device id in create request: %s", control.createRequest.DeviceId)
	}
	if control.createRequest.CwdLabel != "project" {
		t.Fatalf("expected cwd label project, got %q", control.createRequest.CwdLabel)
	}
	if control.updateRequest.Status != openapi.Running {
		t.Fatalf("expected running patch, got %q", control.updateRequest.Status)
	}
	if !tmuxRunner.ensureCalled {
		t.Fatal("expected tmux availability check")
	}
	if tmuxRunner.startSpec.ToolCommand != "codex" {
		t.Fatalf("expected tool command codex, got %q", tmuxRunner.startSpec.ToolCommand)
	}
	if tmuxRunner.startSpec.SessionName != "termix_33333333-3333-3333-3333-333333333333" {
		t.Fatalf("unexpected tmux session name %q", tmuxRunner.startSpec.SessionName)
	}

	persisted, err := store.Load(sessionID.String())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if persisted.Status != "running" {
		t.Fatalf("expected persisted status running, got %q", persisted.Status)
	}
	if resp.AttachCommand != "tmux attach-session -t termix_33333333-3333-3333-3333-333333333333" {
		t.Fatalf("unexpected attach command %q", resp.AttachCommand)
	}
}

func TestManagerListSessionsAndAttachInfoUsePersistedState(t *testing.T) {
	store := session.NewStore(t.TempDir())
	if err := store.Save(session.LocalSession{
		SessionID:       "session-1",
		Name:            "investigate flaky test",
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_session-1",
		AttachCommand:   "tmux attach-session -t termix_session-1",
		Cwd:             "/tmp/project",
		LaunchCommand:   "claude",
		StartedAt:       time.Date(2026, 4, 23, 13, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	manager := session.NewManager(session.ManagerOptions{
		Store: store,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		Control: &fakeControlClient{},
		Tmux:    &fakeTmuxRunner{},
		Now:     time.Now,
		Hostname: func() (string, error) {
			return "devbox", nil
		},
		DoctorChecks: func(context.Context) ([]string, error) {
			return []string{"tmux: ok"}, nil
		},
	})

	listResp, err := manager.ListSessions(context.Background(), &daemonv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(listResp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listResp.Sessions))
	}
	if listResp.Sessions[0].SessionId != "session-1" {
		t.Fatalf("expected listed session id session-1, got %q", listResp.Sessions[0].SessionId)
	}

	attachResp, err := manager.AttachInfo(context.Background(), &daemonv1.AttachInfoRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("AttachInfo returned error: %v", err)
	}
	if attachResp.AttachCommand != "tmux attach-session -t termix_session-1" {
		t.Fatalf("unexpected attach command %q", attachResp.AttachCommand)
	}
}

func TestManagerDoctorReturnsConfiguredChecks(t *testing.T) {
	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(t.TempDir()),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		Control: &fakeControlClient{},
		Tmux:    &fakeTmuxRunner{},
		Now:     time.Now,
		Hostname: func() (string, error) {
			return "devbox", nil
		},
		DoctorChecks: func(context.Context) ([]string, error) {
			return []string{"tmux: ok", "credentials: ok"}, nil
		},
	})

	resp, err := manager.Doctor(context.Background(), &daemonv1.DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if len(resp.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(resp.Checks))
	}
}

type fakeControlClient struct {
	accessToken    string
	createRequest  openapi.CreateSessionRequest
	createResponse *openapi.CreateSessionResponse
	updateSession  string
	updateRequest  openapi.UpdateSessionRequest
	updateResponse *openapi.Session
}

func (f *fakeControlClient) CreateHostSession(_ context.Context, accessToken string, req openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error) {
	f.accessToken = accessToken
	f.createRequest = req
	return f.createResponse, nil
}

func (f *fakeControlClient) UpdateHostSession(_ context.Context, accessToken string, sessionID string, req openapi.UpdateSessionRequest) (*openapi.Session, error) {
	f.accessToken = accessToken
	f.updateSession = sessionID
	f.updateRequest = req
	return f.updateResponse, nil
}

type fakeTmuxRunner struct {
	ensureCalled bool
	startSpec    session.StartSpec
}

func (f *fakeTmuxRunner) EnsureAvailable(context.Context) error {
	f.ensureCalled = true
	return nil
}

func (f *fakeTmuxRunner) StartSession(_ context.Context, spec session.StartSpec) error {
	f.startSpec = spec
	return nil
}
