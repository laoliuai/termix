package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/credentials"
	"google.golang.org/grpc"
)

func TestRunLoginStoresCredentials(t *testing.T) {
	paths := testPaths(t)
	control := &fakeLoginClient{
		response: &openapi.LoginResponse{
			AccessToken:      "access-token",
			RefreshToken:     "refresh-token",
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
	deps.newControlClient = func(string) (loginClient, error) {
		return &fakeLoginClient{
			response: &openapi.LoginResponse{
				AccessToken:      "access-token",
				RefreshToken:     "refresh-token",
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

func TestRunStartLaunchesDaemonAndAttachesSession(t *testing.T) {
	paths := testPaths(t)
	client := &fakeDaemonClient{
		healthResponse: &daemonv1.HealthResponse{Status: "ok"},
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

func TestRunSessionsAttachUsesDaemonAttachInfo(t *testing.T) {
	deps := testDeps(testPaths(t))
	client := &fakeDaemonClient{
		healthResponse: &daemonv1.HealthResponse{Status: "ok"},
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

type fakeLoginClient struct {
	request  openapi.LoginRequest
	response *openapi.LoginResponse
}

func (f *fakeLoginClient) Login(_ context.Context, req openapi.LoginRequest) (*openapi.LoginResponse, error) {
	f.request = req
	return f.response, nil
}

type fakeDaemonClient struct {
	healthResponse *daemonv1.HealthResponse
	startRequest   *daemonv1.StartSessionRequest
	startResponse  *daemonv1.StartSessionResponse
	attachRequest  *daemonv1.AttachInfoRequest
	attachResponse *daemonv1.AttachInfoResponse
	doctorResponse *daemonv1.DoctorResponse
}

func (f *fakeDaemonClient) Health(context.Context, *daemonv1.HealthRequest, ...grpc.CallOption) (*daemonv1.HealthResponse, error) {
	return f.healthResponse, nil
}

func (f *fakeDaemonClient) StartSession(_ context.Context, req *daemonv1.StartSessionRequest, _ ...grpc.CallOption) (*daemonv1.StartSessionResponse, error) {
	f.startRequest = req
	return f.startResponse, nil
}

func (f *fakeDaemonClient) ListSessions(context.Context, *daemonv1.ListSessionsRequest, ...grpc.CallOption) (*daemonv1.ListSessionsResponse, error) {
	return &daemonv1.ListSessionsResponse{}, nil
}

func (f *fakeDaemonClient) AttachInfo(_ context.Context, req *daemonv1.AttachInfoRequest, _ ...grpc.CallOption) (*daemonv1.AttachInfoResponse, error) {
	f.attachRequest = req
	return f.attachResponse, nil
}

func (f *fakeDaemonClient) Doctor(context.Context, *daemonv1.DoctorRequest, ...grpc.CallOption) (*daemonv1.DoctorResponse, error) {
	return f.doctorResponse, nil
}

type nopCloser struct{}

func (nopCloser) Close() error {
	return nil
}
