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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/buildinfo"
	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/daemonipc"
	"github.com/termix/termix/go/internal/hostdaemon"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
)

var version = "dev"

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
	runDaemon        func(ctx context.Context, paths config.HostPaths, version string) error
	attachTmux       func(ctx context.Context, sessionName string) error
	sleep            func(time.Duration)
	socketExists     func(path string) bool
	// hostWinsize returns the cols/rows of the terminal `termix start` is
	// running in so the daemon can size the new tmux pane to match. Returns
	// (0, 0) when the CLI was not invoked from a tty (piped, daemonized,
	// etc.); in that case the daemon falls back to its 120×40 default.
	hostWinsize func() (cols, rows int)
}

func main() {
	os.Exit(run(context.Background(), os.Args, defaultDeps()))
}

func run(ctx context.Context, args []string, deps cliDeps) int {
	if len(args) < 2 {
		fmt.Fprintln(deps.stderr, "usage: termix <login|start|sessions|status|doctor|version>")
		fmt.Fprintln(deps.stderr, "       termix sessions <attach|list|shutdown> ...")
		return 2
	}

	var err error
	switch args[1] {
	case "--version", "version":
		err = runVersion(deps)
	case "__daemon":
		err = runDaemon(ctx, deps)
	case "login":
		err = runLogin(ctx, deps)
	case "start":
		err = runStart(ctx, args[2:], deps)
	case "sessions":
		err = runSessions(ctx, args[2:], deps)
	case "doctor":
		err = runDoctor(ctx, deps)
	case "status":
		err = runStatus(ctx, deps)
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
		runDaemon:    hostdaemon.Run,
		attachTmux:   attachTmuxSession,
		sleep:        time.Sleep,
		socketExists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		hostWinsize: hostStdoutWinsize,
	}
}

func runVersion(deps cliDeps) error {
	fmt.Fprintf(deps.stdout, "termix %s\n", version)
	return nil
}

func runDaemon(ctx context.Context, deps cliDeps) error {
	if deps.runDaemon == nil {
		return errors.New("daemon is not available")
	}
	return deps.runDaemon(ctx, deps.paths, version)
}

func runLogin(ctx context.Context, deps cliDeps) error {
	// Share one bufio.Reader across the three prompts. A fresh reader per
	// prompt would buffer the entire piped stdin on the first read and lose
	// everything past the first newline when it went out of scope.
	reader := bufio.NewReader(deps.stdin)
	serverURL, err := readLineWithDefault(reader, deps.stdout, "Server URL", "https://termix.cloud")
	if err != nil {
		return err
	}
	email, err := readLine(reader, deps.stdout, "Email: ")
	if err != nil {
		return err
	}
	password, err := readLine(reader, deps.stdout, "Password: ")
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
		DeviceType:  openapi.LoginRequestDeviceType("host"),
		Platform:    hostPlatform(runtime.GOOS),
		DeviceLabel: host,
	})
	if err != nil {
		return err
	}

	hostConfig, err := config.DeriveHostConfig(serverURL)
	if err != nil {
		return err
	}
	if err := config.SaveHostConfig(deps.paths.HostConfigFile, hostConfig); err != nil {
		return err
	}

	refreshToken := ""
	if resp.RefreshToken != nil {
		refreshToken = *resp.RefreshToken
	}
	return credentials.Save(deps.paths.CredentialsFile, credentials.StoredCredentials{
		ServerBaseURL: serverURL,
		UserID:        resp.User.Id.String(),
		DeviceID:      resp.Device.Id.String(),
		AccessToken:   resp.AccessToken,
		RefreshToken:  refreshToken,
		ExpiresAt:     deps.now().Add(time.Duration(resp.ExpiresInSeconds) * time.Second).UTC().Format(time.RFC3339),
	})
}

