package tests

import (
	"testing"
	"time"

	"github.com/termix/termix/go/internal/session"
)

func TestSessionStoreRoundTrip(t *testing.T) {
	store := session.NewStore(t.TempDir())
	startedAt := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	want := session.LocalSession{
		SessionID:       "session-1",
		Name:            "fix auth",
		Tool:            "codex",
		Status:          "running",
		TmuxSessionName: "termix_session-1",
		AttachCommand:   "tmux attach-session -t termix_session-1",
		Cwd:             "/tmp/project",
		LaunchCommand:   "codex",
		StartedAt:       startedAt,
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := store.Load("session-1")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got.SessionID != want.SessionID {
		t.Fatalf("expected session id %q, got %q", want.SessionID, got.SessionID)
	}
	if got.TmuxSessionName != want.TmuxSessionName {
		t.Fatalf("expected tmux session name %q, got %q", want.TmuxSessionName, got.TmuxSessionName)
	}
	if got.StartedAt != startedAt {
		t.Fatalf("expected started_at %s, got %s", startedAt, got.StartedAt)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session, got %d", len(list))
	}
	if list[0].SessionID != want.SessionID {
		t.Fatalf("expected listed session id %q, got %q", want.SessionID, list[0].SessionID)
	}
}
