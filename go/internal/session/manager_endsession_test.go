package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
)

// endSessionFakeTmux records the kill-session call and reports HasSession
// based on whether KillSession has been invoked. PanePID is unused here.
type endSessionFakeTmux struct {
	killCalls   []string
	killErr     error
	startedDead bool
}

func (e *endSessionFakeTmux) EnsureAvailable(context.Context) error                 { return nil }
func (e *endSessionFakeTmux) StartSession(context.Context, StartSpec) error         { return nil }
func (e *endSessionFakeTmux) StartOutputPipe(context.Context, string, string) error { return nil }
func (e *endSessionFakeTmux) StopOutputPipe(context.Context, string) error          { return nil }
func (e *endSessionFakeTmux) HasSession(_ context.Context, _ string) bool {
	return !e.startedDead && len(e.killCalls) == 0
}
func (e *endSessionFakeTmux) ResizeWindow(context.Context, string, uint32, uint32) error {
	return nil
}
func (e *endSessionFakeTmux) KillSession(_ context.Context, name string) error {
	e.killCalls = append(e.killCalls, name)
	return e.killErr
}
func (e *endSessionFakeTmux) PanePID(context.Context, string) (int, error) { return 0, nil }

// endSessionFakeControl captures UpdateHostSession requests and lets each
// test pre-program success or failure. CreateHostSession and
// HeartbeatHostSession are unused by EndSession but must be present to
// satisfy ControlClient.
type endSessionFakeControl struct {
	updates    []openapi.UpdateSessionRequest
	updateIDs  []string
	updateErr  error
	updateResp *openapi.Session
}

func (e *endSessionFakeControl) CreateHostSession(context.Context, string, openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error) {
	return nil, errors.New("not used")
}
func (e *endSessionFakeControl) UpdateHostSession(_ context.Context, _ string, sessionID string, req openapi.UpdateSessionRequest) (*openapi.Session, error) {
	e.updateIDs = append(e.updateIDs, sessionID)
	e.updates = append(e.updates, req)
	if e.updateErr != nil {
		return nil, e.updateErr
	}
	return e.updateResp, nil
}
func (e *endSessionFakeControl) HeartbeatHostSession(context.Context, string, string, string) (*openapi.Session, error) {
	return nil, errors.New("not used")
}

func newEndSessionManager(t *testing.T, store *Store, tmux TmuxRunner, control ControlClient) *Manager {
	t.Helper()
	return NewManager(ManagerOptions{
		Store: store,
		Tmux:  tmux,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{AccessToken: "token"}, nil
		},
		Control: control,
	})
}

// TestEndSessionKillsTmuxThenPatchesAndDeletesLocalRow asserts the
// happy-path ordering: tmux is killed, the control row is PATCHed to
// `exited`, and the local-store entry is removed.
func TestEndSessionKillsTmuxThenPatchesAndDeletesLocalRow(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	id := uuid.NewString()
	if err := store.Save(LocalSession{
		SessionID:       id,
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_test_endsession",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tmux := &endSessionFakeTmux{}
	control := &endSessionFakeControl{}
	manager := newEndSessionManager(t, store, tmux, control)

	if _, err := manager.EndSession(context.Background(), &daemonv1.EndSessionRequest{SessionId: id}); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if len(tmux.killCalls) != 1 || tmux.killCalls[0] != "termix_test_endsession" {
		t.Fatalf("expected tmux.KillSession to be called once with the tmux name, got %v", tmux.killCalls)
	}
	if len(control.updates) != 1 || control.updates[0].Status != openapi.UpdateSessionRequestStatusExited {
		t.Fatalf("expected one PATCH=exited, got %+v", control.updates)
	}
	if control.updateIDs[0] != id {
		t.Fatalf("expected PATCH for id=%s, got %s", id, control.updateIDs[0])
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sessions", id+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected local-store row to be deleted, stat returned %v", err)
	}
}

// TestEndSessionReturnsErrSessionNotFoundForUnknownID asserts the CLI's
// "wrong host" signal — when the session id is not in the local store,
// the manager surfaces ErrSessionNotFound and does not call tmux or
// control.
func TestEndSessionReturnsErrSessionNotFoundForUnknownID(t *testing.T) {
	store := NewStore(t.TempDir())
	tmux := &endSessionFakeTmux{}
	control := &endSessionFakeControl{}
	manager := newEndSessionManager(t, store, tmux, control)

	_, err := manager.EndSession(context.Background(), &daemonv1.EndSessionRequest{SessionId: uuid.NewString()})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
	if len(tmux.killCalls) != 0 {
		t.Fatalf("expected tmux.KillSession to be skipped on not-found, got %v", tmux.killCalls)
	}
	if len(control.updates) != 0 {
		t.Fatalf("expected no PATCH on not-found, got %+v", control.updates)
	}
}

// TestEndSessionKeepsLocalRowWhenPatchFails asserts that a transient
// control-plane error leaves the local-store row intact so the
// periodic reaper retries the PATCH on its next sweep. (tmux is already
// dead at that point, so the row will be picked up by the reaper's
// tmux-gone branch.)
func TestEndSessionKeepsLocalRowWhenPatchFails(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	id := uuid.NewString()
	if err := store.Save(LocalSession{
		SessionID:       id,
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_test_endsession_patchfail",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tmux := &endSessionFakeTmux{}
	control := &endSessionFakeControl{updateErr: errors.New("network down")}
	manager := newEndSessionManager(t, store, tmux, control)

	if _, err := manager.EndSession(context.Background(), &daemonv1.EndSessionRequest{SessionId: id}); err == nil {
		t.Fatal("expected EndSession to surface the PATCH error")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sessions", id+".json")); err != nil {
		t.Fatalf("expected local-store row to survive PATCH failure, stat: %v", err)
	}
	if len(tmux.killCalls) != 1 {
		t.Fatalf("expected tmux to be killed before PATCH, got %v", tmux.killCalls)
	}
}
