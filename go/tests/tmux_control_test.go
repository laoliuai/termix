package tests

import (
	"testing"

	"github.com/termix/termix/go/internal/tmux"
)

func TestSnapshotCommandArgs(t *testing.T) {
	args := tmux.SnapshotArgs("termix_session-1")
	want := []string{"capture-pane", "-p", "-e", "-S", "-200", "-t", "termix_session-1:main.0"}
	if len(args) != len(want) {
		t.Fatalf("expected %d args, got %d", len(want), len(args))
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d: expected %q, got %q", i, want[i], args[i])
		}
	}
}

func TestParseOutputLine(t *testing.T) {
	event, ok := tmux.ParseControlLine("%output %1 hello world")
	if !ok {
		t.Fatalf("%s", "expected %output line to parse")
	}
	if string(event.Payload) != "hello world" {
		t.Fatalf("unexpected payload: %q", event.Payload)
	}
	if event.PaneID != "%1" {
		t.Fatalf("unexpected pane id: %q", event.PaneID)
	}
}
