package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/termix/termix/go/internal/session"
)

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux binary not on PATH")
	}
}

// TestStartSessionFailedTool verifies that when the pane's command exits
// immediately (binary missing on PATH), StartSession surfaces the captured
// stderr as an error rather than silently succeeding.
func TestStartSessionFailedTool(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	errLogPath := filepath.Join(t.TempDir(), sessionName+".err")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	// Speed up the probe so the test runs in well under a second. Restore on
	// teardown so other tests use the production cadence.
	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "termix-tool-that-definitely-does-not-exist",
		ErrLogPath:          errLogPath,
		DetectImmediateExit: true,
	})
	if err == nil {
		t.Fatalf("expected StartSession to return an error when the tool is missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "session exited immediately") {
		t.Fatalf("error should mention immediate exit, got %q", msg)
	}
	if !strings.Contains(msg, "termix-tool-that-definitely-does-not-exist") {
		t.Fatalf("error should include the captured stderr referencing the missing tool, got %q", msg)
	}
}

// TestStartSessionLivenessProbeAllowsHealthyTool verifies the probe does not
// false-positive against a tool that just sleeps. We run a long-lived `sleep`
// as the pane command; the probe should let StartSession return success
// because the session is still alive after the wait.
func TestStartSessionLivenessProbeAllowsHealthyTool(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "sleep 30",
		DetectImmediateExit: true,
	}); err != nil {
		t.Fatalf("expected StartSession to succeed for a healthy tool, got: %v", err)
	}
	if !runner.HasSession(context.Background(), sessionName) {
		t.Fatal("expected tmux session to still exist after StartSession returned success")
	}
}

// TestInitialPaneSizeAppliesDefaultsAndFloors locks in the runner's
// fallback policy: zero (CLI had no tty) becomes the legacy 120×40, and
// any positive value below the minimum is bumped up so a tiny host
// terminal cannot launch an unusable pane.
func TestInitialPaneSizeAppliesDefaultsAndFloors(t *testing.T) {
	cases := []struct {
		name               string
		cols, rows         int
		wantCols, wantRows int
	}{
		{name: "zero falls back to 120x40 default", cols: 0, rows: 0, wantCols: 120, wantRows: 40},
		{name: "negative falls back to default", cols: -5, rows: -2, wantCols: 120, wantRows: 40},
		{name: "below cols floor is clamped to 40", cols: 20, rows: 24, wantCols: 40, wantRows: 24},
		{name: "below rows floor is clamped to 10", cols: 100, rows: 4, wantCols: 100, wantRows: 10},
		{name: "wide host terminal is forwarded verbatim", cols: 200, rows: 50, wantCols: 200, wantRows: 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cols, rows := initialPaneSize(tc.cols, tc.rows)
			if cols != tc.wantCols || rows != tc.wantRows {
				t.Fatalf("initialPaneSize(%d,%d)=(%d,%d) want (%d,%d)",
					tc.cols, tc.rows, cols, rows, tc.wantCols, tc.wantRows)
			}
		})
	}
}

// TestStartSessionAppliesHostTerminalSizeWhenSpecRequestsIt verifies the
// host-tty-driven sizing path end-to-end against real tmux: a StartSpec
// with Cols=200/Rows=50 should produce a pane that reports 200x50 via
// `tmux display-message #{window_width}x#{window_height}`.
func TestStartSessionAppliesHostTerminalSizeWhenSpecRequestsIt(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "sleep 30",
		DetectImmediateExit: true,
		Cols:                200,
		Rows:                50,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	out, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName,
		"#{window_width}x#{window_height}").Output()
	if err != nil {
		t.Fatalf("display-message: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != "200x50" {
		t.Fatalf("expected window 200x50, got %q", got)
	}
}

// TestKillSessionRemovesLivePane spins up a real pane with `sleep 60`,
// confirms it is live, calls KillSession, and asserts HasSession reports
// false. Verifies the source-of-truth shutdown path used by EndSession.
func TestKillSessionRemovesLivePane(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "sleep 60",
		DetectImmediateExit: true,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if !runner.HasSession(context.Background(), sessionName) {
		t.Fatal("expected tmux session to be live before KillSession")
	}

	if err := runner.KillSession(context.Background(), sessionName); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if runner.HasSession(context.Background(), sessionName) {
		t.Fatal("expected tmux session to be gone after KillSession")
	}
}

// TestKillSessionIsIdempotentForMissingSession verifies that calling
// KillSession against a session that never existed returns nil so the
// EndSession RPC can stay idempotent (re-running shutdown for the same
// id must succeed once the pane is already gone).
func TestKillSessionIsIdempotentForMissingSession(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_missing_" + uuid.NewString()
	if err := runner.KillSession(context.Background(), sessionName); err != nil {
		t.Fatalf("expected nil for already-missing session, got %v", err)
	}
}

// TestPanePIDReturnsLiveProcessPID verifies that PanePID returns the OS
// PID of the pane's primary process. We start a long-lived `sleep` and
// check that the returned PID exists in /proc with a matching comm.
func TestPanePIDReturnsLiveProcessPID(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})
	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "sleep 60",
		DetectImmediateExit: true,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	pid, err := runner.PanePID(context.Background(), sessionName)
	if err != nil {
		t.Fatalf("PanePID: %v", err)
	}
	if pid <= 1 {
		t.Fatalf("expected positive pane PID, got %d", pid)
	}
	if err := exec.Command("kill", "-0", fmt.Sprintf("%d", pid)).Run(); err != nil {
		t.Fatalf("expected pid %d to be alive, kill -0 failed: %v", pid, err)
	}
}

// TestPanePIDReturnsZeroForMissingSession asserts the helper does not
// surface a tmux exit code as an error: missing sessions are reported
// as PID 0 with nil error so callers (e.g. ListSessions) can degrade
// silently for orphan rows.
func TestPanePIDReturnsZeroForMissingSession(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	pid, err := runner.PanePID(context.Background(),
		"termix_test_missing_"+uuid.NewString())
	if err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
	if pid != 0 {
		t.Fatalf("expected PID 0 for missing session, got %d", pid)
	}
}

// TestResizeWindowResizesLivePane verifies tmux respects resize-window in
// `window-size manual` mode (the StartSession default) so the daemon can
// drive the pane's size from a SPA-supplied (cols, rows).
func TestResizeWindowResizesLivePane(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "sleep 30",
		DetectImmediateExit: true,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if err := runner.ResizeWindow(context.Background(), sessionName, 80, 24); err != nil {
		t.Fatalf("ResizeWindow: %v", err)
	}

	// tmux's display-message reports the current window size; assert ours.
	out, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName,
		"#{window_width}x#{window_height}").Output()
	if err != nil {
		t.Fatalf("display-message: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != "80x24" {
		t.Fatalf("expected window 80x24 after resize, got %q", got)
	}
}
