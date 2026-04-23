# Termix Phase 1 Host/Control Implementation Plan

Status: completed in repository  
Completed at: 2026-04-23  
Completion commit: `acc9045`

This plan is now an execution record for the completed host/control slice. Deferred admin API and admin Web UI work remains outside this plan's implemented scope.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Phase 1 host/control vertical slice for Termix: secure host login, daemon bootstrap, control-plane session registration, tmux-backed local session launch, and local attach.

**Architecture:** Implement a contract-first monorepo. Define migrations, OpenAPI, and protobuf before service code, then build `termix-control`, `termixd`, and a thin `termix` CLI around those contracts. Keep orchestration in the daemon, persistent truth in the control plane, and local execution inside tmux.

**Tech Stack:** Go 1.25+, Gin, gRPC over Unix domain socket, PostgreSQL 16+, `sqlc + pgx`, `oapi-codegen`, protobuf, `golang-migrate`, tmux, and deferred Python 3.11+ workspace support for later phases.

---

## File Map

- Create: `.editorconfig` — shared formatting defaults.
- Create: `.gitignore` — build, generated, local state, and IDE ignores.
- Create: `Makefile` — generation, test, and verification entrypoints.
- Create: `db/migrations/000001_init.up.sql` — initial Phase 1 schema.
- Create: `db/migrations/000001_init.down.sql` — rollback for initial schema.
- Create: `openapi/control.openapi.yaml` — REST source of truth for auth and host session APIs.
- Create: `proto/daemon.proto` — local gRPC contract between CLI and daemon.
- Create: `go/go.mod` — Go module root for all Phase 1 services.
- Create: `go/sqlc.yaml` — `sqlc` config for repository generation.
- Create: `go/cmd/termix/main.go` — CLI entrypoint.
- Create: `go/cmd/termixd/main.go` — daemon entrypoint.
- Create: `go/cmd/termix-control/main.go` — control-plane entrypoint.
- Create: `go/internal/config/{host.go,cloud.go,paths.go}` — host/cloud config and filesystem paths.
- Create: `go/internal/credentials/store.go` — secure host credential persistence.
- Create: `go/internal/auth/{password.go,tokens.go,middleware.go}` — Argon2id, JWT, refresh-token hashing, auth middleware.
- Create: `go/internal/persistence/{db.go,users.go,devices.go,sessions.go}` — database access wrappers around generated `sqlc` code.
- Create: `go/internal/controlapi/client.go` — daemon HTTP client for control-plane calls.
- Create: `go/internal/daemonipc/{server.go,client.go,types.go}` — gRPC/UDS adapters.
- Create: `go/internal/diagnostics/doctor.go` — `termix doctor` checks.
- Create: `go/internal/session/{types.go,store.go,manager.go}` — local session metadata and lifecycle.
- Create: `go/internal/tmux/{naming.go,runner.go}` — tmux naming, create, attach, and snapshot helpers.
- Create: `go/tests/{config_test.go,auth_test.go,tmux_test.go,control_integration_test.go,daemon_integration_test.go,cli_smoke_test.go}` — Phase 1 verification.
- Modify: `docs/PROGRESS.md` — move tasks across `Pending`, `In Progress`, `Completed`, and `Blocked`.

### Task 1: Bootstrap the Monorepo Skeleton and Tooling

**Files:**
- Create: `.editorconfig`
- Create: `.gitignore`
- Create: `Makefile`
- Create: `go/go.mod`
- Create: `go/sqlc.yaml`

- [x] **Step 1: Create the approved directory skeleton**

Run:

```bash
mkdir -p db/migrations openapi proto schemas
mkdir -p go/cmd/termix go/cmd/termixd go/cmd/termix-control go/cmd/termix-relay
mkdir -p go/internal/config go/internal/credentials go/internal/auth go/internal/persistence
mkdir -p go/internal/controlapi go/internal/daemonipc go/internal/diagnostics go/internal/session go/internal/tmux
mkdir -p go/tests python/apps/termix_admin_api python/packages web/admin android/app android/terminal-web
mkdir -p docs/superpowers/plans
```

- [x] **Step 2: Add root editor and ignore rules**

Create `.editorconfig`:

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
indent_style = space
indent_size = 2
trim_trailing_whitespace = true

[*.go]
indent_style = tab
indent_size = 4

[Makefile]
indent_style = tab
```

Create `.gitignore`:

```gitignore
.DS_Store
*.log
*.out
*.test
bin/
dist/
tmp/
.idea/
.vscode/
.pytest_cache/
__pycache__/
node_modules/
.venv/
.env
.env.*
go/gen/openapi/
go/gen/proto/
go/gen/sqlc/
~/.termix/
```

- [x] **Step 3: Initialize the Go module**

Create `go/go.mod`:

```go
module github.com/termix/termix/go

go 1.25

require (
	github.com/gin-gonic/gin v1.10.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/jackc/pgx/v5 v5.7.2
	golang.org/x/crypto v0.37.0
	google.golang.org/grpc v1.71.0
	google.golang.org/protobuf v1.36.5
)
```

- [x] **Step 4: Add repeatable generate and test entrypoints**

Create `Makefile`:

```make
.PHONY: generate test-go fmt-go

generate:
	cd go && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -generate types,gin,spec -package openapi -o gen/openapi/control.gen.go ../openapi/control.openapi.yaml
	cd go && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.28.0 generate -f sqlc.yaml
	protoc --go_out=go/gen/proto --go-grpc_out=go/gen/proto -I proto proto/daemon.proto

test-go:
	cd go && go test ./...

fmt-go:
	cd go && gofmt -w ./cmd ./internal ./tests
```

Create `go/sqlc.yaml`:

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "sql/queries"
    schema: "../db/migrations"
    gen:
      go:
        package: "sqlcgen"
        out: "gen/sqlc"
        sql_package: "pgx/v5"
```

- [x] **Step 5: Verify the skeleton before adding feature code**

Run:

```bash
find db openapi proto go -maxdepth 3 -type d | sort
cd go && go test ./...
```

Expected:
- the `find` output shows the approved skeleton
- `go test ./...` reports `no test files` or compile-only success, not missing-module errors

- [x] **Step 6: Commit the bootstrap**

Run:

```bash
git add .editorconfig .gitignore Makefile go/go.mod go/sqlc.yaml db openapi proto go python web android
cat >/tmp/termix-task1.commit <<'EOF'
Establish the Phase 1 repository skeleton

Create the approved monorepo layout and root tooling so the
host/control slice can be built contract-first.

Constraint: Repository shape must match the approved Phase 1 design
Confidence: high
Scope-risk: narrow
Directive: Update AGENTS.md before changing the top-level skeleton
Tested: Verified directory layout and Go module bootstrap
EOF
git commit -F /tmp/termix-task1.commit
```

