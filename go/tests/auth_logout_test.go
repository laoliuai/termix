package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/persistence"
)

func TestLogoutRevokesAndClearsCookie(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("logout-revoke-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Logout Test', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")

	// Log in with cookie mode to get the termix_refresh cookie.
	refreshCookie := loginAndExtractRefreshCookie(t, router, email, "pw")
	if refreshCookie == nil {
		t.Fatal("no termix_refresh cookie in login response")
	}

	// POST /logout with the cookie attached.
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(""))
	logoutReq.AddCookie(refreshCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, logoutReq)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d — body: %s", rec.Code, rec.Body.String())
	}

	// Response must clear the cookie (MaxAge < 0, empty Value).
	var clearCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "termix_refresh" {
			clearCookie = c
			break
		}
	}
	if clearCookie == nil {
		t.Fatal("logout response must include a Set-Cookie for termix_refresh")
	}
	if clearCookie.MaxAge >= 0 {
		t.Fatalf("logout Set-Cookie must have MaxAge < 0, got %d", clearCookie.MaxAge)
	}
	if clearCookie.Value != "" {
		t.Fatalf("logout Set-Cookie must have empty Value, got %q", clearCookie.Value)
	}

	// Subsequent refresh with the SAME cookie must return 401 (row revoked).
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader("{}"))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshReq.AddCookie(refreshCookie)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, refreshReq)

	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("after logout, refresh must return 401, got %d — body: %s", rec2.Code, rec2.Body.String())
	}
}

func TestLogoutWithoutCookieReturns400ButStillClears(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	router := newRouter(store, "test-secret")

	// POST /logout with NO cookie.
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(""))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, logoutReq)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — body: %s", rec.Code, rec.Body.String())
	}

	// Response must still clear the cookie.
	var clearCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "termix_refresh" {
			clearCookie = c
			break
		}
	}
	if clearCookie == nil {
		t.Fatal("logout without cookie must still include a Set-Cookie to clear termix_refresh")
	}
	if clearCookie.MaxAge >= 0 {
		t.Fatalf("Set-Cookie must have MaxAge < 0, got %d", clearCookie.MaxAge)
	}
}
