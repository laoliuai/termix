# Phase 2 Relay/Watch Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first Phase 2 transport slice: `termixd` connects to `termix-relay`, relay serves a generic viewer WSS protocol, new watchers receive an initial snapshot, and live output fans out to multiple viewers.

**Architecture:** Keep `termixd` as the only authority for current screen state. Add a lightweight relay that tracks daemon/viewer connections, requests snapshots from the daemon on watch, and forwards snapshot plus live output to viewers. Reuse the existing control-plane REST stack for session authorization instead of introducing a second service-to-service contract in this slice.

**Tech Stack:** Go 1.25+, Gin, `oapi-codegen`, protobuf, `sqlc + pgx`, tmux control mode, WSS via `github.com/coder/websocket`, JSON control envelopes, binary frame payloads

---

## File Map

- Modify: `go/internal/config/paths.go` — add a stable host config file path for relay settings.
- Create: `go/internal/config/store.go` — save/load host config and derive control/relay URLs from the login server URL.
- Modify: `go/cmd/termix/main.go` — persist host config during `termix login`.
- Modify: `go/cmd/termix/main_test.go` — verify login writes host config.
- Modify: `go/tests/config_test.go` — verify host config round-trip and derived relay URL.
- Modify: `openapi/control.openapi.yaml` — add bearer-protected session detail endpoint for watch authorization.
- Modify: `go/internal/persistence/sessions.go` — add `GetSessionForUser`.
- Modify: `go/internal/controlapi/{server.go,client.go,client_test.go}` — serve and call session detail reads.
- Modify: `go/tests/control_integration_test.go` — verify owner-visible session detail and non-owner denial.
- Create: `schemas/ws/{envelope.schema.json,control.session_watch.schema.json,binary_header.schema.json}` — protocol artifacts for relay transport.
- Create: `go/internal/relayproto/{envelope.go,frame.go}` — shared JSON envelope and binary frame codec.
- Test: `go/tests/relay_protocol_test.go`
- Create: `go/internal/tmux/control.go` — snapshot capture and control-mode stream helpers.
- Test: `go/tests/tmux_control_test.go`
- Create: `go/internal/relayclient/{messages.go,client.go}` — daemon-side outbound WSS relay client.
- Modify: `go/internal/session/{types.go,manager.go}` — track relay publishing hooks and snapshot callbacks.
- Modify: `go/cmd/termixd/main.go` — wire relay client into daemon startup.
- Test: `go/tests/daemon_relay_test.go`
- Create: `go/internal/relay/{auth.go,registry.go,server.go}` — relay auth, connection registry, and watch handshake.
- Create: `go/cmd/termix-relay/main.go` — relay entrypoint.
- Test: `go/tests/relay_integration_test.go`
- Modify: `docs/PROGRESS.md` — record the slice as completed after execution.

### Task 1: Persist Relay-Capable Host Config During Login

**Files:**
- Modify: `go/internal/config/paths.go`
- Create: `go/internal/config/store.go`
- Modify: `go/cmd/termix/main.go`
- Modify: `go/cmd/termix/main_test.go`
- Modify: `go/tests/config_test.go`

- [ ] **Step 1: Write the failing tests for host config persistence**

Add to `go/tests/config_test.go`:

```go
func TestHostConfigSaveAndLoadRoundTrip(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "host.json")
	cfg := config.HostConfig{
		ServerBaseURL:            "https://termix.example.com",
		ControlAPIURL:            "https://termix.example.com",
		RelayWSURL:               "wss://termix.example.com/ws",
		LogLevel:                 "info",
		PreviewMaxBytes:          8192,
		HeartbeatIntervalSeconds: 15,
	}

	if err := config.SaveHostConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveHostConfig returned error: %v", err)
	}

	got, err := config.LoadHostConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadHostConfig returned error: %v", err)
	}
	if got.RelayWSURL != cfg.RelayWSURL {
		t.Fatalf("expected relay url %q, got %q", cfg.RelayWSURL, got.RelayWSURL)
	}
}

func TestDeriveHostConfig(t *testing.T) {
	cfg, err := config.DeriveHostConfig("https://termix.example.com")
	if err != nil {
		t.Fatalf("DeriveHostConfig returned error: %v", err)
	}
	if cfg.ControlAPIURL != "https://termix.example.com" {
		t.Fatalf("expected control api base url to stay at server root, got %q", cfg.ControlAPIURL)
	}
	if cfg.RelayWSURL != "wss://termix.example.com/ws" {
		t.Fatalf("expected relay ws url, got %q", cfg.RelayWSURL)
	}
}
```

Add to `go/cmd/termix/main_test.go`:

```go
func TestRunLoginStoresHostConfig(t *testing.T) {
	paths := testPaths(t)
	deps := testDeps(paths)
	deps.stdin = strings.NewReader("https://termix.example.com\nuser@example.com\nsecret\n")
	deps.hostname = func() (string, error) { return "devbox", nil }
	deps.newControlClient = func(string) (loginClient, error) {
		return &fakeLoginClient{
			response: &openapi.LoginResponse{
				AccessToken:      "access-token",
				RefreshToken:     "refresh-token",
				ExpiresInSeconds: 900,
				User: openapi.User{Id: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
				Device: openapi.Device{Id: uuid.MustParse("22222222-2222-2222-2222-222222222222")},
			},
		}, nil
	}

	if code := run(context.Background(), []string{"termix", "login"}, deps); code != 0 {
		t.Fatalf("expected login success, got exit code %d", code)
	}

	cfg, err := config.LoadHostConfig(filepath.Join(paths.ConfigDir, "host.json"))
	if err != nil {
		t.Fatalf("LoadHostConfig returned error: %v", err)
	}
	if cfg.RelayWSURL != "wss://termix.example.com/ws" {
		t.Fatalf("expected derived relay url, got %q", cfg.RelayWSURL)
	}
}
```

