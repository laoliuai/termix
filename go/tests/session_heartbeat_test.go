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
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/persistence"
)

func TestHostSessionHeartbeatRefreshesLastSeenAndRunningStatus(t *testing.T) {
	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	userID := uuid.NewString()
	hostDeviceID := uuid.NewString()
	sessionID := uuid.NewString()
	email := fmt.Sprintf("session-heartbeat-%s@test.local", uuid.NewString())
	tmuxName := "termix_" + uuid.NewString()

	if _, err := store.Pool.Exec(ctx, `
insert into users (id, email, display_name, password_hash, role, status)
values ($1, $2, 'Heartbeat User', 'not-used', 'user', 'active')
`, userID, email); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := store.Pool.Exec(ctx, `
insert into devices (id, user_id, device_type, platform, label, hostname)
values ($1, $2, 'host', 'ubuntu', 'Heartbeat Host', 'heartbeat-host')
`, hostDeviceID, userID); err != nil {
		t.Fatalf("seed host device: %v", err)
	}
	if _, err := store.Pool.Exec(ctx, `
insert into sessions (id, user_id, host_device_id, tool, launch_command, cwd, cwd_label, tmux_session_name, status, last_seen_at)
values ($1, $2, $3, 'claude', 'claude', '/tmp/p', 'p', $4, 'disconnected', now() - interval '10 minutes')
`, sessionID, userID, hostDeviceID, tmuxName); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	token, err := auth.IssueAccessToken("signing-key", userID, hostDeviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	router := newRouter(store, "signing-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/host/sessions/"+sessionID+"/heartbeat", strings.NewReader(`{"status":"running"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from heartbeat, got %d with body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != sessionID || body.Status != "running" {
		t.Fatalf("unexpected heartbeat response: %+v", body)
	}

	var status string
	var stale bool
	if err := store.Pool.QueryRow(ctx, `
select status, last_seen_at < now() - interval '1 minute'
from sessions
where id = $1
`, sessionID).Scan(&status, &stale); err != nil {
		t.Fatalf("read session heartbeat: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected status running after heartbeat, got %q", status)
	}
	if stale {
		t.Fatal("expected heartbeat to refresh last_seen_at")
	}
}