func runStart(ctx context.Context, args []string, deps cliDeps) error {
	tool, name, err := parseStartArgs(args)
	if err != nil {
		return err
	}
	if !isSupportedTool(tool) {
		return fmt.Errorf("unsupported tool %q; expected claude, codex, or opencode", tool)
	}
	if err := ensureLoggedIn(deps.paths); err != nil {
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

	cols, rows := 0, 0
	if deps.hostWinsize != nil {
		cols, rows = deps.hostWinsize()
	}
	resp, err := client.StartSession(ctx, &daemonv1.StartSessionRequest{
		Tool:     tool,
		Name:     name,
		Cwd:      cwd,
		Shell:    deps.getenv("SHELL"),
		Term:     deps.getenv("TERM"),
		Language: firstNonEmpty(deps.getenv("LC_ALL"), deps.getenv("LANG")),
		Env:      captureEnv(deps.environ()),
		Cols:     int32(cols),
		Rows:     int32(rows),
	})
	if err != nil {
		return err
	}

	if err := deps.attachTmux(ctx, resp.TmuxSessionName); err != nil {
		return fmt.Errorf("attach failed, run manually: %s", firstNonEmpty(resp.AttachCommand, "tmux attach-session -t "+resp.TmuxSessionName))
	}
	return nil
}

func ensureLoggedIn(paths config.HostPaths) error {
	if _, err := credentials.Load(paths.CredentialsFile); err != nil {
		if os.IsNotExist(err) {
			return errors.New("Not logged in. Run: termix login")
		}
		return fmt.Errorf("load credentials: %w", err)
	}
	if _, err := config.LoadHostConfig(paths.HostConfigFile); err != nil {
		if os.IsNotExist(err) {
			return errors.New("Not logged in. Run: termix login")
		}
		return fmt.Errorf("load host config: %w", err)
	}
	return nil
}

func runSessions(ctx context.Context, args []string, deps cliDeps) error {
	if len(args) == 0 {
		return errors.New("usage: termix sessions <attach|list|shutdown> ...")
	}
	switch args[0] {
	case "attach":
		return runSessionsAttach(ctx, args[1:], deps)
	case "list":
		return runSessionsList(ctx, args[1:], deps)
	case "shutdown":
		return runSessionsShutdown(ctx, args[1:], deps)
	default:
		return fmt.Errorf("unknown sessions subcommand %q (expected attach|list|shutdown)", args[0])
	}
}

func runSessionsAttach(ctx context.Context, args []string, deps cliDeps) error {
	if len(args) != 1 {
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

	resp, err := client.AttachInfo(ctx, &daemonv1.AttachInfoRequest{SessionId: args[0]})
	if err != nil {
		return err
	}
	return deps.attachTmux(ctx, resp.TmuxSessionName)
}

// runSessionsList prints one row per local session as a TSV-like table:
//
//	SESSION_ID  TOOL  NAME  PID  STATUS  TMUX  STARTED  CWD
//
// SESSION_ID is shown unabbreviated so it can be copied straight into
// `termix sessions shutdown`. STATUS is `live` when tmux still has the
// session, `orphan` otherwise (those rows are scheduled for the next
// reaper sweep, ≤30s away). PID is 0 for orphans.
func runSessionsList(ctx context.Context, args []string, deps cliDeps) error {
	if len(args) != 0 {
		return errors.New("usage: termix sessions list")
	}
	if err := ensureDaemon(ctx, deps); err != nil {
		return err
	}
	client, conn, err := deps.dialDaemon(ctx, daemonipc.SocketPath(deps.paths))
	if err != nil {
		return err
	}
	defer conn.Close()

	resp, err := client.ListSessions(ctx, &daemonv1.ListSessionsRequest{})
	if err != nil {
		return err
	}
	if len(resp.GetSessions()) == 0 {
		fmt.Fprintln(deps.stdout, "No sessions on this host.")
		return nil
	}
	fmt.Fprintln(deps.stdout, "SESSION_ID\tTOOL\tNAME\tPID\tSTATUS\tTMUX\tSTARTED\tCWD")
	for _, s := range resp.GetSessions() {
		state := "orphan"
		if s.GetLiveInTmux() {
			state = "live"
		}
		name := s.GetName()
		if name == "" {
			name = "-"
		}
		started := s.GetStartedAt()
		if started == "" {
			started = "-"
		}
		cwd := s.GetCwd()
		if cwd == "" {
			cwd = "-"
		}
		fmt.Fprintf(deps.stdout, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			s.GetSessionId(), s.GetTool(), name, s.GetPanePid(), state,
			s.GetTmuxSessionName(), started, cwd)
	}
	return nil
}

// runSessionsShutdown kills tmux sessions at the source and synchronously
// PATCHes them to status=exited in the control DB. Accepts one or more
// session IDs, or `--all` to target every local session. Per-id outcome
// is printed as `OK <id>` / `FAIL <id>: <err>`; the command exits non-zero
// if any shutdown failed.
func runSessionsShutdown(ctx context.Context, args []string, deps cliDeps) error {
	if len(args) == 0 {
		return errors.New("usage: termix sessions shutdown <session_id> [<session_id>...] | --all")
	}
	all := false
	ids := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--all" {
			all = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag %q (expected --all or session ids)", a)
		}
		ids = append(ids, a)
	}
	if all && len(ids) > 0 {
		return errors.New("--all cannot be combined with explicit session ids")
	}

	if err := ensureDaemon(ctx, deps); err != nil {
		return err
	}
	client, conn, err := deps.dialDaemon(ctx, daemonipc.SocketPath(deps.paths))
	if err != nil {
		return err
	}
	defer conn.Close()

	if all {
		resp, err := client.ListSessions(ctx, &daemonv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(resp.GetSessions()) == 0 {
			fmt.Fprintln(deps.stdout, "No sessions on this host.")
			return nil
		}
		for _, s := range resp.GetSessions() {
			ids = append(ids, s.GetSessionId())
		}
	}

	failed := 0
	for _, id := range ids {
		if _, err := client.EndSession(ctx, &daemonv1.EndSessionRequest{SessionId: id}); err != nil {
			fmt.Fprintf(deps.stdout, "FAIL %s: %v\n", id, err)
			failed++
			continue
		}
		fmt.Fprintf(deps.stdout, "OK %s\n", id)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d session(s) failed to shut down", failed, len(ids))
	}
	return nil
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
	local := buildinfo.Current(version)

	// Existing daemon present + matching identity -> reuse.
	// Existing daemon present + identity mismatch -> tear it down, fall
	// through to launch. No daemon (or unhealthy) -> fall through to launch.
	if client, conn, err := deps.dialDaemon(ctx, socketPath); err == nil {
		resp, healthErr := client.Health(ctx, &daemonv1.HealthRequest{})
		if healthErr == nil {
			remote := buildinfo.Identity{
				Version:  resp.GetVersion(),
				Revision: resp.GetRevision(),
				Modified: resp.GetModified(),
			}
			if local.Matches(remote) {
				_ = conn.Close()
				return nil
			}
			fmt.Fprintf(deps.stderr, "termix: daemon version mismatch (%s -> %s), restarting...\n", remote, local)
			shutdownCtx, cancelShutdown := context.WithTimeout(ctx, time.Second)
			_, _ = client.Shutdown(shutdownCtx, &daemonv1.ShutdownRequest{})
			cancelShutdown()
		}
		_ = conn.Close()

		// Wait up to 2s for the socket file to disappear; force-remove if not.
		waitForSocketGone(deps, socketPath, 2*time.Second)
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
				remote := buildinfo.Identity{
					Version:  healthResp.GetVersion(),
					Revision: healthResp.GetRevision(),
					Modified: healthResp.GetModified(),
				}
				if !local.Matches(remote) {
					return fmt.Errorf("daemon spawned with mismatched identity (%s vs %s)", remote, local)
				}
				return nil
			}
		}
		if deps.sleep != nil {
			deps.sleep(100 * time.Millisecond)
		}
	}
	return errors.New("daemon did not become healthy")
}