- [ ] **Step 2: Run the tests to confirm the config store does not exist yet**

Run:

```bash
cd go && go test ./tests ./cmd/termix -run 'TestHostConfigSaveAndLoadRoundTrip|TestDeriveHostConfig|TestRunLoginStoresHostConfig' -v
```

Expected:
- compile or test failure because `SaveHostConfig`, `LoadHostConfig`, and `DeriveHostConfig` do not exist yet

- [ ] **Step 3: Implement the config store and derived relay URL logic**

Create `go/internal/config/store.go`:

```go
package config

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func SaveHostConfig(path string, cfg HostConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func LoadHostConfig(path string) (HostConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return HostConfig{}, err
	}
	var cfg HostConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return HostConfig{}, err
	}
	return cfg, cfg.Validate()
}

func DeriveHostConfig(serverBaseURL string) (HostConfig, error) {
	u, err := url.Parse(serverBaseURL)
	if err != nil {
		return HostConfig{}, err
	}
	wsScheme := "wss"
	if strings.EqualFold(u.Scheme, "http") {
		wsScheme = "ws"
	}
	relayURL := *u
	relayURL.Scheme = wsScheme
	relayURL.Path = "/ws"
	return HostConfig{
		ServerBaseURL:            serverBaseURL,
		ControlAPIURL:            serverBaseURL,
		RelayWSURL:               relayURL.String(),
		LogLevel:                 "info",
		PreviewMaxBytes:          8192,
		HeartbeatIntervalSeconds: 15,
	}, nil
}
```

Modify `go/internal/config/paths.go`:

```go
type HostPaths struct {
	ConfigDir       string
	StateDir        string
	LogDir          string
	RunDir          string
	CredentialsFile string
	HostConfigFile  string
}
```

Set `HostConfigFile: filepath.Join(base, "host.json")` on macOS and `HostConfigFile: filepath.Join(configDir, "host.json")` on Linux.

- [ ] **Step 4: Save derived host config during `termix login`**

Modify `go/cmd/termix/main.go` inside `runLogin` after `resp, err := client.Login(...)`:

```go
	cfg, err := config.DeriveHostConfig(serverURL)
	if err != nil {
		return err
	}
	if err := config.SaveHostConfig(deps.paths.HostConfigFile, cfg); err != nil {
		return err
	}
```

Update test helper paths in `go/cmd/termix/main_test.go`:

```go
	return config.HostPaths{
		ConfigDir:       filepath.Join(base, "config"),
		StateDir:        filepath.Join(base, "state"),
		LogDir:          filepath.Join(base, "logs"),
		RunDir:          filepath.Join(base, "run"),
		CredentialsFile: filepath.Join(base, "config", "credentials.json"),
		HostConfigFile:  filepath.Join(base, "config", "host.json"),
	}
```

- [ ] **Step 5: Run the tests to verify the login path now persists relay config**

Run:

```bash
cd go && go test ./tests ./cmd/termix -run 'TestHostConfigSaveAndLoadRoundTrip|TestDeriveHostConfig|TestRunLoginStoresHostConfig' -v
```

Expected:
- PASS

- [ ] **Step 6: Commit the host config groundwork**

Run:

```bash
git add go/internal/config/paths.go go/internal/config/store.go go/cmd/termix/main.go go/cmd/termix/main_test.go go/tests/config_test.go
git commit -F - <<'EOF'
Persist relay-capable host config during login

Save a derived relay URL and host runtime settings at login time so
Phase 2 daemon and relay work can reuse stable local config instead
of inferring connection parameters ad hoc.

Constraint: Login still prompts only for the server URL
Rejected: Read relay URL only from environment | would make the host runtime path less reproducible
Confidence: high
Scope-risk: narrow
Directive: Keep server_base_url authoritative and derive relay/control URLs from it unless the product later adds explicit config editing
Tested: cd go && go test ./tests ./cmd/termix -run 'TestHostConfigSaveAndLoadRoundTrip|TestDeriveHostConfig|TestRunLoginStoresHostConfig' -v
EOF
```

### Task 2: Add Session Detail Reads for Relay Watch Authorization

**Files:**
- Modify: `openapi/control.openapi.yaml`
- Modify: `go/internal/persistence/sessions.go`
- Modify: `go/internal/controlapi/{server.go,client.go,client_test.go}`
- Modify: `go/tests/control_integration_test.go`

- [ ] **Step 1: Write the failing tests for owner-visible session reads**

Add to `go/internal/controlapi/client_test.go`:

