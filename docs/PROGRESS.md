# PROGRESS

`docs/PROGRESS.md` is the single task ledger for this repository. Add tasks when they are identified, keep incomplete work visible, and update this file before reporting completion.

## Current Milestone
Phase 2: relay/watch foundation design

Status: the host/control slice is complete. A Phase 2 design for relay/watch transport, initial snapshot delivery, and generic viewer protocol has been drafted and is awaiting review before implementation planning.

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

## In Progress
- [ ] Review the Phase 2 relay/watch foundation design and turn it into an implementation plan.

## Pending
- [ ] Deferred: revisit `termix-admin-api` and admin Web UI after the host/control mainline when those surfaces are ready to be scheduled.

## Blocked
- [ ] No active blockers.

## Next Up
1. Review the Phase 2 relay/watch foundation design and write the implementation plan.
2. Deferred: revisit `termix-admin-api` and admin Web UI when those surfaces are ready to be scheduled.
