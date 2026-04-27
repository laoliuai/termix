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

	codes := make([]int, 6)
	for i := range codes {
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		// 203.0.113.0/24 is RFC 5737 TEST-NET-3 (documentation address).
		req.RemoteAddr = "203.0.113.1:54321"
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		codes[i] = rr.Code
	}
	for i, code := range codes[:5] {
		if code == http.StatusTooManyRequests {
			t.Errorf("request %d: expected non-429, got 429", i+1)
		}
	}
	if codes[5] != http.StatusTooManyRequests {
		t.Fatalf("request 6: expected 429, got %d", codes[5])
	}
}
