# PROGRESS

`docs/PROGRESS.md` is the single task ledger for this repository. Add tasks when they are identified, keep incomplete work visible, and update this file before reporting completion.

## Current Milestone
Phase 2: control lease and remote input complete; starting Android UI slice 1 (`terminal-web` MVP) in parallel with developer devbox container work

Status: the host/control slice, Phase 2 relay/watch foundation, backend control lease/input loop, internal relay-control gRPC adapter, and end-to-end gRPC validation are complete. `termix-relay` now requires `TERMIX_RELAY_CONTROL_GRPC_ADDR` and no longer accepts the REST fallback. Android UI is now in scope, decomposed into two sub-slices: slice 1 builds `android/terminal-web/` (xterm.js+TS WSS client bundle, browser-testable in isolation); slice 2 will follow with the Kotlin/Compose shell.

## Completed
- [x] Choose the original spec phase sequence for delivery.
- [x] Narrow the immediate Phase 1 focus to the host/control mainline.
- [x] Approve contract-first vertical-slice implementation for Phase 1.
- [x] Define Phase 1 minimum success criteria for `login`, `start`, session registration, tmux creation, and local attach.
- [x] Approve the target repository skeleton and directory ownership.
- [x] Approve repository governance: `AGENTS.md` for rules, `docs/PROGRESS.md` for task tracking.
- [x] Write the Phase 1 host/control design document.
- [x] Update `AGENTS.md` with repository skeleton and progress-tracking rules.
- [x] Create `docs/PROGRESS.md`.
- [x] Write the Phase 1 implementation plan.
- [x] Select subagent-driven execution for the approved implementation plan.
- [x] Execute Task 1: bootstrap the monorepo skeleton and tooling.
- [x] Execute Task 2: build config, credential, and auth primitives.
- [x] Execute Task 3: define the initial PostgreSQL migrations and generated query layer.
- [x] Execute Task 4: define `openapi/control.openapi.yaml` and the generated control client.
- [x] Execute Task 5: define `proto/daemon.proto` and daemon IPC adapters.
- [x] Execute Task 6: implement `termix-control` auth and host session endpoints.
- [x] Initialize the local Git repository and add the GitHub remote.
- [x] Create the initial repository commit and push `main` to `origin`.
- [x] Adjust Codex local approval and filesystem-permission defaults.
- [x] Implement `termixd` bootstrap, local state, and tmux orchestration.
- [x] Implement thin `termix` CLI commands: `login`, `start`, `sessions attach`, `doctor`.
- [x] Add unit, integration, and smoke-test coverage for the Phase 1 slice.
- [x] Draft the Phase 2 relay/watch foundation design.
- [x] Write the Phase 2 relay/watch foundation implementation plan.
- [x] Complete Phase 2 Task 1: persist relay-capable host config during login.
- [x] Complete Phase 2 Task 2: add session detail reads for relay watch authorization.
- [x] Complete Phase 2 Task 3: define the relay protocol artifacts and Go codec layer.
- [x] Complete Phase 2 Task 4: add tmux snapshot and control-mode stream helpers.
- [x] Complete Phase 2 Task 5: add the daemon-side relay client and session publishing hooks.
- [x] Complete Phase 2 Task 6: implement the relay WSS server and watch handshake.
- [x] Complete Phase 2 Task 7: finish snapshot/live-output forwarding, verify the slice, and update the ledger.
- [x] Implement the Phase 2 relay/watch foundation.
- [x] Draft the Phase 2 control lease and remote input design.
- [x] Write the Phase 2 control lease and remote input implementation plan.
- [x] Implement the Phase 2 backend control lease and remote input slice.
- [x] Add a repo-specific Codex sandbox override so `git` commands in the `termix` workspace can write `.git` without approval prompts.
- [x] Brainstorm the internal relay-control gRPC adapter design.
- [x] Approve the internal relay-control gRPC adapter design.
- [x] Write the internal relay-control gRPC adapter implementation plan.
- [x] Implement the internal relay-control gRPC adapter for the Android backend watch/control loop.
- [x] Fix integration-test flake: randomize `tmux_session_name` in `TestCreateSessionRecord` and `TestOwnerCanFetchSessionDetailAndForeignUserCannot` so they no longer collide on the unique constraint when sharing a Postgres test database.
- [x] Validate the Phase 2 relay-control gRPC path end-to-end with a real Postgres + gRPC + relay + WSS integration test.
- [x] Remove the relay REST authorizer fallback so `termix-relay` requires the internal gRPC adapter.
- [x] Brainstorm the developer devbox container design.
- [x] Approve the developer devbox container design.
- [x] Write the developer devbox container implementation plan.
- [x] Decide to start Android control UI next, decomposed into two slices: `terminal-web` first, Compose shell second.
- [x] Brainstorm the Android slice 1 (`terminal-web` MVP) design.
- [x] Write the Android slice 1 (`terminal-web` MVP) design doc (`docs/superpowers/specs/2026-04-25-android-terminal-web-mvp-design.md`).
- [x] Write the Android slice 1 (`terminal-web` MVP) implementation plan (`docs/superpowers/plans/2026-04-25-android-terminal-web-mvp.md`).
- [x] Implement the developer devbox container under `dev/devbox/` (Ubuntu 22.04 + Go 1.25 + Node 22 LTS + Python 3.12 + uv + Go tooling + Claude Code/Codex/opencode, isolated agent state via named volumes; build args `APT_MIRROR`/`GOPROXY`/`NPM_REGISTRY`/`HOST_UID`/`HOST_GID`).
- [x] Implement Android slice 1: `terminal-web` MVP per `docs/superpowers/plans/2026-04-25-android-terminal-web-mvp.md` (Vite + TS + xterm.js bundle, JS bridge contract, WSS protocol, control-lease state machine with auto-renew, dev harness, 67 unit tests; manual smoke checklist deferred to user pre-merge per README).
- [x] Make relay WSS accept `?access_token=` query parameter as a fallback when the Authorization header is missing (browser WebSocket and Android WebView both block setting custom headers). Header still wins when both are present. Covered by two new integration tests in `relay_integration_test.go`.
- [x] Extend `accessTokenTTL` in `controlapi` from 15 minutes to 30 days for a less painful dev/smoke-test loop.