func waitForSocketGone(deps cliDeps, socketPath string, timeout time.Duration) {
	if deps.socketExists == nil {
		return
	}
	deadline := timeout
	step := 50 * time.Millisecond
	for elapsed := time.Duration(0); elapsed < deadline; elapsed += step {
		if !deps.socketExists(socketPath) {
			return
		}
		if deps.sleep != nil {
			deps.sleep(step)
		}
	}
	// Force-remove as last resort. Ignore the error: if Remove fails the
	// socket may still belong to a live daemon; the launch path that
	// follows will see EADDRINUSE and surface a real error.
	_ = os.Remove(socketPath)
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

func isSupportedTool(tool string) bool {
	switch tool {
	case "claude", "codex", "opencode":
		return true
	default:
		return false
	}
}

func readLine(reader *bufio.Reader, output io.Writer, prompt string) (string, error) {
	if output != nil {
		fmt.Fprint(output, prompt)
	}

	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func readLineWithDefault(reader *bufio.Reader, output io.Writer, prompt, defaultValue string) (string, error) {
	value, err := readLine(reader, output, fmt.Sprintf("%s [%s]: ", prompt, defaultValue))
	if err != nil {
		return "", err
	}
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
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
		return openapi.LoginRequestPlatform("macos")
	}
	return openapi.LoginRequestPlatform("ubuntu")
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

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := daemonCommand(ctx, executable)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr

	// Route the daemon's stderr to a log file so the daemon does not depend on
	// the launching terminal's stderr (otherwise reaper output leaks into the
	// user's interactive shell, and the daemon dies when the terminal closes).
	// Falls back to the launching terminal's stderr if the log file cannot be
	// opened.
	if err := os.MkdirAll(paths.LogDir, 0o700); err == nil {
		logPath := filepath.Join(paths.LogDir, "termixd.log")
		if logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
			defer logFile.Close() // child has dup'd the fd before Start returns.
		}
	}

	// Detach from the terminal session so SIGHUP on terminal close does not
	// kill the daemon. cmd.Process.Release below already gives up the parent's
	// wait handle; Setsid completes the daemon decoupling.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func daemonCommand(ctx context.Context, executable string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, "__daemon")
}

