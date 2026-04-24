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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

func TestOwnerCanFetchSessionDetailAndForeignUserCannot(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run control-plane integration tests")
	}

	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ownerHash, err := auth.HashPassword("owner-secret")
	if err != nil {
		t.Fatalf("hash owner password: %v", err)
	}
	otherHash, err := auth.HashPassword("other-secret")
	if err != nil {
		t.Fatalf("hash other password: %v", err)
	}

	var ownerID, otherID, deviceID, sessionID string
	if err := store.Pool.QueryRow(ctx, `
insert into users (email, display_name, password_hash, role, status)
values ('owner@example.com', 'Owner', $1, 'user', 'active')
returning id
`, ownerHash).Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if err := store.Pool.QueryRow(ctx, `
insert into users (email, display_name, password_hash, role, status)
values ('other@example.com', 'Other', $1, 'user', 'active')
returning id
`, otherHash).Scan(&otherID); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	if err := store.Pool.QueryRow(ctx, `
insert into devices (user_id, device_type, platform, label, hostname)
values ($1, 'host', 'ubuntu', 'owner-host', 'owner-box')
returning id
`, ownerID).Scan(&deviceID); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	if err := store.Pool.QueryRow(ctx, `
insert into sessions (user_id, host_device_id, tool, launch_command, cwd, cwd_label, tmux_session_name, status)
values ($1, $2, 'codex', 'codex', '/tmp/project', 'project', 'termix_33333333-3333-3333-3333-333333333333', 'running')
returning id
`, ownerID, deviceID).Scan(&sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	router := newRouter(store, "signing-key")
	login := func(email, password string) openapi.LoginResponse {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{
		  "email":"%s",
		  "password":"%s",
		  "device_type":"host",
		  "platform":"ubuntu",
		  "device_label":"devbox"
		}`, email, password)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
		}
		var resp openapi.LoginResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal login: %v", err)
		}
		return resp
	}

	ownerLogin := login("owner@example.com", "owner-secret")
	otherLogin := login("other@example.com", "other-secret")

	ownerReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID, nil)
	ownerReq.Header.Set("Authorization", "Bearer "+ownerLogin.AccessToken)
	ownerRec := httptest.NewRecorder()
	router.ServeHTTP(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusOK {
		t.Fatalf("expected owner to fetch session, got %d with body %s", ownerRec.Code, ownerRec.Body.String())
	}

	otherReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID, nil)
	otherReq.Header.Set("Authorization", "Bearer "+otherLogin.AccessToken)
	otherRec := httptest.NewRecorder()
	router.ServeHTTP(otherRec, otherReq)
	if otherRec.Code != http.StatusNotFound {
		t.Fatalf("expected foreign user to get 404, got %d with body %s", otherRec.Code, otherRec.Body.String())
	}
}

func TestControlLeasePersistenceAcquireRenewRelease(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run control-plane integration tests")
	}

	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	seed := seedLeaseSession(t, ctx, store)

	now := time.Now().UTC().Truncate(time.Microsecond)
	initialExpiry := now.Add(2 * time.Minute)

	lease, err := store.UpsertControlLease(ctx, persistence.UpsertControlLeaseParams{
		SessionID:          seed.sessionID,
		ControllerDeviceID: seed.controllerDeviceID,
		Now:                now,
		ExpiresAt:          initialExpiry,
	})
	if err != nil {
		t.Fatalf("UpsertControlLease returned error: %v", err)
	}

	if lease.LeaseVersion != 1 {
		t.Fatalf("expected lease version 1 on acquire, got %d", lease.LeaseVersion)
	}
	if lease.SessionID != seed.sessionID {
		t.Fatalf("expected session id %s, got %s", seed.sessionID, lease.SessionID)
	}
	if lease.ControllerDeviceID != seed.controllerDeviceID {
		t.Fatalf("expected controller device id %s, got %s", seed.controllerDeviceID, lease.ControllerDeviceID)
	}

	device, err := store.GetDeviceForUser(ctx, seed.controllerDeviceID, seed.userID)
	if err != nil {
		t.Fatalf("GetDeviceForUser returned error: %v", err)
	}
	if device.ID != seed.controllerDeviceID {
		t.Fatalf("expected device id %s, got %s", seed.controllerDeviceID, device.ID)
	}

	active, ok, err := store.GetActiveControlLease(ctx, seed.sessionID, now)
	if err != nil {
		t.Fatalf("GetActiveControlLease returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected active lease")
	}
	if active.LeaseVersion != 1 {
		t.Fatalf("expected active lease version 1, got %d", active.LeaseVersion)
	}

	renewNow := now.Add(30 * time.Second)
	renewedExpiry := renewNow.Add(3 * time.Minute)
	renewed, err := store.RenewControlLease(ctx, persistence.RenewControlLeaseParams{
		SessionID:          seed.sessionID,
		ControllerDeviceID: seed.controllerDeviceID,
		LeaseVersion:       active.LeaseVersion,
		Now:                renewNow,
		ExpiresAt:          renewedExpiry,
	})
	if err != nil {
		t.Fatalf("RenewControlLease returned error: %v", err)
	}
	if renewed.LeaseVersion != 2 {
		t.Fatalf("expected lease version 2 after renew, got %d", renewed.LeaseVersion)
	}

	released, err := store.ReleaseControlLease(ctx, persistence.ReleaseControlLeaseParams{
		SessionID:          seed.sessionID,
		ControllerDeviceID: seed.controllerDeviceID,
		LeaseVersion:       renewed.LeaseVersion,
	})
	if err != nil {
		t.Fatalf("ReleaseControlLease returned error: %v", err)
	}
	if released.LeaseVersion != 2 {
		t.Fatalf("expected released lease version 2, got %d", released.LeaseVersion)
	}

	_, ok, err = store.GetActiveControlLease(ctx, seed.sessionID, renewNow)
	if err != nil {
		t.Fatalf("GetActiveControlLease after release returned error: %v", err)
	}
	if ok {
		t.Fatal("expected no active lease after release")
	}
}

func TestControlLeaseRESTAcquireRenewRelease(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run control-plane integration tests")
	}

	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	seed := seedLeaseSession(t, ctx, store)

	token, err := auth.IssueAccessToken("signing-key", seed.userID, seed.controllerDeviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	router := newRouter(store, "signing-key")

	acquireReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+seed.sessionID+"/control/acquire", nil)
	acquireReq.Header.Set("Authorization", "Bearer "+token)
	acquireRec := httptest.NewRecorder()
	router.ServeHTTP(acquireRec, acquireReq)

	if acquireRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from acquire, got %d with body %s", acquireRec.Code, acquireRec.Body.String())
	}

	var acquireResp openapi.ControlLeaseResponse
	if err := json.Unmarshal(acquireRec.Body.Bytes(), &acquireResp); err != nil {
		t.Fatalf("failed to parse acquire response: %v", err)
	}
	if acquireResp.LeaseVersion != 1 {
		t.Fatalf("expected acquire lease version 1, got %d", acquireResp.LeaseVersion)
	}
	if acquireResp.RenewAfterSeconds != 15 {
		t.Fatalf("expected acquire renew_after_seconds 15, got %d", acquireResp.RenewAfterSeconds)
	}

	renewReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+seed.sessionID+"/control/renew", strings.NewReader(`{"lease_version":1}`))
	renewReq.Header.Set("Authorization", "Bearer "+token)
	renewReq.Header.Set("Content-Type", "application/json")
	renewRec := httptest.NewRecorder()
	router.ServeHTTP(renewRec, renewReq)

	if renewRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from renew, got %d with body %s", renewRec.Code, renewRec.Body.String())
	}

	var renewResp openapi.ControlLeaseResponse
	if err := json.Unmarshal(renewRec.Body.Bytes(), &renewResp); err != nil {
		t.Fatalf("failed to parse renew response: %v", err)
	}
	if renewResp.LeaseVersion != 2 {
		t.Fatalf("expected renew lease version 2, got %d", renewResp.LeaseVersion)
	}

	releaseReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+seed.sessionID+"/control/release", strings.NewReader(`{"lease_version":2}`))
	releaseReq.Header.Set("Authorization", "Bearer "+token)
	releaseReq.Header.Set("Content-Type", "application/json")
	releaseRec := httptest.NewRecorder()
	router.ServeHTTP(releaseRec, releaseReq)

	if releaseRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from release, got %d with body %s", releaseRec.Code, releaseRec.Body.String())
	}

	var releaseResp openapi.ReleaseControlLeaseResponse
	if err := json.Unmarshal(releaseRec.Body.Bytes(), &releaseResp); err != nil {
		t.Fatalf("failed to parse release response: %v", err)
	}
	if !releaseResp.Released {
		t.Fatal("expected release response to indicate released=true")
	}
}

func newRouter(store *persistence.Store, signingKey string) *gin.Engine {
	return controlapi.NewRouter(store, signingKey)
}

type leaseSeed struct {
	userID             string
	hostDeviceID       string
	controllerDeviceID string
	sessionID          string
}

func seedLeaseSession(t *testing.T, ctx context.Context, store *persistence.Store) leaseSeed {
	t.Helper()

	userID := uuid.NewString()
	hostDeviceID := uuid.NewString()
	controllerDeviceID := uuid.NewString()
	sessionID := uuid.NewString()
	email := fmt.Sprintf("lease-%s@example.com", uuid.NewString())
	tmuxSessionName := fmt.Sprintf("termix_%s", uuid.NewString())

	_, err := store.Pool.Exec(ctx, `
insert into users (id, email, display_name, password_hash, role, status)
values ($1, $2, $3, $4, $5, $6)
`, userID, email, "Lease Test User", "not-used", "user", "active")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	_, err = store.Pool.Exec(ctx, `
insert into devices (id, user_id, device_type, platform, label, hostname)
values ($1, $2, 'host', 'ubuntu', $3, $4)
`, hostDeviceID, userID, "Lease Host Device", "lease-host")
	if err != nil {
		t.Fatalf("insert host device: %v", err)
	}

	_, err = store.Pool.Exec(ctx, `
insert into devices (id, user_id, device_type, platform, label)
values ($1, $2, 'android', 'android', $3)
`, controllerDeviceID, userID, "Lease Controller Device")
	if err != nil {
		t.Fatalf("insert controller device: %v", err)
	}

	_, err = store.Pool.Exec(ctx, `
insert into sessions (id, user_id, host_device_id, name, tool, launch_command, cwd, cwd_label, tmux_session_name, status)
values ($1, $2, $3, $4, 'claude', 'claude', '/tmp/lease', 'lease', $5, 'running')
`, sessionID, userID, hostDeviceID, "lease-session", tmuxSessionName)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	return leaseSeed{
		userID:             userID,
		hostDeviceID:       hostDeviceID,
		controllerDeviceID: controllerDeviceID,
		sessionID:          sessionID,
	}
}
