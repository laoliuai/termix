package tests

import (
	"testing"

	"github.com/termix/termix/go/internal/tmux"
)

func TestSessionName(t *testing.T) {
	if got := tmux.SessionName("1234"); got != "termix_1234" {
		t.Fatalf("expected termix_1234, got %q", got)
	}
}

func TestAttachCommand(t *testing.T) {
	if got := tmux.AttachCommand("termix_1234"); got != "tmux attach-session -t termix_1234" {
		t.Fatalf("unexpected attach command: %q", got)
	}
}