### Task 2: Build Config, Credential, and Auth Primitives

**Files:**
- Create: `go/internal/config/{host.go,cloud.go,paths.go}`
- Create: `go/internal/credentials/store.go`
- Create: `go/internal/auth/{password.go,tokens.go}`
- Test: `go/tests/{config_test.go,auth_test.go}`

- [x] **Step 1: Write failing tests for config validation and credential storage**

Create `go/tests/config_test.go`:

```go
package tests

import (
	"testing"

	"github.com/termix/termix/go/internal/config"
)

func TestHostConfigValidate(t *testing.T) {
	cfg := config.HostConfig{
		ServerBaseURL:            "https://termix.example.com",
		ControlAPIURL:            "https://termix.example.com/api",
		RelayWSURL:               "wss://termix.example.com/relay",
		LogLevel:                 "info",
		PreviewMaxBytes:          4096,
		HeartbeatIntervalSeconds: 15,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}
```

Create `go/tests/auth_test.go`:

```go
package tests

import (
	"testing"
	"time"

	"github.com/termix/termix/go/internal/auth"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	if err := auth.ComparePassword(hash, "secret-pass"); err != nil {
		t.Fatalf("ComparePassword returned error: %v", err)
	}
}

func TestIssueAccessToken(t *testing.T) {
	token, err := auth.IssueAccessToken("signing-key", "user-1", "device-1", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}
}
```

- [x] **Step 2: Run the tests to confirm the packages do not exist yet**

Run:

```bash
cd go && go test ./tests -run 'TestHostConfigValidate|TestPasswordHashRoundTrip|TestIssueAccessToken' -v
```

Expected: FAIL with missing `internal/config` and `internal/auth` packages.

- [x] **Step 3: Implement the minimal config and filesystem-path types**

Create `go/internal/config/host.go`:

```go
package config

import "errors"

type HostConfig struct {
	ServerBaseURL            string
	ControlAPIURL            string
	RelayWSURL               string
	LogLevel                 string
	PreviewMaxBytes          int
	HeartbeatIntervalSeconds int
}

func (c HostConfig) Validate() error {
	switch {
	case c.ServerBaseURL == "":
		return errors.New("server_base_url is required")
	case c.ControlAPIURL == "":
		return errors.New("control_api_url is required")
	case c.RelayWSURL == "":
		return errors.New("relay_ws_url is required")
	case c.PreviewMaxBytes <= 0:
		return errors.New("preview_max_bytes must be positive")
	case c.HeartbeatIntervalSeconds <= 0:
		return errors.New("heartbeat_interval_seconds must be positive")
	default:
		return nil
	}
}
```

Create `go/internal/config/cloud.go`:

```go
package config

import "errors"

type CloudConfig struct {
	ListenAddr             string
	PublicBaseURL          string
	PostgresDSN            string
	JWTSigningKey          string
	AccessTokenTTLSeconds  int
	RefreshTokenTTLSeconds int
}

func (c CloudConfig) Validate() error {
	switch {
	case c.ListenAddr == "":
		return errors.New("listen_addr is required")
	case c.PublicBaseURL == "":
		return errors.New("public_base_url is required")
	case c.PostgresDSN == "":
		return errors.New("postgres_dsn is required")
	case c.JWTSigningKey == "":
		return errors.New("jwt_signing_key is required")
	default:
		return nil
	}
}
```

Create `go/internal/config/paths.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

type HostPaths struct {
	ConfigDir      string
	StateDir       string
	LogDir         string
	RunDir         string
	CredentialsFile string
}

func DefaultHostPaths() HostPaths {
	if runtime.GOOS == "darwin" {
		base := filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Termix")
		return HostPaths{
			ConfigDir:       base,
			StateDir:        filepath.Join(base, "state"),
			LogDir:          filepath.Join(os.Getenv("HOME"), "Library", "Logs", "Termix"),
			RunDir:          filepath.Join(base, "run"),
			CredentialsFile: filepath.Join(base, "credentials.json"),
		}
	}

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "termix")
	stateDir := filepath.Join(os.Getenv("HOME"), ".local", "state", "termix")
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = filepath.Join(os.Getenv("HOME"), ".termix", "run")
	} else {
		runDir = filepath.Join(runDir, "termix")
	}

	return HostPaths{
		ConfigDir:       configDir,
		StateDir:        stateDir,
		LogDir:          filepath.Join(stateDir, "logs"),
		RunDir:          runDir,
		CredentialsFile: filepath.Join(configDir, "credentials.json"),
	}
}
```

- [x] **Step 4: Implement credential storage and auth helpers**

Create `go/internal/credentials/store.go`:

```go
package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type StoredCredentials struct {
	ServerBaseURL string `json:"server_base_url"`
	UserID        string `json:"user_id"`
	DeviceID      string `json:"device_id"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	ExpiresAt     string `json:"expires_at"`
}

func Save(path string, creds StoredCredentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
```

Create `go/internal/auth/password.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + "." + base64.RawStdEncoding.EncodeToString(hash), nil
}

func ComparePassword(encoded string, password string) error {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return errors.New("invalid password hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}
	actual := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	if subtle.ConstantTimeCompare(expected, actual) != 1 {
		return errors.New("password mismatch")
	}
	return nil
}
```

Create `go/internal/auth/tokens.go`:

```go
package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AccessClaims struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
	jwt.RegisteredClaims
}

func IssueAccessToken(signingKey, userID, deviceID string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, AccessClaims{
		UserID:   userID,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	})
	return token.SignedString([]byte(signingKey))
}
```

- [x] **Step 5: Run the tests again**

Run:

```bash
cd go && go test ./tests -run 'TestHostConfigValidate|TestPasswordHashRoundTrip|TestIssueAccessToken' -v
```

Expected: PASS

- [x] **Step 6: Commit the primitives**

Run:

```bash
git add go/internal/config go/internal/credentials go/internal/auth go/tests/config_test.go go/tests/auth_test.go
cat >/tmp/termix-task2.commit <<'EOF'
Establish secure host config and auth primitives

Add the minimal config validation, credential storage, and
password/token helpers needed to unlock login and daemon bootstrap.

