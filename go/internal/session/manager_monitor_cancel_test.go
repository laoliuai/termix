package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
)

// TestEndSessionStopsPaneMonitor asserts that the per-session host-resize
// monitor goroutine is cancelled when the session ends, so it stops polling
// tmux.PaneSize (and forking `tmux display-message` subprocesses) for a
// now-dead session. Regression guard for the goroutine/subprocess leak where
// the monitor was tied to the daemon lifetime context and never stopped on
// EndSession.
func TestEndSessionStopsPaneMonitor(t *testing.T) {
	store := NewStore(t.TempDir())
	id := uuid.NewString()
	if err := store.Save(LocalSession{
		SessionID:       id,
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_monitor_cancel",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tmux := &paneMonitorFakeTmux{seq: [][2]uint32{{120, 40}}}
	relay := &paneMonitorFakeRelay{}
	m := NewManager(ManagerOptions{
		Store: store,
		Tmux:  tmux,
		Relay: relay,
		Snapshot: func(context.Context, string) ([]byte, error) {
			return []byte("snap"), nil
		},
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{AccessToken: "token"}, nil
		},
		Control: &endSessionFakeControl{},
	})

	// Start the monitor for this session the way StartSession does, but with a
	// fast interval so the test runs quickly.
	m.startPaneSizeMonitor(m.monitorContext(), id, "termix_monitor_cancel", 10*time.Millisecond)

	// Let it poll a few times so we have a non-zero baseline.
	deadline := time.After(2 * time.Second)
	for tmux.callCount() < 3 {
		select {
		case <-deadline:
			t.Fatal("monitor never polled PaneSize")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if _, err := m.EndSession(context.Background(), &daemonv1.EndSessionRequest{SessionId: id}); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	// After EndSession cancels the monitor, polling must stop. Give the
	// goroutine a moment to observe the cancellation, snapshot the count, then
	// confirm it does not advance over several more intervals.
	time.Sleep(40 * time.Millisecond)
	stopped := tmux.callCount()
	time.Sleep(120 * time.Millisecond)
	if got := tmux.callCount(); got != stopped {
		t.Fatalf("monitor kept polling after EndSession: count went %d -> %d (expected no further polls)", stopped, got)
	}
}
