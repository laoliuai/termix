package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/daemonipc"
	"google.golang.org/grpc"
)

type loginClient interface {
	Login(ctx context.Context, req openapi.LoginRequest) (*openapi.LoginResponse, error)
}

type cliDeps struct {
	stdin            io.Reader
	stdout           io.Writer
	stderr           io.Writer
	now              func() time.Time
	getwd            func() (string, error)
	hostname         func() (string, error)
	getenv           func(string) string
	environ          func() []string
	paths            config.HostPaths
	newControlClient func(baseURL string) (loginClient, error)
	dialDaemon       func(ctx context.Context, socketPath string) (daemonv1.DaemonServiceClient, io.Closer, error)
	launchDaemon     func(ctx context.Context, paths config.HostPaths) error
	attachTmux       func(ctx context.Context, sessionName string) error
	sleep            func(time.Duration)
}

func main() {
	os.Exit(run(context.Background(), os.Args, defaultDeps()))
}

func run(ctx context.Context, args []string, deps cliDeps) int {
	if len(args) < 2 {
		fmt.Fprintln(deps.stderr, "usage: termix <login|start|sessions|doctor>")
		return 2
	}

	var err error
	switch args[1] {
	case "login":
		err = runLogin(ctx, deps)
	case "start":
		err = runStart(ctx, args[2:], deps)
	case "sessions":
		err = runSessions(ctx, args[2:], deps)
	case "doctor":
		err = runDoctor(ctx, deps)
	default:
		fmt.Fprintf(deps.stderr, "unknown command: %s\n", args[1])
		return 2
	}

	if err != nil {
		fmt.Fprintln(deps.stderr, err)
		return 1
	}
	return 0
}

func defaultDeps() cliDeps {
	return cliDeps{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		now:    time.Now,
		getwd:  os.Getwd,
		hostname: func() (string, error) {
			return os.Hostname()
		},
		getenv:  os.Getenv,
		environ: os.Environ,
		paths:   config.DefaultHostPaths(),
		newControlClient: func(baseURL string) (loginClient, error) {
			return controlapi.New(baseURL, http.DefaultTransport)
		},
		dialDaemon: func(ctx context.Context, socketPath string) (daemonv1.DaemonServiceClient, io.Closer, error) {
			client, conn, err := daemonipc.Dial(ctx, socketPath)
			return client, conn, err
		},
		launchDaemon: launchDaemonProcess,
		attachTmux:   attachTmuxSession,
		sleep:        time.Sleep,
	}
}

func runLogin(ctx context.Context, deps cliDeps) error {
	serverURL, err := readLine(deps.stdin, deps.stdout, "Server URL: ")
	if err != nil {
		return err
	}
	email, err := readLine(deps.stdin, deps.stdout, "Email: ")
	if err != nil {
		return err
	}
	password, err := readLine(deps.stdin, deps.stdout, "Password: ")
	if err != nil {
		return err
	}

	client, err := deps.newControlClient(serverURL)
	if err != nil {
		return err
	}

	host, err := deps.hostname()
	if err != nil || host == "" {
		host = "termix-host"
	}

	resp, err := client.Login(ctx, openapi.LoginRequest{
		Email:       openapi_types.Email(email),
		Password:    password,
		DeviceType:  openapi.Host,
		Platform:    hostPlatform(runtime.GOOS),
		DeviceLabel: host,
	})
	if err != nil {
		return err
	}

	return credentials.Save(deps.paths.CredentialsFile, credentials.StoredCredentials{
		ServerBaseURL: serverURL,
		UserID:        resp.User.Id.String(),
		DeviceID:      resp.Device.Id.String(),
		AccessToken:   resp.AccessToken,
		RefreshToken:  resp.RefreshToken,
		ExpiresAt:     deps.now().Add(time.Duration(resp.ExpiresInSeconds) * time.Second).UTC().Format(time.RFC3339),
	})
}

func runStart(ctx context.Context, args []string, deps cliDeps) error {
	tool, name, err := parseStartArgs(args)
	if err != nil {
		return err
	}
	if err := ensureDaemon(ctx, deps); err != nil {
		return err
	}

	client, conn, err := deps.dialDaemon(ctx, daemonipc.SocketPath(deps.paths))
	if err != nil {
		return err
	}
	defer conn.Close()

	cwd, err := deps.getwd()
	if err != nil {
		return err
	}

	resp, err := client.StartSession(ctx, &daemonv1.StartSessionRequest{
		Tool:     tool,
		Name:     name,
		Cwd:      cwd,
		Shell:    deps.getenv("SHELL"),
		Term:     deps.getenv("TERM"),
		Language: firstNonEmpty(deps.getenv("LC_ALL"), deps.getenv("LANG")),
		Env:      captureEnv(deps.environ()),
	})
	if err != nil {
		return err
	}

	if err := deps.attachTmux(ctx, resp.TmuxSessionName); err != nil {
		return fmt.Errorf("attach failed, run manually: %s", firstNonEmpty(resp.AttachCommand, "tmux attach-session -t "+resp.TmuxSessionName))
	}
	return nil
}

