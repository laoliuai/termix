# PROGRESS

`docs/PROGRESS.md` is the single task ledger for this repository. Add tasks when they are identified, keep incomplete work visible, and update this file before reporting completion.

## Current Milestone
Phase 2: control lease and remote input plan review

Status: the host/control slice and Phase 2 relay/watch foundation are complete. The backend-only control lease and remote input design is approved, and the implementation plan is written for review. Android UI remains outside this pass.

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

## In Progress
- [ ] No active in-progress tasks.

## Pending
- [ ] Deferred: revisit `termix-admin-api` and admin Web UI after the host/control mainline when those surfaces are ready to be scheduled.

## Blocked
- [ ] No active blockers.

## Next Up
1. Execute the Phase 2 control lease and remote input implementation plan after review.
2. Deferred: revisit `termix-admin-api` and admin Web UI when those surfaces are ready to be scheduled.
