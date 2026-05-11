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

// TestLogoutSurfacesRevokeError pins the contract that a real DB failure
// during revoke cannot be silently swallowed. The cookie is still cleared
// (so the browser stops sending it), but the response code must signal the
// failure so the SPA can surface "log out failed; please retry" instead of
// advertising success while the refresh row remains live server-side.
func TestLogoutSurfacesRevokeError(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("logout-error-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Logout Error Test', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")

	refreshCookie := loginAndExtractRefreshCookie(t, router, email, "pw")
	if refreshCookie == nil {
		t.Fatal("no termix_refresh cookie in login response")
	}

	// Close the pool to make the next RevokeRefreshToken call fail with a
	// real DB error. cleanup() is safe to call after Close() because it
	// only re-Close()s and drops the test schema via a fresh connection.
	store.Pool.Close()

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(""))
	logoutReq.AddCookie(refreshCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, logoutReq)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when revoke fails, got %d — body: %s", rec.Code, rec.Body.String())
	}

	// Cookie must still be cleared so the client stops re-presenting it.
	var clearCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "termix_refresh" {
			clearCookie = c
			break
		}
	}
	if clearCookie == nil {
		t.Fatal("logout-on-error must still include a Set-Cookie clearing termix_refresh")
	}
	if clearCookie.MaxAge >= 0 || clearCookie.Value != "" {
		t.Fatalf("Set-Cookie must clear the cookie: MaxAge=%d Value=%q", clearCookie.MaxAge, clearCookie.Value)
	}
}

func TestDoubleLogoutIsIdempotent(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("logout-idempotent-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Double Logout Test', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := newRouter(store, "test-secret")

	// Log in with cookie mode to get the termix_refresh cookie.
	refreshCookie := loginAndExtractRefreshCookie(t, router, email, "pw")
	if refreshCookie == nil {
		t.Fatal("no termix_refresh cookie in login response")
	}

	// First POST /logout with the cookie attached.
	logoutReq1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(""))
	logoutReq1.AddCookie(refreshCookie)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, logoutReq1)

	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first logout: expected 204, got %d — body: %s", rec1.Code, rec1.Body.String())
	}

	// Verify first response clears the cookie (MaxAge < 0, empty Value).
	var clearCookie1 *http.Cookie
	for _, c := range rec1.Result().Cookies() {
		if c.Name == "termix_refresh" {
			clearCookie1 = c
			break
		}
	}
	if clearCookie1 == nil {
		t.Fatal("first logout response must include a Set-Cookie for termix_refresh")
	}
	if clearCookie1.MaxAge >= 0 {
		t.Fatalf("first logout Set-Cookie must have MaxAge < 0, got %d", clearCookie1.MaxAge)
	}
	if clearCookie1.Value != "" {
		t.Fatalf("first logout Set-Cookie must have empty Value, got %q", clearCookie1.Value)
	}

	// Second POST /logout with the SAME cookie (idempotency test).
	logoutReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(""))
	logoutReq2.AddCookie(refreshCookie)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, logoutReq2)

	if rec2.Code != http.StatusNoContent {
		t.Fatalf("second logout: expected 204, got %d — body: %s", rec2.Code, rec2.Body.String())
	}

	// Verify second response also clears the cookie.
	var clearCookie2 *http.Cookie
	for _, c := range rec2.Result().Cookies() {
		if c.Name == "termix_refresh" {
			clearCookie2 = c
			break
		}
	}
	if clearCookie2 == nil {
		t.Fatal("second logout response must include a Set-Cookie for termix_refresh")
	}
	if clearCookie2.MaxAge >= 0 {
		t.Fatalf("second logout Set-Cookie must have MaxAge < 0, got %d", clearCookie2.MaxAge)
	}
	if clearCookie2.Value != "" {
		t.Fatalf("second logout Set-Cookie must have empty Value, got %q", clearCookie2.Value)
	}
}
