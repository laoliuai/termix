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

func TestWebLoginCookieMode(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("web-%s@example.com", uuid.NewString())
	passwordHash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := store.Pool.Exec(ctx, `
		insert into users (email, display_name, password_hash, role, status)
		values ($1, $2, $3, $4, $5)`,
		email, "Web Test", passwordHash, "user", "active"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	router := newRouter(store, "signing-key")
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := fmt.Sprintf(
		`{"email":%q,"password":"secret-pass","device_type":"web","platform":"web","device_label":"Mozilla/5.0","cookie_mode":true}`,
		email,
	)
	res, err := http.Post(srv.URL+"/api/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var resp openapi.LoginResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// access_token must be non-empty
	if resp.AccessToken == "" {
		t.Fatal("access_token must be non-empty")
	}

	// refresh_token must be absent from body in cookie mode
	if resp.RefreshToken != nil {
		t.Fatalf("refresh_token must be nil in cookie mode, got %q", *resp.RefreshToken)
	}

	// cookie_mode must be echoed as true
	if resp.CookieMode == nil || !*resp.CookieMode {
		t.Fatal("cookie_mode must be echoed as true in response")
	}

	// Set-Cookie header must include termix_refresh cookie
	cookies := res.Cookies()
	var refreshCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "termix_refresh" {
			refreshCookie = c
			break
		}
	}
	if refreshCookie == nil {
		t.Fatal("Set-Cookie header must contain termix_refresh cookie")
	}
	if refreshCookie.Value == "" {
		t.Fatal("termix_refresh cookie value must be non-empty")
	}
	if !refreshCookie.HttpOnly {
		t.Fatal("termix_refresh cookie must be HttpOnly")
	}
	if !refreshCookie.Secure {
		t.Fatal("termix_refresh cookie must be Secure")
	}
	if refreshCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("termix_refresh cookie must be SameSite=Strict, got %v", refreshCookie.SameSite)
	}
	if refreshCookie.Path != "/api/v1/auth" {
		t.Fatalf("termix_refresh cookie path must be /api/v1/auth, got %q", refreshCookie.Path)
	}
}
