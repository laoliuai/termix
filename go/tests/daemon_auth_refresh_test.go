package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/session"
)

// flakyAuthControlClient returns a fake auth error on the first CreateHostSession
// call, then succeeds on retry. It records every access token it saw so the
// test can assert that the refreshed token was used the second time.
type flakyAuthControlClient struct {
	tokensSeen     []string
	createResponse *openapi.CreateSessionResponse
	updateResponse *openapi.Session
}

type fakeAuthError struct{}

func (fakeAuthError) Error() string { return "fake 401" }

func (f *flakyAuthControlClient) CreateHostSession(_ context.Context, accessToken string, _ openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error) {
	f.tokensSeen = append(f.tokensSeen, accessToken)
	if len(f.tokensSeen) == 1 {
		return nil, fakeAuthError{}
	}
	return f.createResponse, nil
}

func (f *flakyAuthControlClient) UpdateHostSession(_ context.Context, accessToken string, _ string, _ openapi.UpdateSessionRequest) (*openapi.Session, error) {
	f.tokensSeen = append(f.tokensSeen, accessToken)
	return f.updateResponse, nil
}

func TestManagerStartSessionRetriesOnAuthErrorWithRefreshedToken(t *testing.T) {
	sessionID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	deviceID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	control := &flakyAuthControlClient{
		createResponse: &openapi.CreateSessionResponse{
			SessionId:       sessionID,
			TmuxSessionName: "termix_44444444",
			Status:          "starting",
		},
		updateResponse: &openapi.Session{
			Id:              sessionID,
			HostDeviceId:    deviceID,
			UserId:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Tool:            openapi.SessionToolClaude,
			LaunchCommand:   "bash",
			Cwd:             "/tmp",
			TmuxSessionName: "termix_44444444",
			Status:          "running",
		},
	}

	loadCalls := 0
	refreshCalls := 0
	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(t.TempDir()),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			loadCalls++
			return credentials.StoredCredentials{
				DeviceID:    "22222222-2222-2222-2222-222222222222",
				AccessToken: "stale-token",
			}, nil
		},
		RefreshCredentials: func(context.Context) (credentials.StoredCredentials, error) {
			refreshCalls++
			return credentials.StoredCredentials{
				DeviceID:    "22222222-2222-2222-2222-222222222222",
				AccessToken: "fresh-token",
			}, nil
		},
		IsAuthError: func(err error) bool {
			var fe fakeAuthError
			return errors.As(err, &fe)
		},
		Control: control,
		Tmux:    &fakeTmuxRunner{},
		Now: func() time.Time {
			return time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
		},
		Hostname: func() (string, error) { return "devbox", nil },
	})

	if _, err := manager.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool: "claude",
		Cwd:  "/tmp",
	}); err != nil {
		t.Fatalf("StartSession returned error: %v", err)
	}

	if refreshCalls != 1 {
		t.Fatalf("expected 1 refresh call, got %d", refreshCalls)
	}
	// Three control calls: failed Create, retry Create, Update. The first must
	// have used the stale token, the rest the fresh one.
	if len(control.tokensSeen) < 3 {
		t.Fatalf("expected at least 3 control calls, got %d", len(control.tokensSeen))
	}
	if control.tokensSeen[0] != "stale-token" {
		t.Fatalf("first call should use stale token, got %q", control.tokensSeen[0])
	}
	for i := 1; i < len(control.tokensSeen); i++ {
		if control.tokensSeen[i] != "fresh-token" {
			t.Fatalf("call %d should use fresh token, got %q", i, control.tokensSeen[i])
		}
	}
}

func TestManagerStartSessionPropagatesNonAuthErrors(t *testing.T) {
	control := &alwaysFailControlClient{err: errors.New("connection refused")}
	refreshCalls := 0

	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(t.TempDir()),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{
				DeviceID:    "22222222-2222-2222-2222-222222222222",
				AccessToken: "tok",
			}, nil
		},
		RefreshCredentials: func(context.Context) (credentials.StoredCredentials, error) {
			refreshCalls++
			return credentials.StoredCredentials{}, nil
		},
		IsAuthError: func(error) bool { return false }, // non-auth → no retry
		Control:     control,
		Tmux:        &fakeTmuxRunner{},
		Now:         time.Now,
		Hostname:    func() (string, error) { return "devbox", nil },
	})

	_, err := manager.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool: "claude",
		Cwd:  "/tmp",
	})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if refreshCalls != 0 {
		t.Fatalf("non-auth error must not trigger refresh, got %d refresh calls", refreshCalls)
	}
}

type alwaysFailControlClient struct {
	err error
}

func (a *alwaysFailControlClient) CreateHostSession(context.Context, string, openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error) {
	return nil, a.err
}

func (a *alwaysFailControlClient) UpdateHostSession(context.Context, string, string, openapi.UpdateSessionRequest) (*openapi.Session, error) {
	return nil, a.err
}
