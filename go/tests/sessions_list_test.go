package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	for _, s := range body.Sessions {
		if s.HostLabel == nil || *s.HostLabel == "" {
			t.Fatalf("expected non-empty host_label for session %s, got %v", s.Id, s.HostLabel)
		}
	}

	// Other user sees only their one.
	body2 := getSessions(t, srv.URL, otherLogin.AccessToken, "running")
	if len(body2.Sessions) != 1 {
		t.Fatalf("other user expected 1 session, got %d", len(body2.Sessions))
	}
}

func TestListSessionsSurfacesControlHolder(t *testing.T) {
	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	email := fmt.Sprintf("control-holder-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Control Holder Test User', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Two device sessions for the same user; the second login pretends to be a
	// different device so we can compare the holder against each viewer.
	hostLogin := loginAndGetTokens(t, srv.URL, email, "pw")
	otherLogin := loginAndGetTokens(t, srv.URL, email, "pw")
	if hostLogin.Device.Id == otherLogin.Device.Id {
		t.Fatalf("expected two distinct devices from successive logins")
	}

	freeSessionID := seedRunningSession(t, store, hostLogin.User.Id.String(), hostLogin.Device.Id.String())
	heldSessionID := seedRunningSession(t, store, hostLogin.User.Id.String(), hostLogin.Device.Id.String())

	if _, err := store.Pool.Exec(ctx,
		`insert into control_leases (session_id, controller_device_id, lease_version, granted_at, expires_at)
		 values ($1, $2, 1, now(), now() + interval '5 minutes')`,
		heldSessionID, hostLogin.Device.Id); err != nil {
		t.Fatalf("seed lease: %v", err)
	}

	hostList := getSessions(t, srv.URL, hostLogin.AccessToken, "running")
	for _, s := range hostList.Sessions {
		switch s.Id.String() {
		case freeSessionID:
			if s.Control != nil {
				t.Fatalf("expected free session to omit control, got %+v", s.Control)
			}
		case heldSessionID:
			if s.Control == nil {
				t.Fatalf("expected held session to include control state")
			}
			if s.Control.Holder != openapi.Self {
				t.Fatalf("expected self holder for own device, got %q", s.Control.Holder)
			}
			if s.Control.HolderLabel == nil || *s.Control.HolderLabel == "" {
				t.Fatalf("expected non-empty holder_label")
			}
		}
	}

	otherList := getSessions(t, srv.URL, otherLogin.AccessToken, "running")
	var sawHeld bool
	for _, s := range otherList.Sessions {
		if s.Id.String() == heldSessionID {
			sawHeld = true
			if s.Control == nil || s.Control.Holder != openapi.Other {
				t.Fatalf("expected other holder for second device, got %+v", s.Control)
			}
		}
	}
	if !sawHeld {
		t.Fatalf("expected held session to appear in second viewer's list")
	}

	// Expired leases are ignored.
	if _, err := store.Pool.Exec(ctx,
		`update control_leases set expires_at = now() - interval '1 second' where session_id = $1`,
		heldSessionID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	hostListAfterExpiry := getSessions(t, srv.URL, hostLogin.AccessToken, "running")
	for _, s := range hostListAfterExpiry.Sessions {
		if s.Id.String() == heldSessionID && s.Control != nil {
			t.Fatalf("expected expired lease to omit control, got %+v", s.Control)
		}
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

// TestListSessionsIncludesCreatedAt locks in the OpenAPI ↔ SPA contract
// for the session-creation timestamp surfaced in the SPA's Sessions
// workbench. The SPA's `s.created_at` is rendered as the row's
// "created at" column; the field was added to sessions.tsx in 2352597
// but the OpenAPI Session schema and `toOpenAPISession` mapper missed
// the corresponding wiring, so the field always arrived as undefined
// and the column rendered empty in production. This test exercises
// the full ListSessions HTTP path and asserts each row carries a
// non-zero `created_at` close to the seed time.
func TestListSessionsIncludesCreatedAt(t *testing.T) {
	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	email := fmt.Sprintf("created-at-%s@test.local", uuid.NewString())
	pwHash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.Pool.Exec(ctx,
		`insert into users (email, display_name, password_hash, role, status)
		 values ($1, 'Created At Test User', $2, 'user', 'active')`, email, pwHash); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	router := newRouter(store, "test-secret")
	srv := httptest.NewServer(router)
	defer srv.Close()

	login := loginAndGetTokens(t, srv.URL, email, "pw")
	before := time.Now().UTC().Add(-2 * time.Second)
	seedRunningSession(t, store, login.User.Id.String(), login.Device.Id.String())
	after := time.Now().UTC().Add(2 * time.Second)

	body := getSessions(t, srv.URL, login.AccessToken, "running")
	if len(body.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(body.Sessions))
	}
	got := body.Sessions[0].CreatedAt
	if got.IsZero() {
		t.Fatalf("CreatedAt is zero — the OpenAPI mapping or persistence column wiring is broken")
	}
	if got.Before(before) || got.After(after) {
		t.Fatalf("CreatedAt=%s outside expected window [%s, %s]", got, before, after)
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
