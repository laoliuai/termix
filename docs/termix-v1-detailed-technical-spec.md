# Termix V1 Detailed Technical Design Spec

Version: 2.1  
Status: Authoritative full-V1 target with current repository status overlay  
Audience: Codex / implementation engineers  
Last updated: 2026-04-23

---

## 1. Document Purpose

This document is the authoritative technical design specification for **Termix V1**.

It is intended to be detailed enough that an implementation agent can build the first production-capable version of Termix directly from this document with minimal additional design work.

This spec defines:

- product scope and non-goals
- runtime architecture
- service boundaries
- module ownership by language/runtime
- CLI and daemon behavior
- tmux integration model
- cloud control plane and relay plane design
- PostgreSQL schema
- REST API design
- internal gRPC contracts
- WebSocket/binary terminal protocol
- admin Web UI scope
- Android app scope
- repository structure
- implementation constraints
- test requirements
- acceptance criteria

This spec supersedes earlier lightweight architecture notes.

## 1.1 Current Repository Status

As of 2026-04-23, the repository implements the **host/control mainline** only:

- PostgreSQL schema and generated query layer for the host/control slice
- `termix-control` auth and host session APIs
- `termixd` bootstrap, local state, and tmux orchestration
- thin `termix` CLI commands for `login`, `start`, `sessions attach`, and `doctor`
- local tmux-backed attach and repository-level verification for that slice

The following surfaces remain intentionally deferred and are **not** part of the currently completed milestone:

- `termix-admin-api`
- admin Web UI
- relay/runtime streaming path
- Android watch/control flows

Unless a section explicitly says otherwise, later parts of this document describe the **target full-V1 design**, not the current implementation completeness.

---

## 2. Product Summary

Termix is a managed remote terminal companion for AI coding CLIs.

A user opens a terminal on **macOS** or **Ubuntu** and starts a managed session:

```bash
termix start claude -n "optimize loading speed"
termix start codex -n "refactor auth middleware"
termix start opencode -n "investigate flaky tests"
```

Termix then:

1. captures launch context from the current shell
2. creates a dedicated **tmux-backed** session
3. launches the selected AI CLI inside that tmux session
4. keeps the local terminal as the primary interface
5. streams the terminal output through a cloud relay over **WSS**
6. allows the user's Android app to discover the session by account
7. allows the Android app to view the session in near real time
8. allows the Android app to request and obtain remote control
9. exposes a simple cloud admin Web UI for user management and read-only visibility into current sessions

---

## 3. V1 Scope

### 3.1 Host platforms
- macOS
- Ubuntu

### 3.2 Remote client platforms
- Android only

### 3.3 Cloud deployment platforms
- Ubuntu or Debian only

### 3.4 Supported managed tools
- `claude`
- `codex`
- `opencode`

### 3.5 Included cloud surfaces
- public control API
- realtime relay service
- admin API
- admin Web UI

Current repository note: only the public control API is implemented today. The other surfaces remain planned.

### 3.6 Explicit non-goals for V1
- iOS client
- end-user Web terminal client
- Windows host support
- direct attach to arbitrary pre-existing non-Termix sessions
- peer-to-peer as the primary transport
- multiple simultaneous remote writers with conflict resolution
- direct terminal session sharing with third parties
- file upload/download workflows
- screen/video streaming
- browser-based user self-service registration
- SSO/OIDC
- multi-region active-active deployment

### 3.7 Forward compatibility goals
Although V1 ships only with Android as the remote client, the wire protocol and service model must remain compatible with future iOS and Web remote clients.

---

## 4. Core Product Decisions

### 4.1 tmux is mandatory in V1
Termix V1 must use **tmux** as the session persistence and multiplexing layer.

Reason:
- stable session detach/reattach semantics
- persistence across local terminal closure
- easier host daemon recovery
- clean machine-readable control mode
- lower risk than inventing a custom PTY lifecycle manager

### 4.2 Cloud relay is the default transport path
V1 must assume:
- phone and host are not on the same LAN
- direct connectivity cannot be relied upon
- users move between networks frequently

Therefore the primary path is:

```text
Host daemon <-> WSS <-> Cloud relay/control plane <-> WSS <-> Android app
```

Do not make LAN discovery or NAT traversal a V1 dependency.

### 4.3 Session discovery is account-based
A user must authenticate on:
- the host CLI/daemon
- the Android app

The Android app discovers sessions by account, not by LAN scan and not by manual IP entry.

### 4.4 Local terminal remains primary
The local terminal is always the primary interface.
The Android app is a secondary remote interface.

In V1:
- local terminal size is authoritative
- Android viewport resize must **not** resize tmux
- Android renders a view of the current terminal and may scroll if needed

### 4.5 One Termix session per `termix start`
Each invocation of:

```bash
termix start claude -n "optimize loading speed"
```

creates:
- one product-level **Termix session**
- one tmux session named `termix_<session_id>`

### 4.6 Remote clients never attach directly to tmux
Android must never connect to tmux directly.
All remote viewing and control must flow through the local daemon and the cloud relay.

### 4.7 V1 control model
V1 supports:
- multiple viewers
- at most one remote controller at a time

The local host always remains usable.
If both local and remote input occur at the same time, tmux will process both; Termix only prevents multiple remote controllers, not local-vs-host concurrency.

---

## 5. Technology Stack Allocation

This project is intentionally polyglot. Each language/runtime has a specific ownership boundary.

## 5.1 Go ownership

Use **Go 1.25+** for all core runtime-critical services:

- `termix` CLI
- `termixd` local daemon
- `termix-control` public control plane REST API
- `termix-relay` realtime relay plane
- internal gRPC SDK and service contracts
- PostgreSQL access using `sqlc + pgx`
- OpenAPI-driven REST server/client generation using `oapi-codegen`
- JSON Schema validation/generation using `go-jsonschema`
- schema migrations using `golang-migrate`

