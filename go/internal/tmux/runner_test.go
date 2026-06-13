package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
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
	if has, err := runner.HasSession(context.Background(), sessionName); err != nil {
		t.Fatalf("HasSession: %v", err)
	} else if !has {
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
	if has, err := runner.HasSession(context.Background(), sessionName); err != nil {
		t.Fatalf("HasSession before kill: %v", err)
	} else if !has {
		t.Fatal("expected tmux session to be live before KillSession")
	}

	if err := runner.KillSession(context.Background(), sessionName); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if has, err := runner.HasSession(context.Background(), sessionName); err != nil {
		t.Fatalf("HasSession after kill: %v", err)
	} else if has {
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

// TestNewSessionArgsNeverEmitsTmuxENewSessionFlag locks in the v0.4.1 fix:
// environment variables must be inlined into the `sh -c` command rather than
// forwarded through `tmux new-session -e KEY=VAL`. The `-e` flag was added
// in tmux 3.2 and rejecting it on tmux <3.2 (e.g. Ubuntu 20.04 ships 3.0a)
// caused `termix start` to fail with an opaque "exit status 1".
func TestNewSessionArgsNeverEmitsTmuxENewSessionFlag(t *testing.T) {
	args := newSessionArgs(session.StartSpec{
		SessionName: "probe",
		ToolCommand: "claude",
		Env: map[string]string{
			"FOO":     "bar",
			"BAZ":     "qux",
			"TERM":    "xterm-256color",  // must be filtered (tmux-managed)
			"TMUX":    "/tmp/socket,1,0", // must be filtered
			"WEIRD%%": "skipme",          // bash function export — must be filtered
		},
	}, "")
	if slices.Contains(args, "-e") {
		t.Fatalf("newSessionArgs emitted `-e` flag (incompatible with tmux <3.2): %v", args)
	}
	// The shell command should be the last arg after `sh -c`.
	shellIdx := slices.Index(args, "sh")
	if shellIdx < 0 || shellIdx+2 >= len(args) || args[shellIdx+1] != "-c" {
		t.Fatalf("expected trailing `sh -c <command>`, got %v", args)
	}
	cmd := args[shellIdx+2]
	for _, want := range []string{"FOO='bar'", "BAZ='qux'"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("shell cmd missing %q; got %q", want, cmd)
		}
	}
	for _, banned := range []string{"TERM=", "TMUX=", "WEIRD%%="} {
		if strings.Contains(cmd, banned) {
			t.Errorf("shell cmd unexpectedly contains %q; got %q", banned, cmd)
		}
	}
	if !strings.Contains(cmd, "exec claude") {
		t.Errorf("shell cmd missing `exec claude`; got %q", cmd)
	}
}

// TestBuildShellCommandEscapesValuesContainingSingleQuotes verifies that an
// env value with embedded `'` survives the POSIX single-quote escape so the
// inlined assignment doesn't break sh -c parsing.
func TestBuildShellCommandEscapesValuesContainingSingleQuotes(t *testing.T) {
	got := buildShellCommand("claude", map[string]string{
		"NOTE": "it's mine",
	}, "")
	want := `NOTE='it'"'"'s mine' exec claude`
	if got != want {
		t.Fatalf("buildShellCommand escape mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestBuildShellCommandPreservesErrLogRedirect verifies that when an error
// log path is supplied the command appends `2>>'<path>'` so a fast-failing
// tool's stderr survives for DetectImmediateExit to surface.
func TestBuildShellCommandPreservesErrLogRedirect(t *testing.T) {
	got := buildShellCommand("claude", nil, "/var/log/x.err")
	want := `exec claude 2>>'/var/log/x.err'`
	if got != want {
		t.Fatalf("buildShellCommand with errLog mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestBuildShellCommandSortsEnvKeysDeterministically locks the alphabetical
// ordering so test assertions over the resulting command don't flake on
// Go's randomized map iteration.
func TestBuildShellCommandSortsEnvKeysDeterministically(t *testing.T) {
	got := buildShellCommand("claude", map[string]string{
		"ZZZ": "z",
		"AAA": "a",
		"MMM": "m",
	}, "")
	want := "AAA='a' MMM='m' ZZZ='z' exec claude"
	if got != want {
		t.Fatalf("buildShellCommand ordering mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestStartSessionForwardsInlinedEnvToToolEndToEnd starts a real tmux
// session whose pane writes $TERMIX_TEST_FOO to a temp file. Validates the
// inlined-env fix end-to-end: the env var must reach the tool's process
// even though we no longer use `tmux new-session -e`.
func TestStartSessionForwardsInlinedEnvToToolEndToEnd(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	outPath := filepath.Join(t.TempDir(), "env.out")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName: sessionName,
		ToolCommand: fmt.Sprintf("sh -c 'printf %%s \"$TERMIX_TEST_FOO\" > %s; sleep 30'", outPath),
		Env: map[string]string{
			"TERMIX_TEST_FOO": "hello world",
		},
		DetectImmediateExit: true,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Wait briefly for the inner sh -c to write the file.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := readFileIfExists(outPath); err == nil && len(data) > 0 {
			if string(data) != "hello world" {
				t.Fatalf("env not forwarded: got %q want %q", data, "hello world")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for env-forwarded file at %s", outPath)
}

func readFileIfExists(path string) ([]byte, error) {
	cmd := exec.Command("cat", path)
	out, err := cmd.Output()
	return out, err
}

// TestBinaryInfoReturnsPathAndVersionForRealTmux verifies the daemon can
// surface a concrete path + version when tmux is on PATH (used by
// `termix status`).
func TestBinaryInfoReturnsPathAndVersionForRealTmux(t *testing.T) {
	skipIfNoTmux(t)

	info := NewRunner().BinaryInfo(context.Background())
	if !info.Installed {
		t.Fatalf("BinaryInfo returned Installed=false; got %#v", info)
	}
	if info.Path == "" {
		t.Errorf("BinaryInfo returned empty Path; got %#v", info)
	}
	if info.Version == "" {
		t.Errorf("BinaryInfo returned empty Version; got %#v", info)
	}
	// `tmux -V` outputs `tmux <version>` so after stripping the prefix we
	// should not see "tmux " in the version field.
	if strings.HasPrefix(info.Version, "tmux ") {
		t.Errorf("Version still has `tmux ` prefix: %q", info.Version)
	}
}

// TestBinaryInfoReportsNotInstalledWhenBinaryMissing verifies the helper
// degrades gracefully so Status can report "not installed" instead of
// erroring out.
func TestBinaryInfoReportsNotInstalledWhenBinaryMissing(t *testing.T) {
	r := &Runner{binary: "/nonexistent/termix-tmux-probe"}
	info := r.BinaryInfo(context.Background())
	if info.Installed {
		t.Fatalf("expected Installed=false for missing binary, got %#v", info)
	}
}

// TestResizeWindowResizesLivePane verifies tmux respects resize-window so the
// daemon can drive the pane's size from an explicit (cols, rows) while no host
// client is attached. (StartSession defaults to `window-size latest`, under
// which a host `tmux attach` would be authoritative; this test exercises the
// no-host case.)
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

// TestStartSessionSetsWindowSizeLatest asserts the final set-option argv
// configures `window-size latest` so the pane follows the host terminal
// (not pinned to the CLI-supplied birth size). Replaces the prior `manual`
// expectation.
func TestStartSessionSetsWindowSizeLatest(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sessionName).Run() })

	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "sleep 30",
		DetectImmediateExit: true,
		Cols:                120,
		Rows:                40,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	out, err := exec.Command("tmux", "show-options", "-t", sessionName, "window-size").Output()
	if err != nil {
		t.Fatalf("show-options: %v", err)
	}
	if opt := strings.TrimSpace(string(out)); !strings.Contains(opt, "latest") {
		t.Fatalf("expected window-size latest, got %q", opt)
	}
}
