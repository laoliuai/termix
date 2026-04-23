package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/daemonipc"
)

func TestTermixDoctorSmoke(t *testing.T) {
	paths := smokeHostPaths(t)
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	listener, err := daemonipc.Listen(daemonipc.SocketPath(paths))
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer listener.Close()

	server := daemonipc.NewServer(smokeDaemonServer{})
	defer server.Stop()
	go func() {
		_ = server.Serve(listener)
	}()

	binaryPath := filepath.Join(t.TempDir(), "termix")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/termix")
	buildCmd.Dir = moduleRoot(t)
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(output))
	}

	cmd := exec.Command(binaryPath, "doctor")
	cmd.Env = append(os.Environ(),
		"HOME="+homeDirFor(paths),
		"XDG_RUNTIME_DIR="+xdgRuntimeDirFor(paths),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("termix doctor failed: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "tmux: ok") {
		t.Fatalf("expected tmux check in output, got %q", string(output))
	}
}

func TestTermixUsageSmoke(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "termix")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/termix")
	buildCmd.Dir = moduleRoot(t)
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(output))
	}

	cmd := exec.Command(binaryPath)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected usage invocation to fail")
	}
	if !strings.Contains(string(output), "usage: termix") {
		t.Fatalf("expected usage output, got %q", string(output))
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join(".."))
}

func smokeHostPaths(t *testing.T) config.HostPaths {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	runtimeDir := filepath.Join(base, "runtime")
	return config.HostPaths{
		ConfigDir:       filepath.Join(home, ".config", "termix"),
		StateDir:        filepath.Join(home, ".local", "state", "termix"),
		LogDir:          filepath.Join(home, ".local", "state", "termix", "logs"),
		RunDir:          filepath.Join(runtimeDir, "termix"),
		CredentialsFile: filepath.Join(home, ".config", "termix", "credentials.json"),
	}
}

func homeDirFor(paths config.HostPaths) string {
	return filepath.Dir(filepath.Dir(paths.ConfigDir))
}

func xdgRuntimeDirFor(paths config.HostPaths) string {
	return filepath.Dir(paths.RunDir)
}

type smokeDaemonServer struct {
	daemonv1.UnimplementedDaemonServiceServer
}

func (smokeDaemonServer) Health(context.Context, *daemonv1.HealthRequest) (*daemonv1.HealthResponse, error) {
	return &daemonv1.HealthResponse{Status: "ok"}, nil
}

func (smokeDaemonServer) StartSession(context.Context, *daemonv1.StartSessionRequest) (*daemonv1.StartSessionResponse, error) {
	return &daemonv1.StartSessionResponse{}, nil
}

func (smokeDaemonServer) ListSessions(context.Context, *daemonv1.ListSessionsRequest) (*daemonv1.ListSessionsResponse, error) {
	return &daemonv1.ListSessionsResponse{}, nil
}

func (smokeDaemonServer) AttachInfo(context.Context, *daemonv1.AttachInfoRequest) (*daemonv1.AttachInfoResponse, error) {
	return &daemonv1.AttachInfoResponse{}, nil
}

func (smokeDaemonServer) Doctor(context.Context, *daemonv1.DoctorRequest) (*daemonv1.DoctorResponse, error) {
	return &daemonv1.DoctorResponse{Checks: []string{"tmux: ok", "socket: ok"}}, nil
}
