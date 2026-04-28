package tmux

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

type OutputEvent struct {
	PaneID  string
	Payload []byte
}

func SnapshotArgs(sessionName string) []string {
	return []string{"capture-pane", "-p", "-e", "-S", "-200", "-t", sessionName + ":main.0"}
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
	// Collapse any pre-existing CRLF first so we don't double the CR, then
	// expand all LF to CRLF.
	out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
	out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	return out, nil
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
