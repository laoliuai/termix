package session

import (
	"context"
	"time"
)

type LocalSession struct {
	SessionID       string    `json:"session_id"`
	Name            string    `json:"name,omitempty"`
	Tool            string    `json:"tool"`
	Status          string    `json:"status"`
	TmuxSessionName string    `json:"tmux_session_name"`
	AttachCommand   string    `json:"attach_command"`
	Cwd             string    `json:"cwd"`
	LaunchCommand   string    `json:"launch_command"`
	StartedAt       time.Time `json:"started_at"`
}

type StartSpec struct {
	SessionName string
	WorkingDir  string
	Shell       string
	Env         map[string]string
	ToolCommand string
	// ErrLogPath, when set, is appended to the shell command as `2>>path`
	// so a tool that exits immediately leaves a readable error tail at that
	// path.
	ErrLogPath string
	// DetectImmediateExit asks the runner to verify the tmux session is still
	// alive shortly after creation and return an error (with the ErrLogPath
	// tail when available) if the pane exited synchronously. Intended for
	// long-lived TUI tools like claude/codex/opencode where an immediate exit
	// is always a launch failure. Should be left false for one-shot commands.
	DetectImmediateExit bool
	// Cols/Rows is the desired initial pane size. When zero (e.g. CLI ran
	// without a tty on stdout), the runner falls back to a sensible default.
	// Floors are applied so a tiny host terminal cannot produce an unusable
	// pane.
	Cols int
	Rows int
}

type SnapshotFunc func(context.Context, string) ([]byte, error)
type InputFunc func(context.Context, string, []byte) error

type RelayClient interface {
	AnnounceSession(context.Context, LocalSession) error
	PublishSnapshot(context.Context, string, []byte) error
	PublishOutput(context.Context, string, []byte) error
	SetSnapshotHandler(func(context.Context, string) ([]byte, error))
	SetInputHandler(func(context.Context, string, []byte) error)
	// SetSizeHandler injects the daemon's session-id -> (cols, rows) lookup so
	// the relay can put the authoritative pane size on snapshot.ready (Stage 2).
	SetSizeHandler(func(context.Context, string) (uint32, uint32, error))
	// RepushSnapshot re-publishes a snapshot with a new authoritative size and
	// the current generation after a host-driven pane resize (Stage 2).
	RepushSnapshot(ctx context.Context, sessionID string, snapshot []byte, cols, rows uint32) error
}
