package session

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/credentials"
)

type reannounceFakeRelay struct {
	announced []string
	snapshots []string
}

func (r *reannounceFakeRelay) AnnounceSession(_ context.Context, s LocalSession) error {
	r.announced = append(r.announced, s.SessionID)
	return nil
}
func (r *reannounceFakeRelay) PublishSnapshot(_ context.Context, sessionID string, _ []byte) error {
	r.snapshots = append(r.snapshots, sessionID)
	return nil
}
func (r *reannounceFakeRelay) PublishOutput(context.Context, string, []byte) error { return nil }
func (r *reannounceFakeRelay) SetSnapshotHandler(func(context.Context, string) ([]byte, error)) {}
func (r *reannounceFakeRelay) SetInputHandler(func(context.Context, string, []byte) error)     {}

func TestReannounceAllSessionsSkipsNonRunningRows(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	idA := uuid.NewString()
	idB := uuid.NewString()
	idC := uuid.NewString()
	if err := store.Save(LocalSession{SessionID: idA, Status: "running", TmuxSessionName: "termix_a"}); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := store.Save(LocalSession{SessionID: idB, Status: "exited", TmuxSessionName: "termix_b"}); err != nil {
		t.Fatalf("save B: %v", err)
	}
	if err := store.Save(LocalSession{SessionID: idC, Status: "idle", TmuxSessionName: "termix_c"}); err != nil {
		t.Fatalf("save C: %v", err)
	}

	relay := &reannounceFakeRelay{}
	m := NewManager(ManagerOptions{
		Store: store,
		Relay: relay,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		Snapshot: func(_ context.Context, name string) ([]byte, error) {
			if name == "" {
				return nil, errors.New("empty")
			}
			return []byte("snap-" + name), nil
		},
	})

	m.ReannounceAllSessions(context.Background())

	if len(relay.announced) != 2 {
		t.Fatalf("announced=%v want 2 entries (A and C only)", relay.announced)
	}
	gotSet := map[string]bool{relay.announced[0]: true, relay.announced[1]: true}
	if !gotSet[idA] || !gotSet[idC] {
		t.Fatalf("expected to announce A and C, got %v", relay.announced)
	}
	if len(relay.snapshots) != 2 {
		t.Fatalf("snapshots=%v want 2 (one per running/idle session)", relay.snapshots)
	}
	_ = openapi.UpdateSessionRequestStatusRunning // silence import in case not used elsewhere
}