```go
func TestGetSessionForViewerSetsBearerToken(t *testing.T) {
	client, err := New("https://termix.example.com", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/sessions/33333333-3333-3333-3333-333333333333" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"33333333-3333-3333-3333-333333333333",
				"user_id":"11111111-1111-1111-1111-111111111111",
				"host_device_id":"22222222-2222-2222-2222-222222222222",
				"tool":"codex",
				"launch_command":"codex",
				"cwd":"/tmp/project",
				"cwd_label":"project",
				"tmux_session_name":"termix_33333333-3333-3333-3333-333333333333",
				"status":"running"
			}`)),
			Request: r,
		}, nil
	}))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	session, err := client.GetSessionForViewer(context.Background(), "access-token", "33333333-3333-3333-3333-333333333333")
	if err != nil {
		t.Fatalf("GetSessionForViewer returned error: %v", err)
	}
	if session.Status != "running" {
		t.Fatalf("expected running status, got %q", session.Status)
	}
}
```

Add to `go/tests/control_integration_test.go`:

```go
func TestOwnerCanFetchSessionDetailAndForeignUserCannot(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run control-plane integration tests")
	}

	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	ownerHash, _ := auth.HashPassword("owner-secret")
	otherHash, _ := auth.HashPassword("other-secret")

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
```

- [ ] **Step 2: Run the tests to verify the route does not exist yet**

Run:

```bash
cd go && go test ./internal/controlapi ./tests -run 'TestGetSessionForViewerSetsBearerToken|TestOwnerCanFetchSessionDetailAndForeignUserCannot' -v
```

Expected:
- FAIL because the OpenAPI route and client method do not exist yet

- [ ] **Step 3: Add the contract-first session detail endpoint**

Add to `openapi/control.openapi.yaml`:

```yaml
  /sessions/{session_id}:
    get:
      operationId: getSession
      security:
        - bearerAuth: []
      parameters:
        - in: path
          name: session_id
          required: true
          schema:
            type: string
            format: uuid
      responses:
        "200":
          description: session detail
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Session'
        "404":
          description: session not found
```

Then regenerate:

```bash
make generate
```

Expected:
- generated OpenAPI client/server code now includes `GetSession`

- [ ] **Step 4: Implement persistence and server/client support**

Add to `go/internal/persistence/sessions.go`:

```go
func (s *Store) GetSessionForUser(ctx context.Context, sessionID string, userID string) (Session, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return Session{}, err
	}
	uid, err := parseUUID(userID)
	if err != nil {
		return Session{}, err
	}
	row := s.Pool.QueryRow(ctx, `
select id, user_id, host_device_id, name, tool, launch_command, cwd, cwd_label, tmux_session_name, status
from sessions
where id = $1 and user_id = $2
`, id, uid)

	var session Session
	var name pgtype.Text
	if err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.HostDeviceID,
		&name,
		&session.Tool,
		&session.LaunchCommand,
		&session.Cwd,
		&session.CwdLabel,
		&session.TmuxSessionName,
		&session.Status,
	); err != nil {
		return Session{}, err
	}
	session.Name = textPtr(name)
	return session, nil
}
```

Add to `go/internal/controlapi/server.go`:

```go
func (s *server) GetSession(c *gin.Context, sessionID openapi_types.UUID) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer claims"})
		return
	}
	session, err := s.store.GetSessionForUser(c.Request.Context(), sessionID.String(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	response, err := toOpenAPISession(session)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}
```

Add to `go/internal/controlapi/client.go`:

```go
func (c *Client) GetSessionForViewer(ctx context.Context, accessToken string, sessionID string) (*openapi.Session, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.GetSessionWithResponse(ctx, id, bearerEditor(accessToken))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, responseError("get session", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}
```

- [ ] **Step 5: Run the control-plane tests again**

Run:

```bash
make generate
cd go && go test ./internal/controlapi ./tests -run 'TestGetSessionForViewerSetsBearerToken|TestOwnerCanFetchSessionDetailAndForeignUserCannot' -v
```

Expected:
- PASS

- [ ] **Step 6: Commit the session detail contract**

Run:

```bash
git add openapi/control.openapi.yaml go/gen/openapi/control.gen.go go/internal/persistence/sessions.go go/internal/controlapi/client.go go/internal/controlapi/client_test.go go/internal/controlapi/server.go go/tests/control_integration_test.go
git commit -F - <<'EOF'
Add session detail reads for relay watch authorization

Expose a bearer-protected session detail read so the relay can verify
that a viewer is allowed to watch a session without becoming the
authority for session ownership.

Constraint: Relay must not read PostgreSQL directly
Rejected: Introduce a second internal service contract in the first relay slice | would add protocol surface before the watch path is stabilized
Confidence: high
Scope-risk: narrow
Directive: Keep user-visible session authorization in termix-control
Tested: make generate; cd go && go test ./internal/controlapi ./tests -run 'TestGetSessionForViewerSetsBearerToken|TestOwnerCanFetchSessionDetailAndForeignUserCannot' -v
EOF
```

### Task 3: Define the Relay Protocol Artifacts and Go Codec Layer

**Files:**
- Create: `schemas/ws/envelope.schema.json`
- Create: `schemas/ws/control.session_watch.schema.json`
- Create: `schemas/ws/binary_header.schema.json`
- Create: `go/internal/relayproto/envelope.go`
- Create: `go/internal/relayproto/frame.go`
- Test: `go/tests/relay_protocol_test.go`

- [ ] **Step 1: Write the failing protocol tests**

Create `go/tests/relay_protocol_test.go`:

```go
package tests

import (
	"bytes"
	"testing"

	"github.com/termix/termix/go/internal/relayproto"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	data, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:      relayproto.TypeSessionWatch,
		RequestID: "req-1",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	if err != nil {
		t.Fatalf("EncodeEnvelope returned error: %v", err)
	}
	env, err := relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope returned error: %v", err)
	}
	if env.Type != relayproto.TypeSessionWatch {
		t.Fatalf("expected session.watch, got %q", env.Type)
	}
}

func TestBinaryFrameRoundTrip(t *testing.T) {
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeSnapshotChunk,
		Header: map[string]any{
			"session_id": "session-1",
			"seq":        1,
			"is_last":    true,
		},
		Payload: []byte("snapshot-data"),
	})
	if err != nil {
		t.Fatalf("EncodeBinaryFrame returned error: %v", err)
	}
	decoded, err := relayproto.DecodeBinaryFrame(frame)
	if err != nil {
		t.Fatalf("DecodeBinaryFrame returned error: %v", err)
	}
	if decoded.FrameType != relayproto.FrameTypeSnapshotChunk {
		t.Fatalf("unexpected frame type: %d", decoded.FrameType)
	}
	if !bytes.Equal(decoded.Payload, []byte("snapshot-data")) {
		t.Fatalf("unexpected payload: %q", decoded.Payload)
	}
}
```

- [ ] **Step 2: Run the tests to confirm the codec layer does not exist yet**

Run:

```bash
cd go && go test ./tests -run 'TestEnvelopeRoundTrip|TestBinaryFrameRoundTrip' -v
```

Expected:
- FAIL with missing `internal/relayproto`

- [ ] **Step 3: Write the schema files and Go codec package**

Create `schemas/ws/envelope.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "RelayEnvelope",
  "type": "object",
  "required": ["type", "payload"],
  "properties": {
    "type": { "type": "string" },
    "request_id": { "type": ["string", "null"] },
    "payload": { "type": "object" }
  },
  "additionalProperties": false
}
```

Create `schemas/ws/control.session_watch.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "SessionWatch",
  "type": "object",
  "required": ["session_id"],
  "properties": {
    "session_id": { "type": "string" }
  },
  "additionalProperties": false
}
```

Create `schemas/ws/binary_header.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "RelayBinaryHeader",
  "type": "object",
  "required": ["session_id", "seq"],
  "properties": {
    "session_id": { "type": "string" },
    "seq": { "type": "integer", "minimum": 0 }
  }
}
```

Create `go/internal/relayproto/envelope.go`:

```go
package relayproto