func runSessions(ctx context.Context, args []string, deps cliDeps) error {
	if len(args) != 2 || args[0] != "attach" {
		return errors.New("usage: termix sessions attach <session_id>")
	}
	if err := ensureDaemon(ctx, deps); err != nil {
		return err
	}

	client, conn, err := deps.dialDaemon(ctx, daemonipc.SocketPath(deps.paths))
	if err != nil {
		return err
	}
	defer conn.Close()

	resp, err := client.AttachInfo(ctx, &daemonv1.AttachInfoRequest{SessionId: args[1]})
	if err != nil {
		return err
	}
	return deps.attachTmux(ctx, resp.TmuxSessionName)
}

func runDoctor(ctx context.Context, deps cliDeps) error {
	client, conn, err := deps.dialDaemon(ctx, daemonipc.SocketPath(deps.paths))
	if err != nil {
		return err
	}
	defer conn.Close()

	resp, err := client.Doctor(ctx, &daemonv1.DoctorRequest{})
	if err != nil {
		return err
	}

	for _, check := range resp.Checks {
		fmt.Fprintln(deps.stdout, check)
	}
	return nil
}

func ensureDaemon(ctx context.Context, deps cliDeps) error {
	socketPath := daemonipc.SocketPath(deps.paths)

	if client, conn, err := deps.dialDaemon(ctx, socketPath); err == nil {
		defer conn.Close()
		if _, err := client.Health(ctx, &daemonv1.HealthRequest{}); err == nil {
			return nil
		}
	}

	if deps.launchDaemon == nil {
		return errors.New("daemon is not available")
	}
	if err := deps.launchDaemon(ctx, deps.paths); err != nil {
		return err
	}

	for attempt := 0; attempt < 20; attempt++ {
		client, conn, err := deps.dialDaemon(ctx, socketPath)
		if err == nil {
			healthResp, healthErr := client.Health(ctx, &daemonv1.HealthRequest{})
			_ = conn.Close()
			if healthErr == nil && healthResp.GetStatus() == "ok" {
				return nil
			}
		}
		if deps.sleep != nil {
			deps.sleep(100 * time.Millisecond)
		}
	}
	return errors.New("daemon did not become healthy")
}

func parseStartArgs(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", errors.New("usage: termix start <tool> [-n name]")
	}

	tool := args[0]
	name := ""
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "-n", "--name":
			index++
			if index >= len(args) {
				return "", "", errors.New("missing value for --name")
			}
			name = args[index]
		default:
			return "", "", fmt.Errorf("unknown start argument: %s", args[index])
		}
	}
	return tool, name, nil
}

func readLine(input io.Reader, output io.Writer, prompt string) (string, error) {
	if output != nil {
		fmt.Fprint(output, prompt)
	}

	reader := bufio.NewReader(input)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func captureEnv(values []string) map[string]string {
	env := make(map[string]string, len(values))
	for _, item := range values {
		key, value, found := strings.Cut(item, "=")
		if !found {
			continue
		}
		env[key] = value
	}
	return env
}

func hostPlatform(goos string) openapi.LoginRequestPlatform {
	if goos == "darwin" {
		return openapi.Macos
	}
	return openapi.Ubuntu
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func launchDaemonProcess(ctx context.Context, paths config.HostPaths) error {
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "termixd")
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func attachTmuxSession(ctx context.Context, sessionName string) error {
	cmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", sessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type daemonClient interface {
	Health(ctx context.Context, in *daemonv1.HealthRequest, opts ...grpc.CallOption) (*daemonv1.HealthResponse, error)
	StartSession(ctx context.Context, in *daemonv1.StartSessionRequest, opts ...grpc.CallOption) (*daemonv1.StartSessionResponse, error)
	AttachInfo(ctx context.Context, in *daemonv1.AttachInfoRequest, opts ...grpc.CallOption) (*daemonv1.AttachInfoResponse, error)
	Doctor(ctx context.Context, in *daemonv1.DoctorRequest, opts ...grpc.CallOption) (*daemonv1.DoctorResponse, error)
}
