# Repository Guidelines

## Governance Priority
`AGENTS.md` is the top-level contributor contract for this repository. `docs/termix-v1-detailed-technical-spec.md` is the authoritative product and technical spec. Approved phase-level design decisions should be written under `docs/` and treated as binding until replaced. `docs/PROGRESS.md` is the single task ledger for the project: every new task, status change, completion, deferral, and blocker must be recorded there. Work is not considered complete until `docs/PROGRESS.md` has been updated.

## Project Skeleton
The repository should grow into this monorepo layout, even if a given phase implements only part of it:

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

Any change to this top-level skeleton must also update `AGENTS.md`. Keep contracts and schemas at the root, runtime services under `go/`, admin-only Python services under `python/`, admin UI under `web/admin`, and Android code under `android/`.

## Progress Tracking Rules
`docs/PROGRESS.md` is the only project status surface.

- Add a task to `docs/PROGRESS.md` as soon as it is identified.
- Update the file whenever a task changes state: `Pending`, `In Progress`, `Completed`, or `Blocked`.
- Keep unfinished, deferred, and blocked work in the file; do not delete it just because it is incomplete.
- Update `docs/PROGRESS.md` before reporting a task as finished.

## Architecture Guardrails
Follow the Termix V1 spec strictly.

- `tmux` is mandatory; do not replace it with a raw PTY-only design.
- Go owns the CLI, daemon, control plane, relay, gRPC contracts, and PostgreSQL access.
- Python is for the admin API and QA tooling only; do not put Python on the terminal byte path.
- Android and future clients must never talk to `tmux` directly.
- Use `sqlc + pgx` for Go database access, `oapi-codegen` for Go REST contracts, protobuf for gRPC, and `golang-migrate` for schema migrations.
- Do not upload full environment snapshots to cloud APIs.
- Do not store full terminal transcripts in PostgreSQL.

## Worktree Convention
Project-local git worktrees live under `.worktrees/` at the repository root. The directory is git-ignored, so worktree contents never leak into commits.

- Create a worktree with a fresh branch: `git worktree add .worktrees/<short-name> -b <branch>`.
- Use one worktree per feature slice; never share a worktree across unrelated work.
- After a slice is merged or abandoned, remove its worktree with `git worktree remove .worktrees/<short-name>` so the directory stays small.

## Development Flow
Prefer contract-first vertical slices. Define or update migrations, OpenAPI, and protobuf contracts before implementing service code that depends on them. Keep `termix` thin, put orchestration in `termixd`, and keep `termix-control` as the source of truth for users, devices, tokens, and session metadata.

Standard commands once the workspace exists are:

```bash
cd go && go test ./...
cd python && uv sync && uv run pytest
cd web/admin && npm run dev
cd android && ./gradlew test
```

Regenerate typed code whenever `openapi/`, `proto/`, `schemas/`, or `go/sql/queries/*.sql` changes.