import "encoding/json"

const (
	TypeHelloDaemon         = "hello.daemon"
	TypeHelloViewer         = "hello.viewer"
	TypeSessionWatch        = "session.watch"
	TypeSessionUnwatch      = "session.unwatch"
	TypeSessionJoined       = "session.joined"
	TypeSessionLeft         = "session.left"
	TypeSessionSnapshotReq  = "session.snapshot.request"
	TypeSessionSnapshotReady = "session.snapshot.ready"
	TypeSessionOnline       = "session.online"
	TypeSessionOffline      = "session.offline"
	TypeHeartbeat           = "heartbeat"
	TypeError               = "error"
)

type Envelope struct {
	Type      string         `json:"type"`
	RequestID string         `json:"request_id,omitempty"`
	Payload   map[string]any `json:"payload"`
}

func EncodeEnvelope(env Envelope) ([]byte, error) {
	return json.Marshal(env)
}

func DecodeEnvelope(data []byte) (Envelope, error) {
	var env Envelope
	err := json.Unmarshal(data, &env)
	return env, err
}
```

Create `go/internal/relayproto/frame.go`:

```go
package relayproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
)

const (
	FrameTypeTerminalOutput byte = 1
	FrameTypeSnapshotChunk  byte = 3
)

var magic = []byte("TMX1")

type BinaryFrame struct {
	FrameType byte
	Header    map[string]any
	Payload   []byte
}

func EncodeBinaryFrame(frame BinaryFrame) ([]byte, error) {
	header, err := json.Marshal(frame.Header)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 10+len(header)+len(frame.Payload))
	copy(buf[:4], magic)
	buf[4] = 1
	buf[5] = frame.FrameType
	binary.BigEndian.PutUint32(buf[6:10], uint32(len(header)))
	copy(buf[10:10+len(header)], header)
	copy(buf[10+len(header):], frame.Payload)
	return buf, nil
}

func DecodeBinaryFrame(data []byte) (BinaryFrame, error) {
	if len(data) < 10 || string(data[:4]) != "TMX1" {
		return BinaryFrame{}, errors.New("invalid frame magic")
	}
	headerLen := binary.BigEndian.Uint32(data[6:10])
	if len(data) < 10+int(headerLen) {
		return BinaryFrame{}, errors.New("truncated frame header")
	}
	var header map[string]any
	if err := json.Unmarshal(data[10:10+headerLen], &header); err != nil {
		return BinaryFrame{}, err
	}
	return BinaryFrame{
		FrameType: data[5],
		Header:    header,
		Payload:   data[10+headerLen:],
	}, nil
}
```

- [ ] **Step 4: Run the codec tests**

Run:

```bash
cd go && go test ./tests -run 'TestEnvelopeRoundTrip|TestBinaryFrameRoundTrip' -v
```

Expected:
- PASS

- [ ] **Step 5: Commit the shared relay protocol layer**

Run:

```bash
git add schemas/ws go/internal/relayproto go/tests/relay_protocol_test.go
git commit -F - <<'EOF'
Define the relay watch protocol artifacts

Lock down the control envelope and binary frame shapes before wiring
daemon and relay implementations so the transport contract stays
stable across future Android and Web viewers.

Constraint: The first Phase 2 slice must stay client-agnostic
Rejected: Delay the viewer-facing protocol until after relay bring-up | would make the first transport slice disposable
Confidence: high
Scope-risk: narrow
Directive: Extend the protocol by adding new message types, not by redefining envelope or binary frame fundamentals
Tested: cd go && go test ./tests -run 'TestEnvelopeRoundTrip|TestBinaryFrameRoundTrip' -v
EOF
```

### Task 4: Add tmux Snapshot and Control-Mode Stream Helpers

**Files:**
- Create: `go/internal/tmux/control.go`
- Test: `go/tests/tmux_control_test.go`

- [ ] **Step 1: Write the failing tests for snapshot capture and `%output` parsing**

Create `go/tests/tmux_control_test.go`:

```go
package tests