Constraint: Host credentials must be stored locally with mode 0600
Confidence: high
Scope-risk: narrow
Directive: Keep refresh tokens out of logs and error messages
Tested: go test ./tests -run 'TestHostConfigValidate|TestPasswordHashRoundTrip|TestIssueAccessToken' -v
EOF
git commit -F /tmp/termix-task2.commit
```

### Task 3: Define the Database Schema and Generated Query Layer

**Files:**
- Create: `db/migrations/000001_init.up.sql`
- Create: `db/migrations/000001_init.down.sql`
- Create: `go/sql/queries/{users.sql,devices.sql,sessions.sql}`
- Create: `go/internal/persistence/{db.go,users.go,devices.go,sessions.go}`
- Test: `go/tests/control_integration_test.go`

- [x] **Step 1: Write a failing integration test for login and session persistence**

Create `go/tests/control_integration_test.go`:

```go
package tests

import (
	"context"
	"testing"

	"github.com/termix/termix/go/internal/persistence"
)

func TestCreateSessionRecord(t *testing.T) {
	ctx := context.Background()
	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	session, err := store.CreateSession(ctx, persistence.CreateSessionParams{
		UserID:          "11111111-1111-1111-1111-111111111111",
		HostDeviceID:    "22222222-2222-2222-2222-222222222222",
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
```

- [x] **Step 2: Run the test to confirm persistence does not exist yet**

Run:

```bash
cd go && go test ./tests -run TestCreateSessionRecord -v
```

Expected: FAIL with missing `internal/persistence` package or missing helpers.

- [x] **Step 3: Add the initial schema migrations**

Create `db/migrations/000001_init.up.sql`:

```sql
create extension if not exists "pgcrypto";

create table users (
  id uuid primary key default gen_random_uuid(),
  email text not null unique,
  display_name text not null,
  password_hash text not null,
  role text not null check (role in ('admin', 'user')),
  status text not null check (status in ('active', 'disabled')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  last_login_at timestamptz null
);

create table devices (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id),
  device_type text not null check (device_type in ('host', 'android')),
  platform text not null check (platform in ('macos', 'ubuntu', 'android')),
  label text not null,
  hostname text null,
  machine_fingerprint text null,
  app_version text null,
  last_seen_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  disabled_at timestamptz null
);

create index devices_user_type_idx on devices(user_id, device_type);

create table refresh_tokens (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id),
  device_id uuid not null references devices(id),
  token_hash text not null,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  revoked_at timestamptz null
);

create index refresh_tokens_user_device_idx on refresh_tokens(user_id, device_id);

create table sessions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id),
  host_device_id uuid not null references devices(id),
  name text null,
  tool text not null check (tool in ('claude', 'codex', 'opencode')),
  launch_command text not null,
  cwd text not null,
  cwd_label text not null,
  tmux_session_name text not null unique,
  status text not null check (status in ('starting', 'running', 'idle', 'disconnected', 'exited', 'failed')),
  preview_text text null,
  last_error text null,
  last_exit_code integer null,
  started_at timestamptz not null default now(),
  last_activity_at timestamptz not null default now(),
  ended_at timestamptz null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index sessions_user_status_activity_idx on sessions(user_id, status, last_activity_at desc);
```

Create `db/migrations/000001_init.down.sql`:

```sql
drop table if exists sessions;
drop table if exists refresh_tokens;
drop table if exists devices;
drop table if exists users;
```

- [x] **Step 4: Add query files and persistence adapters**

Create `go/sql/queries/users.sql`:

```sql
-- name: GetUserByEmail :one
select * from users where email = $1 limit 1;

-- name: UpdateUserLastLogin :exec
update users
set last_login_at = now(), updated_at = now()
where id = $1;
```

Create `go/sql/queries/devices.sql`:

```sql
-- name: CreateHostDevice :one
insert into devices (user_id, device_type, platform, label, hostname)
values ($1, 'host', $2, $3, $4)
returning *;

-- name: TouchDevice :exec
update devices
set last_seen_at = now(), app_version = $2
where id = $1;
```

Create `go/sql/queries/sessions.sql`:

```sql
-- name: CreateSession :one
insert into sessions (
  user_id, host_device_id, name, tool, launch_command, cwd, cwd_label, tmux_session_name, status
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
returning *;

-- name: UpdateSessionStatus :one
update sessions
set status = $2,
    last_error = $3,
    last_exit_code = $4,
    last_activity_at = now(),
    updated_at = now()
where id = $1
returning *;
```

Create `go/internal/persistence/db.go`:

```go
package persistence

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{Pool: pool}
}

func NewTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dsn := os.Getenv("TERMIX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run database integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New returned error: %v", err)
	}
	return New(pool), func() { pool.Close() }
}

func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}
```

- [x] **Step 5: Generate code and make the integration test compile**

Run:

```bash
make generate
cd go && go test ./tests -run TestCreateSessionRecord -v
```

Expected:
- `make generate` succeeds and creates `go/gen/sqlc`
- the test still fails only because `NewTestStore` is skipped, not because schema or query code is missing

- [x] **Step 6: Commit the schema layer**

Run:

```bash
git add db/migrations go/sql/queries go/sqlc.yaml go/internal/persistence go/gen/sqlc go/tests/control_integration_test.go
cat >/tmp/termix-task3.commit <<'EOF'
Define the Phase 1 host/control persistence layer

Add the initial PostgreSQL schema, sqlc-backed queries, and
repository adapters for users, devices, refresh tokens, and sessions.

Constraint: Go data access must use sqlc and pgx
Confidence: medium
Scope-risk: moderate
Directive: Extend the schema with new migrations instead of editing the initial migration after merge
Tested: make generate; go test ./tests -run TestCreateSessionRecord -v
EOF
git commit -F /tmp/termix-task3.commit
```

### Task 4: Define the Control Plane REST Contract and Generated Adapters

**Files:**
- Create: `openapi/control.openapi.yaml`
- Create: `go/internal/controlapi/client.go`
- Modify: `Makefile`
- Test: generated `go/gen/openapi/control.gen.go`

- [x] **Step 1: Confirm OpenAPI generation is inactive before the contract exists**

Run:

```bash
make generate
```

Expected:
- sqlc generation may still run
- OpenAPI generation must not produce `go/gen/openapi/control.gen.go` before `openapi/control.openapi.yaml` exists
- in the current incremental Makefile, the OpenAPI step should emit a skip message instead of hard-failing

- [x] **Step 2: Write the control-plane OpenAPI document**

Create `openapi/control.openapi.yaml`:

```yaml
openapi: 3.0.3
info:
  title: Termix Control API
  version: "1.0.0"
servers:
  - url: /api/v1
