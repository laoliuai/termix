package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/persistence"
)

func TestAuthRefreshHappyPath(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("refresh-happy-%s@test.local", uuid.NewString())
	pwHash, _ := auth.HashPassword("pw")
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Refresh Test', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()

	login := loginAndGetTokens(t, srv.URL, email, "pw")

	body, _ := json.Marshal(openapi.RefreshRequest{RefreshToken: login.RefreshToken})
	res, err := http.Post(srv.URL+"/api/v1/auth/refresh",
		"application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var resp openapi.RefreshResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("access_token must be non-empty")
	}
	// Note: we don't strictly require AccessToken != login.AccessToken
	// because they *could* be the same if issued in the same second.
	// The important thing is that refresh returns a valid token.
}

func TestAuthRefreshRejectsUnknownToken(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()
	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()

	token := "deadbeef-not-a-real-token"
	body, _ := json.Marshal(openapi.RefreshRequest{RefreshToken: &token})
	res, err := http.Post(srv.URL+"/api/v1/auth/refresh",
		"application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestAuthRefreshRejectsRevokedToken(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()
	ctx := context.Background()
	email := fmt.Sprintf("refresh-rev-%s@test.local", uuid.NewString())
	pwHash, _ := auth.HashPassword("pw")
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Refresh Revoked', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()
	login := loginAndGetTokens(t, srv.URL, email, "pw")

	// Manually revoke the freshly issued refresh token.
	row, err := store.GetActiveRefreshTokenByHash(ctx, auth.HashRefreshToken(*login.RefreshToken))
	if err != nil {
		t.Fatalf("fetch row: %v", err)
	}
	if err := store.RevokeRefreshToken(ctx, row.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	body, _ := json.Marshal(openapi.RefreshRequest{RefreshToken: login.RefreshToken})
	res, _ := http.Post(srv.URL+"/api/v1/auth/refresh", "application/json", strings.NewReader(string(body)))
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("revoked token must be 401, got %d", res.StatusCode)
	}
}
