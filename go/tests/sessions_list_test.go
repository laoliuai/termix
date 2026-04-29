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

func TestListSessionsOwnerScoped(t *testing.T) {
	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ownerEmail := fmt.Sprintf("owner-list-%s@test.local", uuid.NewString())
	otherEmail := fmt.Sprintf("other-list-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	for _, email := range []string{ownerEmail, otherEmail} {
		if _, err := store.Pool.Exec(ctx,
			`insert into users (email, display_name, password_hash, role, status)
			 values ($1, 'List Test User', $2, 'user', 'active')`, email, pwHash); err != nil {
			t.Fatalf("seed user %s: %v", email, err)
		}
	}

	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()

	ownerLogin := loginAndGetTokens(t, srv.URL, ownerEmail, "pw")
	otherLogin := loginAndGetTokens(t, srv.URL, otherEmail, "pw")

	seedRunningSession(t, store, ownerLogin.User.Id.String(), ownerLogin.Device.Id.String())
	seedRunningSession(t, store, ownerLogin.User.Id.String(), ownerLogin.Device.Id.String())
	seedRunningSession(t, store, otherLogin.User.Id.String(), otherLogin.Device.Id.String())

	// Owner sees their two sessions.
	body := getSessions(t, srv.URL, ownerLogin.AccessToken, "running")
	if len(body.Sessions) != 2 {
		t.Fatalf("owner expected 2 sessions, got %d", len(body.Sessions))
	}

	// Other user sees only their one.
	body2 := getSessions(t, srv.URL, otherLogin.AccessToken, "running")
	if len(body2.Sessions) != 1 {
		t.Fatalf("other user expected 1 session, got %d", len(body2.Sessions))
	}
}

func TestListSessionsRunningRequiresFreshSessionHeartbeat(t *testing.T) {
	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	email := fmt.Sprintf("heartbeat-list-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Heartbeat Test User', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()

	login := loginAndGetTokens(t, srv.URL, email, "pw")
	freshID := seedRunningSession(t, store, login.User.Id.String(), login.Device.Id.String())
	staleID := seedRunningSession(t, store, login.User.Id.String(), login.Device.Id.String())

	if _, err := store.Pool.Exec(ctx,
		`update sessions set last_seen_at = now() - interval '10 minutes' where id = $1`, staleID); err != nil {
		t.Fatalf("make stale session heartbeat: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`update sessions set last_seen_at = now() where id = $1`, freshID); err != nil {
		t.Fatalf("make fresh session heartbeat: %v", err)
	}

	running := getSessions(t, srv.URL, login.AccessToken, "running")
	if len(running.Sessions) != 1 {
		t.Fatalf("expected only fresh running session, got %d: %+v", len(running.Sessions), running.Sessions)
	}
	if running.Sessions[0].Id.String() != freshID {
		t.Fatalf("expected fresh session %s, got %s", freshID, running.Sessions[0].Id.String())
	}

	all := getSessions(t, srv.URL, login.AccessToken, "all")
	statuses := map[string]string{}
	for _, s := range all.Sessions {
		statuses[s.Id.String()] = s.Status
	}
	if statuses[freshID] != "running" {
		t.Fatalf("expected fresh session to remain running, got %q", statuses[freshID])
	}
	if statuses[staleID] != "disconnected" {
		t.Fatalf("expected stale session to be exposed as disconnected, got %q", statuses[staleID])
	}

}

// --- helpers ---

func loginAndGetTokens(t *testing.T, baseURL, email, password string) openapi.LoginResponse {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q,"device_type":"android","platform":"android","device_label":"test"}`, email, password)
	res, err := http.Post(baseURL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("login %s: status %d", email, res.StatusCode)
	}
	var lr openapi.LoginResponse
	if err := json.NewDecoder(res.Body).Decode(&lr); err != nil {
		t.Fatalf("login decode: %v", err)
	}
	return lr
}

func seedRunningSession(t *testing.T, store *persistence.Store, userID, hostDeviceID string) string {
	t.Helper()
	sessionID := uuid.NewString()
	tmuxName := "termix_" + uuid.NewString()
	if _, err := store.Pool.Exec(context.Background(),
		`insert into sessions (id, user_id, host_device_id, tool, launch_command, cwd, cwd_label, tmux_session_name, status)
		 values ($1, $2, $3, 'claude', 'claude', '/tmp/p', 'p', $4, 'running')`,
		sessionID, userID, hostDeviceID, tmuxName); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return sessionID
}

type listResp struct {
	Sessions []openapi.Session `json:"sessions"`
}

func getSessions(t *testing.T, baseURL, token, status string) listResp {
	t.Helper()
	url := baseURL + "/api/v1/sessions"
	if status != "" {
		url += "?status=" + status
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("list status %d", res.StatusCode)
	}
	var b listResp
	if err := json.NewDecoder(res.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return b
}
