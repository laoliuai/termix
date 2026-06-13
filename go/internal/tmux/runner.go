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

// BinaryInfo resolves the tmux binary on PATH and reports its version. A
// missing binary or a failing `tmux -V` returns an empty (Installed=false)
// TmuxInfo so the daemon can surface the absence in `termix status`
// rather than blowing up the response.
func (r *Runner) BinaryInfo(ctx context.Context) session.TmuxInfo {
	path, err := exec.LookPath(r.binary)
	if err != nil {
		return session.TmuxInfo{}
	}
	out, err := exec.CommandContext(ctx, r.binary, "-V").Output()
	if err != nil {
		return session.TmuxInfo{Installed: true, Path: path}
	}
	version := strings.TrimSpace(string(out))
	version = strings.TrimPrefix(version, "tmux ")
	return session.TmuxInfo{Installed: true, Path: path, Version: version}
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

// HasSession reports whether `tmux has-session -t sessionName` succeeds.
// Returns (true, nil) when tmux confirms the session, (false, nil) when
// tmux returns "session not found" (exit code 1), and (false, err) when
// the tmux invocation itself failed — binary missing, socket unreachable,
// permission denied, context canceled, etc.
//
// The error split matters for the reaper: collapsing every nonzero exit to
// `missing` would PATCH live sessions to `exited` whenever tmux briefly
// failed to respond (a transient socket error, a daemon restart), and the
// SPA would show the session as dead even though the pane is still running.
func (r *Runner) HasSession(ctx context.Context, sessionName string) (bool, error) {
	err := exec.CommandContext(ctx, r.binary, "has-session", "-t", sessionName).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// Documented "no such session" path — tmux exits 1 and prints
		// "can't find session" on stderr. Treat as a definitive missing.
		return false, nil
	}
	return false, err
}

// KillSession runs `tmux kill-session -t sessionName`. tmux delivers SIGHUP
// to the pane's process group, giving the tool (claude/codex/opencode) a
// chance to flush state before exiting. Idempotent: returns nil if the
// session is already gone. Surfaces a real has-session failure (tmux down,
// binary missing) instead of attempting a kill that would itself fail.
func (r *Runner) KillSession(ctx context.Context, sessionName string) error {
	if sessionName == "" {
		return errors.New("tmux session name is required")
	}
	has, err := r.HasSession(ctx, sessionName)
	if err != nil {
		return err
	}
	if !has {
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

// ResizeWindow drives an explicit (cols, rows) into tmux's pane via
// resize-window. Under Stage 2 D1 the pane is configured with
// `window-size latest` (in StartSession), so a concurrent host-side
// `tmux attach` is authoritative for the pane size — this call does NOT
// pin the pane against the host. It is only effective while no host client
// is attached (e.g. a detached session), and any later host attach will
// override it. SPA viewers adopt the published authoritative size and
// CSS-downscale to fit rather than driving a resize through here.
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

	// Run the tool as the pane's primary process via `sh -c`. tmux forks
	// the command directly — nothing is typed as keystrokes, so no shell
	// prompt echo and no env-var wall on screen before the tool starts.
	// When ErrLogPath is set, mirror stderr to that file so a tool that
	// exits immediately leaves a readable tail (e.g. "exec: codex: not
	// found") instead of just an opaque [exited].
	effectiveErrLog := ""
	if spec.ErrLogPath != "" {
		if err := os.MkdirAll(filepath.Dir(spec.ErrLogPath), 0o700); err == nil {
			effectiveErrLog = spec.ErrLogPath
		}
	}
	args := newSessionArgs(spec, effectiveErrLog)

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
		// Treat a HasSession error as "couldn't determine, assume still
		// alive" so a transient tmux blip doesn't fail a legitimate start;
		// only a confirmed "missing" triggers the stderr-tail diagnostic.
		if has, err := r.HasSession(ctx, spec.SessionName); err == nil && !has {
			tail := readErrLogTail(spec.ErrLogPath)
			if tail != "" {
				return fmt.Errorf("session exited immediately: %s", tail)
			}
			return errors.New("session exited immediately (no captured stderr)")
		}
	}

	// Make the pane track the most-recently-active local tmux client
	// (`window-size latest`). The host terminal's `tmux attach` is a real
	// tmux client and is therefore authoritative for the pane size (Stage 2
	// D1: host-authoritative sizing). SPA viewers attach via capture-pane /
	// pipe-pane, which are NOT tmux clients and so do not trigger a resize —
	// they adopt the published authoritative size and CSS-downscale to fit.
	// A detached session keeps its last size (the -x/-y birth size set above)
	// until a host client attaches.
	return exec.CommandContext(
		ctx, r.binary,
		"set-option", "-t", spec.SessionName,
		"window-size", "latest",
	).Run()
}

// newSessionArgs builds the argv (after the binary name) for the
// `tmux new-session ...` invocation that StartSession runs. Factored out so
// tests can lock in the bug-fix that environment variables are inlined into
// the `sh -c` command instead of appearing as `-e KEY=VAL` flags
// (incompatible with tmux <3.2; see buildShellCommand). errLogPath empty =
// no stderr redirect.
func newSessionArgs(spec session.StartSpec, errLogPath string) []string {
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
	args = append(args, "sh", "-c", buildShellCommand(spec.ToolCommand, spec.Env, errLogPath))
	return args
}

// buildShellCommand returns the full POSIX shell command run inside the
// tmux pane. Environment variables are inlined as `KEY='value' ` prefixes
// in front of `exec <tool>` so they reach the tool without depending on
// `tmux new-session -e` (which was added in tmux 3.2 and fails on earlier
// versions like the 3.0a shipped by Ubuntu 20.04). errLogPath, when
// non-empty, redirects the tool's stderr to a per-session log file so a
// fast-failing launch surfaces readable output via DetectImmediateExit.
func buildShellCommand(toolCommand string, env map[string]string, errLogPath string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		if key == "" || key == "TERM" || key == "TMUX" || key == "TMUX_PANE" {
			continue
		}
		if !validShellVarName(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(shellSingleQuote(env[key]))
		b.WriteByte(' ')
	}
	b.WriteString("exec ")
	b.WriteString(toolCommand)
	if errLogPath != "" {
		b.WriteString(" 2>>")
		b.WriteString(shellSingleQuote(errLogPath))
	}
	return b.String()
}

// validShellVarName reports whether name is safe to use on the LHS of a
// `KEY=value` assignment in a POSIX `sh -c` command. Bash exports function
// definitions through environment entries like `BASH_FUNC_foo%%=...` whose
// names contain `%` and would produce a shell syntax error if inlined.
func validShellVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, ch := range name {
		switch {
		case ch >= 'A' && ch <= 'Z':
		case ch >= 'a' && ch <= 'z':
		case ch == '_':
		case i > 0 && ch >= '0' && ch <= '9':
		default:
			return false
		}
	}
	return true
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
