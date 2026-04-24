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

type fakeRelayClient struct {
	announced       session.LocalSession
	snapshot        []byte
	snapshotHandler func(context.Context, string) ([]byte, error)
	inputHandler    func(context.Context, string, []byte) error
}

func (f *fakeRelayClient) AnnounceSession(_ context.Context, s session.LocalSession) error {
	f.announced = s
	return nil
}

func (f *fakeRelayClient) PublishSnapshot(_ context.Context, _ string, snapshot []byte) error {
	f.snapshot = snapshot
	return nil
}

func (f *fakeRelayClient) PublishOutput(context.Context, string, []byte) error {
	return nil
}

func (f *fakeRelayClient) SetSnapshotHandler(fn func(context.Context, string) ([]byte, error)) {
	f.snapshotHandler = fn
}

func (f *fakeRelayClient) SetInputHandler(fn func(context.Context, string, []byte) error) {
	f.inputHandler = fn
}

func TestManagerAnnouncesRunningSessionToRelay(t *testing.T) {
	relay := &fakeRelayClient{}
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
				Tool:            openapi.SessionToolCodex,
				LaunchCommand:   "codex",
				Cwd:             "/tmp/project",
				CwdLabel:        "project",
				TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
				Status:          "running",
			},
		},
		Tmux: &fakeTmuxRunner{},
		Now: func() time.Time {
			return time.Date(2026, 4, 23, 17, 0, 0, 0, time.UTC)
		},
		Hostname: func() (string, error) {
			return "devbox", nil
		},
		DoctorChecks: func(context.Context) ([]string, error) {
			return []string{"tmux: ok"}, nil
		},
		Relay: relay,
	})

	_, err := manager.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool: "codex",
		Cwd:  "/tmp/project",
	})
	if err != nil {
		t.Fatalf("StartSession returned error: %v", err)
	}
	if relay.announced.SessionID == "" {
		t.Fatal("expected session announcement to relay")
	}
}

func TestManagerWiresRelayInputToTmuxSession(t *testing.T) {
	store := session.NewStore(t.TempDir())
	if err := store.Save(session.LocalSession{
		SessionID:       "session-1",
		TmuxSessionName: "termix_session-1",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	relay := &fakeRelayClient{}
	var gotTmuxSessionName string
	var gotPayload []byte

	_ = session.NewManager(session.ManagerOptions{
		Store: store,
		Relay: relay,
		Input: func(_ context.Context, tmuxSessionName string, payload []byte) error {
			gotTmuxSessionName = tmuxSessionName
			gotPayload = append([]byte(nil), payload...)
			return nil
		},
	})

	if relay.inputHandler == nil {
		t.Fatal("expected relay input handler to be set")
	}
	if err := relay.inputHandler(context.Background(), "session-1", []byte("echo hi\n")); err != nil {
		t.Fatalf("input handler returned error: %v", err)
	}
	if gotTmuxSessionName != "termix_session-1" {
		t.Fatalf("unexpected tmux session name: %q", gotTmuxSessionName)
	}
	if string(gotPayload) != "echo hi\n" {
		t.Fatalf("unexpected payload: %q", gotPayload)
	}
}
