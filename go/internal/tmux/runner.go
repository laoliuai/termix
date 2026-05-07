package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/termix/termix/go/internal/session"
)

// startSessionLivenessProbe is how long the runner waits after `tmux
// new-session -d` before asking tmux whether the session still exists. A
// non-existent command (`exec: not found`) crashes the pane synchronously and
// tmux reaps the session well within this window; legitimate TUIs (claude,
// codex, opencode) take >>1s to settle, so this probe never gives a false
// negative for them.
var startSessionLivenessProbe = 300 * time.Millisecond

type Runner struct {
	binary string
}

func NewRunner() *Runner {
	return &Runner{binary: "tmux"}
}

func (r *Runner) EnsureAvailable(ctx context.Context) error {
	if _, err := exec.LookPath(r.binary); err != nil {
		return err
	}
	return exec.CommandContext(ctx, r.binary, "-V").Run()
}

// initialPaneSize derives the (cols, rows) tmux should size a freshly-created
// pane to. Caller-supplied values from `termix start`'s host tty win when set;
// zero (caller had no tty) falls back to the legacy 120×40 default. Floors
// guard against a tiny host terminal that would render claude/codex unusable
// (cols≥40, rows≥10 — enough for most TUIs to lay out without wrapping every
// other line).
func initialPaneSize(cols, rows int) (int, int) {
	const (
		defaultCols = 120
		defaultRows = 40
		minCols     = 40
		minRows     = 10
	)
	if cols <= 0 {
		cols = defaultCols
	} else if cols < minCols {
		cols = minCols
	}
	if rows <= 0 {
		rows = defaultRows
	} else if rows < minRows {
		rows = minRows
	}
	return cols, rows
}

// HasSession returns true iff `tmux has-session -t sessionName` exits 0.
// Used by the reaper to detect sessions whose tmux pane has gone away
// (claude exited cleanly, user ran tmux kill-session, server lost the pane,
// etc.) so the row can be marked exited in the control DB.
func (r *Runner) HasSession(ctx context.Context, sessionName string) bool {
	return exec.CommandContext(ctx, r.binary, "has-session", "-t", sessionName).Run() == nil
}

// KillSession runs `tmux kill-session -t sessionName`. tmux delivers SIGHUP
// to the pane's process group, giving the tool (claude/codex/opencode) a
// chance to flush state before exiting. Idempotent: returns nil if the
// session is already gone.
func (r *Runner) KillSession(ctx context.Context, sessionName string) error {
	if sessionName == "" {
		return errors.New("tmux session name is required")
	}
	if !r.HasSession(ctx, sessionName) {
		return nil
	}
	return exec.CommandContext(ctx, r.binary, "kill-session", "-t", sessionName).Run()
}

