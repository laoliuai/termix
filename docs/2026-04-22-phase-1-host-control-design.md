# Termix Phase 1 Host/Control Design

Status: approved for planning  
Date: 2026-04-22  
Source of truth: `docs/termix-v1-detailed-technical-spec.md`

## Purpose
This document captures the approved design for **Phase 1** of Termix. It narrows the full V1 spec to the first implementation milestone while preserving the final architecture.

## Phase 1 Scope
Phase 1 follows the original spec phases, but prioritizes the host/control mainline inside that phase. The immediate delivery target is:

- PostgreSQL schema and migrations for the first host/control entities
- `termix-control` authentication and host session APIs
- `termixd` bootstrap, local state, and tmux orchestration
- thin `termix` CLI commands for login, start, attach, and doctor
- local attach to the real tmux-backed session

Still part of broader Phase 1, but intentionally delayed until after the host/control mainline is stable:

- `termix-admin-api`
- admin Web UI user CRUD

Explicitly out of scope for this design slice:

- relay WSS data plane
- Android watch/control flows
- remote control lease
- preview streaming to remote clients

## Success Criteria
Phase 1 is successful when all of the following are true:

1. `termix login` obtains and stores host-side credentials securely.
2. `termix start <tool> [-n name]` starts or reuses `termixd`.
3. `termixd` creates a server-side session record through `termix-control`.
4. `termixd` creates a tmux session named `termix_<session_id>`.
5. The local terminal attaches to that tmux session.
6. `termixd` can recover known local sessions after restart.

## Repository Skeleton
The project should adopt the full target monorepo shape from the beginning so later phases do not need structural rewrites:

```text
docs/
  termix-v1-detailed-technical-spec.md
  PROGRESS.md

db/
  migrations/

openapi/
proto/
schemas/

go/
  cmd/
    termix/
    termixd/
    termix-control/
    termix-relay/
  internal/
    auth/
    config/
    controlapi/
    daemonipc/
    diagnostics/
    persistence/
    session/
    tmux/
  gen/
  sql/
  tests/

python/
  apps/
    termix_admin_api/
  packages/

web/
  admin/

android/
  app/
  terminal-web/
```

Only the host/control slice needs implementation in Phase 1, but the skeleton above is the default repository shape.

## Core Architecture
Phase 1 uses a contract-first vertical slice:

`termix CLI -> termixd over gRPC/UDS -> termix-control over HTTPS -> Postgres`

`termixd -> tmux`

Responsibilities:

- `termix`: user-facing entrypoint. Captures launch context, ensures the daemon is running, calls daemon gRPC, then hands off to `tmux attach-session`.
- `termixd`: local orchestrator. Manages daemon lifecycle, local session state files, tmux creation, recovery, and control-plane calls.
- `termix-control`: source of truth for users, devices, refresh tokens, and session metadata.
- Postgres: stores `users`, `devices`, `refresh_tokens`, and `sessions` first.
- `tmux`: mandatory session substrate. One Termix session maps to one tmux session.

## Contract Decisions
REST base path is fixed to `/api/v1`. Earlier `/v1` examples in the spec should be treated as shorthand, not a second supported prefix.

### Login flow
`termix login` collects `server_url`, `email`, and `password`, then calls `POST /api/v1/auth/login`. `termix-control` authenticates the user, creates or updates the host device row, issues a JWT access token and opaque refresh token, and returns `user`, `device`, `access_token`, `refresh_token`, and `expires_in_seconds`.

Local credential storage for Phase 1 should be abstracted behind a `credential store` interface:

- Ubuntu: `~/.config/termix/credentials.json`, mode `0600`
- macOS: Phase 1 may use `~/Library/Application Support/Termix/credentials.json`, mode `0600`, while preserving a future Keychain swap

Stored fields are limited to:

- `server_base_url`
- `device_id`
- `user_id`
- `access_token`
- `refresh_token`
- `expires_at`

### Start flow
`termix start <tool> [-n name]` captures launch context locally only:

- `cwd`
- `env`
- `shell`
- `TERM`
- `LANG`
- `LC_*`
- requested `tool`
- requested `name`

The CLI starts or reuses `termixd`, waits for UDS health, and calls `StartSession`. `termixd` then:

1. Calls `POST /api/v1/host/sessions`
2. Receives `session_id` and `tmux_session_name`
3. Creates `termix_<session_id>` in tmux
4. Injects cwd and environment locally
5. Starts the requested tool
6. Marks the session `running` via `PATCH /api/v1/host/sessions/{session_id}`

Finally, the CLI runs `tmux attach-session -t termix_<session_id>`.

Full environment data never leaves the host. The cloud stores safe metadata only, such as `tool`, `name`, `launch_command`, `cwd_label`, and `hostname`.

## Phase 1 Module Layout
Files and modules to implement first:

- `db/migrations/`: initial schema for `users`, `devices`, `refresh_tokens`, `sessions`
- `openapi/control.openapi.yaml`: auth, device, and host session endpoints
- `proto/daemon.proto`: local gRPC between CLI and daemon
- `go/cmd/termix`: thin CLI
- `go/cmd/termixd`: daemon
- `go/cmd/termix-control`: control plane

Initial Go internal packages:

- `auth`
- `config`
- `controlapi`
- `daemonipc`
- `diagnostics`
- `persistence`
- `session`
- `tmux`

## Failure Handling
Phase 1 should favor clear recovery over complex compensation:

- login failure must not leave partial credentials on disk
- if cloud session creation fails, do not create tmux state
- if tmux creation fails after a cloud session exists, mark the session `failed` with a size-limited error
- if local attach fails after tmux starts, do not kill the running session; return the manual `tmux attach-session` command
- daemon restart should scan local session files, compare against `tmux ls`, rebuild memory state, and reconcile with control-plane status

## Verification Strategy
Required verification for Phase 1:

- unit tests for credential storage, token/auth logic, configuration loading, tmux command generation, and daemon session state
- integration tests for `termix-control` auth/device/session handlers against Postgres
- integration tests for `termixd` tmux creation and local state persistence
- CLI happy-path tests for `login`, `start`, and `sessions attach`
- manual smoke validation on at least Ubuntu and macOS hosts

## Repository Governance
The following governance decisions are part of the approved design:

- the repository uses the full target skeleton from the beginning
- `AGENTS.md` is the top-level contributor contract
- `docs/PROGRESS.md` is the single task ledger
- every task must be recorded in `docs/PROGRESS.md` when identified and when its state changes
- work is not complete until `docs/PROGRESS.md` has been updated

## Phase 1 Acceptance Boundary
Accepted in Phase 1:

- host login
- daemon bootstrap
- session metadata creation and update
- tmux-backed local session launch
- local attach
- daemon restart recovery

Deferred after the host/control mainline:

- admin API and admin Web UI

Deferred to later spec phases:

- relay
- Android live watch
- remote control
- preview delivery to remote clients
