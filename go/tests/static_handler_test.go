package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termix/termix/go/internal/persistence"
)

func TestStaticDevOverrideServesIndex(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run integration tests")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX_MARKER"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMIX_CONTROL_WEB_DIR", dir)

	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()
	router := newRouter(store, "signing-key")

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "INDEX_MARKER") {
		t.Errorf("body did not contain index marker: %q", rr.Body.String())
	}
}

func TestStaticSPAFallbackForUnknownPath(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run integration tests")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("SPA_FALLBACK"), 0644)
	t.Setenv("TERMIX_CONTROL_WEB_DIR", dir)

	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()
	router := newRouter(store, "signing-key")

	// /sessions doesn't exist as a file → fallback to index.html
	req := httptest.NewRequest("GET", "/sessions", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("SPA fallback should 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "SPA_FALLBACK") {
		t.Errorf("expected fallback to index.html, got %q", rr.Body.String())
	}
}

func TestStaticAssetsPathReturns404IfMissing(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run integration tests")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0644)
	t.Setenv("TERMIX_CONTROL_WEB_DIR", dir)

	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()
	router := newRouter(store, "signing-key")

	req := httptest.NewRequest("GET", "/assets/missing.js", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != 404 {
		t.Errorf("/assets/missing should 404, got %d", rr.Code)
	}
}

func TestStaticDoesNotInterceptAPIRoutes(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run integration tests")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0644)
	t.Setenv("TERMIX_CONTROL_WEB_DIR", dir)

	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()
	router := newRouter(store, "signing-key")

	// GET healthz is a real API route — should NOT be served as SPA.
	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("healthz should 200, got %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "INDEX") {
		t.Errorf("API route should not be served as SPA")
	}
}

func TestStaticEmbedModeServesStubIndex(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run integration tests")
	}
	// Empty string is treated by StaticHandler as "unset" (devDir != "" is false),
	// so the handler falls through to the embedded web_dist FS.
	t.Setenv("TERMIX_CONTROL_WEB_DIR", "")

	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()
	router := newRouter(store, "signing-key")

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("embed mode GET /: want 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `id="app"`) {
		t.Errorf("embed mode body did not contain stub marker: %q", rr.Body.String())
	}
}