### Rationale
Go is the best fit for:
- long-running daemons
- process and tmux orchestration
- HTTP/WSS services
- static deployment on macOS/Linux
- straightforward debugging and ops

## 5.2 Python ownership

Use **Python 3.11+** with **uv workspace**, **Pydantic v2**, **FastAPI (async)**, and **pytest** for:

- `termix-admin-api` (admin-only backend/BFF)
- integration-test utilities
- protocol fixtures
- local dev mocks and QA tools
- compatibility validation helpers for REST/WSS contracts

Python is **not** on the terminal data path in V1.

### Rationale
Python is best used here for:
- fast admin/API iteration
- schema-rich validation with Pydantic v2
- test harnesses and mocks
- operational tooling and diagnostics

## 5.3 Database
- PostgreSQL 16+ recommended

## 5.4 Frontend
- React
- TypeScript
- Vite
- shadcn/ui
- Tailwind CSS

Used for:
- cloud admin Web UI only

## 5.5 Android
- Kotlin
- Jetpack Compose
- embedded WebView terminal frontend

Used for:
- login
- device management
- session list
- terminal session UI
- toolbar/special keys
- reconnect UX

The terminal rendering surface in V1 should be an embedded WebView that loads bundled local static assets implementing the terminal UI.

---

## 6. High-Level Architecture

```text
+--------------------------------------------------------------------------------------------------+
| Host machine: macOS / Ubuntu                                                                     |
|                                                                                                  |
|   User terminal (Terminal.app / iTerm2 / Ghostty / Terminator / Tabby / etc.)                   |
|        |                                                                                         |
|        v                                                                                         |
|   termix CLI                                                                                     |
|        | local gRPC over UDS                                                                     |
|        v                                                                                         |
|   termixd local daemon                                                                           |
|     - auth client                                                                                |
|     - session manager                                                                            |
|     - tmux manager                                                                               |
|     - control-mode reader                                                                        |
|     - relay client                                                                               |
|     - local state store                                                                          |
|     - recovery scanner                                                                           |
|        |                                                                                         |
|        v                                                                                         |
|   tmux                                                                                           |
|     - 1 tmux session per Termix session                                                          |
|     - tool runs inside tmux pane                                                                 |
+--------------------------------------------------------------------------------------------------+
                                   | HTTPS / WSS
                                   v
+--------------------------------------------------------------------------------------------------+
| Cloud: Ubuntu/Debian                                                                             |
|                                                                                                  |
|   termix-control (Go)                                                                            |
|     - auth                                                                                       |
|     - device registry                                                                            |
|     - session registry                                                                           |
|     - session metadata                                                                           |
|     - token issuance                                                                             |
|                                                                                                  |
|   termix-relay (Go)                                                                              |
|     - WSS gateway                                                                                |
|     - terminal stream routing                                                                    |
|     - control lease coordination                                                                 |
|     - presence                                                                                   |
|                                                                                                  |
|   termix-admin-api (Python FastAPI)                                                              |
|     - admin auth                                                                                 |
|     - user CRUD                                                                                  |
|     - session listing                                                                            |
|                                                                                                  |
|   PostgreSQL                                                                                     |
|                                                                                                  |
|   Admin Web UI (React)                                                                           |
+--------------------------------------------------------------------------------------------------+
                                   | HTTPS / WSS
                                   v
+--------------------------------------------------------------------------------------------------+
| Android App                                                                                      |
|   Compose shell                                                                                  |
|   - login                                                                                        |
|   - session list                                                                                 |
|   - session details                                                                              |
|   - toolbar                                                                                      |
|   - reconnect UI                                                                                 |
|                                                                                                  |
|   WebView terminal frontend                                                                      |
|   - terminal rendering                                                                           |
|   - WSS session transport                                                                        |
+--------------------------------------------------------------------------------------------------+
```

---

## 7. Runtime Component Breakdown

## 7.1 `termix` CLI (Go)

### Responsibilities
- user-facing entry point
- login/logout
- daemon bootstrap and health checks
- start session requests
- list/attach/stop local sessions
- diagnostics

### Required commands

```bash
termix login
termix logout
termix doctor

termix start claude
termix start codex
termix start opencode

termix start claude -n "optimize loading speed"
termix start codex --name "fix prod crash"

termix sessions list
termix sessions attach <session_id>
termix sessions stop <session_id>
termix daemon status
```

### Required start syntax
The first supported shape is:

```bash
termix start <tool> [-n|--name "<session name>"]
```

Where `<tool>` is one of:
- `claude`
- `codex`
- `opencode`

### Launch context capture
Because the daemon is long-running and may not inherit the latest shell state, the CLI must capture and pass the following launch context to the daemon on every `termix start`:

- current working directory
- current environment variables
- current shell path
- `TERM`
- `LANG`
- `LC_*`
- current host terminal type if available
- requested tool
- requested session name

Important:
- this launch context is used **locally only**
- it must not be uploaded to the cloud except for safe metadata fields such as tool name and cwd basename
- full environment must never be persisted to cloud storage

### Local daemon bootstrap
`termix start ...` must:
1. detect whether `termixd` is already running for the current user
2. if not, start it in the background
3. wait for local UDS health check
4. call daemon gRPC `StartSession`
5. on success, replace or hand off the current process to `tmux attach-session -t termix_<session_id>`

The local terminal must land inside the tmux session so the user interacts with the real session immediately.

## 7.2 `termixd` local daemon (Go)

### Responsibilities
- maintain authenticated state with cloud
- maintain daemon registration
- manage local session lifecycle
- manage tmux session creation and recovery
- consume tmux control-mode output
- stream terminal data to relay
- receive remote input and inject it into tmux
- generate previews and snapshots
- maintain local state on disk
- recover after daemon restart

### Local IPC
CLI <-> daemon must use **gRPC over Unix domain socket**.

Socket path:
- Linux: `${XDG_RUNTIME_DIR}/termix/daemon.sock` if available, else `~/.termix/run/daemon.sock`
- macOS: `~/Library/Application Support/Termix/run/daemon.sock`

