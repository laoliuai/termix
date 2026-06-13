package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
)

func TestManagerStatusReportsConnectedRelayWithSessions(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	id := uuid.NewString()
	if err := store.Save(LocalSession{
		SessionID:       id,
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_test",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	start := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	connectedAt := time.Date(2026, 5, 8, 9, 5, 30, 0, time.UTC)
	now := time.Date(2026, 5, 8, 9, 10, 0, 0, time.UTC)

	m := NewManager(ManagerOptions{
		Store:     store,
		StartTime: start,
		Now:       func() time.Time { return now },
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		RelayStateSource: func() RelayStateSnapshot {
			return RelayStateSnapshot{
				Phase:           "connected",
				Attempt:         0,
				LastConnectedAt: connectedAt,
			}
		},
	})

	resp, err := m.Status(context.Background(), &daemonv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetUptimeSeconds() != 600 { // 9:10 - 9:00
		t.Errorf("UptimeSeconds=%d want 600", resp.GetUptimeSeconds())
	}
	if resp.GetRelay().GetPhase() != "connected" {
		t.Errorf("Phase=%q want connected", resp.GetRelay().GetPhase())
	}
	if resp.GetRelay().GetLastConnectedAt() != connectedAt.Unix() {
		t.Errorf("LastConnectedAt=%d want %d", resp.GetRelay().GetLastConnectedAt(), connectedAt.Unix())
	}
	if len(resp.GetSessions()) != 1 || resp.GetSessions()[0].GetSessionId() != id {
		t.Errorf("Sessions=%v want one entry with id=%s", resp.GetSessions(), id)
	}
}

// TestManagerStatusReportsTmuxBinaryInfo verifies the daemon surfaces tmux
// installation state (path + version) so `termix status` can show users
// whether tmux is present and whether the version is recent enough.
func TestManagerStatusReportsTmuxBinaryInfo(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	now := time.Date(2026, 5, 8, 9, 10, 0, 0, time.UTC)
	m := NewManager(ManagerOptions{
		Store:     store,
		StartTime: now.Add(-time.Hour),
		Now:       func() time.Time { return now },
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		Tmux: &statusTmuxStub{info: TmuxInfo{Installed: true, Path: "/usr/bin/tmux", Version: "3.0a"}},
	})
	resp, err := m.Status(context.Background(), &daemonv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	tmux := resp.GetTmux()
	if !tmux.GetInstalled() {
		t.Errorf("Tmux.Installed=false want true")
	}
	if tmux.GetPath() != "/usr/bin/tmux" {
		t.Errorf("Tmux.Path=%q want /usr/bin/tmux", tmux.GetPath())
	}
	if tmux.GetVersion() != "3.0a" {
		t.Errorf("Tmux.Version=%q want 3.0a", tmux.GetVersion())
	}
}

// TestManagerStatusReportsTmuxNotInstalled verifies the daemon does not
// silently lie when tmux is missing — Installed=false propagates so the
// CLI can render an actionable hint.
func TestManagerStatusReportsTmuxNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	now := time.Date(2026, 5, 8, 9, 10, 0, 0, time.UTC)
	m := NewManager(ManagerOptions{
		Store:     store,
		StartTime: now.Add(-time.Hour),
		Now:       func() time.Time { return now },
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		Tmux: &statusTmuxStub{info: TmuxInfo{}},
	})
	resp, err := m.Status(context.Background(), &daemonv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetTmux().GetInstalled() {
		t.Errorf("Tmux.Installed=true want false")
	}
}

// statusTmuxStub is a stub TmuxRunner that only implements BinaryInfo for
// status tests; other methods either no-op or are unreachable in the
// Status code path.
type statusTmuxStub struct {
	info TmuxInfo
}

func (s *statusTmuxStub) EnsureAvailable(context.Context) error                 { return nil }
func (s *statusTmuxStub) StartSession(context.Context, StartSpec) error         { return nil }
func (s *statusTmuxStub) StartOutputPipe(context.Context, string, string) error { return nil }
func (s *statusTmuxStub) StopOutputPipe(context.Context, string) error          { return nil }
func (s *statusTmuxStub) HasSession(context.Context, string) (bool, error)      { return false, nil }
func (s *statusTmuxStub) ResizeWindow(context.Context, string, uint32, uint32) error {
	return nil
}
func (s *statusTmuxStub) KillSession(context.Context, string) error    { return nil }
func (s *statusTmuxStub) PanePID(context.Context, string) (int, error) { return 0, nil }
func (s *statusTmuxStub) PaneSize(context.Context, string) (uint32, uint32, error) {
	return 0, 0, nil
}
func (s *statusTmuxStub) BinaryInfo(context.Context) TmuxInfo { return s.info }

func TestManagerStatusReportsReconnectingPhase(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	now := time.Date(2026, 5, 8, 9, 10, 0, 0, time.UTC)
	nextRetry := now.Add(8 * time.Second)
	m := NewManager(ManagerOptions{
		Store:     store,
		StartTime: now.Add(-time.Hour),
		Now:       func() time.Time { return now },
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		RelayStateSource: func() RelayStateSnapshot {
			return RelayStateSnapshot{
				Phase:        "reconnecting",
				Attempt:      4,
				LastError:    "write tcp ... broken pipe",
				NextRetryAt:  nextRetry,
				AuthFailures: 0,
			}
		},
	})
	resp, err := m.Status(context.Background(), &daemonv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetRelay().GetPhase() != "reconnecting" {
		t.Errorf("Phase=%q want reconnecting", resp.GetRelay().GetPhase())
	}
	if resp.GetRelay().GetAttempt() != 4 {
		t.Errorf("Attempt=%d want 4", resp.GetRelay().GetAttempt())
	}
	if resp.GetRelay().GetLastError() != "write tcp ... broken pipe" {
		t.Errorf("LastError=%q", resp.GetRelay().GetLastError())
	}
	if resp.GetRelay().GetNextRetryAt() != nextRetry.Unix() {
		t.Errorf("NextRetryAt=%d want %d", resp.GetRelay().GetNextRetryAt(), nextRetry.Unix())
	}
}