import (
	"testing"

	"github.com/termix/termix/go/internal/tmux"
)

func TestSnapshotCommandArgs(t *testing.T) {
	args := tmux.SnapshotArgs("termix_session-1")
	want := []string{"capture-pane", "-p", "-e", "-S", "-200", "-t", "termix_session-1:main.0"}
	if len(args) != len(want) {
		t.Fatalf("expected %d args, got %d", len(want), len(args))
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d: expected %q, got %q", i, want[i], args[i])
		}
	}
}

func TestParseOutputLine(t *testing.T) {
	event, ok := tmux.ParseControlLine("%output %1 hello world")
	if !ok {
		t.Fatal("expected %output line to parse")
	}
	if string(event.Payload) != "hello world" {
		t.Fatalf("unexpected payload: %q", event.Payload)
	}
}
```

- [ ] **Step 2: Run the tests to confirm the helper file is missing**

Run:

```bash
cd go && go test ./tests -run 'TestSnapshotCommandArgs|TestParseOutputLine' -v
```

Expected:
- FAIL because `SnapshotArgs` and `ParseControlLine` do not exist yet

- [ ] **Step 3: Implement snapshot and control-line parsing helpers**

Create `go/internal/tmux/control.go`:

```go
package tmux

import (
	"context"
	"os/exec"
	"strings"
)

type OutputEvent struct {
	PaneID  string
	Payload []byte
}

func SnapshotArgs(sessionName string) []string {
	return []string{"capture-pane", "-p", "-e", "-S", "-200", "-t", sessionName + ":main.0"}
}

func CaptureSnapshot(ctx context.Context, sessionName string) ([]byte, error) {
	return exec.CommandContext(ctx, "tmux", SnapshotArgs(sessionName)...).Output()
}

func ParseControlLine(line string) (OutputEvent, bool) {
	if !strings.HasPrefix(line, "%output ") {
		return OutputEvent{}, false
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return OutputEvent{}, false
	}
	return OutputEvent{
		PaneID:  parts[1],
		Payload: []byte(parts[2]),
	}, true
}
```

- [ ] **Step 4: Run the tmux helper tests**

Run:

```bash
cd go && go test ./tests -run 'TestSnapshotCommandArgs|TestParseOutputLine' -v
```

Expected:
- PASS

- [ ] **Step 5: Commit the tmux helper layer**

Run:

```bash
git add go/internal/tmux/control.go go/tests/tmux_control_test.go
git commit -F - <<'EOF'
Add tmux snapshot and control-mode helpers

Create a small host-side helper layer for current-screen capture and
control-mode output parsing so the daemon can answer watch requests
without embedding tmux command assembly everywhere.

Constraint: termixd remains the authority for snapshot generation
Rejected: Let relay reconstruct screen state from live output only | leaves new watchers blind when the session is idle
Confidence: high
Scope-risk: narrow
Directive: Keep tmux-specific wire parsing isolated under internal/tmux
Tested: cd go && go test ./tests -run 'TestSnapshotCommandArgs|TestParseOutputLine' -v
EOF
```

### Task 5: Add the Daemon-Side Relay Client and Session Publishing Hooks

**Files:**
- Create: `go/internal/relayclient/messages.go`
- Create: `go/internal/relayclient/client.go`
- Modify: `go/internal/session/{types.go,manager.go}`
- Modify: `go/cmd/termixd/main.go`
- Test: `go/tests/daemon_relay_test.go`
- Modify: `go/go.mod`

- [ ] **Step 1: Write the failing daemon relay tests**

Create `go/tests/daemon_relay_test.go`:

```go
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/session"
)

type fakeRelayClient struct {
	announced session.LocalSession
	snapshot  []byte
}

func (f *fakeRelayClient) AnnounceSession(_ context.Context, s session.LocalSession) error {
	f.announced = s
	return nil
}

func (f *fakeRelayClient) PublishSnapshot(_ context.Context, sessionID string, snapshot []byte) error {
	f.snapshot = snapshot
	return nil
}

func (f *fakeRelayClient) PublishOutput(context.Context, string, []byte) error { return nil }
func (f *fakeRelayClient) SetSnapshotHandler(func(context.Context, string) ([]byte, error)) {}

func TestManagerAnnouncesRunningSessionToRelay(t *testing.T) {
	relay := &fakeRelayClient{}
	manager := session.NewManager(session.ManagerOptions{
		Store: session.NewStore(t.TempDir()),
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{
				ServerBaseURL: "https://termix.example.com",
				UserID:        "11111111-1111-1111-1111-111111111111",
				DeviceID:      "22222222-2222-2222-2222-222222222222",
				AccessToken:   "access-token",
			}, nil
		},
		Control: &fakeControlClient{
			createResponse: &openapi.CreateSessionResponse{
				SessionId:       uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
				Status:          "starting",
			},
			updateResponse: &openapi.Session{
				Id:              uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				UserId:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				HostDeviceId:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Tool:            openapi.SessionToolCodex,
				LaunchCommand:   "codex",
				Cwd:             "/tmp/project",
				CwdLabel:        "project",
				TmuxSessionName: "termix_33333333-3333-3333-3333-333333333333",
				Status:          "running",
			},
		},
		Tmux: &fakeTmuxRunner{},
		Now: func() time.Time { return time.Date(2026, 4, 23, 17, 0, 0, 0, time.UTC) },
		Hostname: func() (string, error) { return "devbox", nil },
		DoctorChecks: func(context.Context) ([]string, error) { return []string{"tmux: ok"}, nil },
		Relay: relay,
	})

	_, err := manager.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool: "codex",
		Cwd:  "/tmp/project",
	})
	if err != nil {
		t.Fatalf("StartSession returned error: %v", err)
	}
	if relay.announced.SessionID == "" {
		t.Fatal("expected session announcement to relay")
	}
}
```

- [ ] **Step 2: Run the tests to confirm the relay client hook does not exist yet**

Run:

```bash
cd go && go test ./tests -run 'TestManagerAnnouncesRunningSessionToRelay' -v
```

Expected:
- FAIL because `ManagerOptions` does not yet accept relay publishing hooks

- [ ] **Step 3: Add the websocket dependency and daemon relay client package**

Run:

```bash
cd go && go get github.com/coder/websocket@latest
```

Create `go/internal/relayclient/messages.go`:

```go
package relayclient