## In Progress
- [ ] No active in-progress tasks.

## Pending
- [ ] Brainstorm and design Android slice 2: `android/app/` Kotlin+Compose shell (login, session list, WebView host, toolbar, reconnect).
- [ ] Implement Android slice 2: Kotlin+Compose shell.
- [ ] Fix `config.DeriveHostConfig` (go/internal/config/store.go:42) so the derived `relay_ws_url` can target a different host:port from the control server. Today it copies host:port from the server base URL with the path swapped to `/ws`, which forces local dev to manually patch `host.json` after login. Likely fix: read a separate relay base URL from login response or env var.
- [ ] Add `termix sessions list` CLI subcommand. The smoke-test README references it but only `sessions attach` is implemented; users currently have to query Postgres directly to discover their session_id.
- [ ] Fix `TestOwnerCanFetchSessionDetailAndForeignUserCannot` flake: hardcoded `owner@example.com`/`other@example.com` user emails collide on repeat runs against a shared Postgres test DB. Randomize per-run via `uuid.NewString()` like the earlier `tmux_session_name` fix did.
- [ ] Fix `termix login` interactive-prompt input handling: `runLogin` calls `readLine` three times, and `readLine` (`go/cmd/termix/main.go:291`) creates a new `bufio.NewReader` per call. Piped stdin gets buffered into the first reader, then lost when it's garbage-collected, so non-TTY logins fail email-regex validation with empty values. The smoke script works around this by calling the login REST endpoint directly via curl. Fix: hoist a single bufio.Reader into `cliDeps` and reuse it across all prompts.
- [ ] Deferred: remove the now-unused REST control-lease HTTP handlers (`POST /sessions/{id}/control/{acquire,renew,release}`, `GET /sessions/{id}`) and the matching `controlapi.Client` lease/viewer methods after Android end-to-end testing confirms no REST consumer is needed.
- [ ] Deferred: implement relay-control connection lifecycle RPCs when audit or online presence is scheduled.
- [ ] Deferred: revisit `termix-admin-api` and admin Web UI after the host/control mainline when those surfaces are ready to be scheduled.

## Blocked
- [ ] No active blockers.

## Next Up
1. Run the manual smoke checklist in `android/terminal-web/README.md` against a live Go stack (Postgres, control, relay, termixd, a started session) to validate slice 1 before slice 2 work begins.
2. Brainstorm and design Android slice 2: `android/app/` Kotlin+Compose shell.
3. Implement Android slice 2: Kotlin+Compose shell.
4. Deferred: remove the REST control-lease HTTP surface once Android end-to-end testing confirms no REST consumer.
5. Deferred: revisit `termix-admin-api` and admin Web UI when ready.
