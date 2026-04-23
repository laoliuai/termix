package tmux

import (
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"

	"github.com/termix/termix/go/internal/session"
)

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

func (r *Runner) StartSession(ctx context.Context, spec session.StartSpec) error {
	if spec.SessionName == "" {
		return errors.New("tmux session name is required")
	}
	if spec.ToolCommand == "" {
		return errors.New("tool command is required")
	}

	args := []string{
		"new-session",
		"-d",
		"-s", spec.SessionName,
		"-n", "main",
		"-x", "120",
		"-y", "40",
	}
	if spec.WorkingDir != "" {
		args = append(args, "-c", spec.WorkingDir)
	}
	if err := exec.CommandContext(ctx, r.binary, args...).Run(); err != nil {
		return err
	}

	return exec.CommandContext(
		ctx,
		r.binary,
		"send-keys",
		"-t", spec.SessionName+":main.0",
		buildLaunchCommand(spec),
		"C-m",
	).Run()
}

func buildLaunchCommand(spec session.StartSpec) string {
	if len(spec.Env) == 0 {
		return "exec " + spec.ToolCommand
	}

	keys := make([]string, 0, len(spec.Env))
	for key := range spec.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys)+2)
	parts = append(parts, "env")
	for _, key := range keys {
		parts = append(parts, key+"="+shellQuote(spec.Env[key]))
	}
	parts = append(parts, "exec", spec.ToolCommand)
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
