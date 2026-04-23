package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/persistence"
)

func TestCreateSessionRecord(t *testing.T) {
	const (
		userID       = "11111111-1111-1111-1111-111111111111"
		hostDeviceID = "22222222-2222-2222-2222-222222222222"
	)

	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	_, err := store.Pool.Exec(ctx, `
insert into users (id, email, display_name, password_hash, role, status)
values ($1, $2, $3, $4, $5, $6)
on conflict (id) do nothing
`, userID, "task3-test-user@example.com", "Task 3 Test User", "not-used-for-login", "user", "active")
	if err != nil {
		t.Fatalf("failed to seed users row: %v", err)
	}

	_, err = store.Pool.Exec(ctx, `
insert into devices (id, user_id, device_type, platform, label, hostname)
values ($1, $2, 'host', 'ubuntu', $3, $4)
on conflict (id) do nothing
`, hostDeviceID, userID, "Task 3 Host Device", "task3-host")
	if err != nil {
		t.Fatalf("failed to seed devices row: %v", err)
	}

	session, err := store.CreateSession(ctx, persistence.CreateSessionParams{
		UserID:          userID,
		HostDeviceID:    hostDeviceID,
		Name:            "optimize loading speed",
		Tool:            "claude",
		LaunchCommand:   "claude",
		Cwd:             "/tmp/project",
		CwdLabel:        "project",
		TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
		Status:          "starting",
	})
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if session.Status != "starting" {
		t.Fatalf("expected starting status, got %s", session.Status)
	}
}

func TestLoginAndCreateSessionHandlers(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run control-plane integration tests")
	}

	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	passwordHash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	_, err = store.Pool.Exec(ctx, `
insert into users (email, display_name, password_hash, role, status)
values ('user@example.com', 'Task 6 Test User', $1, 'user', 'active')
on conflict (email) do update
set display_name = excluded.display_name,
    password_hash = excluded.password_hash,
    role = excluded.role,
    status = excluded.status
`, passwordHash)
	if err != nil {
		t.Fatalf("failed to seed users row: %v", err)
	}

	router := newRouter(store, "signing-key")
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{
	  "email":"user@example.com",
	  "password":"secret",
	  "device_type":"host",
	  "platform":"ubuntu",
	  "device_label":"devbox"
	}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", loginRec.Code, loginRec.Body.String())
	}

	var loginResp openapi.LoginResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("failed to parse login response: %v", err)
	}
	if loginResp.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}

	createSessionReq := httptest.NewRequest(http.MethodPost, "/api/v1/host/sessions", strings.NewReader(fmt.Sprintf(`{
	  "device_id":"%s",
	  "tool":"claude",
	  "name":"integration-run",
	  "launch_command":"claude",
	  "cwd":"/tmp/project",
	  "cwd_label":"project",
	  "hostname":"devbox"
	}`, loginResp.Device.Id)))
	createSessionReq.Header.Set("Content-Type", "application/json")
	createSessionReq.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	createSessionRec := httptest.NewRecorder()
	router.ServeHTTP(createSessionRec, createSessionReq)

	if createSessionRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from create session, got %d with body %s", createSessionRec.Code, createSessionRec.Body.String())
	}

	var createSessionResp openapi.CreateSessionResponse
	if err := json.Unmarshal(createSessionRec.Body.Bytes(), &createSessionResp); err != nil {
		t.Fatalf("failed to parse create session response: %v", err)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/host/sessions/"+createSessionResp.SessionId.String(), strings.NewReader(`{
	  "status":"running"
	}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	patchRec := httptest.NewRecorder()
	router.ServeHTTP(patchRec, patchReq)

	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from patch session, got %d with body %s", patchRec.Code, patchRec.Body.String())
	}

	var patchResp openapi.Session
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patchResp); err != nil {
		t.Fatalf("failed to parse patch session response: %v", err)
	}

	if patchResp.Status != "running" {
		t.Fatalf("expected patched status running, got %s", patchResp.Status)
	}
	if patchResp.Id.String() != createSessionResp.SessionId.String() {
		t.Fatalf("expected patched session id %s, got %s", createSessionResp.SessionId.String(), patchResp.Id.String())
	}
}

func newRouter(store *persistence.Store, signingKey string) *gin.Engine {
	return controlapi.NewRouter(store, signingKey)
}
