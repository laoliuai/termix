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

func TestAndroidDeviceLoginPersistsRefreshToken(t *testing.T) {
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ctx := context.Background()
	email := fmt.Sprintf("android-login-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx, `
insert into users (email, display_name, password_hash, role, status)
values ($1, $2, $3, 'user', 'active')`, email, "Android Login Test", pwHash); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := fmt.Sprintf(`{"email":%q,"password":"pw","device_type":"android","platform":"android","device_label":"Pixel 9 Pro"}`, email)
	res, err := http.Post(srv.URL+"/api/v1/auth/login",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("login post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var resp openapi.LoginResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(resp.Device.DeviceType) != "android" {
		t.Fatalf("expected android device_type, got %q", resp.Device.DeviceType)
	}
	if string(resp.Device.Platform) != "android" {
		t.Fatalf("expected android platform, got %q", resp.Device.Platform)
	}

	if resp.RefreshToken == nil || *resp.RefreshToken == "" {
		t.Fatal("expected non-empty refresh_token in body")
	}
	if _, err := store.GetActiveRefreshTokenByHash(ctx,
		auth.HashRefreshToken(*resp.RefreshToken)); err != nil {
		t.Fatalf("refresh token row missing: %v", err)
	}
}
