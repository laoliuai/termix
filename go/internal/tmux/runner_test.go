package tmux

import (
	"context"
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