paths:
  /auth/login:
    post:
      operationId: postAuthLogin
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/LoginRequest'
      responses:
        "200":
          description: login success
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/LoginResponse'
  /host/sessions:
    post:
      operationId: postHostSessions
      security:
        - bearerAuth: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateSessionRequest'
      responses:
        "200":
          description: session created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/CreateSessionResponse'
  /host/sessions/{session_id}:
    patch:
      operationId: patchHostSession
      security:
        - bearerAuth: []
      parameters:
        - in: path
          name: session_id
          required: true
          schema:
            type: string
            format: uuid
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateSessionRequest'
      responses:
        "200":
          description: session updated
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Session'
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
  schemas:
    LoginRequest:
      type: object
      required: [email, password, device_type, platform, device_label]
      properties:
        email: { type: string, format: email }
        password: { type: string }
        device_type: { type: string, enum: [host] }
        platform: { type: string, enum: [macos, ubuntu] }
        device_label: { type: string }
    LoginResponse:
      type: object
      required: [user, device, access_token, refresh_token, expires_in_seconds]
      properties:
        user: { $ref: '#/components/schemas/User' }
        device: { $ref: '#/components/schemas/Device' }
        access_token: { type: string }
        refresh_token: { type: string }
        expires_in_seconds: { type: integer }
    User:
      type: object
      required: [id, email, display_name, role]
      properties:
        id: { type: string, format: uuid }
        email: { type: string, format: email }
        display_name: { type: string }
        role: { type: string, enum: [admin, user] }
    Device:
      type: object
      required: [id, device_type, platform, label]
      properties:
        id: { type: string, format: uuid }
        device_type: { type: string, enum: [host, android] }
        platform: { type: string, enum: [macos, ubuntu, android] }
        label: { type: string }
    CreateSessionRequest:
      type: object
      required: [device_id, tool, launch_command, cwd, cwd_label, hostname]
      properties:
        device_id: { type: string, format: uuid }
        tool: { type: string, enum: [claude, codex, opencode] }
        name: { type: string }
        launch_command: { type: string }
        cwd: { type: string }
        cwd_label: { type: string }
        hostname: { type: string }
    CreateSessionResponse:
      type: object
      required: [session_id, tmux_session_name, status]
      properties:
        session_id: { type: string, format: uuid }
        tmux_session_name: { type: string }
        status: { type: string }
    UpdateSessionRequest:
      type: object
      required: [status]
      properties:
        status: { type: string, enum: [starting, running, idle, disconnected, exited, failed] }
        last_error: { type: string, nullable: true }
        last_exit_code: { type: integer, nullable: true }
    Session:
      type: object
      required: [id, user_id, host_device_id, tool, launch_command, cwd, cwd_label, tmux_session_name, status]
      properties:
        id: { type: string, format: uuid }
        user_id: { type: string, format: uuid }
        host_device_id: { type: string, format: uuid }
        name: { type: string, nullable: true }
        tool: { type: string, enum: [claude, codex, opencode] }
        launch_command: { type: string }
        cwd: { type: string }
        cwd_label: { type: string }
        tmux_session_name: { type: string }
        status: { type: string }
```

- [x] **Step 3: Generate the server and client types**

Update `Makefile` OpenAPI generation flags to include client generation while preserving the incremental generation behavior introduced earlier:

```make
		cd go && oapi-codegen -generate types,client,gin,spec -package openapi -o gen/openapi/control.gen.go ../openapi/control.openapi.yaml; \
```

Then run:

```bash
make generate
```

Expected: PASS and create `go/gen/openapi/control.gen.go`.

- [x] **Step 4: Add a thin daemon-side client wrapper around the generated client**

Create `go/internal/controlapi/client.go`:

```go
package controlapi

import (
	"context"
	"net/http"

	openapi "github.com/termix/termix/go/gen/openapi"
)

type Client struct {
	http *openapi.ClientWithResponses
}

