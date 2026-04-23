# PROGRESS

`docs/PROGRESS.md` is the single task ledger for this repository. Add tasks when they are identified, keep incomplete work visible, and update this file before reporting completion.

## Current Milestone
Phase 1: host/control mainline

Goal: deliver the first vertical slice of Termix using the original spec phases, with focus on `termix`, `termixd`, `termix-control`, PostgreSQL, tmux session creation, and local attach.

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

## In Progress
- [ ] No active in-progress tasks.

## Pending
- [ ] Implement `termixd` bootstrap, local state, and tmux orchestration.
- [ ] Implement thin `termix` CLI commands: `login`, `start`, `sessions attach`, `doctor`.
- [ ] Add unit, integration, and smoke-test coverage for the Phase 1 slice.
- [ ] Revisit delayed Phase 1 work for `termix-admin-api` and admin Web UI after the host/control slice is stable.

## Blocked
- [ ] No active blockers.

## Next Up
1. Implement `termixd` bootstrap, local state, and tmux orchestration.
2. Implement thin `termix` CLI commands: `login`, `start`, `sessions attach`, `doctor`.
3. Add unit, integration, and smoke-test coverage for the Phase 1 slice.
