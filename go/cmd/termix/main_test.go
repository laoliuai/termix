package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/buildinfo"
	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/proxyenv"
	"google.golang.org/grpc"
)

// TestMain enforces the default proxy policy (enable_proxy=false) for the
// entire test binary so `proxyenv.Fingerprint()` returns a deterministic
// "all-empty" digest regardless of what proxy env vars the dev's shell
// happened to export. Without this, every test that goes through
// `ensureDaemon` would fail when run on a machine with `http_proxy` set,
// because the fake daemon's HealthResponse leaves ProxyFingerprint at the
// zero value while the CLI's local fingerprint would be the hash of the
// dev's actual proxy env.
func TestMain(m *testing.M) {
	proxyenv.Apply(false)
	os.Exit(m.Run())
}

func TestRunLoginStoresCredentials(t *testing.T) {
	paths := testPaths(t)
	refreshToken := "refresh-token"
	control := &fakeLoginClient{
		response: &openapi.LoginResponse{
			AccessToken:      "access-token",
			RefreshToken:     &refreshToken,
			ExpiresInSeconds: 900,
			User: openapi.User{
				Id:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				Email:       "user@example.com",
				DisplayName: "User",
				Role:        openapi.UserRoleUser,
			},
			Device: openapi.Device{
				Id:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				DeviceType: openapi.DeviceDeviceTypeHost,
				Platform:   openapi.DevicePlatformUbuntu,
				Label:      "devbox",
			},
		},
	}

	deps := testDeps(paths)
	deps.stdin = strings.NewReader("https://termix.example.com\nuser@example.com\nsecret\n")
	deps.newControlClient = func(baseURL string) (loginClient, error) {
		if baseURL != "https://termix.example.com" {
			t.Fatalf("unexpected base url %q", baseURL)
		}
		return control, nil
	}
	deps.now = func() time.Time {
		return time.Date(2026, 4, 23, 14, 0, 0, 0, time.UTC)
	}
	deps.hostname = func() (string, error) {
		return "devbox", nil
	}

	code := run(context.Background(), []string{"termix", "login"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	creds, err := credentials.Load(paths.CredentialsFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if creds.AccessToken != "access-token" {
		t.Fatalf("expected access token to be saved, got %q", creds.AccessToken)
	}
	if control.request.DeviceLabel != "devbox" {
		t.Fatalf("expected hostname to be used as device label, got %q", control.request.DeviceLabel)
	}
}

func TestRunLoginStoresHostConfig(t *testing.T) {
	paths := testPaths(t)
	deps := testDeps(paths)
	deps.stdin = strings.NewReader("https://termix.example.com\nuser@example.com\nsecret\n")
	deps.hostname = func() (string, error) {
		return "devbox", nil
	}
	refreshToken := "refresh-token"
	deps.newControlClient = func(string) (loginClient, error) {
		return &fakeLoginClient{
			response: &openapi.LoginResponse{
				AccessToken:      "access-token",
				RefreshToken:     &refreshToken,
				ExpiresInSeconds: 900,
				User: openapi.User{
					Id: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				},
				Device: openapi.Device{
					Id: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				},
			},
		}, nil
	}

	if code := run(context.Background(), []string{"termix", "login"}, deps); code != 0 {
		t.Fatalf("expected login success, got exit code %d", code)
	}

	cfg, err := config.LoadHostConfig(paths.HostConfigFile)
	if err != nil {
		t.Fatalf("LoadHostConfig returned error: %v", err)
	}
	if cfg.RelayWSURL != "wss://termix.example.com/ws" {
		t.Fatalf("expected derived relay url, got %q", cfg.RelayWSURL)
	}
}

func TestRunVersionPrintsVersion(t *testing.T) {
	oldVersion := version
	version = "1.2.3-test"
	t.Cleanup(func() {
		version = oldVersion
	})

	for _, args := range [][]string{
		{"termix", "--version"},
		{"termix", "version"},
	} {
		t.Run(strings.Join(args[1:], "_"), func(t *testing.T) {
			deps := testDeps(testPaths(t))
			var stdout bytes.Buffer
			deps.stdout = &stdout

			code := run(context.Background(), args, deps)
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
			if stdout.String() != "termix 1.2.3-test\n" {
				t.Fatalf("unexpected version output %q", stdout.String())
			}
		})
	}
}

func TestRunUsageListsUserFacingCommandsOnly(t *testing.T) {
	deps := testDeps(testPaths(t))
	var stderr bytes.Buffer
	deps.stderr = &stderr

	code := run(context.Background(), []string{"termix"}, deps)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	output := stderr.String()
	if strings.Contains(output, "__daemon") {
		t.Fatalf("usage should not expose hidden daemon command, got %q", output)
	}
	if !strings.Contains(output, "version") {
		t.Fatalf("usage should include version command, got %q", output)
	}
}

func TestRunHiddenDaemonUsesInternalRunner(t *testing.T) {
	paths := testPaths(t)
	deps := testDeps(paths)
	called := false
	deps.runDaemon = func(_ context.Context, gotPaths config.HostPaths, _ string) error {
		called = true
		if gotPaths != paths {
			t.Fatalf("expected paths %#v, got %#v", paths, gotPaths)
		}
		return nil
	}

	code := run(context.Background(), []string{"termix", "__daemon"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !called {
		t.Fatal("expected hidden daemon command to invoke runDaemon")
	}
}

func TestDaemonCommandUsesCurrentExecutableHiddenMode(t *testing.T) {
	cmd := daemonCommand(context.Background(), "/tmp/bin/termix")
	if cmd.Path != "/tmp/bin/termix" {
		t.Fatalf("expected command path /tmp/bin/termix, got %q", cmd.Path)
	}
	if got := strings.Join(cmd.Args, " "); got != "/tmp/bin/termix __daemon" {
		t.Fatalf("unexpected command args %q", got)
	}
}

func TestRunStartLaunchesDaemonAndAttachesSession(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		startResponse: &daemonv1.StartSessionResponse{
			SessionId:       "33333333-3333-3333-3333-333333333333",
			TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
			AttachCommand:   "tmux attach-session -t termix_33333333-3333-3333-3333-333333333333",
			Status:          "running",
		},
	}

	deps := testDeps(paths)
	deps.getenv = func(key string) string {
		switch key {
		case "SHELL":
			return "/bin/bash"
		case "TERM":
			return "xterm-256color"
		case "LANG":
			return "en_US.UTF-8"
		default:
			return ""
		}
	}
	deps.environ = func() []string {
		return []string{
			"SHELL=/bin/bash",
			"TERM=xterm-256color",
			"LANG=en_US.UTF-8",
			"FOO=bar",
		}
	}
	deps.getwd = func() (string, error) {
		return "/tmp/project", nil
	}
	dialCount := 0
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		dialCount++
		if dialCount == 1 {
			return nil, nil, errors.New("dial failed")
		}
		return client, nopCloser{}, nil
	}
	launched := false
	deps.launchDaemon = func(context.Context, config.HostPaths) error {
		launched = true
		return nil
	}
	attachedTo := ""
	deps.attachTmux = func(_ context.Context, sessionName string) error {
		attachedTo = sessionName
		return nil
	}

	code := run(context.Background(), []string{"termix", "start", "codex", "--name", "fix auth"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !launched {
		t.Fatal("expected daemon launch after failed dial")
	}
	if client.startRequest.Tool != "codex" {
		t.Fatalf("expected tool codex, got %q", client.startRequest.Tool)
	}
	if client.startRequest.Name != "fix auth" {
		t.Fatalf("expected session name fix auth, got %q", client.startRequest.Name)
	}
	if attachedTo != "termix_33333333-3333-3333-3333-333333333333" {
		t.Fatalf("unexpected attached session %q", attachedTo)
	}
}

// TestRunStartForwardsHostWinsizeIntoStartSessionRequest verifies the
// CLI plumbs the host terminal's (cols, rows) into the StartSession RPC
// so the daemon can size the new tmux pane to match. Uses the injected
// hostWinsize hook because real tty fds are not available in tests.
func TestRunStartForwardsHostWinsizeIntoStartSessionRequest(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		startResponse: &daemonv1.StartSessionResponse{
			SessionId:       "33333333-3333-3333-3333-333333333333",
			TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
		},
	}
	deps := testDeps(paths)
	deps.hostWinsize = func() (int, int) { return 184, 50 }
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}
	deps.attachTmux = func(context.Context, string) error { return nil }

	code := run(context.Background(), []string{"termix", "start", "codex"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if client.startRequest == nil {
		t.Fatal("expected StartSession to be called")
	}
	if client.startRequest.GetCols() != 184 || client.startRequest.GetRows() != 50 {
		t.Fatalf("expected cols=184 rows=50 forwarded, got cols=%d rows=%d",
			client.startRequest.GetCols(), client.startRequest.GetRows())
	}
}

// TestRunStartLeavesWinsizeZeroWhenHostIsNotATty verifies the
// non-tty fallback: when hostWinsize reports (0, 0) — `termix start`
// invoked from a piped stdout, daemonized launcher, etc. — the request
// carries zeros so the daemon falls back to its 120×40 default.
func TestRunStartLeavesWinsizeZeroWhenHostIsNotATty(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		startResponse: &daemonv1.StartSessionResponse{
			SessionId:       "44444444-4444-4444-4444-444444444444",
			TmuxSessionName: "termix_44444444-4444-4444-4444-444444444444",
		},
	}
	deps := testDeps(paths)
	deps.hostWinsize = func() (int, int) { return 0, 0 }
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}
	deps.attachTmux = func(context.Context, string) error { return nil }

	code := run(context.Background(), []string{"termix", "start", "codex"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if client.startRequest.GetCols() != 0 || client.startRequest.GetRows() != 0 {
		t.Fatalf("expected zero cols/rows for non-tty host, got cols=%d rows=%d",
			client.startRequest.GetCols(), client.startRequest.GetRows())
	}
}

func TestRunStartRequiresLoginBeforeLaunchingDaemon(t *testing.T) {
	paths := testPaths(t)
	deps := testDeps(paths)
	var stderr bytes.Buffer
	deps.stderr = &stderr
	launched := false
	deps.launchDaemon = func(context.Context, config.HostPaths) error {
		launched = true
		return nil
	}

	code := run(context.Background(), []string{"termix", "start", "codex"}, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stderr.String() != "Not logged in. Run: termix login\n" {
		t.Fatalf("unexpected stderr %q", stderr.String())
	}
	if launched {
		t.Fatal("expected start to reject missing login before launching daemon")
	}
}

func TestRunStartRejectsUnsupportedTool(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	deps := testDeps(paths)
	var stderr bytes.Buffer
	deps.stderr = &stderr
	launched := false
	deps.launchDaemon = func(context.Context, config.HostPaths) error {
		launched = true
		return nil
	}

	code := run(context.Background(), []string{"termix", "start", "vim"}, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stderr.String() != "unsupported tool \"vim\"; expected claude, codex, or opencode\n" {
		t.Fatalf("unexpected stderr %q", stderr.String())
	}
	if launched {
		t.Fatal("expected unsupported tool to be rejected before launching daemon")
	}
}

func TestRunStartReportsMalformedCredentialsInsteadOfLoginHint(t *testing.T) {
	paths := testPaths(t)
	mustWriteFile(t, paths.CredentialsFile, "{")
	if err := config.SaveHostConfig(paths.HostConfigFile, validHostConfig()); err != nil {
		t.Fatalf("SaveHostConfig returned error: %v", err)
	}
	deps := testDeps(paths)
	var stderr bytes.Buffer
	deps.stderr = &stderr

	code := run(context.Background(), []string{"termix", "start", "codex"}, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if strings.Contains(stderr.String(), "Not logged in. Run: termix login") {
		t.Fatalf("expected malformed credentials error, got login hint %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "load credentials:") {
		t.Fatalf("expected credentials context in error, got %q", stderr.String())
	}
}

func TestRunStartReportsMalformedOrInvalidHostConfigInsteadOfLoginHint(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "malformed_json", content: "{"},
		{name: "invalid_config", content: `{"server_base_url":"https://termix.example.com"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := testPaths(t)
			if err := credentials.Save(paths.CredentialsFile, validCredentials()); err != nil {
				t.Fatalf("Save credentials returned error: %v", err)
			}
			mustWriteFile(t, paths.HostConfigFile, tc.content)
			deps := testDeps(paths)
			var stderr bytes.Buffer
			deps.stderr = &stderr

			code := run(context.Background(), []string{"termix", "start", "codex"}, deps)
			if code != 1 {
				t.Fatalf("expected exit code 1, got %d", code)
			}
			if strings.Contains(stderr.String(), "Not logged in. Run: termix login") {
				t.Fatalf("expected host config error, got login hint %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), "load host config:") {
				t.Fatalf("expected host config context in error, got %q", stderr.String())
			}
		})
	}
}

func TestRunSessionsAttachUsesDaemonAttachInfo(t *testing.T) {
	deps := testDeps(testPaths(t))
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		attachResponse: &daemonv1.AttachInfoResponse{
			TmuxSessionName: "termix_custom",
			AttachCommand:   "tmux attach-session -t termix_custom",
		},
	}
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}
	attachedTo := ""
	deps.attachTmux = func(_ context.Context, sessionName string) error {
		attachedTo = sessionName
		return nil
	}

	code := run(context.Background(), []string{"termix", "sessions", "attach", "session-1"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if client.attachRequest.GetSessionId() != "session-1" {
		t.Fatalf("expected attach request for session-1, got %q", client.attachRequest.GetSessionId())
	}
	if attachedTo != "termix_custom" {
		t.Fatalf("expected attach to termix_custom, got %q", attachedTo)
	}
}

func TestRunSessionsListPrintsTSVTableWithLiveAndOrphanRows(t *testing.T) {
	deps := testDeps(testPaths(t))
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		listResponse: &daemonv1.ListSessionsResponse{
			Sessions: []*daemonv1.SessionSummary{
				{
					SessionId:       "11111111-1111-1111-1111-111111111111",
					Tool:            "claude",
					Name:            "fix auth",
					Status:          "running",
					TmuxSessionName: "termix_a",
					Cwd:             "/tmp/proj",
					StartedAt:       "2026-05-07T10:00:00Z",
					PanePid:         4242,
					LiveInTmux:      true,
				},
				{
					SessionId:       "22222222-2222-2222-2222-222222222222",
					Tool:            "codex",
					Status:          "running",
					TmuxSessionName: "termix_b",
				},
			},
		},
	}
	var stdout bytes.Buffer
	deps.stdout = &stdout
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "sessions", "list"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "SESSION_ID\tTOOL\tNAME\tPID\tSTATUS\tTMUX\tSTARTED\tCWD") {
		t.Fatalf("expected header row, got %q", output)
	}
	// Live row carries the populated fields.
	if !strings.Contains(output,
		"11111111-1111-1111-1111-111111111111\tclaude\tfix auth\t4242\tlive\ttermix_a\t2026-05-07T10:00:00Z\t/tmp/proj") {
		t.Fatalf("expected live row, got %q", output)
	}
	// Orphan row falls back to "-" for empty fields and shows status=orphan.
	if !strings.Contains(output,
		"22222222-2222-2222-2222-222222222222\tcodex\t-\t0\torphan\ttermix_b\t-\t-") {
		t.Fatalf("expected orphan row, got %q", output)
	}
}

func TestRunSessionsListPrintsEmptyStateWhenDaemonHasNoSessions(t *testing.T) {
	deps := testDeps(testPaths(t))
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
	}
	var stdout bytes.Buffer
	deps.stdout = &stdout
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "sessions", "list"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "No sessions on this host.") {
		t.Fatalf("expected empty-state line, got %q", stdout.String())
	}
}

func TestRunSessionsShutdownCallsEndSessionForEachID(t *testing.T) {
	deps := testDeps(testPaths(t))
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
	}
	var stdout bytes.Buffer
	deps.stdout = &stdout
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(),
		[]string{"termix", "sessions", "shutdown",
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stdout=%q)", code, stdout.String())
	}
	if len(client.endRequests) != 2 {
		t.Fatalf("expected 2 EndSession calls, got %d", len(client.endRequests))
	}
	if client.endRequests[0].GetSessionId() != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("expected first id sent first, got %q", client.endRequests[0].GetSessionId())
	}
	if !strings.Contains(stdout.String(), "OK 11111111-1111-1111-1111-111111111111") ||
		!strings.Contains(stdout.String(), "OK 22222222-2222-2222-2222-222222222222") {
		t.Fatalf("expected OK lines for both ids, got %q", stdout.String())
	}
}

func TestRunSessionsShutdownReportsPerIDFailuresAndExitsNonZero(t *testing.T) {
	deps := testDeps(testPaths(t))
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		endErrByID: map[string]error{
			"22222222-2222-2222-2222-222222222222": errors.New("end-session: tmux down"),
		},
	}
	var stdout, stderr bytes.Buffer
	deps.stdout = &stdout
	deps.stderr = &stderr
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(),
		[]string{"termix", "sessions", "shutdown",
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222"}, deps)
	if code == 0 {
		t.Fatalf("expected non-zero exit when one id fails, stdout=%q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "OK 11111111-1111-1111-1111-111111111111") {
		t.Fatalf("expected OK for first id, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "FAIL 22222222-2222-2222-2222-222222222222: end-session: tmux down") {
		t.Fatalf("expected FAIL line for second id with error detail, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "1 of 2 session(s) failed") {
		t.Fatalf("expected summary error on stderr, got %q", stderr.String())
	}
}

func TestRunSessionsShutdownAllExpandsToEverySessionFromList(t *testing.T) {
	deps := testDeps(testPaths(t))
	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		listResponse: &daemonv1.ListSessionsResponse{
			Sessions: []*daemonv1.SessionSummary{
				{SessionId: "aaaaaaaa-1111-1111-1111-111111111111"},
				{SessionId: "bbbbbbbb-2222-2222-2222-222222222222"},
			},
		},
	}
	var stdout bytes.Buffer
	deps.stdout = &stdout
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "sessions", "shutdown", "--all"}, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout=%q)", code, stdout.String())
	}
	if len(client.endRequests) != 2 {
		t.Fatalf("expected 2 EndSession calls, got %d", len(client.endRequests))
	}
}

func TestRunSessionsShutdownRejectsAllCombinedWithExplicitIDs(t *testing.T) {
	deps := testDeps(testPaths(t))
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		t.Fatal("daemon must not be dialed when args are invalid")
		return nil, nil, nil
	}
	var stderr bytes.Buffer
	deps.stderr = &stderr

	code := run(context.Background(),
		[]string{"termix", "sessions", "shutdown", "--all", "deadbeef-dead-dead-dead-deadbeefdead"}, deps)
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid combination")
	}
	if !strings.Contains(stderr.String(), "--all cannot be combined with explicit session ids") {
		t.Fatalf("expected explanatory error, got %q", stderr.String())
	}
}

func TestRunDoctorPrintsChecks(t *testing.T) {
	paths := testPaths(t)
	client := &fakeDaemonClient{
		doctorResponse: &daemonv1.DoctorResponse{
			Checks: []string{"tmux: ok", "credentials: ok"},
		},
	}

	deps := testDeps(paths)
	var stdout bytes.Buffer
	deps.stdout = &stdout
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "doctor"}, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "tmux: ok") {
		t.Fatalf("expected tmux check in output, got %q", stdout.String())
	}
}

func testDeps(paths config.HostPaths) cliDeps {
	return cliDeps{
		stdin:  strings.NewReader(""),
		stdout: io.Discard,
		stderr: io.Discard,
		now:    time.Now,
		getwd: func() (string, error) {
			return "/tmp/project", nil
		},
		hostname: func() (string, error) {
			return "devbox", nil
		},
		getenv:  func(string) string { return "" },
		environ: func() []string { return nil },
		paths:   paths,
	}
}

func testPaths(t *testing.T) config.HostPaths {
	t.Helper()
	base := t.TempDir()
	return config.HostPaths{
		ConfigDir:       filepath.Join(base, "config"),
		StateDir:        filepath.Join(base, "state"),
		LogDir:          filepath.Join(base, "logs"),
		RunDir:          filepath.Join(base, "run"),
		CredentialsFile: filepath.Join(base, "config", "credentials.json"),
		HostConfigFile:  filepath.Join(base, "config", "host.json"),
	}
}

func writeLoggedInHostFiles(t *testing.T, paths config.HostPaths) {
	t.Helper()
	if err := credentials.Save(paths.CredentialsFile, validCredentials()); err != nil {
		t.Fatalf("Save credentials returned error: %v", err)
	}
	if err := config.SaveHostConfig(paths.HostConfigFile, validHostConfig()); err != nil {
		t.Fatalf("SaveHostConfig returned error: %v", err)
	}
}

func validCredentials() credentials.StoredCredentials {
	return credentials.StoredCredentials{
		ServerBaseURL: "https://termix.example.com",
		UserID:        "11111111-1111-1111-1111-111111111111",
		DeviceID:      "22222222-2222-2222-2222-222222222222",
		AccessToken:   "access-token",
		RefreshToken:  "refresh-token",
		ExpiresAt:     time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
}

func validHostConfig() config.HostConfig {
	return config.HostConfig{
		ServerBaseURL:            "https://termix.example.com",
		ControlAPIURL:            "https://termix.example.com",
		RelayWSURL:               "wss://termix.example.com/ws",
		LogLevel:                 "info",
		PreviewMaxBytes:          8192,
		HeartbeatIntervalSeconds: 15,
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

type fakeLoginClient struct {
	request  openapi.LoginRequest
	response *openapi.LoginResponse
}

func (f *fakeLoginClient) Login(_ context.Context, req openapi.LoginRequest) (*openapi.LoginResponse, error) {
	f.request = req
	return f.response, nil
}

type fakeDaemonClient struct {
	healthResponses  []*daemonv1.HealthResponse // popped FIFO; last item is repeated
	healthErrs       []error                    // popped FIFO; last item is repeated
	healthCalls      int
	shutdownCalls    int
	shutdownResponse *daemonv1.ShutdownResponse
	shutdownErr      error
	startRequest     *daemonv1.StartSessionRequest
	startResponse    *daemonv1.StartSessionResponse
	attachRequest    *daemonv1.AttachInfoRequest
	attachResponse   *daemonv1.AttachInfoResponse
	doctorResponse   *daemonv1.DoctorResponse
	statusResponse   *daemonv1.StatusResponse

	listResponse *daemonv1.ListSessionsResponse
	listErr      error

	endRequests []*daemonv1.EndSessionRequest
	endErrByID  map[string]error
}

func (f *fakeDaemonClient) Health(context.Context, *daemonv1.HealthRequest, ...grpc.CallOption) (*daemonv1.HealthResponse, error) {
	f.healthCalls++
	resp := pickHealth(f.healthResponses, f.healthCalls)
	err := pickHealthErr(f.healthErrs, f.healthCalls)
	return resp, err
}

func (f *fakeDaemonClient) Shutdown(context.Context, *daemonv1.ShutdownRequest, ...grpc.CallOption) (*daemonv1.ShutdownResponse, error) {
	f.shutdownCalls++
	if f.shutdownResponse == nil {
		return &daemonv1.ShutdownResponse{}, f.shutdownErr
	}
	return f.shutdownResponse, f.shutdownErr
}

func (f *fakeDaemonClient) StartSession(_ context.Context, req *daemonv1.StartSessionRequest, _ ...grpc.CallOption) (*daemonv1.StartSessionResponse, error) {
	f.startRequest = req
	return f.startResponse, nil
}

func (f *fakeDaemonClient) ListSessions(context.Context, *daemonv1.ListSessionsRequest, ...grpc.CallOption) (*daemonv1.ListSessionsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listResponse != nil {
		return f.listResponse, nil
	}
	return &daemonv1.ListSessionsResponse{}, nil
}

func (f *fakeDaemonClient) EndSession(_ context.Context, req *daemonv1.EndSessionRequest, _ ...grpc.CallOption) (*daemonv1.EndSessionResponse, error) {
	f.endRequests = append(f.endRequests, req)
	if err, ok := f.endErrByID[req.GetSessionId()]; ok && err != nil {
		return nil, err
	}
	return &daemonv1.EndSessionResponse{}, nil
}

func (f *fakeDaemonClient) AttachInfo(_ context.Context, req *daemonv1.AttachInfoRequest, _ ...grpc.CallOption) (*daemonv1.AttachInfoResponse, error) {
	f.attachRequest = req
	return f.attachResponse, nil
}

func (f *fakeDaemonClient) Doctor(context.Context, *daemonv1.DoctorRequest, ...grpc.CallOption) (*daemonv1.DoctorResponse, error) {
	return f.doctorResponse, nil
}

func (f *fakeDaemonClient) Status(context.Context, *daemonv1.StatusRequest, ...grpc.CallOption) (*daemonv1.StatusResponse, error) {
	if f.statusResponse == nil {
		return &daemonv1.StatusResponse{}, nil
	}
	return f.statusResponse, nil
}

type nopCloser struct{}

func (nopCloser) Close() error {
	return nil
}

func pickHealth(slice []*daemonv1.HealthResponse, n int) *daemonv1.HealthResponse {
	if len(slice) == 0 {
		return &daemonv1.HealthResponse{Status: "ok"}
	}
	if n-1 >= len(slice) {
		return slice[len(slice)-1]
	}
	return slice[n-1]
}

func pickHealthErr(slice []error, n int) error {
	if len(slice) == 0 {
		return nil
	}
	if n-1 >= len(slice) {
		return slice[len(slice)-1]
	}
	return slice[n-1]
}

// healthResponseMatching returns a HealthResponse whose identity tuple
// matches buildinfo.Current(version). Use it in tests that exercise
// ensureDaemon and expect the existing daemon to be reused without a
// respawn.
func healthResponseMatching() *daemonv1.HealthResponse {
	id := buildinfo.Current(version)
	return &daemonv1.HealthResponse{
		Status:           "ok",
		Version:          id.Version,
		Revision:         id.Revision,
		Modified:         id.Modified,
		ProxyFingerprint: proxyenv.Fingerprint(),
	}
}

func TestEnsureDaemonRespawnsOnIdentityMismatch(t *testing.T) {
	matching := healthResponseMatching()
	old := &daemonv1.HealthResponse{Status: "ok", Version: "v0-old", Revision: "deadbeefdead", Modified: false}

	fake := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{old, matching},
	}
	socketRemoved := false
	launches := 0
	deps := cliDeps{
		paths:  config.HostPaths{RunDir: t.TempDir(), StateDir: t.TempDir()},
		stderr: io.Discard,
		dialDaemon: func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
			return fake, nopCloser{}, nil
		},
		launchDaemon: func(context.Context, config.HostPaths) error {
			launches++
			return nil
		},
		socketExists: func(string) bool {
			if fake.shutdownCalls > 0 {
				socketRemoved = true
				return false
			}
			return true
		},
		sleep: func(time.Duration) {},
	}

	if err := ensureDaemon(context.Background(), deps); err != nil {
		t.Fatalf("ensureDaemon: %v", err)
	}
	if fake.shutdownCalls != 1 {
		t.Fatalf("shutdownCalls=%d want 1", fake.shutdownCalls)
	}
	if launches != 1 {
		t.Fatalf("launchDaemon calls=%d want 1", launches)
	}
	if !socketRemoved {
		t.Fatalf("socket polling never observed removal")
	}
	if fake.healthCalls < 2 {
		t.Fatalf("healthCalls=%d want >=2 (pre + post-respawn)", fake.healthCalls)
	}
}

// TestEnsureDaemonRespawnsOnProxyFingerprintMismatch verifies the new
// proxy-aware path: identity is identical but the fake daemon's
// proxy_fingerprint differs from what the CLI computes locally — the
// CLI must trigger Shutdown+respawn just like for a version mismatch.
func TestEnsureDaemonRespawnsOnProxyFingerprintMismatch(t *testing.T) {
	matching := healthResponseMatching()
	stale := *healthResponseMatching()
	stale.ProxyFingerprint = "deadbeefdead" // simulate "daemon was launched with different proxy env"

	fake := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{&stale, matching},
	}
	launches := 0
	deps := cliDeps{
		paths:  config.HostPaths{RunDir: t.TempDir(), StateDir: t.TempDir()},
		stderr: io.Discard,
		dialDaemon: func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
			return fake, nopCloser{}, nil
		},
		launchDaemon: func(context.Context, config.HostPaths) error {
			launches++
			return nil
		},
		socketExists: func(string) bool { return fake.shutdownCalls == 0 },
		sleep:        func(time.Duration) {},
	}
	if err := ensureDaemon(context.Background(), deps); err != nil {
		t.Fatalf("ensureDaemon: %v", err)
	}
	if fake.shutdownCalls != 1 {
		t.Fatalf("shutdownCalls=%d want 1", fake.shutdownCalls)
	}
	if launches != 1 {
		t.Fatalf("launches=%d want 1", launches)
	}
}

// TestRunLoginPersistsTermixEnableProxyOverride verifies the bootstrap
// path: when a user runs `TERMIX_ENABLE_PROXY=1 termix login` to clear
// a corporate-proxy first-login, the resulting host.json carries
// `enable_proxy: true` so subsequent invocations work without the env.
func TestRunLoginPersistsTermixEnableProxyOverride(t *testing.T) {
	paths := testPaths(t)
	refreshToken := "refresh-token"
	control := &fakeLoginClient{
		response: &openapi.LoginResponse{
			AccessToken:      "access-token",
			RefreshToken:     &refreshToken,
			ExpiresInSeconds: 900,
			User: openapi.User{
				Id:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				Email:       "user@example.com",
				DisplayName: "User",
				Role:        openapi.UserRoleUser,
			},
			Device: openapi.Device{
				Id:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				DeviceType: openapi.DeviceDeviceTypeHost,
				Label:      "laptop",
			},
		},
	}
	deps := testDeps(paths)
	deps.stdin = strings.NewReader("https://example.com\nuser@example.com\nsecret\nlaptop\n")
	deps.getenv = func(name string) string {
		if name == proxyenv.EnvOverride {
			return "1"
		}
		return ""
	}
	deps.newControlClient = func(string) (loginClient, error) { return control, nil }

	code := run(context.Background(), []string{"termix", "login"}, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	cfg, err := config.LoadHostConfig(paths.HostConfigFile)
	if err != nil {
		t.Fatalf("LoadHostConfig: %v", err)
	}
	if !cfg.EnableProxy {
		t.Fatalf("expected enable_proxy=true persisted; cfg=%+v", cfg)
	}
}

func TestEnsureDaemonReusesMatchingDaemon(t *testing.T) {
	match := healthResponseMatching()

	fake := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{match},
	}
	deps := cliDeps{
		paths:  config.HostPaths{RunDir: t.TempDir(), StateDir: t.TempDir()},
		stderr: io.Discard,
		dialDaemon: func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
			return fake, nopCloser{}, nil
		},
		launchDaemon: func(context.Context, config.HostPaths) error {
			t.Fatalf("launchDaemon called for matching daemon")
			return nil
		},
		socketExists: func(string) bool { return true },
		sleep:        func(time.Duration) {},
	}
	if err := ensureDaemon(context.Background(), deps); err != nil {
		t.Fatalf("ensureDaemon: %v", err)
	}
	if fake.shutdownCalls != 0 {
		t.Fatalf("shutdownCalls=%d want 0", fake.shutdownCalls)
	}
}

func TestEnsureDaemonTreatsOldDaemonZeroIdentityAsMismatch(t *testing.T) {
	zero := &daemonv1.HealthResponse{Status: "ok"}
	match := healthResponseMatching()

	fake := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{zero, match},
	}
	launches := 0
	deps := cliDeps{
		paths:  config.HostPaths{RunDir: t.TempDir(), StateDir: t.TempDir()},
		stderr: io.Discard,
		dialDaemon: func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
			return fake, nopCloser{}, nil
		},
		launchDaemon: func(context.Context, config.HostPaths) error {
			launches++
			return nil
		},
		socketExists: func(string) bool { return fake.shutdownCalls == 0 },
		sleep:        func(time.Duration) {},
	}
	if err := ensureDaemon(context.Background(), deps); err != nil {
		t.Fatalf("ensureDaemon: %v", err)
	}
	if fake.shutdownCalls != 1 {
		t.Fatalf("shutdownCalls=%d want 1", fake.shutdownCalls)
	}
	if launches != 1 {
		t.Fatalf("launches=%d want 1", launches)
	}
}

func TestEnsureDaemonFailsWhenSpawnedDaemonIsAlsoMismatched(t *testing.T) {
	wrong := &daemonv1.HealthResponse{Status: "ok", Version: "v0", Revision: "abcdefabcdef", Modified: false}
	fake := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{wrong, wrong},
	}
	deps := cliDeps{
		paths:  config.HostPaths{RunDir: t.TempDir(), StateDir: t.TempDir()},
		stderr: io.Discard,
		dialDaemon: func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
			return fake, nopCloser{}, nil
		},
		launchDaemon: func(context.Context, config.HostPaths) error { return nil },
		socketExists: func(string) bool { return fake.shutdownCalls == 0 },
		sleep:        func(time.Duration) {},
	}
	err := ensureDaemon(context.Background(), deps)
	if err == nil {
		t.Fatalf("ensureDaemon: expected mismatched-identity error")
	}
	if !strings.Contains(err.Error(), "mismatched identity") {
		t.Fatalf("err=%q want contains \"mismatched identity\"", err)
	}
}

func TestRunStatusPrintsConnectedSection(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	deps := testDeps(paths)
	var stdout bytes.Buffer
	deps.stdout = &stdout

	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		statusResponse: &daemonv1.StatusResponse{
			Version:       "v0.4.0",
			Revision:      "abc123",
			UptimeSeconds: 600,
			Relay: &daemonv1.RelayState{
				Phase:           "connected",
				Attempt:         2,
				LastConnectedAt: time.Date(2026, 5, 8, 9, 5, 0, 0, time.UTC).Unix(),
			},
			Sessions: []*daemonv1.SessionSummary{
				{
					SessionId:  "11111111-1111-1111-1111-111111111111",
					Tool:       "claude",
					Name:       "main",
					Status:     "running",
					LiveInTmux: true,
					PanePid:    4242,
				},
			},
			ProxyFingerprint: "fp123",
		},
	}
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "status"}, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout=%q)", code, stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"USER", "DAEMON", "RELAY", "SESSIONS", "PROXY",
		"v0.4.0",
		"connected",
		"11111111-1111-1111-1111-111111111111",
		"claude",
		"fp123",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestRunStatusPrintsReconnectingDetails(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	deps := testDeps(paths)
	var stdout bytes.Buffer
	deps.stdout = &stdout

	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		statusResponse: &daemonv1.StatusResponse{
			Version: "v0.4.0",
			Relay: &daemonv1.RelayState{
				Phase:       "reconnecting",
				Attempt:     4,
				NextRetryAt: time.Date(2026, 5, 8, 9, 5, 8, 0, time.UTC).Unix(),
				LastError:   "write tcp ... broken pipe",
			},
		},
	}
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "status"}, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "reconnecting") {
		t.Errorf("missing reconnecting; got:\n%s", out)
	}
	if !strings.Contains(out, "attempt 4") {
		t.Errorf("missing attempt 4; got:\n%s", out)
	}
	if !strings.Contains(out, "broken pipe") {
		t.Errorf("missing last error; got:\n%s", out)
	}
}