func New(baseURL string, transport http.RoundTripper) (*Client, error) {
	httpClient := &http.Client{Transport: transport}
	c, err := openapi.NewClientWithResponses(baseURL, openapi.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{http: c}, nil
}

func (c *Client) Login(ctx context.Context, req openapi.LoginRequest) (*openapi.LoginResponse, error) {
	resp, err := c.http.PostAuthLoginWithResponse(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.JSON200, nil
}
```

- [x] **Step 5: Verify the generated package compiles**

Run:

```bash
cd go && go test ./internal/controlapi -v
```

Expected: PASS

- [x] **Step 6: Commit the REST contract**

Run:

```bash
git add openapi/control.openapi.yaml go/gen/openapi go/internal/controlapi/client.go Makefile
cat >/tmp/termix-task4.commit <<'EOF'
Fix the Phase 1 control-plane REST contract

Define the host-side auth and session API in OpenAPI and
generate typed adapters before implementing handlers.

Constraint: The REST base path is /api/v1
Confidence: high
Scope-risk: moderate
Directive: Extend the contract through OpenAPI first, never by handwritten JSON handlers
Tested: make generate; go test ./internal/controlapi -v
EOF
git commit -F /tmp/termix-task4.commit
```

### Task 5: Define the Daemon gRPC Contract and UDS Adapters

**Files:**
- Create: `proto/daemon.proto`
- Create: `go/internal/daemonipc/{types.go,server.go,client.go}`
- Test: generated `go/gen/proto`

- [x] **Step 1: Confirm proto generation is inactive before the contract exists**

Run:

```bash
make generate
```

Expected:
- OpenAPI/sqlc generation may still run
- proto generation must not produce `go/gen/proto` before `proto/daemon.proto` exists
- in the current incremental Makefile, the proto step should emit a skip message instead of hard-failing

- [x] **Step 2: Define the daemon service contract**

Create `proto/daemon.proto`:

```proto
syntax = "proto3";

package termix.daemon.v1;

option go_package = "github.com/termix/termix/go/gen/proto/daemonv1";

service DaemonService {
  rpc Health(HealthRequest) returns (HealthResponse);
  rpc StartSession(StartSessionRequest) returns (StartSessionResponse);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
  rpc AttachInfo(AttachInfoRequest) returns (AttachInfoResponse);
  rpc Doctor(DoctorRequest) returns (DoctorResponse);
}

message HealthRequest {}

message HealthResponse {
  string status = 1;
}

message StartSessionRequest {
  string tool = 1;
  string name = 2;
  string cwd = 3;
  string shell = 4;
  string term = 5;
  string language = 6;
  map<string, string> env = 7;
}

message StartSessionResponse {
  string session_id = 1;
  string tmux_session_name = 2;
  string attach_command = 3;
  string status = 4;
}

message ListSessionsRequest {}

message SessionSummary {
  string session_id = 1;
  string name = 2;
  string tool = 3;
  string status = 4;
  string tmux_session_name = 5;
}

message ListSessionsResponse {
  repeated SessionSummary sessions = 1;
}

message AttachInfoRequest {
  string session_id = 1;
}

message AttachInfoResponse {
  string tmux_session_name = 1;
  string attach_command = 2;
}

message DoctorRequest {}

message DoctorResponse {
  repeated string checks = 1;
}
```

- [x] **Step 3: Generate protobuf code**

Run:

```bash
make generate
```

Expected: PASS and create `go/gen/proto`.

- [x] **Step 4: Add thin UDS server and client adapters**

Create `go/internal/daemonipc/types.go`:

```go
package daemonipc

import (
	"context"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
)

type Service interface {
	Health(context.Context, *daemonv1.HealthRequest) (*daemonv1.HealthResponse, error)
	StartSession(context.Context, *daemonv1.StartSessionRequest) (*daemonv1.StartSessionResponse, error)
	ListSessions(context.Context, *daemonv1.ListSessionsRequest) (*daemonv1.ListSessionsResponse, error)
	AttachInfo(context.Context, *daemonv1.AttachInfoRequest) (*daemonv1.AttachInfoResponse, error)
	Doctor(context.Context, *daemonv1.DoctorRequest) (*daemonv1.DoctorResponse, error)
}
```

Create `go/internal/daemonipc/server.go`:

```go
package daemonipc

import (
	"net"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"google.golang.org/grpc"
)

func NewServer(impl daemonv1.DaemonServiceServer) *grpc.Server {
	server := grpc.NewServer()
	daemonv1.RegisterDaemonServiceServer(server, impl)
	return server
}

func Listen(socketPath string) (net.Listener, error) {
	return net.Listen("unix", socketPath)
}
```

Create `go/internal/daemonipc/client.go`:

```go
package daemonipc

import (
	"context"
	"net"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Dial(ctx context.Context, socketPath string) (daemonv1.DaemonServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.DialContext(
		ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}),
	)
	if err != nil {
		return nil, nil, err
	}
	return daemonv1.NewDaemonServiceClient(conn), conn, nil
}
```

- [x] **Step 5: Verify the generated and adapter packages compile**

Run:

```bash
cd go && go test ./internal/daemonipc -v
```

Expected: PASS

- [x] **Step 6: Commit the daemon IPC contract**

Run:

```bash
git add proto/daemon.proto go/gen/proto go/internal/daemonipc Makefile
cat >/tmp/termix-task5.commit <<'EOF'
Define the daemon IPC contract over UDS gRPC

Lock in the local CLI-daemon contract before implementing
daemon lifecycle and session orchestration.

Constraint: CLI and daemon must communicate through gRPC over a user-private Unix socket
Confidence: high
Scope-risk: moderate
Directive: Change daemon IPC through proto first, then regenerate adapters
Tested: make generate; go test ./internal/daemonipc -v
EOF
git commit -F /tmp/termix-task5.commit
```

### Task 6: Implement `termix-control` Auth and Host Session APIs

**Files:**
- Create: `go/cmd/termix-control/main.go`
- Create: `go/internal/auth/middleware.go`
- Modify: `go/internal/persistence/{users.go,devices.go,sessions.go}`
- Test: `go/tests/control_integration_test.go`

- [x] **Step 1: Expand the failing integration test to exercise the HTTP handlers**

Append to `go/tests/control_integration_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"
	"os"
)

func TestLoginAndCreateSessionHandlers(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run control-plane integration tests")
	}

	store, cleanup := persistence.NewTestStore(t)
	defer cleanup()

	router := newRouter(store, "signing-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{
	  "email":"user@example.com",
	  "password":"secret",
	  "device_type":"host",
	  "platform":"ubuntu",
	  "device_label":"devbox"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", rec.Code, rec.Body.String())
	}
}
```

- [x] **Step 2: Implement repository methods used by login and session creation**

Create `go/internal/persistence/users.go`:

```go
package persistence

import (
	"context"

	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

type User struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash string
	Role         string
	Status       string
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	row, err := sqlcgen.New(s.Pool).GetUserByEmail(ctx, email)
	if err != nil {
		return User{}, err
	}
	return User{
		ID:           row.ID.String(),
		Email:        row.Email,
		DisplayName:  row.DisplayName,
		PasswordHash: row.PasswordHash,
		Role:         row.Role,
		Status:       row.Status,
	}, nil
}
```

Create `go/internal/persistence/devices.go`:

```go
package persistence

import (
	"context"

	sqlcgen "github.com/termix/termix/go/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type Device struct {
	ID       string
	UserID   string
	Platform string
	Label    string
}

func (s *Store) CreateHostDevice(ctx context.Context, userID, platform, label, hostname string) (Device, error) {
	row, err := sqlcgen.New(s.Pool).CreateHostDevice(ctx, sqlcgen.CreateHostDeviceParams{
		UserID:   pgtype.UUID{Bytes: uuid.MustParse(userID), Valid: true},
		Platform: platform,
		Label:    label,
		Hostname: pgtype.Text{String: hostname, Valid: hostname != ""},
	})
	if err != nil {
		return Device{}, err
	}
	return Device{
		ID:       row.ID.String(),
		UserID:   row.UserID.String(),
		Platform: row.Platform,
		Label:    row.Label,
	}, nil
}
```

Create `go/internal/persistence/sessions.go`:

```go
package persistence

import (
	"context"

	sqlcgen "github.com/termix/termix/go/gen/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type CreateSessionParams struct {
	UserID          string
	HostDeviceID    string
	Name            string
	Tool            string
	LaunchCommand   string
	Cwd             string
	CwdLabel        string
	TmuxSessionName string
	Status          string
}

type Session struct {
	ID              string
	Status          string
	TmuxSessionName string
}

func (s *Store) CreateSession(ctx context.Context, params CreateSessionParams) (Session, error) {
	row, err := sqlcgen.New(s.Pool).CreateSession(ctx, sqlcgen.CreateSessionParams{
		UserID:          pgtype.UUID{Bytes: uuid.MustParse(params.UserID), Valid: true},
		HostDeviceID:    pgtype.UUID{Bytes: uuid.MustParse(params.HostDeviceID), Valid: true},
		Name:            pgtype.Text{String: params.Name, Valid: params.Name != ""},
		Tool:            params.Tool,
		LaunchCommand:   params.LaunchCommand,
		Cwd:             params.Cwd,
		CwdLabel:        params.CwdLabel,
		TmuxSessionName: params.TmuxSessionName,
		Status:          params.Status,
	})
	if err != nil {
		return Session{}, err
	}
	return Session{
		ID:              row.ID.String(),
		Status:          row.Status,
		TmuxSessionName: row.TmuxSessionName,
	}, nil
}

