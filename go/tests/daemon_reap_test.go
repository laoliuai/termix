package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/session"
)

// reapingControlClient records every PATCH it receives so the test can
// assert which sessions were marked exited.
type reapingControlClient struct {
	patches   []reapPatch
	updateErr error
}

type reapPatch struct {
	sessionID string
	status    string
}

func (r *reapingControlClient) CreateHostSession(context.Context, string, openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error) {
	return nil, nil
}

func (r *reapingControlClient) UpdateHostSession(_ context.Context, _ string, sessionID string, req openapi.UpdateSessionRequest) (*openapi.Session, error) {
	r.patches = append(r.patches, reapPatch{sessionID: sessionID, status: string(req.Status)})
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	id, _ := uuid.Parse(sessionID)
	return &openapi.Session{Id: id, Status: string(req.Status)}, nil
}

func TestManagerReapMarksDeadSessionsExitedAndKeepsLiveOnes(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)

	// Three local sessions: two "running", one already "exited".
	mustSave := func(s session.LocalSession) {
		t.Helper()
		if err := store.Save(s); err != nil {
			t.Fatalf("Save %s: %v", s.SessionID, err)
		}
	}
	mustSave(session.LocalSession{
		SessionID:       "11111111-1111-1111-1111-111111111111",
		Tool:            "claude",
		TmuxSessionName: "termix_alive",
		Status:          "running",
		StartedAt:       time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
	})
	mustSave(session.LocalSession{
		SessionID:       "22222222-2222-2222-2222-222222222222",
		Tool:            "claude",
		TmuxSessionName: "termix_dead",
		Status:          "running",
		StartedAt:       time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
	})
	mustSave(session.LocalSession{
		SessionID:       "33333333-3333-3333-3333-333333333333",
		Tool:            "claude",
		TmuxSessionName: "termix_already_done",
		Status:          "exited",
		StartedAt:       time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
	})

	tmux := &fakeTmuxRunner{
		hasSession: func(name string) bool {
			return name == "termix_alive"
		},
	}
	control := &reapingControlClient{}

	manager := session.NewManager(session.ManagerOptions{
		Store: store,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{
				DeviceID:    "22222222-2222-2222-2222-222222222222",
				AccessToken: "tok",
			}, nil
		},
		Control:  control,
		Tmux:     tmux,
		Now:      func() time.Time { return time.Date(2026, 4, 28, 1, 0, 0, 0, time.UTC) },
		Hostname: func() (string, error) { return "devbox", nil },
	})

	if err := manager.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	// Exactly one PATCH, on the dead session, with status=exited.
	if len(control.patches) != 1 {
		t.Fatalf("expected 1 PATCH, got %d: %+v", len(control.patches), control.patches)
	}
	p := control.patches[0]
	if p.sessionID != "22222222-2222-2222-2222-222222222222" || p.status != "exited" {
		t.Fatalf("unexpected patch: %+v", p)
	}

	// Local state: alive session and already-exited session remain;
	// dead session is removed.
	remaining, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := map[string]bool{}
	for _, s := range remaining {
		got[s.SessionID] = true
	}
	if !got["11111111-1111-1111-1111-111111111111"] {
		t.Fatalf("alive session should still be in local store")
	}
	if got["22222222-2222-2222-2222-222222222222"] {
		t.Fatalf("dead session should have been removed from local store")
	}
	if !got["33333333-3333-3333-3333-333333333333"] {
		t.Fatalf("already-exited session should be left untouched")
	}
}

func TestManagerReapDeletesLocalSessionWhenControlSessionIsMissing(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)

	const staleSessionID = "44444444-4444-4444-4444-444444444444"
	if err := store.Save(session.LocalSession{
		SessionID:       staleSessionID,
		Tool:            "claude",
		TmuxSessionName: "termix_stale",
		Status:          "running",
		StartedAt:       time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Save stale session: %v", err)
	}

	control := &reapingControlClient{
		updateErr: &controlapi.APIError{
			Action:     "update host session",
			StatusCode: http.StatusNotFound,
			Body:       []byte(`{"error":"session not found","reason":"session_not_found"}`),
		},
	}

	manager := session.NewManager(session.ManagerOptions{
		Store: store,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{
				DeviceID:    "22222222-2222-2222-2222-222222222222",
				AccessToken: "tok",
			}, nil
		},
		Control:  control,
		Tmux:     &fakeTmuxRunner{hasSession: func(string) bool { return false }},
		Now:      func() time.Time { return time.Date(2026, 4, 29, 1, 0, 0, 0, time.UTC) },
		Hostname: func() (string, error) { return "devbox", nil },
	})

	if err := manager.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(control.patches) != 1 {
		t.Fatalf("expected 1 PATCH attempt, got %d: %+v", len(control.patches), control.patches)
	}

	remaining, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, s := range remaining {
		if s.SessionID == staleSessionID {
			t.Fatalf("missing control session should remove stale local session")
		}
	}
}

func TestStoreDeleteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)

	// Delete on missing file is a no-op, not an error.
	if err := store.Delete("does-not-exist"); err != nil {
		t.Fatalf("Delete on missing should be nil, got %v", err)
	}

	if err := store.Save(session.LocalSession{
		SessionID:       "abc",
		Tool:            "claude",
		TmuxSessionName: "termix_abc",
		Status:          "running",
		StartedAt:       time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete("abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Second delete on the same id should also be a no-op.
	if err := store.Delete("abc"); err != nil {
		t.Fatalf("second Delete should be nil, got %v", err)
	}

	// File is actually gone.
	if _, err := store.Load("abc"); err == nil {
		t.Fatalf("Load after Delete should error")
	}
}