The daemon socket must be user-private.

## 7.3 tmux integration layer (Go)

### Requirements
- verify `tmux` exists at startup and in `termix doctor`
- create one tmux session per Termix session
- maintain one machine client in control mode for each running session
- allow local user attachment through normal tmux attach
- recover sessions after daemon restart

### tmux naming
- tmux session name: `termix_<session_id>`
- window name: `main`
- target pane: `termix_<session_id>:main.0`

### Required tmux command model

#### Create session
Create the session detached, with explicit default size for no-attached-client cases:

```bash
tmux new-session -d -s termix_<session_id> -n main -x 120 -y 40
```

#### Start tool in pane
After session creation:
- start a login shell with the captured environment and cwd
- then execute the selected tool inside that shell

Preferred model:
1. create tmux session
2. set environment variables in tmux session
3. `send-keys` to run a login shell or command bootstrap
4. `send-keys` again for the final tool command if needed

Do not rely on daemon process environment alone.

#### Control mode reader
For each running session, daemon starts a control mode client:

```bash
tmux -C attach-session -t termix_<session_id>
```

The daemon must parse control-mode output and forward pane output events to the relay.

#### Snapshot capture
For reconnect and preview generation use:

```bash
tmux capture-pane -p -e -S -200 -t termix_<session_id>:main.0
```

The daemon must periodically or event-driven refresh preview text.

### Resize model in V1
- local attached terminal size is authoritative
- Android app does not change tmux window size
- if no local terminal is attached, tmux remains at default size until a local attach occurs

## 7.4 `termix-control` (Go, Gin REST)

### Responsibilities
- authentication
- token issuance
- token refresh
- user profile retrieval
- device registration/update
- session metadata CRUD
- session query for mobile
- daemon heartbeat/session heartbeat
- admin-internal gRPC authority for relay

### Public exposure
- HTTPS REST only

### Internal exposure
- gRPC to relay and optionally to other Go services

## 7.5 `termix-relay` (Go)

### Responsibilities
- WSS endpoint for daemon and Android
- authenticated session subscriptions
- terminal output routing
- terminal input routing
- heartbeat and presence
- control lease request/renew/release
- lightweight replay handshake
- backpressure handling

### Important V1 limitation
V1 relay can be deployed as a single instance.
Do not design V1 around horizontal scaling requirements that require Redis or NATS.

## 7.6 `termix-admin-api` (Python FastAPI)

### Responsibilities
- admin login
- admin session validation
- create/update/disable users
- reset user passwords
- list users
- list current sessions by user
- return read-only session metadata to admin Web UI

### Important boundary
Admin API must not:
- attach to sessions
- proxy terminal traffic
- send input to sessions
- stop user sessions in V1

## 7.7 Admin Web UI (React)
### Responsibilities
- admin login
- user list
- create/edit/disable user
- current session list per user
- read-only details: session name, tool, command, host, status, timestamps

## 7.8 Android App (Kotlin + Compose + WebView)
### Responsibilities
- login
- token refresh
- session list
- session detail header
- terminal view
- request/release control
- toolbar with special keys
- reconnect UI
- concurrent session tabs or recents switching

---

## 8. Networking and Transport Design

## 8.1 Why no P2P in V1
P2P/NAT traversal adds:
- candidate gathering
- NAT behavior complexity
- TURN fallback
- mobile network path switching complexity

Terminal traffic is low-bandwidth enough that a cloud relay is acceptable in V1.

## 8.2 External transports
Use:
- HTTPS for REST APIs
- WSS for realtime

Both daemon and Android connect outbound to cloud.

## 8.3 Transport map

### CLI -> daemon
- gRPC over Unix domain socket

### daemon -> control plane
- HTTPS REST
- WSS to relay

### Android -> control plane
- HTTPS REST
- WSS to relay

### Admin Web UI -> admin API
- HTTPS REST

### relay -> control plane
- internal gRPC
- optional direct JWT validation without synchronous call for every frame

---

## 9. Auth and Identity Model

## 9.1 Users
V1 users are created by admin only.
There is no self-registration.

### Roles
- `admin`
- `user`

Only admin can access admin API and admin Web UI.

## 9.2 Login model

### CLI login
`termix login` prompts for:
- server URL
- email
- password

It then:
- calls `POST /v1/auth/login`
- receives access token and refresh token
- registers a host device if needed
- stores credentials locally

### Android login
The Android app shows:
- server URL field
- email
- password

It then:
- calls `POST /v1/auth/login`
- registers or updates Android device record
- stores access and refresh tokens securely

### Admin login
Admin Web UI calls Python admin API login endpoint.

## 9.3 Password hashing
Store user passwords using **Argon2id**.

## 9.4 Token model
Use:
- access token: JWT, 15 minutes
- refresh token: opaque random secret, 30 days

Refresh tokens must be stored hashed in the database.

## 9.5 Local credential storage

### Ubuntu
Store refresh token in:
- `~/.config/termix/credentials.json`
- file mode `0600`

### macOS
Preferred:
- Keychain
Fallback:
- `~/Library/Application Support/Termix/credentials.json`
- file mode `0600`

### Android
Use encrypted local storage.

## 9.6 Device identity
Every login establishes or updates a `devices` row.

Device types:
- `host`
- `android`

Platforms:
- `macos`
- `ubuntu`
- `android`

---

## 10. Session Lifecycle Design

## 10.1 Session start flow

### User action
```bash
termix start claude -n "optimize loading speed"
```

### Flow
1. CLI validates tool and local config.
2. CLI captures launch context: cwd, env, shell, name, tool.
3. CLI calls daemon `StartSession`.
4. Daemon creates a local session object with state `starting`.
5. Daemon calls control plane `POST /v1/host/sessions`.
6. Control plane creates `sessions` row with server-generated `session_id`.
7. Daemon creates tmux session `termix_<session_id>`.
8. Daemon injects cwd/env and starts requested tool.
9. Daemon starts tmux control-mode reader.
10. Daemon opens or reuses WSS relay connection and announces the running session.
11. Daemon updates control plane session state to `running`.
12. CLI attaches local terminal to `tmux attach-session -t termix_<session_id>`.

