package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

// paneMonitorFakeTmux scripts a sequence of PaneSize results. Each call returns
// the next entry; once the sequence is exhausted it keeps returning the last
// entry (steady state). Concurrency-safe so the monitor goroutine can poll it.
type paneMonitorFakeTmux struct {
	mu    sync.Mutex
	seq   [][2]uint32
	calls int
}

func (f *paneMonitorFakeTmux) next() (uint32, uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	if i >= len(f.seq) {
		i = len(f.seq) - 1
	}
	f.calls++
	return f.seq[i][0], f.seq[i][1]
}

func (f *paneMonitorFakeTmux) EnsureAvailable(context.Context) error                 { return nil }
func (f *paneMonitorFakeTmux) StartSession(context.Context, StartSpec) error         { return nil }
func (f *paneMonitorFakeTmux) StartOutputPipe(context.Context, string, string) error { return nil }
func (f *paneMonitorFakeTmux) StopOutputPipe(context.Context, string) error          { return nil }
func (f *paneMonitorFakeTmux) HasSession(context.Context, string) (bool, error)      { return true, nil }
func (f *paneMonitorFakeTmux) ResizeWindow(context.Context, string, uint32, uint32) error {
	return nil
}
func (f *paneMonitorFakeTmux) KillSession(context.Context, string) error    { return nil }
func (f *paneMonitorFakeTmux) PanePID(context.Context, string) (int, error) { return 0, nil }
func (f *paneMonitorFakeTmux) PaneSize(context.Context, string) (uint32, uint32, error) {
	c, r := f.next()
	return c, r, nil
}
func (f *paneMonitorFakeTmux) BinaryInfo(context.Context) TmuxInfo { return TmuxInfo{} }

// paneMonitorFakeRelay records RepushSnapshot calls (cols/rows) so the test can
// assert the host-resize monitor re-pushed exactly once with the new size.
type paneMonitorFakeRelay struct {
	mu      sync.Mutex
	repush  [][2]uint32
	repushN int
}

func (r *paneMonitorFakeRelay) AnnounceSession(context.Context, LocalSession) error  { return nil }
func (r *paneMonitorFakeRelay) PublishSnapshot(context.Context, string, []byte) error { return nil }
func (r *paneMonitorFakeRelay) PublishOutput(context.Context, string, []byte) error   { return nil }
func (r *paneMonitorFakeRelay) SetSnapshotHandler(func(context.Context, string) ([]byte, error)) {
}
func (r *paneMonitorFakeRelay) SetInputHandler(func(context.Context, string, []byte) error) {}
func (r *paneMonitorFakeRelay) SetSizeHandler(func(context.Context, string) (uint32, uint32, error)) {
}
func (r *paneMonitorFakeRelay) RepushSnapshot(_ context.Context, _ string, _ []byte, cols, rows uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.repush = append(r.repush, [2]uint32{cols, rows})
	r.repushN++
	return nil
}

func (r *paneMonitorFakeRelay) snapshotRepush() [][2]uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][2]uint32, len(r.repush))
	copy(out, r.repush)
	return out
}

// TestPaneMonitorRepushesOnStableResize asserts that when the pane size changes
// and then stays at the new value for one extra tick (double-tick debounce),
// the monitor re-pushes exactly once with the NEW cols/rows.
func TestPaneMonitorRepushesOnStableResize(t *testing.T) {
	tmux := &paneMonitorFakeTmux{seq: [][2]uint32{
		{120, 40}, // initial lastStable
		{200, 50}, // change detected -> pending
		{200, 50}, // stable -> re-push (200x50)
		{200, 50}, // steady; no further re-push
	}}
	relay := &paneMonitorFakeRelay{}
	m := NewManager(ManagerOptions{
		Tmux:  tmux,
		Relay: relay,
		Snapshot: func(context.Context, string) ([]byte, error) {
			return []byte("snap"), nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	m.startPaneSizeMonitor(ctx, "s1", "termix_s1", 10*time.Millisecond)

	deadline := time.After(2 * time.Second)
	for {
		if got := relay.snapshotRepush(); len(got) >= 1 {
			if got[0] != [2]uint32{200, 50} {
				cancel()
				t.Fatalf("re-push size = %v, want [200 50]", got[0])
			}
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for a re-push after a stable resize")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Let a few more ticks elapse to confirm steady state yields no extra pushes.
	time.Sleep(80 * time.Millisecond)
	cancel()
	if n := len(relay.snapshotRepush()); n != 1 {
		t.Fatalf("expected exactly one re-push, got %d", n)
	}
}

// TestPaneMonitorNoRepushWhenSizeStable asserts that a pane whose size never
// changes triggers zero re-pushes.
func TestPaneMonitorNoRepushWhenSizeStable(t *testing.T) {
	tmux := &paneMonitorFakeTmux{seq: [][2]uint32{{120, 40}}}
	relay := &paneMonitorFakeRelay{}
	m := NewManager(ManagerOptions{
		Tmux:  tmux,
		Relay: relay,
		Snapshot: func(context.Context, string) ([]byte, error) {
			return []byte("snap"), nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	m.startPaneSizeMonitor(ctx, "s1", "termix_s1", 10*time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	cancel()

	if n := len(relay.snapshotRepush()); n != 0 {
		t.Fatalf("expected zero re-pushes for a stable pane, got %d", n)
	}
}
