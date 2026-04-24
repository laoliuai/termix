package tests

import (
	"reflect"
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

func TestInputArgsMapPrintableAndSpecialBytes(t *testing.T) {
	got := tmux.InputArgs("termix_session-1", []byte("ls\n\t\x03\x1b"))
	want := [][]string{
		{"send-keys", "-t", "termix_session-1:main.0", "-l", "--", "ls"},
		{"send-keys", "-t", "termix_session-1:main.0", "Enter"},
		{"send-keys", "-t", "termix_session-1:main.0", "Tab"},
		{"send-keys", "-t", "termix_session-1:main.0", "C-c"},
		{"send-keys", "-t", "termix_session-1:main.0", "Escape"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected input args:\nwant: %#v\ngot:  %#v", want, got)
	}
}