## 10.2 Session discovery flow
1. Android logs in.
2. Android calls `GET /v1/sessions?status=running`.
3. Control plane returns sessions for that user ordered by last activity.
4. User taps a session.
5. Android opens relay WSS if not already open.
6. Android sends `session.watch` control message.
7. Relay validates authorization and joins session stream.

## 10.3 Remote control flow
1. Android user taps "Request control".
2. Android sends `control.acquire` message over WSS.
3. Relay calls control plane gRPC `AcquireControlLease`.
4. Control plane grants if no active remote controller exists.
5. Relay sends `control.granted`.
6. Android may now send terminal input frames.

## 10.4 Session stop flow
A session stops when:
- tool exits
- user runs `termix sessions stop <session_id>`
- host machine is shut down

Daemon must:
1. detect pane exit
2. capture final preview
3. update control plane status to `exited`
4. close control-mode client
5. terminate or archive local session metadata

## 10.5 Daemon restart recovery
On daemon startup:
1. scan local state directory
2. query `tmux ls`
3. identify `termix_*` sessions
4. rebuild in-memory session registry
5. restart control-mode readers
6. reconnect to relay
7. reconcile status with control plane

---

## 11. Repository and Workspace Layout

Use a monorepo.

```text
termix/
  README.md
  Makefile
  .editorconfig
  .gitignore

  docs/
    termix-v1-technical-spec.md

  db/
    migrations/
      000001_init.up.sql
      000001_init.down.sql
      ...

  openapi/
    control.openapi.yaml
    admin.openapi.yaml

  proto/
    relay_control.proto
    daemon.proto

  schemas/
    ws/
      envelope.schema.json
      control.hello.schema.json
      control.session_watch.schema.json
      control.control_acquire.schema.json
      binary_header.schema.json
    config/
      daemon_config.schema.json

  go/
    go.work
    go.mod
    cmd/
      termix/
      termixd/
      termix-control/
      termix-relay/
    internal/
      auth/
      config/
      daemonipc/
      session/
      tmux/
      relay/
      controlapi/
      persistence/
      wsproto/
      diagnostics/
    gen/
      openapi/
      proto/
      jsonschema/
    sql/
      queries/
    tests/

  python/
    pyproject.toml
    uv.lock
    apps/
      termix_admin_api/
        app/
        tests/
    packages/
      protocol_fixtures/
      qa_tools/

  web/
    admin/
      package.json
      src/

  android/
    app/
    terminal-web/
      package.json
      src/
```

## 11.1 Go project conventions
- single `go.mod` under `/go`
- `go.work` for future expansion if needed
- generated code must be committed or reproducibly generated in CI
- `sqlc` is the only query-access layer for Go services

## 11.2 Python workspace conventions
Use `uv workspace` in `/python`.

Minimum structure in `pyproject.toml`:
- workspace members for admin API and support packages
- Ruff/pytest configuration may be added
- Pydantic v2 models are the source of truth for admin API request/response models

---

## 12. Detailed Data Model

Use PostgreSQL. All ids are UUID unless otherwise stated.

## 12.1 `users`
Purpose: application users and admins.

Columns:
- `id uuid primary key`
- `email text not null unique`
- `display_name text not null`
- `password_hash text not null`
- `role text not null check (role in ('admin','user'))`
- `status text not null check (status in ('active','disabled'))`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`
- `last_login_at timestamptz null`

Indexes:
- unique(email)
- index(role, status)

## 12.2 `devices`
Purpose: registered host and Android devices.

Columns:
- `id uuid primary key`
- `user_id uuid not null references users(id)`
- `device_type text not null check (device_type in ('host','android'))`
- `platform text not null check (platform in ('macos','ubuntu','android'))`
- `label text not null`
- `hostname text null`
- `machine_fingerprint text null`
- `app_version text null`
- `last_seen_at timestamptz not null default now()`
- `created_at timestamptz not null default now()`
- `disabled_at timestamptz null`

Indexes:
- index(user_id, device_type)
- index(last_seen_at)

## 12.3 `refresh_tokens`
Purpose: persistent login.

Columns:
- `id uuid primary key`
- `user_id uuid not null references users(id)`
- `device_id uuid not null references devices(id)`
- `token_hash text not null`
- `expires_at timestamptz not null`
- `created_at timestamptz not null default now()`
- `revoked_at timestamptz null`

Indexes:
- index(user_id, device_id)
- index(expires_at)

## 12.4 `sessions`
Purpose: primary product-level session registry.

Columns:
- `id uuid primary key`
- `user_id uuid not null references users(id)`
- `host_device_id uuid not null references devices(id)`
- `name text null`
- `tool text not null check (tool in ('claude','codex','opencode'))`
- `launch_command text not null`
- `cwd text not null`
- `cwd_label text not null`
- `tmux_session_name text not null unique`
- `status text not null check (status in ('starting','running','idle','disconnected','exited','failed'))`
- `preview_text text null`
- `last_error text null`
- `last_exit_code integer null`
- `started_at timestamptz not null default now()`
- `last_activity_at timestamptz not null default now()`
- `ended_at timestamptz null`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`

Indexes:
- index(user_id, status, last_activity_at desc)
- index(host_device_id, status)
- unique(tmux_session_name)

## 12.5 `session_connections`
Purpose: audit active and historical client connections.

Columns:
- `id uuid primary key`
- `session_id uuid not null references sessions(id)`
- `device_id uuid not null references devices(id)`
- `connection_role text not null check (connection_role in ('host','viewer','controller'))`
- `connected_at timestamptz not null default now()`
- `disconnected_at timestamptz null`

Indexes:
- index(session_id, disconnected_at)
- index(device_id, disconnected_at)