func attachTmuxSession(ctx context.Context, sessionName string) error {
	cmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", sessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// hostStdoutWinsize returns the cols/rows of the terminal `termix start` is
// running in, queried via TIOCGWINSZ on stdout's fd. Returns (0, 0) when
// stdout is not a tty so the daemon falls back to its 120×40 default. We
// query stdout (not stdin) because the user's `termix start` is most often
// invoked with `< /dev/null` or with stdin redirected for non-interactive
// flows but stdout still attached to the terminal that will host the
// `tmux attach` after StartSession returns.
func hostStdoutWinsize() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil {
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}

type daemonClient interface {
	Health(ctx context.Context, in *daemonv1.HealthRequest, opts ...grpc.CallOption) (*daemonv1.HealthResponse, error)
	StartSession(ctx context.Context, in *daemonv1.StartSessionRequest, opts ...grpc.CallOption) (*daemonv1.StartSessionResponse, error)
	AttachInfo(ctx context.Context, in *daemonv1.AttachInfoRequest, opts ...grpc.CallOption) (*daemonv1.AttachInfoResponse, error)
	Doctor(ctx context.Context, in *daemonv1.DoctorRequest, opts ...grpc.CallOption) (*daemonv1.DoctorResponse, error)
	Status(ctx context.Context, in *daemonv1.StatusRequest, opts ...grpc.CallOption) (*daemonv1.StatusResponse, error)
}

// runStatus prints a section-block summary covering logged-in user,
// daemon health, relay connection state, active sessions, and proxy
// policy. The output mirrors the spec's worked example so it stays
// stable as a paste-into-a-bug-report artifact.
func runStatus(ctx context.Context, deps cliDeps) error {
	creds, _ := credentials.Load(deps.paths.CredentialsFile)

	if err := ensureDaemon(ctx, deps); err != nil {
		return err
	}
	client, conn, err := deps.dialDaemon(ctx, daemonipc.SocketPath(deps.paths))
	if err != nil {
		return err
	}
	defer conn.Close()

	resp, err := client.Status(ctx, &daemonv1.StatusRequest{})
	if err != nil {
		return err
	}

	w := deps.stdout

	fmt.Fprintln(w, "USER")
	if creds.UserID != "" {
		// Email isn't stored in StoredCredentials; surface the user id
		// instead. Future work: persist email at login.
		fmt.Fprintf(w, "  user_id %s\n", creds.UserID)
	} else {
		fmt.Fprintln(w, "  (not logged in)")
	}
	if creds.ServerBaseURL != "" {
		fmt.Fprintf(w, "  %s\n", creds.ServerBaseURL)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "DAEMON")
	identity := fmt.Sprintf("%s (rev %s)", resp.GetVersion(), resp.GetRevision())
	if resp.GetModified() {
		identity += " dirty"
	}
	uptime := time.Duration(resp.GetUptimeSeconds()) * time.Second
	fmt.Fprintf(w, "  up %s, version %s\n", uptime, identity)
	fmt.Fprintf(w, "  socket %s\n", daemonipc.SocketPath(deps.paths))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "RELAY")
	rs := resp.GetRelay()
	switch rs.GetPhase() {
	case "connected":
		fmt.Fprintf(w, "  connected (since %s UTC)\n", time.Unix(rs.GetLastConnectedAt(), 0).UTC().Format(time.RFC3339))
		if rs.GetAttempt() > 0 {
			fmt.Fprintf(w, "  reconnects this session: %d\n", rs.GetAttempt())
		}
	case "reconnecting":
		nextRetry := time.Unix(rs.GetNextRetryAt(), 0).UTC()
		lastConnected := "never"
		if rs.GetLastConnectedAt() > 0 {
			lastConnected = time.Unix(rs.GetLastConnectedAt(), 0).UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(w, "  reconnecting (attempt %d, next try at %s, last connected %s)\n",
			rs.GetAttempt(), nextRetry.Format(time.RFC3339), lastConnected)
		if rs.GetLastError() != "" {
			fmt.Fprintf(w, "  last error: %s\n", rs.GetLastError())
		}
	case "closed":
		fmt.Fprintf(w, "  closed: %s\n", rs.GetLastError())
	default:
		fmt.Fprintf(w, "  %s\n", rs.GetPhase())
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "SESSIONS  (%d active)\n", len(resp.GetSessions()))
	for _, s := range resp.GetSessions() {
		state := "orphan"
		if s.GetLiveInTmux() {
			state = "live"
		}
		name := s.GetName()
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(w, "  %s  %s  %s  %s  pid %d\n",
			s.GetSessionId(), s.GetTool(), name, state, s.GetPanePid())
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "TMUX")
	tmuxInfo := resp.GetTmux()
	if !tmuxInfo.GetInstalled() {
		fmt.Fprintln(w, "  not installed (required: install tmux >= 3.2)")
	} else {
		version := tmuxInfo.GetVersion()
		if version == "" {
			version = "unknown"
		}
		fmt.Fprintf(w, "  version %s\n", version)
		if path := tmuxInfo.GetPath(); path != "" {
			fmt.Fprintf(w, "  binary  %s\n", path)
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "PROXY")
	fmt.Fprintln(w, "  relay WSS: direct (always bypasses any HTTP proxy)")
	envVals := []string{}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		if v := deps.getenv(name); v != "" {
			envVals = append(envVals, name+"="+v)
		} else if v := deps.getenv(strings.ToLower(name)); v != "" {
			envVals = append(envVals, strings.ToLower(name)+"="+v)
		}
	}
	if len(envVals) == 0 {
		fmt.Fprintln(w, "  shell env: HTTP_PROXY / HTTPS_PROXY / ALL_PROXY / NO_PROXY all unset")
	} else {
		fmt.Fprintf(w, "  shell env: %s\n", strings.Join(envVals, ", "))
	}
	return nil
}