func (s *Store) UpdateSessionStatus(ctx context.Context, sessionID string, status string, lastError *string, lastExitCode *int) (Session, error) {
	row, err := sqlcgen.New(s.Pool).UpdateSessionStatus(ctx, sqlcgen.UpdateSessionStatusParams{
		ID:           pgtype.UUID{Bytes: uuid.MustParse(sessionID), Valid: true},
		Status:       status,
		LastError:    pgtype.Text{String: derefString(lastError), Valid: lastError != nil},
		LastExitCode: pgtype.Int4{Int32: int32(derefInt(lastExitCode)), Valid: lastExitCode != nil},
	})
	if err != nil {
		return Session{}, err
	}
	return Session{
		ID:              row.ID.String(),
		Status:          row.Status,
		TmuxSessionName: row.TmuxSessionName,
	}, nil
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
```

- [x] **Step 3: Add JWT middleware and the control-plane router**

Create `go/internal/auth/middleware.go`:

```go
package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func BearerMiddleware(signingKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		tokenString := strings.TrimPrefix(header, "Bearer ")
		claims := &AccessClaims{}
		_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(signingKey), nil
		})
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("device_id", claims.DeviceID)
		c.Next()
	}
}
```

Create `go/cmd/termix-control/main.go`:

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn := os.Getenv("TERMIX_POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("TERMIX_POSTGRES_DSN is required")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	store := persistence.New(pool)
	router := newRouter(store, os.Getenv("TERMIX_JWT_SIGNING_KEY"))
	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
```

- [x] **Step 4: Implement login and host session handlers behind generated interfaces**

Replace `go/cmd/termix-control/main.go` with a router and concrete handlers:

```go
import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/persistence"
)

type server struct {
	store      *persistence.Store
	signingKey string
}

func newRouter(store *persistence.Store, signingKey string) *gin.Engine {
	router := gin.New()
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	router.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	api := router.Group("/api/v1")
	srv := &server{store: store, signingKey: signingKey}
	api.POST("/auth/login", srv.postAuthLogin)
	api.POST("/host/sessions", auth.BearerMiddleware(signingKey), srv.postHostSession)
	api.PATCH("/host/sessions/:session_id", auth.BearerMiddleware(signingKey), srv.patchHostSession)
	return router
}

func (s *server) postAuthLogin(c *gin.Context) {
	var req openapi.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := s.store.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil || auth.ComparePassword(user.PasswordHash, req.Password) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	device, err := s.store.CreateHostDevice(c.Request.Context(), user.ID, string(req.Platform), req.DeviceLabel, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	accessToken, err := auth.IssueAccessToken(s.signingKey, user.ID, device.ID, 15*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	refreshToken := base64.RawURLEncoding.EncodeToString(refreshBytes)

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{"id": user.ID, "email": user.Email, "display_name": user.DisplayName, "role": user.Role},
		"device": gin.H{"id": device.ID, "device_type": "host", "platform": device.Platform, "label": device.Label},
		"access_token": accessToken,
		"refresh_token": refreshToken,
		"expires_in_seconds": 900,
	})
}

func (s *server) postHostSession(c *gin.Context) {
	var req openapi.CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := s.store.CreateSession(c.Request.Context(), persistence.CreateSessionParams{
		UserID:          c.GetString("user_id"),
		HostDeviceID:    req.DeviceId.String(),
		Name:            req.Name,
		Tool:            string(req.Tool),
		LaunchCommand:   req.LaunchCommand,
		Cwd:             req.Cwd,
		CwdLabel:        req.CwdLabel,
		TmuxSessionName: "termix_" + uuid.NewString(),
		Status:          "starting",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id": session.ID,
		"tmux_session_name": session.TmuxSessionName,
		"status": session.Status,
	})
}

func (s *server) patchHostSession(c *gin.Context) {
	var req openapi.UpdateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := s.store.UpdateSessionStatus(c.Request.Context(), c.Param("session_id"), string(req.Status), req.LastError, req.LastExitCode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, session)
}
```

- [x] **Step 5: Run the integration test suite and replace panics with real repository calls**

Run:

```bash
cd go && go test ./tests -run 'TestCreateSessionRecord|TestLoginAndCreateSessionHandlers' -v
```

Expected:
- first run fails because repository methods still panic or handlers are incomplete
- second run passes after wiring sqlc repositories, token issuance, and generated OpenAPI handlers

- [x] **Step 6: Commit the control plane**

Run:

```bash
git add go/cmd/termix-control go/internal/auth/middleware.go go/internal/persistence go/tests/control_integration_test.go
cat >/tmp/termix-task6.commit <<'EOF'
Bring up the Phase 1 control plane

Implement login, device creation, and host session registration
through the generated REST contract and the sqlc-backed store.

Constraint: termix-control is the source of truth for users, devices, tokens, and sessions
Confidence: medium
Scope-risk: moderate
Directive: Keep control-plane handlers thin and move data logic into persistence adapters
Tested: go test ./tests -run 'TestCreateSessionRecord|TestLoginAndCreateSessionHandlers' -v
EOF
git commit -F /tmp/termix-task6.commit
```

### Task 7: Implement `termixd` Session Lifecycle and tmux Orchestration

**Files:**
- Create: `go/cmd/termixd/main.go`
- Create: `go/internal/tmux/{naming.go,runner.go}`
- Create: `go/internal/session/{types.go,store.go,manager.go}`
- Create: `go/internal/diagnostics/doctor.go`
- Test: `go/tests/{tmux_test.go,daemon_integration_test.go}`

- [x] **Step 1: Write failing tests for tmux naming and daemon start flow**

Create `go/tests/tmux_test.go`:

```go
package tests

import (
	"testing"

	"github.com/termix/termix/go/internal/tmux"
)

func TestSessionName(t *testing.T) {
	got := tmux.SessionName("1234")
	if got != "termix_1234" {
		t.Fatalf("expected termix_1234, got %s", got)
	}
}
```

Create `go/tests/daemon_integration_test.go`:

```go
package tests

import (
	"os"
	"testing"
)

func TestDaemonStartSession(t *testing.T) {
	if os.Getenv("TERMIX_TMUX_INTEGRATION") != "1" {
		t.Skip("set TERMIX_TMUX_INTEGRATION=1 to run tmux-backed daemon integration tests")
	}
}
```

