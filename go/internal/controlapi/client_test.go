package controlapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

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
