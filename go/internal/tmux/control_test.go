package tmux

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
)

func skipIfNoTmuxCtrl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// TestPaneSizeReturnsActualDimensions creates a session at a known size and
// asserts PaneSize reads it back.
func TestPaneSizeReturnsActualDimensions(t *testing.T) {
	skipIfNoTmuxCtrl(t)
	name := "termix_test_" + uuid.NewString()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "main", "-x", "140", "-y", "35", "sleep", "30").Run(); err != nil {
		t.Fatalf("new-session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	// Pin the window to a known size deterministically. A detached session
	// under a host `window-size latest` (or `largest`) global option ignores the
	// requested -x/-y and adopts the most-recent client's size, so we force
	// `manual` + resize-window here. This makes the PaneSize read-back
	// deterministic regardless of the host tmux config.
	if err := exec.Command("tmux", "set-option", "-t", name, "window-size", "manual").Run(); err != nil {
		t.Fatalf("set-option window-size manual: %v", err)
	}
	if err := exec.Command("tmux", "resize-window", "-t", name, "-x", "140", "-y", "35").Run(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	cols, rows, err := PaneSize(context.Background(), name)
	if err != nil {
		t.Fatalf("PaneSize: %v", err)
	}
	if cols != 140 || rows != 35 {
		t.Fatalf("PaneSize()=(%d,%d) want (140,35)", cols, rows)
	}
}
