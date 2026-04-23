package tmux

import (
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