import "github.com/termix/termix/go/internal/relayproto"

type OnlinePayload struct {
	SessionID string `json:"session_id"`
}

type SnapshotRequestPayload struct {
	SessionID string `json:"session_id"`
}

func HelloDaemonEnvelope(deviceID string) relayproto.Envelope {
	return relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": deviceID},
	}
}
```

Create `go/internal/relayclient/client.go`:

```go
package relayclient

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relayproto"
	"github.com/termix/termix/go/internal/session"
)

type Client struct {
	url             string
	accessToken     string
	deviceID        string
	conn            *websocket.Conn
	snapshotHandler func(context.Context, string) ([]byte, error)
}

func New(url string, accessToken string, deviceID string) *Client {
	return &Client{url: url, accessToken: accessToken, deviceID: deviceID}
}

func (c *Client) Connect(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + c.accessToken}},
	})
	if err != nil {
		return err
	}
	c.conn = conn
	data, err := relayproto.EncodeEnvelope(HelloDaemonEnvelope(c.deviceID))
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) AnnounceSession(ctx context.Context, s session.LocalSession) error {
	data, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": s.SessionID},
	})
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) PublishSnapshot(ctx context.Context, sessionID string, snapshot []byte) error {
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeSnapshotChunk,
		Header: map[string]any{
			"session_id": sessionID,
			"seq":        1,
			"is_last":    true,
		},
		Payload: snapshot,
	})
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageBinary, frame)
}

func (c *Client) PublishOutput(ctx context.Context, sessionID string, payload []byte) error {
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalOutput,
		Header: map[string]any{
			"session_id": sessionID,
			"seq":        1,
		},
		Payload: payload,
	})
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageBinary, frame)
}

func (c *Client) SetSnapshotHandler(fn func(context.Context, string) ([]byte, error)) {
	c.snapshotHandler = fn
}
```

- [ ] **Step 4: Wire the relay hook into the session manager and daemon bootstrap**

Modify `go/internal/session/types.go`:

```go
type RelayClient interface {
	AnnounceSession(context.Context, LocalSession) error
	PublishSnapshot(context.Context, string, []byte) error
	PublishOutput(context.Context, string, []byte) error
	SetSnapshotHandler(func(context.Context, string) ([]byte, error))
}
```

Add to `go/internal/session/manager.go`:

```go
type SnapshotFunc func(context.Context, string) ([]byte, error)

type Manager struct {
	daemonv1.UnimplementedDaemonServiceServer

	// existing fields...
	relay    RelayClient
	snapshot SnapshotFunc
}

type ManagerOptions struct {
	// existing fields...
	Relay    RelayClient
	Snapshot SnapshotFunc
}
```

In `NewManager`:

```go
if opts.Relay != nil && opts.Snapshot != nil {
	opts.Relay.SetSnapshotHandler(func(ctx context.Context, sessionID string) ([]byte, error) {
		s, err := opts.Store.Load(sessionID)
		if err != nil {
			return nil, err
		}
		return opts.Snapshot(ctx, s.TmuxSessionName)
	})
}

return &Manager{
	// existing fields...
	relay:    opts.Relay,
	snapshot: opts.Snapshot,
}
```

In `StartSession`, after `m.store.Save(localSession)`:

```go
if m.relay != nil {
	if err := m.relay.AnnounceSession(ctx, localSession); err != nil {
		return nil, err
	}
}
```

Modify `go/cmd/termixd/main.go`:

```go
	cfg, err := config.LoadHostConfig(paths.HostConfigFile)
	if err != nil {
		log.Fatal(err)
	}
	creds, err := credentials.Load(paths.CredentialsFile)
	if err != nil {
		log.Fatal(err)
	}
	relayClient := relayclient.New(cfg.RelayWSURL, creds.AccessToken, creds.DeviceID)
	if err := relayClient.Connect(context.Background()); err != nil {
		log.Fatal(err)
	}
```

Pass into `session.NewManager(...)`:

```go
		Relay: relayClient,
		Snapshot: func(ctx context.Context, sessionName string) ([]byte, error) {
			return tmux.CaptureSnapshot(ctx, sessionName)
		},
```

- [ ] **Step 5: Run the daemon relay tests**

Run:

```bash
cd go && go test ./tests -run 'TestManagerAnnouncesRunningSessionToRelay' -v
```

Expected:
- PASS

- [ ] **Step 6: Commit the daemon relay client groundwork**

Run:

```bash
git add go/go.mod go/go.sum go/internal/relayclient go/internal/session/types.go go/internal/session/manager.go go/cmd/termixd/main.go go/tests/daemon_relay_test.go
git commit -F - <<'EOF'
Connect termixd to the relay transport

Give the daemon an outbound relay client and session publishing hooks
so running sessions can be announced and later answer snapshot
requests without moving screen-state authority off the host.

