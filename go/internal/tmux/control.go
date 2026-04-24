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

func CaptureSnapshot(ctx context.Context, sessionName string) ([]byte, error) {
	return exec.CommandContext(ctx, "tmux", SnapshotArgs(sessionName)...).Output()
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
