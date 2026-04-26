package tests

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/session"
)

type fakeRelayClient struct {
	mu              sync.Mutex
	announced       session.LocalSession
	snapshot        []byte
	outputs         [][]byte
	snapshotHandler func(context.Context, string) ([]byte, error)
	inputHandler    func(context.Context, string, []byte) error
}

func (f *fakeRelayClient) AnnounceSession(_ context.Context, s session.LocalSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.announced = s
	return nil
}

func (f *fakeRelayClient) PublishSnapshot(_ context.Context, _ string, snapshot []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot = snapshot
	return nil
}

func (f *fakeRelayClient) PublishOutput(_ context.Context, _ string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dup := make([]byte, len(payload))
	copy(dup, payload)
	f.outputs = append(f.outputs, dup)
	return nil
}

func (f *fakeRelayClient) outputsCopy() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.outputs))
	for i, b := range f.outputs {
		dup := make([]byte, len(b))
		copy(dup, b)
		out[i] = dup
	}
	return out
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

func TestManagerStreamsLiveTmuxOutputToRelay(t *testing.T) {
	relay := &fakeRelayClient{}
	tmuxRunner := &fakeTmuxRunner{}
	fifoDir := t.TempDir()

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
				SessionId:       uuid.MustParse("44444444-4444-4444-4444-444444444444"),
				TmuxSessionName: "termix_44444444-4444-4444-4444-444444444444",
				Status:          "starting",
			},
			updateResponse: &openapi.Session{
				Id:              uuid.MustParse("44444444-4444-4444-4444-444444444444"),
				UserId:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				HostDeviceId:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Tool:            openapi.SessionToolCodex,
				LaunchCommand:   "codex",
				Cwd:             "/tmp/project",
				CwdLabel:        "project",
				TmuxSessionName: "termix_44444444-4444-4444-4444-444444444444",
				Status:          "running",
			},
		},
		Tmux: tmuxRunner,
		Now: func() time.Time {
			return time.Date(2026, 4, 26, 21, 0, 0, 0, time.UTC)
		},
		Hostname: func() (string, error) {
			return "devbox", nil
		},
		DoctorChecks: func(context.Context) ([]string, error) {
			return nil, nil
		},
		Relay:         relay,
		OutputFifoDir: fifoDir,
	})

	_, err := manager.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool: "codex",
		Cwd:  "/tmp/project",
	})
	if err != nil {
		t.Fatalf("StartSession returned error: %v", err)
	}

	// The manager should have asked tmux to pipe-pane output into a FIFO it created.
	if tmuxRunner.outputPipeSession != "termix_44444444-4444-4444-4444-444444444444" {
		t.Fatalf("expected tmux pipe-pane on the new session, got %q", tmuxRunner.outputPipeSession)
	}
	if tmuxRunner.outputPipeFifoPath == "" {
		t.Fatal("expected manager to pass a FIFO path to tmux pipe-pane")
	}

	// Simulate tmux writing pane output into the FIFO.
	writer, err := os.OpenFile(tmuxRunner.outputPipeFifoPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open FIFO for write: %v", err)
	}
	const live = "tmux-live-output-1234"
	if _, err := writer.Write([]byte(live)); err != nil {
		t.Fatalf("write to FIFO: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close FIFO writer: %v", err)
	}

	// Wait for the manager's read goroutine to forward bytes to the relay.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		outs := relay.outputsCopy()
		joined := bytes.Join(outs, nil)
		if bytes.Contains(joined, []byte(live)) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected relay to receive live output containing %q, got %q",
		live, bytes.Join(relay.outputsCopy(), nil))
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
