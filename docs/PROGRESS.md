# PROGRESS

`docs/PROGRESS.md` is the single task ledger for this repository. Add tasks when they are identified, keep incomplete work visible, and update this file before reporting completion.

## Current Milestone
Phase 2: control lease and remote input complete

Status: the host/control slice, Phase 2 relay/watch foundation, backend control lease/input loop, internal relay-control gRPC adapter, and end-to-end gRPC validation are complete. `termix-relay` now requires `TERMIX_RELAY_CONTROL_GRPC_ADDR` and no longer accepts the REST fallback. Android UI remains deferred.

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

## In Progress
- [ ] Brainstorm and approve the developer devbox container design (`docs/superpowers/specs/2026-04-25-devbox-container-design.md`); design written and pending user review before implementation plan is drafted.

## Pending
- [ ] Write the devbox container implementation plan once the design is approved.
- [ ] Implement the `dev/devbox/` development container (Ubuntu 22.04 image, Go 1.25 + Node.js 22 LTS + Python 3.12 with uv + Go tooling + Claude Code/Codex/opencode, isolated agent state via named volumes).
- [ ] Deferred: remove the now-unused REST control-lease HTTP handlers (`POST /sessions/{id}/control/{acquire,renew,release}`, `GET /sessions/{id}`) and the matching `controlapi.Client` lease/viewer methods after Android end-to-end testing confirms no REST consumer is needed.
- [ ] Deferred: implement relay-control connection lifecycle RPCs when audit or online presence is scheduled.
- [ ] Deferred: revisit `termix-admin-api` and admin Web UI after the host/control mainline when those surfaces are ready to be scheduled.

## Blocked
- [ ] No active blockers.

## Next Up
1. Land the developer devbox container so contributors can develop the project under a separate API key without polluting the host's AI agent state.
2. Decide whether to add Android control UI next.
3. Deferred: remove the REST control-lease HTTP surface once Android testing confirms no REST consumer.
4. Deferred: revisit `termix-admin-api` and admin Web UI when ready.
