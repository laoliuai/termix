package tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/termix/termix/go/internal/session"
	"github.com/termix/termix/go/internal/tmux"
)

func TestTmuxRunnerStartsCommandInDetachedSession(t *testing.T) {
	if os.Getenv("TERMIX_TMUX_INTEGRATION") != "1" {
		t.Skip("set TERMIX_TMUX_INTEGRATION=1 to run tmux-backed integration tests")
	}

	runner := tmux.NewRunner()
	if err := runner.EnsureAvailable(context.Background()); err != nil {
		t.Fatalf("EnsureAvailable returned error: %v", err)
	}

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "marker.txt")
	sessionName := tmux.SessionName(fmt.Sprintf("integration-%d", time.Now().UnixNano()))
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	command := fmt.Sprintf("sh -lc 'printf %%s \"$TERMIX_TEST_MARKER\" > %s'", shellSingleQuote(outputPath))
	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName: sessionName,
		WorkingDir:  tempDir,
		Env: map[string]string{
			"TERMIX_TEST_MARKER": "hello",
		},
		ToolCommand: command,
	}); err != nil {
		t.Fatalf("StartSession returned error: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outputPath)
		if err == nil {
			if strings.TrimSpace(string(data)) != "hello" {
				t.Fatalf("expected marker output hello, got %q", string(data))
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for tmux command output at %s", outputPath)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
