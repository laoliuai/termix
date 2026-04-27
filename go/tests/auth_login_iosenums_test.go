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

// TestIosLoginEnumAccepted verifies that device_type="ios" and platform="ios"
// are accepted by the login endpoint (enum contract test).
func TestIosLoginEnumAccepted(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("ios-%s@example.com", uuid.NewString())
	passwordHash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := store.Pool.Exec(ctx, `
		insert into users (email, display_name, password_hash, role, status)
		values ($1, $2, $3, $4, $5)`,
		email, "iOS Test", passwordHash, "user", "active"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	router := newRouter(store, "signing-key")
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := fmt.Sprintf(
		`{"email":%q,"password":"secret-pass","device_type":"ios","platform":"ios","device_label":"iPhone15,2"}`,
		email,
	)
	res, err := http.Post(srv.URL+"/api/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for ios device_type, got %d", res.StatusCode)
	}
}