Constraint: termixd remains the source of truth for current screen state
Rejected: Move snapshot assembly into relay | would couple watch correctness to cloud-side state reconstruction
Confidence: medium
Scope-risk: moderate
Directive: Keep relay publishing behind a small interface so session manager tests stay local and deterministic
Tested: cd go && go test ./tests -run 'TestManagerAnnouncesRunningSessionToRelay' -v
EOF
```

### Task 6: Implement the Relay WSS Server and Watch Handshake

**Files:**
- Create: `go/internal/relay/auth.go`
- Create: `go/internal/relay/registry.go`
- Create: `go/internal/relay/server.go`
- Create: `go/cmd/termix-relay/main.go`
- Test: `go/tests/relay_integration_test.go`

- [ ] **Step 1: Write the failing relay integration test**

Create `go/tests/relay_integration_test.go`:

```go
package tests

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relay"
	"github.com/termix/termix/go/internal/relayproto"
)

type fakeSessionAuthorizer struct{}

func (fakeSessionAuthorizer) AuthorizeWatch(context.Context, string, string) error { return nil }

func TestRelayWatchHandshakeRequestsSnapshotAndForwardsIt(t *testing.T) {
	server := relay.NewServer(fakeSessionAuthorizer{})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	daemonConn, _, err := websocket.Dial(context.Background(), "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemonConn.Close(websocket.StatusNormalClosure, "done")

	viewerConn, _, err := websocket.Dial(context.Background(), "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewerConn.Close(websocket.StatusNormalClosure, "done")

	daemonHello, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type: relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	if err := daemonConn.Write(context.Background(), websocket.MessageText, daemonHello); err != nil {
		t.Fatalf("daemon hello: %v", err)
	}

	online, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type: relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})
	if err := daemonConn.Write(context.Background(), websocket.MessageText, online); err != nil {
		t.Fatalf("session.online: %v", err)
	}

	viewerHello, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type: relayproto.TypeHelloViewer,
		Payload: map[string]any{},
	})
	if err := viewerConn.Write(context.Background(), websocket.MessageText, viewerHello); err != nil {
		t.Fatalf("viewer hello: %v", err)
	}

	watch, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type: relayproto.TypeSessionWatch,
		Payload: map[string]any{"session_id": "session-1"},
	})
	if err := viewerConn.Write(context.Background(), websocket.MessageText, watch); err != nil {
		t.Fatalf("session.watch: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to confirm relay server does not exist yet**

Run:

```bash
cd go && go test ./tests -run 'TestRelayWatchHandshakeRequestsSnapshotAndForwardsIt' -v
```

Expected:
- FAIL because `internal/relay` and `cmd/termix-relay` do not exist yet

- [ ] **Step 3: Implement authorization, registry, and WSS handshake**

Create `go/internal/relay/auth.go`:

```go
package relay

import "context"

type SessionAuthorizer interface {
	AuthorizeWatch(ctx context.Context, accessToken string, sessionID string) error
}
```

Create `go/internal/relay/registry.go`:

```go
package relay

import (
	"sync"

	"github.com/coder/websocket"
)

type registry struct {
	mu       sync.RWMutex
	daemons  map[string]*websocket.Conn
	watchers map[string]map[*websocket.Conn]struct{}
}

func newRegistry() *registry {
	return &registry{
		daemons:  make(map[string]*websocket.Conn),
		watchers: make(map[string]map[*websocket.Conn]struct{}),
	}
}
```

Create `go/internal/relay/server.go`:

```go
package relay

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/termix/termix/go/internal/relayproto"
)

type Server struct {
	auth SessionAuthorizer
	reg  *registry
}

func NewServer(auth SessionAuthorizer) *Server {
	return &Server{auth: auth, reg: newRegistry()}
}

func (s *Server) Handler() http.Handler {
	router := gin.New()
	router.GET("/ws", func(c *gin.Context) {
		conn, err := websocket.Accept(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		go s.serveConn(c.Request.Context(), conn, c.GetHeader("Authorization"))
	})
	return router
}

func (s *Server) serveConn(ctx context.Context, conn *websocket.Conn, authHeader string) {
	defer conn.Close(websocket.StatusNormalClosure, "done")
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if msgType != websocket.MessageText {
			continue
		}
		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			return
		}
		switch env.Type {
		case relayproto.TypeHelloDaemon:
			continue
		case relayproto.TypeSessionOnline:
			sessionID, _ := env.Payload["session_id"].(string)
			s.reg.mu.Lock()
			s.reg.daemons[sessionID] = conn
			s.reg.mu.Unlock()
		case relayproto.TypeHelloViewer:
			continue
		case relayproto.TypeSessionWatch:
			sessionID, _ := env.Payload["session_id"].(string)
			if err := s.auth.AuthorizeWatch(ctx, authHeader, sessionID); err != nil {
				errMsg, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
					Type:    relayproto.TypeError,
					Payload: map[string]any{"message": err.Error()},
				})
				_ = conn.Write(ctx, websocket.MessageText, errMsg)
				return
			}
			joined, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
				Type:    relayproto.TypeSessionJoined,
				Payload: map[string]any{"session_id": sessionID},
			})
			_ = conn.Write(ctx, websocket.MessageText, joined)
			req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
				Type:    relayproto.TypeSessionSnapshotReq,
				Payload: map[string]any{"session_id": sessionID},
			})
			s.reg.mu.RLock()
			daemonConn := s.reg.daemons[sessionID]
			s.reg.mu.RUnlock()
			if daemonConn != nil {
				_ = daemonConn.Write(ctx, websocket.MessageText, req)
			}
		}
	}
}
```

Create `go/cmd/termix-relay/main.go`:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/termix/termix/go/internal/relay"
)

type allowAllAuthorizer struct{}

func (allowAllAuthorizer) AuthorizeWatch(context.Context, string, string) error { return nil }

func main() {
	addr := os.Getenv("TERMIX_RELAY_LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	server := relay.NewServer(allowAllAuthorizer{})
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
```

- [ ] **Step 4: Run the relay integration tests**

Run:

```bash
cd go && go test ./tests -run 'TestRelayWatchHandshakeRequestsSnapshotAndForwardsIt' -v
```

Expected:
- PASS for the initial text-frame handshake

- [ ] **Step 5: Commit the relay server**

Run:

```bash
git add go/internal/relay go/cmd/termix-relay/main.go go/tests/relay_integration_test.go
git commit -F - <<'EOF'
Bring up the relay watch handshake

Add the first relay server path for daemon and viewer websocket
connections so watch requests can be authorized, mapped to active
sessions, and converted into snapshot requests back to the host.

Constraint: Relay stays light and does not become the source of truth for current screen state
Rejected: Persist watcher/session runtime state in relay | unnecessary for the first watch-only slice
Confidence: medium
Scope-risk: moderate
Directive: Keep relay state ephemeral and recover through reconnects rather than persistence
Tested: cd go && go test ./tests -run 'TestRelayWatchHandshakeRequestsSnapshotAndForwardsIt' -v
EOF
```

### Task 7: Finish the Watch Path, Verify the Slice, and Update the Ledger

**Files:**
- Modify: `go/tests/relay_integration_test.go`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Extend the relay integration test to assert snapshot and live output forwarding**

Add to `go/tests/relay_integration_test.go` after the initial handshake passes:

```go
	snapshotReady, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:    relayproto.TypeSessionSnapshotReady,
		Payload: map[string]any{"session_id": "session-1"},
	})
	if err := daemonConn.Write(context.Background(), websocket.MessageText, snapshotReady); err != nil {
		t.Fatalf("snapshot ready: %v", err)
	}

	snapshotFrame, _ := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeSnapshotChunk,
		Header: map[string]any{
			"session_id": "session-1",
			"seq":        1,
			"is_last":    true,
		},
		Payload: []byte("snapshot"),
	})
	if err := daemonConn.Write(context.Background(), websocket.MessageBinary, snapshotFrame); err != nil {
		t.Fatalf("snapshot frame: %v", err)
	}

	outputFrame, _ := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalOutput,
		Header: map[string]any{
			"session_id": "session-1",
			"seq":        2,
		},
		Payload: []byte("live-output"),
	})
	if err := daemonConn.Write(context.Background(), websocket.MessageBinary, outputFrame); err != nil {
		t.Fatalf("output frame: %v", err)
	}