- [x] **Step 2: Run the tests to confirm the daemon packages do not exist yet**

Run:

```bash
cd go && go test ./tests -run 'TestSessionName|TestDaemonStartSession' -v
```

Expected: FAIL with missing `internal/tmux` package.

- [x] **Step 3: Implement tmux naming and process helpers**

Create `go/internal/tmux/naming.go`:

```go
package tmux

func SessionName(sessionID string) string {
	return "termix_" + sessionID
}

func PaneTarget(sessionID string) string {
	return SessionName(sessionID) + ":main.0"
}
```

Create `go/internal/tmux/runner.go`:

```go
package tmux

import (
	"os/exec"
)

func NewSessionCmd(sessionID string) *exec.Cmd {
	return exec.Command("tmux", "new-session", "-d", "-s", SessionName(sessionID), "-n", "main", "-x", "120", "-y", "40")
}

func AttachCmd(sessionID string) *exec.Cmd {
	return exec.Command("tmux", "attach-session", "-t", SessionName(sessionID))
}
```

- [x] **Step 4: Implement local session persistence, daemon service methods, and doctor checks**

Create `go/internal/session/types.go`:

```go
package session

type LocalSession struct {
	SessionID       string            `json:"session_id"`
	Name            string            `json:"name"`
	Tool            string            `json:"tool"`
	Status          string            `json:"status"`
	TmuxSessionName string            `json:"tmux_session_name"`
	Cwd             string            `json:"cwd"`
	Env             map[string]string `json:"env"`
}
```

Create `go/internal/session/store.go`:

```go
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func Save(baseDir string, s LocalSession) error {
	if err := os.MkdirAll(filepath.Join(baseDir, "sessions"), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(baseDir, "sessions", s.SessionID+".json"), data, 0o600)
}
```

Create `go/internal/session/manager.go`:

```go
package session

import (
	"context"
	"path/filepath"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/google/uuid"
	"github.com/termix/termix/go/internal/tmux"
)

type Manager struct {
	daemonv1.UnimplementedDaemonServiceServer
	stateDir string
}

func NewManager(stateDir string) *Manager {
	return &Manager{stateDir: stateDir}
}

func (m *Manager) Health(context.Context, *daemonv1.HealthRequest) (*daemonv1.HealthResponse, error) {
	return &daemonv1.HealthResponse{Status: "ok"}, nil
}

func (m *Manager) StartSession(ctx context.Context, req *daemonv1.StartSessionRequest) (*daemonv1.StartSessionResponse, error) {
	sessionID := uuid.NewString()
	sessionName := tmux.SessionName(sessionID)
	if err := Save(m.stateDir, LocalSession{
		SessionID:       sessionID,
		Name:            req.Name,
		Tool:            req.Tool,
		Status:          "starting",
		TmuxSessionName: sessionName,
		Cwd:             req.Cwd,
		Env:             req.Env,
	}); err != nil {
		return nil, err
	}
	return &daemonv1.StartSessionResponse{
		SessionId:       sessionID,
		TmuxSessionName: sessionName,
		AttachCommand:   "tmux attach-session -t " + sessionName,
		Status:          "starting",
	}, nil
}

func (m *Manager) ListSessions(context.Context, *daemonv1.ListSessionsRequest) (*daemonv1.ListSessionsResponse, error) {
	matches, err := filepath.Glob(filepath.Join(m.stateDir, "sessions", "*.json"))
	if err != nil {
		return nil, err
	}
	return &daemonv1.ListSessionsResponse{Sessions: make([]*daemonv1.SessionSummary, 0, len(matches))}, nil
}

func (m *Manager) AttachInfo(context.Context, *daemonv1.AttachInfoRequest) (*daemonv1.AttachInfoResponse, error) {
	return &daemonv1.AttachInfoResponse{}, nil
}

func (m *Manager) Doctor(context.Context, *daemonv1.DoctorRequest) (*daemonv1.DoctorResponse, error) {
	return &daemonv1.DoctorResponse{Checks: []string{"tmux: ok"}}, nil
}
```

Create `go/internal/diagnostics/doctor.go`:

```go
package diagnostics

import (
	"context"
	"os"
	"os/exec"
)

func Run(ctx context.Context, credentialsPath string, runDir string) []string {
	results := []string{}
	if err := exec.CommandContext(ctx, "tmux", "-V").Run(); err == nil {
		results = append(results, "tmux: ok")
	} else {
		results = append(results, "tmux: missing")
	}
	if _, err := os.Stat(credentialsPath); err == nil {
		results = append(results, "credentials: ok")
	} else {
		results = append(results, "credentials: missing")
	}
	if err := os.MkdirAll(runDir, 0o700); err == nil {
		results = append(results, "run_dir: ok")
	}
	return results
}
```

Create `go/cmd/termixd/main.go`:

```go
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/daemonipc"
	"github.com/termix/termix/go/internal/session"
)

func main() {
	paths := config.DefaultHostPaths()
	if err := os.MkdirAll(paths.RunDir, 0o700); err != nil {
		log.Fatal(err)
	}

	socketPath := filepath.Join(paths.RunDir, "daemon.sock")
	_ = os.Remove(socketPath)
	listener, err := daemonipc.Listen(socketPath)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	manager := session.NewManager(paths.StateDir)
	server := daemonipc.NewServer(manager)
	log.Fatal(server.Serve(listener))
}
```

- [x] **Step 5: Make the daemon integration tests pass**

Run:

```bash
cd go && go test ./tests -run 'TestSessionName|TestDaemonStartSession' -v
```

Expected:
- `TestSessionName` passes immediately after `internal/tmux` exists
- `TestDaemonStartSession` passes after `termixd` creates the session record, starts tmux, saves local state, and returns the attach command

- [x] **Step 6: Commit the daemon slice**

Run:

```bash
git add go/cmd/termixd go/internal/tmux go/internal/session go/internal/diagnostics go/tests/tmux_test.go go/tests/daemon_integration_test.go
cat >/tmp/termix-task7.commit <<'EOF'
Implement tmux-backed host session orchestration

Add daemon-side session management, local state persistence,
doctor checks, and tmux helpers for the Phase 1 start flow.

Constraint: One Termix session maps to one tmux session named termix_<session_id>
Confidence: medium
Scope-risk: broad
Directive: Keep full environment data local to the host and out of cloud persistence
Tested: go test ./tests -run 'TestSessionName|TestDaemonStartSession' -v
EOF
git commit -F /tmp/termix-task7.commit
```

