package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/termix/termix/go/internal/persistence"
)

func TestLoginRateLimitBlocksAfterFiveAttempts(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run integration tests")
	}
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	// The rate limit triggers BEFORE the handler hits the DB,
	// so no user seed is needed. Use a unique email per run so
	// the (IP, email) bucket is fresh and isolated from other tests.
	body := map[string]string{
		"email":        "rl-" + uuid.NewString() + "@example.com",
		"password":     "wrong",
		"device_type":  "web",
		"platform":     "web",
		"device_label": "test",
	}
	raw, _ := json.Marshal(body)

	router := newRouter(store, "signing-key")

	var lastCode int
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.1:54321"
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		lastCode = rr.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("6th attempt should be 429 (TooManyRequests), got %d", lastCode)
	}
}