// PanePID returns the OS PID of the process running in the session's main
// pane. The pane is launched as `sh -c 'exec <tool>'`, so `exec` replaces sh
// with the tool itself — this PID is therefore the tool's PID, not a shell
// wrapper. Returns 0 (no error) if the session is gone or tmux refuses to
// answer; an unparseable PID is reported as an error.
func (r *Runner) PanePID(ctx context.Context, sessionName string) (int, error) {
	if sessionName == "" {
		return 0, errors.New("tmux session name is required")
	}
	out, err := exec.CommandContext(ctx, r.binary,
		"list-panes", "-t", sessionName, "-F", "#{pane_pid}",
	).Output()
	if err != nil {
		return 0, nil
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if first == "" {
		return 0, nil
	}
	pid, err := strconv.Atoi(first)
	if err != nil {
		return 0, fmt.Errorf("parse pane_pid %q: %w", first, err)
	}
	return pid, nil
}

// StartOutputPipe enables tmux pipe-pane on the session's main pane, redirecting
// its stdout to the given FIFO path. The FIFO must already exist; the caller is
// responsible for opening it for read.
func (r *Runner) StartOutputPipe(ctx context.Context, sessionName, fifoPath string) error {
	args := OutputPipeArgs(sessionName, fifoPath)
	return exec.CommandContext(ctx, r.binary, args...).Run()
}

// StopOutputPipe disables an active pipe-pane on the session's main pane.
func (r *Runner) StopOutputPipe(ctx context.Context, sessionName string) error {
	args := StopOutputPipeArgs(sessionName)
	return exec.CommandContext(ctx, r.binary, args...).Run()
}

// ResizeWindow drives the SPA-supplied grid into tmux's pane. Called by the
// session manager when a `client.resize` envelope arrives from a viewer; the
// pane's `window-size manual` setting (configured in StartSession) lets us
// pin the size against any concurrent host-side `tmux attach`.
func (r *Runner) ResizeWindow(ctx context.Context, sessionName string, cols, rows uint32) error {
	if sessionName == "" {
		return errors.New("session name is required")
	}
	if cols == 0 || rows == 0 {
		return errors.New("cols/rows must be positive")
	}
	return exec.CommandContext(ctx, r.binary,
		"resize-window", "-t", sessionName,
		"-x", strconv.Itoa(int(cols)),
		"-y", strconv.Itoa(int(rows)),
	).Run()
}

func (r *Runner) StartSession(ctx context.Context, spec session.StartSpec) error {
	if spec.SessionName == "" {
		return errors.New("tmux session name is required")
	}
	if spec.ToolCommand == "" {
		return errors.New("tool command is required")
	}

	cols, rows := initialPaneSize(spec.Cols, spec.Rows)
	args := []string{
		"new-session",
		"-d",
		"-s", spec.SessionName,
		"-n", "main",
		"-x", strconv.Itoa(cols),
		"-y", strconv.Itoa(rows),
	}
	if spec.WorkingDir != "" {
		args = append(args, "-c", spec.WorkingDir)
	}
	// Forward CLI-captured env vars to the new session via tmux's own
	// environment mechanism. Skip vars that tmux manages so the pane gets
	// the correct terminal type and doesn't see a stale outer TMUX handle.
	keys := make([]string, 0, len(spec.Env))
	for key := range spec.Env {
		if key == "" || key == "TERM" || key == "TMUX" || key == "TMUX_PANE" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "-e", key+"="+spec.Env[key])
	}
	// Run the tool as the pane's primary process via `sh -c`. tmux forks
	// the command directly — nothing is typed as keystrokes, so no shell
	// prompt echo and no env-var wall on screen before the tool starts.
	// When ErrLogPath is set, mirror stderr to that file so a tool that
	// exits immediately leaves a readable tail (e.g. "exec: codex: not
	// found") instead of just an opaque [exited].
	shellCmd := "exec " + spec.ToolCommand
	if spec.ErrLogPath != "" {
		if err := os.MkdirAll(filepath.Dir(spec.ErrLogPath), 0o700); err == nil {
			shellCmd = fmt.Sprintf("exec %s 2>>%s", spec.ToolCommand, shellSingleQuote(spec.ErrLogPath))
		}
	}
	args = append(args, "sh", "-c", shellCmd)

	if err := exec.CommandContext(ctx, r.binary, args...).Run(); err != nil {
		return err
	}

	// Detect synchronous launch failures (e.g. tool binary missing on PATH
	// inside the pane env). tmux returned success because the *session* was
	// created; the pane process may have died microseconds later. Probe once
	// after a short delay — if the session is gone, surface the captured
	// stderr instead of silently returning success and leaving the user
	// staring at [exited] on attach. Skipped for one-shot commands.
	if spec.DetectImmediateExit {
		time.Sleep(startSessionLivenessProbe)
		if !r.HasSession(ctx, spec.SessionName) {
			tail := readErrLogTail(spec.ErrLogPath)
			if tail != "" {
				return fmt.Errorf("session exited immediately: %s", tail)
			}
			return errors.New("session exited immediately (no captured stderr)")
		}
	}

	// Lock the window size so the pane doesn't resize whenever a client
	// attaches at a different terminal size. Without this, a host-side
	// `tmux attach` from a 184x36 shell would push the pane to 184x36;
	// the SPA's xterm is locked at 120x40, so Claude's TUI (laid out for
	// 184 cols) renders shifted in the browser. With window-size=manual,
	// the pane stays at the -x/-y size set above.
	return exec.CommandContext(
		ctx, r.binary,
		"set-option", "-t", spec.SessionName,
		"window-size", "manual",
	).Run()
}

// readErrLogTail returns the trailing portion of the error log so the caller
// can surface it without flooding the response with megabytes of output.
func readErrLogTail(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	const maxBytes = 2048
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	return strings.TrimSpace(string(data))
}
