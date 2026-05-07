package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
)

// listSessionsFakeTmux drives HasSession/PanePID per-session so we can
// exercise both live and orphan rows from a single ListSessions call.
type listSessionsFakeTmux struct {
	live map[string]bool
	pids map[string]int
}

func (l *listSessionsFakeTmux) EnsureAvailable(context.Context) error                 { return nil }
func (l *listSessionsFakeTmux) StartSession(context.Context, StartSpec) error         { return nil }
func (l *listSessionsFakeTmux) StartOutputPipe(context.Context, string, string) error { return nil }
func (l *listSessionsFakeTmux) StopOutputPipe(context.Context, string) error          { return nil }
func (l *listSessionsFakeTmux) HasSession(_ context.Context, name string) bool {
	return l.live[name]
}
func (l *listSessionsFakeTmux) ResizeWindow(context.Context, string, uint32, uint32) error {
	return nil
}
func (l *listSessionsFakeTmux) KillSession(context.Context, string) error { return nil }
func (l *listSessionsFakeTmux) PanePID(_ context.Context, name string) (int, error) {
	return l.pids[name], nil
}

// TestListSessionsEnrichesLiveSummariesWithPIDAndCWD asserts that
// ListSessions populates cwd, started_at, pane_pid, and live_in_tmux
// for sessions whose tmux pane is still alive.
func TestListSessionsEnrichesLiveSummariesWithPIDAndCWD(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	idLive := uuid.NewString()
	idOrphan := uuid.NewString()
	now := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	if err := store.Save(LocalSession{
		SessionID:       idLive,
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_test_live",
		Cwd:             "/tmp/proj",
		StartedAt:       now,
	}); err != nil {
		t.Fatalf("seed live: %v", err)
	}
	if err := store.Save(LocalSession{
		SessionID:       idOrphan,
		Tool:            "codex",
		Status:          "running",
		TmuxSessionName: "termix_test_orphan",
	}); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	tmux := &listSessionsFakeTmux{
		live: map[string]bool{"termix_test_live": true},
		pids: map[string]int{"termix_test_live": 4242},
	}
	manager := NewManager(ManagerOptions{
		Store: store,
		Tmux:  tmux,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
	})

	resp, err := manager.ListSessions(context.Background(), &daemonv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(resp.GetSessions()) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.GetSessions()))
	}

	byID := map[string]*daemonv1.SessionSummary{}
	for _, s := range resp.GetSessions() {
		byID[s.GetSessionId()] = s
	}
	live := byID[idLive]
	if live == nil {
		t.Fatalf("missing live summary for %s", idLive)
	}
	if !live.GetLiveInTmux() {
		t.Fatalf("expected live_in_tmux=true, got false")
	}
	if live.GetPanePid() != 4242 {
		t.Fatalf("expected pane_pid=4242, got %d", live.GetPanePid())
	}
	if live.GetCwd() != "/tmp/proj" {
		t.Fatalf("expected cwd=/tmp/proj, got %q", live.GetCwd())
	}
	if live.GetStartedAt() != "2026-05-07T10:00:00Z" {
		t.Fatalf("expected started_at=2026-05-07T10:00:00Z, got %q", live.GetStartedAt())
	}

	orphan := byID[idOrphan]
	if orphan == nil {
		t.Fatalf("missing orphan summary for %s", idOrphan)
	}
	if orphan.GetLiveInTmux() {
		t.Fatalf("expected live_in_tmux=false for orphan, got true")
	}
	if orphan.GetPanePid() != 0 {
		t.Fatalf("expected pane_pid=0 for orphan, got %d", orphan.GetPanePid())
	}
}
