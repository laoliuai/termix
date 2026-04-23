package diagnostics

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/daemonipc"
)

type Runner struct {
	paths      config.HostPaths
	socketPath string
}

func NewRunner(paths config.HostPaths) *Runner {
	return &Runner{
		paths:      paths,
		socketPath: daemonipc.SocketPath(paths),
	}
}

func (r *Runner) Checks(ctx context.Context) ([]string, error) {
	checks := make([]string, 0, 5)
	checks = append(checks, checkBinary(ctx, "tmux"))
	checks = append(checks, checkSecureFile("credentials", r.paths.CredentialsFile))
	checks = append(checks, checkWritableDir("run_dir", r.paths.RunDir))
	checks = append(checks, checkWritableDir("state_dir", r.paths.StateDir))
	checks = append(checks, checkSocket(r.socketPath))
	return checks, nil
}

func checkBinary(ctx context.Context, binary string) string {
	if _, err := exec.LookPath(binary); err != nil {
		return binary + ": missing"
	}
	if err := exec.CommandContext(ctx, binary, "-V").Run(); err != nil {
		return binary + ": error"
	}
	return binary + ": ok"
}

func checkSecureFile(label string, path string) string {
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return label + ": missing"
	case err != nil:
		return label + ": error"
	case info.Mode().Perm() != 0o600:
		return label + ": insecure"
	default:
		return label + ": ok"
	}
}

func checkWritableDir(label string, dir string) string {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return label + ": error"
	}

	testFile := filepath.Join(dir, ".termix-write-check")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return label + ": error"
	}
	_ = os.Remove(testFile)
	return label + ": ok"
}

func checkSocket(path string) string {
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return "socket: missing"
	case err != nil:
		return "socket: error"
	case info.Mode().Perm() != 0o600:
		return fmt.Sprintf("socket: insecure (%04o)", info.Mode().Perm())
	default:
		return "socket: ok"
	}
}