## 12.6 `control_leases`
Purpose: single remote controller lock.

Columns:
- `session_id uuid primary key references sessions(id)`
- `controller_device_id uuid not null references devices(id)`
- `lease_version bigint not null`
- `granted_at timestamptz not null default now()`
- `expires_at timestamptz not null`

Rules:
- at most one row per session
- lease is renewable
- relay must treat expired lease as invalid

## 12.7 `session_events`
Purpose: coarse-grained audit and state changes, not full terminal logs.

Columns:
- `id bigserial primary key`
- `session_id uuid not null references sessions(id)`
- `kind text not null`
- `payload jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null default now()`

Suggested event kinds:
- `started`
- `relay_connected`
- `relay_disconnected`
- `remote_watch_joined`
- `remote_watch_left`
- `control_acquired`
- `control_released`
- `exited`
- `failed`

## 12.8 `admin_audit_logs`
Purpose: audit admin actions.

Columns:
- `id bigserial primary key`
- `actor_user_id uuid not null references users(id)`
- `action text not null`
- `target_type text not null`
- `target_id uuid null`
- `metadata jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null default now()`

---

## 13. Go Service Boundaries

## 13.1 `termix-control`
Owns:
- auth tables access
- devices
- sessions
- refresh tokens
- control lease authority
- public OpenAPI REST

Must not:
- proxy terminal bytes
- read terminal stream content except preview text pushed by daemon

## 13.2 `termix-relay`
Owns:
- WSS connection registry
- in-memory session watchers
- frame routing
- lease renewal timers

May call control via gRPC for:
- auth/introspection
- session authorization
- acquire/release lease
- heartbeat persistence if needed

Must not:
- become source of truth for session metadata
- write directly to PostgreSQL in V1

## 13.3 `termixd`
Owns:
- local tmux execution
- local control-mode parsing
- local preview capture
- host-side session state
- outbound relay connection

---

## 14. Python Service Boundaries

## 14.1 `termix-admin-api`
This is the only Python runtime service in V1.

### Responsibilities
- admin authentication
- user CRUD
- password reset
- admin session listing
- read-only current session visibility

### Data access
The admin API may access PostgreSQL directly using async database access.

### Important rules
- do not expose terminal bytes
- do not control live sessions
- do not manipulate tmux
- do not share refresh token internals with the frontend

### FastAPI requirements
- async handlers only
- Pydantic v2 models for all requests/responses
- OpenAPI generated by FastAPI must remain stable
- pytest coverage for auth and CRUD flows

---

## 15. REST API Design

OpenAPI is authoritative for all REST APIs.

## 15.1 Public control API (Go Gin)

Base path:
```text
/api/v1
```

### Auth

#### `POST /auth/login`
Request:
```json
{
  "email": "user@example.com",
  "password": "secret",
  "device_type": "host",
  "platform": "macos",
  "device_label": "MacBook Pro"
}
```

Response:
```json
{
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "display_name": "User Name",
    "role": "user"
  },
  "device": {
    "id": "uuid",
    "device_type": "host",
    "platform": "macos",
    "label": "MacBook Pro"
  },
  "access_token": "jwt",
  "refresh_token": "opaque",
  "expires_in_seconds": 900
}
```

#### `POST /auth/refresh`
Request:
```json
{
  "refresh_token": "opaque",
  "device_id": "uuid"
}
```

Response:
```json
{
  "access_token": "jwt",
  "refresh_token": "opaque",
  "expires_in_seconds": 900
}
```

#### `POST /auth/logout`
Request:
```json
{
  "refresh_token": "opaque",
  "device_id": "uuid"
}
```

Response:
```json
{
  "ok": true
}
```

#### `GET /me`
Returns current user and device summary.

### Host device/session endpoints

#### `POST /host/heartbeat`
Purpose: daemon online heartbeat.

Request:
```json
{
  "device_id": "uuid",
  "app_version": "1.0.0"
}
```

#### `POST /host/sessions`
Purpose: create session metadata record.

Request:
```json
{
  "device_id": "uuid",
  "tool": "claude",
  "name": "optimize loading speed",
  "launch_command": "claude",
  "cwd": "/Users/alice/project-a",
  "cwd_label": "project-a",
  "hostname": "mbp-14"
}
```

Response:
```json
{
  "session_id": "uuid",
  "tmux_session_name": "termix_<session_id>",
  "status": "starting"
}
```

#### `PATCH /host/sessions/{session_id}`
Purpose: update lifecycle state.

Request:
```json
{
  "status": "running",
  "last_error": null,
  "last_exit_code": null,
  "last_activity_at": "2026-04-22T10:00:00Z"
}
```

#### `POST /host/sessions/{session_id}/preview`
Purpose: update preview text.

Request:
```json
{
  "preview_text": "last ~200 lines truncated",
  "last_activity_at": "2026-04-22T10:00:00Z"
}
```

#### `POST /host/sessions/{session_id}/heartbeat`
Purpose: keep session marked alive.

Request:
```json
{
  "status": "running",
  "last_activity_at": "2026-04-22T10:00:00Z"
}
```

### Mobile session endpoints

#### `GET /sessions`
Query params:
- `status` optional: `running|recent|all`
- `limit` default 50

Response item:
```json
{
  "id": "uuid",
  "name": "optimize loading speed",
  "tool": "claude",
  "launch_command": "claude",
  "cwd_label": "project-a",
  "host_device_label": "MacBook Pro",
  "hostname": "mbp-14",
  "status": "running",
  "preview_text": "last lines",
  "started_at": "2026-04-22T09:00:00Z",
  "last_activity_at": "2026-04-22T10:00:00Z",
  "controller_device_id": null
}
```

#### `GET /sessions/{session_id}`
Returns full metadata for that session if owned by caller.

#### `POST /sessions/{session_id}/stop`
Optional V1 support from host user only. If implemented, it marks stop requested; daemon performs local termination.

