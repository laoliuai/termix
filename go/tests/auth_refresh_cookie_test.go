package tests

import (
	"bytes"
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

// loginAndExtractRefreshCookie logs in via cookie_mode=true and returns the
// termix_refresh cookie set by the response.
func loginAndExtractRefreshCookie(t *testing.T, router http.Handler, email, password string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(
		`{"email":%q,"password":%q,"device_type":"web","platform":"web","device_label":"test-browser","cookie_mode":true}`,
		email, password,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie login: expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "termix_refresh" {
			return c
		}
	}
	return nil
}

func TestRefreshUsesCookieWhenPresent(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("refresh-cookie-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Cookie Test', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")

	refreshCookie := loginAndExtractRefreshCookie(t, router, email, "pw")
	if refreshCookie == nil {
		t.Fatal("no termix_refresh cookie in login response")
	}

	// POST /refresh with cookie, empty body (no refresh_token in body)
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		strings.NewReader("{}"))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshReq.AddCookie(refreshCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, refreshReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with cookie, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var resp openapi.RefreshResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("access_token must be non-empty")
	}
	if resp.RefreshToken != nil {
		t.Fatal("refresh_token must be nil in V1 (no rotation)")
	}
	if resp.User == nil {
		t.Error("RefreshResponse.User must be populated")
	}
	if resp.Device == nil {
		t.Error("RefreshResponse.Device must be populated")
	}
}

func TestRefreshCookieWinsOverBody(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("refresh-cookie-wins-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'CookieWins Test', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")

	refreshCookie := loginAndExtractRefreshCookie(t, router, email, "pw")
	if refreshCookie == nil {
		t.Fatal("no termix_refresh cookie in login response")
	}

	// POST /refresh with valid cookie AND an invalid/bogus body refresh_token.
	// The cookie must win, so the request should succeed despite the bogus body token.
	bogusToken := "totally-invalid-token-that-would-fail-if-used"
	bodyBytes, _ := json.Marshal(openapi.RefreshRequest{RefreshToken: &bogusToken})
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		bytes.NewReader(bodyBytes))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshReq.AddCookie(refreshCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, refreshReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (cookie wins), got %d — body: %s", rec.Code, rec.Body.String())
	}
	var resp openapi.RefreshResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("access_token must be non-empty")
	}
}

func TestRefreshRejectsRevokedCookie(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("refresh-revoked-cookie-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Refresh Revoked Cookie', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")

	refreshCookie := loginAndExtractRefreshCookie(t, router, email, "pw")
	if refreshCookie == nil {
		t.Fatal("no termix_refresh cookie in login response")
	}

	// Manually revoke the freshly issued refresh token by its hash.
	_, err = store.Pool.Exec(ctx,
		`update refresh_tokens set revoked_at = now() where token_hash = $1`,
		auth.HashRefreshToken(refreshCookie.Value))
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// POST /refresh with the (now-revoked) cookie attached, empty body.
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		strings.NewReader("{}"))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshReq.AddCookie(refreshCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, refreshReq)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked cookie must be 401, got %d — body: %s", rec.Code, rec.Body.String())
	}
}
