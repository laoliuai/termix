package tests

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/termix/termix/go/internal/persistence"
)

func TestSecurityHeadersPresentOnResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run integration tests")
	}
	t.Setenv("TERMIX_CONTROL_RELAY_ORIGIN", "wss://relay.example.com")

	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	router := newRouter(store, "signing-key")

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	h := rr.Result().Header

	if got := h.Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=31536000") {
		t.Errorf("HSTS missing or wrong: %q", got)
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "connect-src 'self' wss://relay.example.com"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q in %q", want, csp)
		}
	}
	for k, v := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := h.Get(k); got != v {
			t.Errorf("%s: want %q, got %q", k, v, got)
		}
	}
	if got := h.Get("Permissions-Policy"); got != "geolocation=(), microphone=(), camera=()" {
		t.Errorf("Permissions-Policy: want %q, got %q", "geolocation=(), microphone=(), camera=()", got)
	}
}