### OpenAPI rules
- all REST handlers generated from `openapi/control.openapi.yaml`
- use `oapi-codegen` to generate server interfaces and typed clients
- manual handlers implement generated interfaces only
- no ad hoc undocumented JSON shapes

## 15.2 Admin API (Python FastAPI)

Base path:
```text
/admin/api/v1
```

### Auth

#### `POST /auth/login`
Request:
```json
{
  "email": "admin@example.com",
  "password": "secret"
}
```

Response:
```json
{
  "access_token": "jwt-or-session-token",
  "expires_in_seconds": 900,
  "user": {
    "id": "uuid",
    "email": "admin@example.com",
    "display_name": "Admin",
    "role": "admin"
  }
}
```

#### `GET /auth/me`

### User management

#### `GET /users`
Filters:
- `status`
- `role`
- `q`

#### `POST /users`
Request:
```json
{
  "email": "user@example.com",
  "display_name": "User Name",
  "role": "user",
  "initial_password": "temporary-secret"
}
```

#### `PATCH /users/{user_id}`
Request supports:
- `display_name`
- `status`
- `role`

#### `POST /users/{user_id}/reset-password`
Request:
```json
{
  "new_password": "new-secret"
}
```

### Session visibility

#### `GET /users/{user_id}/sessions/current`
Returns read-only list of current sessions:
- session id
- name
- tool
- launch_command
- host device label
- hostname
- status
- started_at
- last_activity_at

Admin API may also provide:
- `GET /sessions/current`

---

## 16. Internal gRPC Contracts

Use protobuf under `/proto`.

## 16.1 Daemon local IPC service

Service name:
```proto
service DaemonService
```

Methods:
- `Health(HealthRequest) returns (HealthResponse)`
- `StartSession(StartSessionRequest) returns (StartSessionResponse)`
- `ListSessions(ListSessionsRequest) returns (ListSessionsResponse)`
- `AttachInfo(AttachInfoRequest) returns (AttachInfoResponse)`
- `StopSession(StopSessionRequest) returns (StopSessionResponse)`
- `Doctor(DoctorRequest) returns (DoctorResponse)`

### `StartSessionRequest`
Fields:
- `tool`
- `name`
- `cwd`
- `shell`
- `term`
- `language`
- `env map<string,string>`

### `StartSessionResponse`
Fields:
- `session_id`
- `tmux_session_name`
- `attach_command`
- `status`

## 16.2 Relay-control internal service

Service name:
```proto
service RelayControlService
```

Methods:
- `ValidateAccessToken`
- `AuthorizeSessionWatch`
- `AcquireControlLease`
- `RenewControlLease`
- `ReleaseControlLease`
- `MarkConnectionOpened`
- `MarkConnectionClosed`

The relay must call these methods, not touch the DB directly.

---

## 17. Realtime Protocol Design

Use **WSS**. WebSocket is appropriate because:
- terminal traffic is duplex but low bandwidth
- browser/WebView support is universal
- Go and Kotlin client support is mature
- control and data frames can coexist on one connection

## 17.1 Connection roles
A relay connection has exactly one role:
- `daemon`
- `android`

## 17.2 Framing model
Use:
- **text frames** for control messages
- **binary frames** for terminal byte payloads

## 17.3 Text control envelope
All text messages must be JSON with this base envelope:

```json
{
  "type": "session.watch",
  "request_id": "uuid",
  "payload": {}
}
```

Required fields:
- `type string`
- `request_id string|null`
- `payload object`

### Control message types from daemon
- `hello.daemon`
- `session.online`
- `session.offline`
- `session.preview`
- `ack`
- `error`
- `heartbeat`

### Control message types from Android
- `hello.android`
- `session.watch`
- `session.unwatch`
- `control.acquire`
- `control.renew`
- `control.release`
- `heartbeat`

### Control message types from relay to clients
- `hello.ok`
- `session.joined`
- `session.left`
- `control.granted`
- `control.denied`
- `control.revoked`
- `session.snapshot.ready`
- `error`
- `heartbeat`

## 17.4 Binary frame design

Binary frames must use this structure:

```text
Bytes 0..3   magic = "TMX1"
Byte 4       version = 1
Byte 5       frame_type
Bytes 6..9   big-endian uint32 header_json_len
Bytes 10..N  UTF-8 JSON header
Bytes N..end raw payload bytes
```

### Frame types
- `1`: terminal output
- `2`: terminal input
- `3`: snapshot chunk

### Binary header common fields
```json
{
  "session_id": "uuid",
  "seq": 12345
}
```

### Terminal output header
```json
{
  "session_id": "uuid",
  "seq": 12345,
  "stream": "stdout"
}
```

Payload:
- raw terminal bytes exactly as produced by tmux control-mode pane output after decoding control envelope framing

### Terminal input header
```json
{
  "session_id": "uuid",
  "seq": 555,
  "encoding": "raw"
}
```

Payload:
- raw bytes to inject into the tmux pane

### Snapshot chunk header
```json
{
  "session_id": "uuid",
  "seq": 999,
  "is_last": true
}
```

Payload:
- UTF-8 encoded snapshot text or raw bytes for initial terminal restore

## 17.5 Input injection rules
The daemon must inject remote input using tmux commands appropriate for raw key/text semantics.

Implementation guidance:
- printable text paste may use `tmux send-keys -l -- "<text>"`
- special keys use symbolic forms, e.g. `Enter`, `C-c`, `Tab`, `Up`, `Down`, `Escape`
- multi-byte raw input should be normalized by the daemon before tmux injection
- Android must not talk to tmux directly

## 17.6 Session watch handshake
When Android opens a session:
1. send `session.watch`
2. relay validates permission
3. relay responds `session.joined`
4. relay instructs daemon if needed to send current snapshot
5. daemon sends `session.snapshot.ready`
6. relay forwards snapshot and then live output continues

## 17.7 Heartbeats
- daemon -> relay heartbeat every 15 seconds
- Android -> relay heartbeat every 20 seconds while terminal page open
- relay disconnect timeout: 45 seconds without heartbeat

