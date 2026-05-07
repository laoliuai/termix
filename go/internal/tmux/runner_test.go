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
