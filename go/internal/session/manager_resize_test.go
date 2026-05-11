package session

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/termix/termix/go/internal/credentials"
)

type resizeFakeTmux struct {
	sessionName string
	cols, rows  uint32
	fail        error
	has         func(string) bool
}

func (f *resizeFakeTmux) EnsureAvailable(context.Context) error                 { return nil }
func (f *resizeFakeTmux) StartSession(context.Context, StartSpec) error         { return nil }
func (f *resizeFakeTmux) StartOutputPipe(context.Context, string, string) error { return nil }
func (f *resizeFakeTmux) StopOutputPipe(context.Context, string) error          { return nil }
func (f *resizeFakeTmux) HasSession(_ context.Context, name string) (bool, error) {
	if f.has != nil {
		return f.has(name), nil
	}
	return true, nil
}
func (f *resizeFakeTmux) ResizeWindow(_ context.Context, name string, cols, rows uint32) error {
	if f.fail != nil {
		return f.fail
	}
	f.sessionName = name
	f.cols = cols
	f.rows = rows
	return nil
}
func (f *resizeFakeTmux) KillSession(context.Context, string) error    { return nil }
func (f *resizeFakeTmux) PanePID(context.Context, string) (int, error) { return 0, nil }
func (f *resizeFakeTmux) BinaryInfo(context.Context) TmuxInfo          { return TmuxInfo{} }

func TestManagerResizeSessionInvokesTmuxRunnerForKnownSession(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	sessionID := uuid.NewString()
	if err := store.Save(LocalSession{
		SessionID:       sessionID,
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_unit_resize",
	}); err != nil {
		t.Fatalf("seed local session: %v", err)
	}

	fake := &resizeFakeTmux{}
	manager := NewManager(ManagerOptions{
		Store: store,
		Tmux:  fake,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
	})

	if err := manager.ResizeSession(context.Background(), sessionID, 80, 24); err != nil {
		t.Fatalf("ResizeSession: %v", err)
	}
	if fake.sessionName != "termix_unit_resize" || fake.cols != 80 || fake.rows != 24 {
		t.Fatalf("unexpected runner call: name=%q cols=%d rows=%d",
			fake.sessionName, fake.cols, fake.rows)
	}
}

func TestManagerResizeSessionReturnsErrorForUnknownSession(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	fake := &resizeFakeTmux{}
	manager := NewManager(ManagerOptions{
		Store: store,
		Tmux:  fake,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
	})

	err := manager.ResizeSession(context.Background(), uuid.NewString(), 80, 24)
	if err == nil {
		t.Fatal("expected error for unknown session id")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}