## 17.8 Backpressure
If a mobile client cannot keep up:
- relay may drop old non-critical snapshot chunks
- relay must not block daemon session forwarding indefinitely
- if buffer exceeds limit, relay disconnects slow consumer with a specific error code

---

## 18. Session Preview Model

V1 requires lightweight preview text for:
- Android session list
- admin session list

### Source
Preview text is generated by the daemon from tmux capture.

### Rules
- max stored preview length: 8 KiB
- preview text is truncated from the head if necessary; keep most recent content
- preview refresh interval:
  - on major output burst debounce 2 seconds
  - at least every 15 seconds while running
  - once on exit

---

## 19. Android App Design

## 19.1 Architecture
Use a hybrid architecture:

### Native Compose shell
Owns:
- login screen
- session list
- settings
- toolbar
- reconnect and errors
- token refresh lifecycle

### WebView terminal surface
Owns:
- terminal rendering
- WSS session connection
- xterm.js based UI
- binary frame processing
- selection/copy within terminal view

The WebView loads a bundled local asset from `android/terminal-web/dist`.

## 19.2 Recommended terminal frontend behavior
The terminal frontend should:
- open WSS with bearer token passed from native layer
- render snapshot first
- append live output
- expose JS bridge methods:
  - `sendText(text)`
  - `sendSpecialKey(key)`
  - `requestControl()`
  - `releaseControl()`
  - `setSession(sessionId, relayUrl, accessToken)`

## 19.3 Special keys
Compose toolbar must provide:
- Ctrl
- Esc
- Tab
- Up
- Down
- Left
- Right
- Enter
- Paste
- Ctrl+C
- Ctrl+D

## 19.4 Session list UI requirements
Each item must display:
- session name if present, else generated fallback
- tool
- host device label
- cwd label
- current status
- last activity time
- preview text excerpt
- whether current Android device holds control

## 19.5 Multiple session switching
V1 must support:
- navigating back to session list
- opening another session
- returning to a recent session quickly

Full tabbed multiplexing is optional; at minimum the app must preserve recent sessions in memory when feasible.

---

## 20. Admin Web UI Design

## 20.1 Scope
V1 admin Web UI is intentionally small.

### Screens
- login
- users list
- create user dialog
- edit user dialog
- current sessions list
- per-user current sessions view

## 20.2 Must display
For current sessions:
- session id
- session name
- tool
- launch command
- host device label
- hostname
- status
- started at
- last activity at

## 20.3 Must not do in V1
- no attach button
- no stop button
- no control button
- no terminal preview page beyond list/detail metadata

---

## 21. Local State and Filesystem Layout

## 21.1 Linux paths
- config: `~/.config/termix/`
- state: `~/.local/state/termix/` or `~/.termix/state/` fallback
- logs: `~/.local/state/termix/logs/`
- run: `${XDG_RUNTIME_DIR}/termix/` or `~/.termix/run/`

## 21.2 macOS paths
- config: `~/Library/Application Support/Termix/`
- state: `~/Library/Application Support/Termix/state/`
- logs: `~/Library/Logs/Termix/`
- run: `~/Library/Application Support/Termix/run/`

## 21.3 Required local files
- `credentials.json`
- `daemon.pid`
- `sessions/<session_id>.json`
- `logs/daemon.log`

The session json file stores only safe local metadata and must never include full terminal history.

---

## 22. Configuration Model

Use JSON or YAML config loaded by Go services, validated against JSON Schema.

### Required host config fields
- `server_base_url`
- `control_api_url`
- `relay_ws_url`
- `log_level`
- `preview_max_bytes`
- `heartbeat_interval_seconds`

### Required cloud config fields
- `listen_addr`
- `public_base_url`
- `postgres_dsn`
- `jwt_signing_key`
- `access_token_ttl_seconds`
- `refresh_token_ttl_seconds`

Use `go-jsonschema` for config validation code generation where practical.

---

## 23. Build and Code Generation Rules

## 23.1 Go rules
- REST contracts originate from `openapi/control.openapi.yaml`
- generate code with `oapi-codegen`
- SQL originates from `/go/sql/queries/*.sql`
- generate DB code with `sqlc`
- migrations managed with `golang-migrate`
- protobuf under `/proto`
- generated gRPC code committed or CI-generated deterministically

## 23.2 Python rules
- `uv sync` is the standard install path
- Pydantic v2 models define request/response bodies
- FastAPI auto-generated OpenAPI is checked into artifacts in CI
- async endpoints only

## 23.3 Frontend rules
- use typed API client generation from OpenAPI where possible
- no direct handwritten fetch payloads for stable APIs without shared types

## 23.4 Android rules
- use Kotlin coroutines
- keep token refresh and session REST in native layer
- keep terminal rendering in WebView bundle
- no remote terminal logic split across Compose and WebView in conflicting ways

---

## 24. Testing Strategy

## 24.1 Go tests
Required:
- unit tests for auth, session manager, tmux command generation, relay control lease logic
- integration tests for control REST handlers against a test Postgres
- relay tests for WSS handshake and routing

## 24.2 Python tests
Required:
- pytest for admin auth
- pytest for user CRUD
- pytest for current session list endpoints
- schema validation tests for Pydantic models

## 24.3 End-to-end tests
Provide at least one automated end-to-end test flow covering:

1. admin creates user
2. host CLI logs in
3. host starts `termix start claude -n "test session"`
4. session appears in control plane
5. Android simulated client lists session
6. Android simulated relay client watches session
7. Android acquires control
8. Android sends input
9. daemon injects input into tmux
10. session exits and control plane reflects exit

Python-based QA tools may be used to drive e2e tests.

## 24.4 Manual smoke tests
Required on:
- macOS host
- Ubuntu host
- Android physical device or emulator

---

## 25. Observability and Logging

