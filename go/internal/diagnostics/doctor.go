package diagnostics

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/daemonipc"
)

// proxyEnvNames lists the env vars Go's net/http reads at process start to
// route HTTP/HTTPS dials through a proxy. Only the canonical uppercase forms
// are listed; checkProxy also probes the lowercase aliases internally.
var proxyEnvNames = []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"}

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
	checks := make([]string, 0, 6)
	checks = append(checks, checkBinary(ctx, "tmux"))
	checks = append(checks, checkSecureFile("credentials", r.paths.CredentialsFile))
	checks = append(checks, checkWritableDir("run_dir", r.paths.RunDir))
	checks = append(checks, checkWritableDir("state_dir", r.paths.StateDir))
	checks = append(checks, checkSocket(r.socketPath))
	checks = append(checks, checkProxy())
	return checks, nil
}

// checkProxy reports the user's shell-level proxy env vars for visibility
// only. The relay WSS uses its own dedicated http.Client with `Proxy: nil`
// so the long-lived stream is never routed through HTTP CONNECT — no
// matter what HTTPS_PROXY etc. are set to. Other endpoints (login,
// refresh, heartbeat, doctor) honor the user's env so corporate-proxy
// users can reach the control plane.
func checkProxy() string {
	var set []string
	for _, name := range proxyEnvNames {
		if os.Getenv(name) != "" || os.Getenv(strings.ToLower(name)) != "" {
			set = append(set, name)
		}
	}
	if len(set) == 0 {
		return "proxy: ok (no shell proxy env; all dials direct)"
	}
	return fmt.Sprintf("proxy: ok (%s set; relay WSS bypasses, other endpoints honor)", strings.Join(set, ", "))
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
