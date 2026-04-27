package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/termix/termix/go/internal/persistence"
)

func TestOriginAllowlistBlocksMismatchedPOST(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	t.Setenv("TERMIX_ALLOWED_ORIGIN", "https://termix.example.com")
	router := newRouter(store, "signing-key")

	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"x","password":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.example.com")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}

func TestOriginAllowlistPermitsMatchedPOST(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	t.Setenv("TERMIX_ALLOWED_ORIGIN", "https://termix.example.com")
	router := newRouter(store, "signing-key")

	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"x","password":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://termix.example.com")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Fatalf("matched origin should NOT be 403, got %d", rr.Code)
	}
}

func TestOriginAllowlistAllowsAbsentOrigin(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	t.Setenv("TERMIX_ALLOWED_ORIGIN", "https://termix.example.com")
	router := newRouter(store, "signing-key")

	// No Origin header (CLI / Compose / curl)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Fatalf("absent Origin should NOT be 403, got %d", rr.Code)
	}
}

func TestOriginAllowlistDisabledWhenEnvUnset(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	// Force unset; t.Setenv with "" is treated by middleware as unset since it skips mount.
	t.Setenv("TERMIX_ALLOWED_ORIGIN", "")
	router := newRouter(store, "signing-key")

	// Cross-origin POST that would normally be blocked.
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.example.com")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Fatalf("with TERMIX_ALLOWED_ORIGIN unset, no Origin enforcement; got 403")
	}
}