## 25.1 Required structured logs
All Go and Python services must emit structured logs with:
- timestamp
- level
- service
- request_id or connection_id where applicable
- session_id where applicable
- device_id where applicable

## 25.2 Metrics
V1 should expose simple health and counters:
- active daemon connections
- active Android connections
- active running sessions
- relay frames forwarded
- control lease grants/denials
- auth login success/failure count

## 25.3 Health endpoints
Go and Python services should expose:
- `/healthz`
- `/readyz`

---

## 26. Security Requirements

## 26.1 Transport security
All external traffic must use TLS.

## 26.2 Authorization
Users can only access their own sessions.
Admins can list all users and all current sessions through admin API only.

## 26.3 Secret handling
- never log passwords
- never log refresh tokens
- never upload full environment variable snapshots
- never store terminal byte history in PostgreSQL in V1
- preview text is allowed but should be size-limited

## 26.4 Host-side safety
`termix doctor` must verify:
- `tmux` installed
- daemon socket permissions
- config file permissions
- credentials file permissions

---

## 27. Deployment Model

## 27.1 V1 cloud topology
A single Ubuntu/Debian VM or small set of services is acceptable.

Recommended deployables:
- `termix-control`
- `termix-relay`
- `termix-admin-api`
- `postgres`
- static admin Web UI served behind reverse proxy

## 27.2 Reverse proxy
A reverse proxy may terminate TLS and route:
- `/api/` -> control
- `/admin/api/` -> admin API
- `/ws/` -> relay
- `/admin/` -> admin Web UI assets

## 27.3 Horizontal scaling
Not required in V1.
Design code so scaling is possible later, but do not require Redis, Kafka, or NATS in the initial implementation.

---

## 28. Implementation Constraints for Codex

The implementation must follow these constraints:

1. Do not replace tmux with a raw PTY-only design.
2. Do not put Python on the terminal byte forwarding path.
3. Do not skip OpenAPI/sqlc/protobuf generation layers.
4. Do not add ORMs to Go services.
5. Do not expose any session attach/control capability in the admin Web UI.
6. Do not make Android resize the tmux session in V1.
7. Do not make session discovery depend on local network broadcast.
8. Do not upload full environment variables to cloud APIs.
9. Do not store full terminal transcripts in PostgreSQL in V1.
10. Do not bypass the daemon by letting Android talk to tmux directly.

---

## 29. Full V1 Acceptance Criteria

These acceptance criteria describe the target end-state for full V1. They are broader than the currently implemented host/control repository milestone.

V1 is accepted when all of the following are true:

1. An admin can create a user from the admin Web UI.
2. A user can log in from macOS and Ubuntu using `termix login`.
3. A user can start a session:
   ```bash
   termix start claude -n "optimize loading speed"
   ```
4. The local terminal attaches to the tmux-backed session and remains usable.
5. The session appears in cloud APIs with:
   - session id
   - name
   - tool
   - launch command
   - host name
   - current status
6. The Android app can log in and list the user's sessions.
7. The Android app can open a session and receive live terminal output.
8. The Android app can request control and send input that reaches the session.
9. Session preview text updates in both Android list and admin Web UI.
10. When the tool exits, the session becomes `exited`.
11. If the daemon restarts while tmux session still exists, it can recover and resume relay.
12. All REST APIs are described by OpenAPI and implemented through generated contracts.
13. Go data access uses `sqlc + pgx`.
14. Database schema is fully migration-managed with `golang-migrate`.
15. Admin API is implemented in Python FastAPI with Pydantic v2 and tested with pytest.

---

## 30. Phase Breakdown

## Phase 1
- PostgreSQL schema
- control API auth + sessions
- local daemon bootstrap
- tmux session creation
- local attach

## Deferred after current Phase 1 mainline
- admin API + admin Web UI user CRUD

## Phase 2
- relay WSS
- daemon control-mode reader
- Android session list
- basic live watch

## Phase 3
- control lease
- Android input
- preview updates
- daemon restart recovery

## Phase 4
- hardening
- tests
- packaging
- deployment scripts

---

## 31. Final Technical Recommendation Summary

### Core runtime
- Go 1.25+
- Gin REST
- gRPC internal SDK
- sqlc + pgx
- oapi-codegen
- go-jsonschema
- golang-migrate

### Admin/runtime tooling
- Python 3.11+
- uv workspace
- FastAPI async
- Pydantic v2
- pytest

### Storage
- PostgreSQL

### Web
- React + TypeScript + Vite + shadcn/ui + Tailwind CSS

### Mobile
- Kotlin + Jetpack Compose + WebView terminal frontend

### Session engine
- tmux

### Primary network path
- HTTPS + WSS through cloud relay

---

## 32. Appendix: Minimal CLI UX Requirements

### `termix doctor`
Must check:
- `tmux` installed and runnable
- daemon socket healthy
- control plane reachable
- relay URL configured
- credentials present
- writable state directories

### `termix sessions list`
Must show local view with:
- session id
- name
- tool
- status
- tmux session name
- started at

### `termix sessions attach <session_id>`
Must run:
```bash
tmux attach-session -t termix_<session_id>
```

### `termix sessions stop <session_id>`
Daemon should:
- send interrupt/terminate strategy to pane
- mark stop requested
- update cloud state after exit

---

## 33. Appendix: Session Naming Rules

When user passes:
```bash
termix start claude -n "optimize loading speed"
```

Store:
- `name = "optimize loading speed"`
- `launch_command = "claude"`
- `tool = "claude"`

If no name is provided:
- `name = null`
- frontend generates fallback display like:
  - `"claude · project-a · mbp-14"`

The cloud session list and admin list must both display:
- `name`
- `launch_command`

---

## 34. Appendix: V1 Simplifications That Are Intentional

These are intentional and must not be treated as bugs in V1:
- single relay instance is acceptable
- Android is the only remote client
- admin Web UI is read-only for sessions
- local terminal size is authoritative
- no full transcript storage
- no P2P path
- no shareable session links
- no collaborative remote control

---

End of spec.