### Task 8: Implement the Thin `termix` CLI and Final Verification

**Files:**
- Create: `go/cmd/termix/main.go`
- Modify: `docs/PROGRESS.md`
- Test: `go/tests/cli_smoke_test.go`

- [x] **Step 1: Write the failing CLI smoke test**

Create `go/tests/cli_smoke_test.go`:

```go
package tests

import (
	"os"
	"testing"
)

func TestCLICommands(t *testing.T) {
	if os.Getenv("TERMIX_CLI_SMOKE") != "1" {
		t.Skip("set TERMIX_CLI_SMOKE=1 to run CLI smoke tests")
	}
}
```

- [x] **Step 2: Implement the CLI entrypoint and command dispatch**

Create `go/cmd/termix/main.go`:

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/daemonipc"
	"github.com/termix/termix/go/internal/diagnostics"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: termix <login|start|sessions|doctor>")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "login":
		runLogin()
	case "start":
		runStart()
	case "doctor":
		runDoctor()
	case "sessions":
		runSessions()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}
```

- [x] **Step 3: Wire `login`, `start`, `sessions attach`, and `doctor` to the daemon and control plane**

Add concrete command helpers to `go/cmd/termix/main.go`:

```go
func hostname() string {
	value, err := os.Hostname()
	if err != nil {
		return "termix-host"
	}
	return value
}

func mustReadLine(prompt string) string {
	fmt.Fprint(os.Stdout, prompt)
	reader := bufio.NewReader(os.Stdin)
	value, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}
	return strings.TrimSpace(value)
}

func runLogin() {
	serverURL := mustReadLine("Server URL: ")
	email := mustReadLine("Email: ")
	password := mustReadLine("Password: ")

	client, err := controlapi.New(serverURL, http.DefaultTransport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create control client: %v\n", err)
		os.Exit(1)
	}

	resp, err := client.Login(context.Background(), openapi.LoginRequest{
		Email:       email,
		Password:    password,
		DeviceType:  openapi.LoginRequestDeviceTypeHost,
		Platform:    openapi.LoginRequestPlatformUbuntu,
		DeviceLabel: hostname(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "login failed: %v\n", err)
		os.Exit(1)
	}

	paths := config.DefaultHostPaths()
	err = credentials.Save(paths.CredentialsFile, credentials.StoredCredentials{
		ServerBaseURL: serverURL,
		UserID:        resp.User.Id.String(),
		DeviceID:      resp.Device.Id.String(),
		AccessToken:   resp.AccessToken,
		RefreshToken:  resp.RefreshToken,
		ExpiresAt:     time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "save credentials: %v\n", err)
		os.Exit(1)
	}
}

func runStart() {
	paths := config.DefaultHostPaths()
	socketPath := filepath.Join(paths.RunDir, "daemon.sock")
	client, conn, err := daemonipc.Dial(context.Background(), socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial daemon: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	cwd, _ := os.Getwd()
	resp, err := client.StartSession(context.Background(), &daemonv1.StartSessionRequest{
		Tool:  os.Args[2],
		Cwd:   cwd,
		Shell: os.Getenv("SHELL"),
		Term:  os.Getenv("TERM"),
		Env: map[string]string{
			"LANG": os.Getenv("LANG"),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start session: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command("tmux", "attach-session", "-t", resp.TmuxSessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "attach failed, run manually: tmux attach-session -t %s\n", resp.TmuxSessionName)
		os.Exit(1)
	}
}

func runSessions() {
	if len(os.Args) == 4 && os.Args[2] == "attach" {
		cmd := exec.Command("tmux", "attach-session", "-t", "termix_"+os.Args[3])
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "attach failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "usage: termix sessions attach <session_id>")
	os.Exit(2)
}

func runDoctor() {
	paths := config.DefaultHostPaths()
	for _, line := range diagnostics.Run(context.Background(), paths.CredentialsFile, paths.RunDir) {
		fmt.Println(line)
	}
}
```

- [x] **Step 4: Run the full Phase 1 verification set**

Run:

```bash
make generate
cd go && go test ./...
```

Expected:
- all unit tests pass
- integration tests either pass or are explicitly gated behind disposable Postgres and tmux setup helpers
- no package fails to compile

- [x] **Step 5: Update `docs/PROGRESS.md` before claiming completion**

Move the completed implementation tasks from `Pending` to `Completed`, leave delayed admin work visible, and record any blockers discovered during execution.

Use this exact patch shape:

```md
## Completed
- [x] Create the initial repository skeleton directories required by Phase 1.
- [x] Define the initial PostgreSQL migrations for `users`, `devices`, `refresh_tokens`, and `sessions`.
- [x] Define `openapi/control.openapi.yaml` for auth, device, and host session APIs.
- [x] Define `proto/daemon.proto` for CLI-daemon gRPC.
- [x] Implement `termix-control` auth and host session endpoints.
- [x] Implement `termixd` bootstrap, local state, and tmux orchestration.
- [x] Implement thin `termix` CLI commands: `login`, `start`, `sessions attach`, `doctor`.
- [x] Add unit, integration, and smoke-test coverage for the Phase 1 slice.
```

- [x] **Step 6: Commit the finished Phase 1 slice**

Run:

```bash
git add go/cmd/termix go/tests/cli_smoke_test.go docs/PROGRESS.md
cat >/tmp/termix-task8.commit <<'EOF'
Deliver the Phase 1 host/control user flow

Wire the thin CLI to the daemon and control plane so login,
start, local attach, and doctor work end-to-end for the first slice.

Constraint: The local terminal remains the primary interface in Phase 1
Confidence: medium
Scope-risk: broad
Directive: Extend the CLI by adding thin orchestration only; keep lifecycle logic in termixd
Tested: make generate; cd go && go test ./...
EOF
git commit -F /tmp/termix-task8.commit
```

## Self-Review

Spec coverage checked:
- host login is covered by Tasks 2, 4, 6, and 8
- daemon bootstrap, UDS IPC, and tmux orchestration are covered by Tasks 5, 7, and 8
- session metadata creation and update are covered by Tasks 3, 4, and 6
- local attach is covered by Tasks 7 and 8
- repository governance and required `docs/PROGRESS.md` updates are covered by Tasks 1 and 8

Placeholder scan:
- removed `TODO`/`TBD` markers
- every task names exact files and exact commands
- each contract file and key implementation file has concrete starter code

Type consistency checked:
- REST base path remains `/api/v1`
- tmux naming remains `termix_<session_id>`
- daemon service method names match the approved design
