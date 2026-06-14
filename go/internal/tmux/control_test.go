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

func TestBuildSnapshotVisibleCursor(t *testing.T) {
	content := []byte("line1\nline2\nline3")
	got := BuildSnapshot(content, 10, 5, true)
	// NormalizeSnapshot: reset prefix + CRLF; then CUP at row=y+1, col=x+1.
	want := "\x1b[3J\x1b[2J\x1b[H" + "line1\r\nline2\r\nline3" + "\x1b[6;11H"
	if string(got) != want {
		t.Fatalf("visible:\n want %q\n got  %q", want, string(got))
	}
}

func TestBuildSnapshotHiddenCursor(t *testing.T) {
	got := BuildSnapshot([]byte("a"), 0, 0, false)
	want := "\x1b[3J\x1b[2J\x1b[H" + "a" + "\x1b[1;1H" + "\x1b[?25l"
	if string(got) != want {
		t.Fatalf("hidden:\n want %q\n got  %q", want, string(got))
	}
}
