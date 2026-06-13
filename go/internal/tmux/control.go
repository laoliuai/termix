package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type OutputEvent struct {
	PaneID  string
	Payload []byte
}

// SnapshotArgs builds the `tmux capture-pane` invocation used to seed a
// browser viewer when it joins (or rejoins) a session. We deliberately
// capture only the *currently visible* pane — no `-S -N` scrollback —
// because termix's primary clients are TUI apps (claude / codex /
// opencode) that redraw the whole UI in the main screen buffer on every
// SIGWINCH or new turn. Each redraw pushes the previous frame into
// tmux's scrollback, so a snapshot that includes scrollback ends up
// containing one or more "ghost" copies of the welcome screen above the
// current state. Capturing visible-only matches what a host-side
// `tmux attach` shows and keeps the browser xterm output single-frame.
func SnapshotArgs(sessionName string) []string {
	return []string{"capture-pane", "-p", "-e", "-t", sessionName + ":main.0"}
}

// CaptureSnapshot runs `tmux capture-pane` and returns the pane content with
// CRLF line endings. tmux emits bare LFs between rows, but the live PTY
// stream from pipe-pane uses CRLF and the SPA's xterm runs with
// convertEol=false — so without this normalization the snapshot lines stair-
// step instead of resetting the cursor to column 0 on each newline.
func CaptureSnapshot(ctx context.Context, sessionName string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "tmux", SnapshotArgs(sessionName)...).Output()
	if err != nil {
		return nil, err
	}
	return NormalizeSnapshot(out), nil
}

// PaneSize returns the current (cols, rows) of the session's main pane via
// `tmux display-message`. Used by snapshot.ready (authoritative size) and the
// host-resize re-push to detect when the pane size changes.
func PaneSize(ctx context.Context, sessionName string) (uint32, uint32, error) {
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p",
		"-t", sessionName+":main.0", "#{pane_width} #{pane_height}").Output()
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("PaneSize: expected two integers, got %q", string(out))
	}
	cols, err := strconv.ParseUint(fields[0], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("PaneSize parse cols %q: %w", fields[0], err)
	}
	rows, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("PaneSize parse rows %q: %w", fields[1], err)
	}
	return uint32(cols), uint32(rows), nil
}

// snapshotResetPrefix clears the scrollback (\e[3J), clears the visible screen
// (\e[2J), and homes the cursor (\e[H). Prepending it makes every snapshot
// *self-clearing*: it draws onto a blank screen even on the paths that publish
// a snapshot without first sending `session.snapshot.ready` (which is what
// triggers the SPA's term.reset()) — notably ReannounceAllSessions after a
// relay reconnect. On the normal watch path this is harmless belt-and-suspenders
// alongside the SPA reset.
const snapshotResetPrefix = "\x1b[3J\x1b[2J\x1b[H"

// NormalizeSnapshot prepares raw `capture-pane` bytes for the browser xterm:
// it normalizes line endings to CRLF (tmux emits bare LFs but the live
// pipe-pane stream uses CRLF and xterm runs convertEol=false, so without this
// the snapshot rows stair-step) and prepends snapshotResetPrefix so the frame
// is self-clearing. Pure and unit-tested; CaptureSnapshot is the only caller.
func NormalizeSnapshot(raw []byte) []byte {
	// Collapse any pre-existing CRLF first so we don't double the CR, then
	// expand all LF to CRLF.
	out := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	return append([]byte(snapshotResetPrefix), out...)
}

// OutputPipeArgs returns the tmux invocation that pipes a pane's stdout to a
// shell command writing to the given FIFO path. tmux runs the command as a
// child of the server; it terminates when the pane closes or pipe-pane is
// disabled. We use `stdbuf -o0 cat` because libc block-buffers stdout when
// it's not a terminal, which would batch live output until a 4 KB buffer
// fills — defeating the point of streaming.
func OutputPipeArgs(sessionName, fifoPath string) []string {
	command := "stdbuf -o0 cat >> " + shellSingleQuote(fifoPath)
	return []string{"pipe-pane", "-t", sessionName + ":main.0", command}
}

// StopOutputPipeArgs disables an in-progress pipe-pane on the given session.
// Calling pipe-pane with no shell-command toggles the pipe off.
func StopOutputPipeArgs(sessionName string) []string {
	return []string{"pipe-pane", "-t", sessionName + ":main.0"}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func InputArgs(sessionName string, payload []byte) [][]string {
	target := sessionName + ":main.0"
	args := make([][]string, 0, len(payload))
	var literal bytes.Buffer

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		args = append(args, []string{"send-keys", "-t", target, "-l", "--", literal.String()})
		literal.Reset()
	}

	appendKey := func(key string) {
		flushLiteral()
		args = append(args, []string{"send-keys", "-t", target, key})
	}

	for _, b := range payload {
		switch b {
		case '\r', '\n':
			appendKey("Enter")
		case '\t':
			appendKey("Tab")
		case 0x03:
			appendKey("C-c")
		case 0x1b:
			appendKey("Escape")
		default:
			if b >= 0x20 && b != 0x7f {
				_ = literal.WriteByte(b)
			}
		}
	}
	flushLiteral()
	return args
}

func InjectInput(ctx context.Context, sessionName string, payload []byte) error {
	for _, args := range InputArgs(sessionName, payload) {
		if err := exec.CommandContext(ctx, "tmux", args...).Run(); err != nil {
			return err
		}
	}
	return nil
}

func ParseControlLine(line string) (OutputEvent, bool) {
	if !strings.HasPrefix(line, "%output ") {
		return OutputEvent{}, false
	}

	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return OutputEvent{}, false
	}
	return OutputEvent{
		PaneID:  parts[1],
		Payload: []byte(parts[2]),
	}, true
}
