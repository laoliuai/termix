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
}

type SnapshotFunc func(context.Context, string) ([]byte, error)
type InputFunc func(context.Context, string, []byte) error

type RelayClient interface {
	AnnounceSession(context.Context, LocalSession) error
	PublishSnapshot(context.Context, string, []byte) error
	PublishOutput(context.Context, string, []byte) error
	SetSnapshotHandler(func(context.Context, string) ([]byte, error))
	SetInputHandler(func(context.Context, string, []byte) error)
}
