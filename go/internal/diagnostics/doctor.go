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

// checkProxy describes the proxy state effective for HTTP/gRPC/WSS dials
// in this process. By the time this runs the proxyenv policy has already
// been applied (CLI applies in `run`, daemon applies at boot), so what
// shows up in the env is what HTTP clients will use. Three cases:
//   - no proxy env set → `proxy: ok (enable_proxy disabled; relay WSS dials directly)`
//   - proxy env set → `proxy: enabled (HTTPS_PROXY=... — relay WSS routes through proxy)`
//   - proxy env set with mixed semantics (rare) → same as above
func checkProxy() string {
	var set []string
	for _, name := range proxyEnvNames {
		if os.Getenv(name) != "" || os.Getenv(strings.ToLower(name)) != "" {
			set = append(set, name)
		}
	}
	if len(set) == 0 {
		return "proxy: ok (enable_proxy disabled; relay WSS dials directly)"
	}
	return fmt.Sprintf("proxy: enabled (%s set — relay WSS routes through proxy; flip enable_proxy to false in host.json to bypass)", strings.Join(set, ", "))
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