```

Then assert the viewer receives:

```go
	_, data, err := viewerConn.Read(context.Background())
	if err != nil {
		t.Fatalf("viewer read snapshot metadata: %v", err)
	}
	env, err := relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("decode snapshot metadata: %v", err)
	}
	if env.Type != relayproto.TypeSessionSnapshotReady {
		t.Fatalf("expected snapshot ready, got %q", env.Type)
	}
```

- [ ] **Step 2: Run the full Phase 2 watch verification set**

Run:

```bash
make generate
cd go && go test ./...
cd go && go vet ./...
```

Expected:
- code generation succeeds
- relay protocol tests pass
- daemon relay tests pass
- relay integration tests pass
- no package fails `go vet`

- [ ] **Step 3: Update `docs/PROGRESS.md` after the implementation lands**

Apply this patch shape:

```md
## Completed
- [x] Draft the Phase 2 relay/watch foundation design.
- [x] Write the Phase 2 relay/watch foundation implementation plan.
- [x] Implement the Phase 2 relay/watch foundation.

## In Progress
- [ ] No active in-progress tasks.

## Next Up
1. Design and implement the control lease and remote input slice on top of the watch foundation.
2. Deferred: revisit `termix-admin-api` and admin Web UI when those surfaces are ready to be scheduled.
```

- [ ] **Step 4: Commit the completed Phase 2 watch foundation**

Run:

```bash
git add docs/PROGRESS.md schemas openapi go
git commit -F - <<'EOF'
Establish the Phase 2 relay and watch foundation

Connect the daemon to a lightweight relay, deliver an initial
snapshot to new watchers, and fan out live terminal output through a
generic viewer protocol that future Android and Web clients can share.

Constraint: Relay is not the authority for current screen state
Rejected: Implement remote control in the same slice | would raise sprint risk before the read path stabilizes
Confidence: medium
Scope-risk: broad
Directive: Preserve the watch protocol and keep control/input as a separate follow-on slice
Tested: make generate; cd go && go test ./...; cd go && go vet ./...
Not-tested: Browser or Android client integration
EOF
```

## Self-Review

Spec coverage checked:
- daemon-to-relay authenticated WSS is covered by Tasks 1 and 5
- viewer-to-relay generic watch protocol is covered by Tasks 2, 3, and 6
- initial snapshot delivery is covered by Tasks 4, 5, and 7
- live output fanout is covered by Tasks 4, 5, and 7
- future single-controller, non-preemptive control semantics are reserved in Task 3 without implementation
- relay remains non-authoritative for session metadata and screen state because authorization stays in `termix-control` and snapshots stay in `termixd`

Placeholder scan:
- removed placeholder terms
- every code-changing step includes concrete file paths and code
- every verification step includes exact commands and expected outcomes

Type consistency checked:
- relay control message names match the approved design doc
- `termixd` remains the authority for snapshots
- viewer role stays generic rather than Android-specific
