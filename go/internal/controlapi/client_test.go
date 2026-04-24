package controlapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLoginReturnsErrorOnNon200(t *testing.T) {
	client, err := New("https://termix.example.com", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/auth/login" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Status:     "401 Unauthorized",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("bad credentials")),
			Request:    r,
		}, nil
	}))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = client.Login(context.Background(), openapi.LoginRequest{
		Email:       "user@example.com",
		Password:    "wrong",
		DeviceType:  openapi.Host,
		Platform:    openapi.Ubuntu,
		DeviceLabel: "devbox",
	})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}

func TestCreateHostSessionSetsBearerToken(t *testing.T) {
	client, err := New("https://termix.example.com", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/host/sessions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"session_id":"33333333-3333-3333-3333-333333333333",
				"tmux_session_name":"termix_33333333-3333-3333-3333-333333333333",
				"status":"starting"
			}`)),
			Request: r,
		}, nil
	}))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	resp, err := client.CreateHostSession(context.Background(), "access-token", openapi.CreateSessionRequest{
		DeviceId:      uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Tool:          openapi.CreateSessionRequestToolCodex,
		LaunchCommand: "codex",
		Cwd:           "/tmp/project",
		CwdLabel:      "project",
		Hostname:      "devbox",
	})
	if err != nil {
		t.Fatalf("CreateHostSession returned error: %v", err)
	}
	if resp.Status != "starting" {
		t.Fatalf("expected starting status, got %q", resp.Status)
	}
}

func TestGetSessionForViewerSetsBearerToken(t *testing.T) {
	client, err := New("https://termix.example.com", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/sessions/33333333-3333-3333-3333-333333333333" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"33333333-3333-3333-3333-333333333333",
				"user_id":"11111111-1111-1111-1111-111111111111",
				"host_device_id":"22222222-2222-2222-2222-222222222222",
				"tool":"codex",
				"launch_command":"codex",
				"cwd":"/tmp/project",
				"cwd_label":"project",
				"tmux_session_name":"termix_33333333-3333-3333-3333-333333333333",
				"status":"running"
			}`)),
			Request: r,
		}, nil
	}))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	session, err := client.GetSessionForViewer(context.Background(), "access-token", "33333333-3333-3333-3333-333333333333")
	if err != nil {
		t.Fatalf("GetSessionForViewer returned error: %v", err)
	}
	if session.Status != "running" {
		t.Fatalf("expected running status, got %q", session.Status)
	}
}

func TestUpdateHostSessionReturnsErrorOnNon200(t *testing.T) {
	client, err := New("https://termix.example.com", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("update failed")),
			Request:    r,
		}, nil
	}))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = client.UpdateHostSession(context.Background(), "access-token", "33333333-3333-3333-3333-333333333333", openapi.UpdateSessionRequest{
		Status: openapi.Running,
	})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}

func TestControlLeaseClientSetsBearerAndParsesAcquire(t *testing.T) {
	const sessionID = "33333333-3333-3333-3333-333333333333"
	const controllerDeviceID = "22222222-2222-2222-2222-222222222222"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/"+sessionID+"/control/acquire" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"session_id":"33333333-3333-3333-3333-333333333333",
			"controller_device_id":"22222222-2222-2222-2222-222222222222",
			"lease_version":3,
			"granted_at":"2026-04-24T01:02:03Z",
			"expires_at":"2026-04-24T01:02:33Z",
			"renew_after_seconds":15
		}`))
	}))
	defer server.Close()

	client, err := New(server.URL+"/api/v1", server.Client().Transport)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	lease, err := client.AcquireControlLease(context.Background(), "access-token", sessionID)
	if err != nil {
		t.Fatalf("AcquireControlLease returned error: %v", err)
	}

	if lease.SessionId.String() != sessionID {
		t.Fatalf("expected session id %s, got %s", sessionID, lease.SessionId.String())
	}
	if lease.ControllerDeviceId.String() != controllerDeviceID {
		t.Fatalf("expected controller device id %s, got %s", controllerDeviceID, lease.ControllerDeviceId.String())
	}
	if lease.LeaseVersion != 3 {
		t.Fatalf("expected lease version 3, got %d", lease.LeaseVersion)
	}
	if lease.RenewAfterSeconds != 15 {
		t.Fatalf("expected renew_after_seconds 15, got %d", lease.RenewAfterSeconds)
	}

	wantGrantedAt := time.Date(2026, time.April, 24, 1, 2, 3, 0, time.UTC)
	if !lease.GrantedAt.Equal(wantGrantedAt) {
		t.Fatalf("expected granted_at %s, got %s", wantGrantedAt, lease.GrantedAt)
	}
	wantExpiresAt := time.Date(2026, time.April, 24, 1, 2, 33, 0, time.UTC)
	if !lease.ExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("expected expires_at %s, got %s", wantExpiresAt, lease.ExpiresAt)
	}
}

func TestControlLeaseClientReturnsStatusError(t *testing.T) {
	const sessionID = "33333333-3333-3333-3333-333333333333"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sessions/"+sessionID+"/control/acquire" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"reason":"already_controlled","error":"control lease is held"}`))
	}))
	defer server.Close()

	client, err := New(server.URL+"/api/v1", server.Client().Transport)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = client.AcquireControlLease(context.Background(), "access-token", sessionID)
	if err == nil {
		t.Fatal("expected error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", apiErr.StatusCode)
	}
	if apiErr.Reason() != "already_controlled" {
		t.Fatalf("expected reason already_controlled, got %q", apiErr.Reason())
	}
}
