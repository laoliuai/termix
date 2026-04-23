package controlapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

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
		Email:      "user@example.com",
		Password:   "wrong",
		DeviceType: openapi.Host,
		Platform:   openapi.Ubuntu,
		DeviceLabel: "devbox",
	})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}
