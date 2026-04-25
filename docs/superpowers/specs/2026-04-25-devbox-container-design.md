# Termix Devbox Container Design

Status: written for review
Date: 2026-04-25
Source of truth: this document

## Purpose

This document defines a self-contained Docker-based development environment ("devbox") that lets contributors work on the Termix repository using AI coding agents (Claude Code, Codex, opencode) under separate API keys, without leaking those keys into the host's user account or polluting the host's `~/.claude` / `~/.codex` configuration.

The devbox is a developer convenience artifact, not part of any Termix runtime path. It does not change the Termix V1 spec, the relay/control architecture, or the production deployment story.

This is a design-only artifact. It does not authorize implementation by itself.

## Repository Context

As of 2026-04-25 the repository contains Go services under `go/`, an in-progress Python admin layer planned under `python/`, a planned `web/admin` SPA, and Android sources under `android/`. CLAUDE.md prescribes the toolchain: Go (with `sqlc + pgx`, `oapi-codegen`, `golang-migrate`, protobuf via `buf`), Python (`uv`), and Node.js for the admin web UI.

The repository currently has no shared development container. Contributors install toolchains directly on the host. Running AI coding agents on the host shares one `~/.claude` profile across all projects and personal use, which conflicts with the user's need to develop under a different API key for this project.

## Scope

This design includes:

- A new `dev/devbox/` directory containing a `Dockerfile`, `docker-compose.yml`, `.env.example`, `entrypoint.sh`, `.dockerignore`, and `README.md`.
- A CPU-only Ubuntu 22.04 image carrying Go 1.25.x, Node.js 22 LTS, Python 3.12 with `uv`, the Go tools required by the Termix workflow (`sqlc`, `oapi-codegen`, `golang-migrate`, `protoc`, `buf`, `protoc-gen-go`, `protoc-gen-go-grpc`), and three AI agents (Claude Code, Codex, opencode).
- A non-root `dev` user whose UID/GID are aligned at container start to the values supplied by the host so that bind-mounted source files do not become root-owned.
- A `docker-compose.yml` that runs the devbox as a long-lived service, bind-mounts the project directory, and persists agent configuration plus shell history in named volumes.
- A `.env`-driven secret injection model that exposes `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, and provider keys for opencode without baking secrets into the image and without ever mounting the host's `~/.claude` or `~/.codex` directories.
- Documentation describing build, start, exec, validation, teardown, and migration to a different repository.

This design explicitly excludes:

- GPU support (CUDA, cuDNN, NVIDIA runtime). Deferred to a later, separately-designed image.
- Android SDK, JDK, and `./gradlew` flows. Android development continues on the host.
- CI integration and image publishing to a registry. The image is built locally on demand.
- Any change to Termix runtime services, contracts, schemas, or PostgreSQL migrations.
- IDE-specific integrations (VS Code Remote-Containers `devcontainer.json`, JetBrains Gateway). The compose file is plain enough that those tools can wrap it later if desired.

## Decision

Use a **self-contained `dev/devbox/` directory backed by docker compose with named volumes for persistent agent state**.

The devbox is a long-running container started with `docker compose up -d` and entered via `docker compose exec devbox bash`. The image is built from `dev/devbox/Dockerfile`; nothing inside the image is `COPY`'d from outside `dev/devbox/`. The project source is supplied at runtime by bind-mounting the repository root into `/workspace`.

Three properties are load-bearing:

1. **Self-containment.** `dev/devbox/` can be copied verbatim into a new repository. Migration is `cp -r dev/devbox /path/to/new/repo/dev/devbox`, with no follow-up edits required other than (optionally) overriding `WORKSPACE_DIR` in `.env` if the new layout puts the compose file at a different relative depth from the project root.
2. **Host isolation of agent identity.** The host's `~/.claude` and `~/.codex` directories are never mounted into the container. The container's `/home/dev/.claude`, `/home/dev/.codex`, and `/home/dev/.config/opencode` live in named docker volumes (`devbox-claude-home`, `devbox-codex-home`, `devbox-opencode-home`). Each agent therefore runs against the API key supplied through `.env`, not against the host's logged-in account.
3. **Persistence of dev state across restarts.** Shell history (`/home/dev/.history`) and the Go module cache (`/home/dev/go`) are also in named volumes, so a `docker compose down` (without `-v`) followed by `docker compose up -d` retains agent sessions, shell history, and module downloads.

## Directory Layout

```
dev/devbox/
  Dockerfile
  docker-compose.yml
  .env.example
  .gitignore           # ignores .env so the secrets file never gets committed
  entrypoint.sh
  .dockerignore
  README.md
```

`dev/devbox/Dockerfile` does not reference any path outside `dev/devbox/`. The bind-mount of the repository root is configured exclusively in `docker-compose.yml`.

## Image Contents

Base image: `ubuntu:22.04`.

System packages (apt):

- `git`, `curl`, `wget`, `ca-certificates`, `gnupg`, `lsb-release`
- `build-essential`, `pkg-config`
- `tmux`, `openssh-client`
- `ripgrep`, `jq`, `vim`, `less`, `unzip`, `xz-utils`
- `postgresql-client` (for ad-hoc psql against Termix dev databases)
- `sudo`, `gosu` (gosu for the entrypoint UID-switch step)
- `protobuf-compiler` (provides `protoc`)
- `gh` (GitHub CLI, installed from the official apt repository)

Language runtimes:

- **Go 1.25.x** installed from the official `https://go.dev/dl/` tarball into `/usr/local/go`, with `/usr/local/go/bin` and `/home/dev/go/bin` on `PATH`.
- **Node.js 22 LTS** installed from the NodeSource apt repository.
- **Python 3.12** installed from the `ppa:deadsnakes/ppa` PPA (Ubuntu 22.04's archive ships Python 3.10, which is too old for the user's preference), accompanied by `python3.12-venv` and `python3-pip`. `uv` manages project virtualenvs and does not require a system `pip` for project work, but `python3-pip` is included for convenience.
- **`uv`** installed via the official Astral install script into `/usr/local/bin`.

Go developer tools (installed during build via `go install`):

- `github.com/sqlc-dev/sqlc/cmd/sqlc@latest`
- `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest`
- `github.com/golang-migrate/migrate/v4/cmd/migrate@latest`
- `google.golang.org/protobuf/cmd/protoc-gen-go@latest`
- `google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest`
- `github.com/bufbuild/buf/cmd/buf@latest`

Versions are pinned at `@latest` for the first cut. Reproducibility (pinning specific tool versions) is intentionally deferred. If a future build fails because a tool changed, that is the trigger to introduce pins; not before.

AI agent CLIs (installed via `npm install -g`):

- `@anthropic-ai/claude-code`
- `@openai/codex`
- `opencode-ai`

User account:

- A non-root user `dev` with default UID `1000`, GID `1000`, login shell `bash`, home `/home/dev`, sudo group membership.

Working directory:

- `WORKDIR /workspace` at the end of the Dockerfile. The compose file mounts the repository root onto `/workspace`.

## Compose Service Definition

`dev/devbox/docker-compose.yml`:

```yaml
services:
  devbox:
    build:
      context: .
    image: termix-devbox:latest
    container_name: termix-devbox
    env_file: .env
    environment:
      HOST_UID: ${HOST_UID:-1000}
      HOST_GID: ${HOST_GID:-1000}
    volumes:
      - ${WORKSPACE_DIR:-../..}:/workspace:rw
      - devbox-claude-home:/home/dev/.claude
      - devbox-codex-home:/home/dev/.codex
      - devbox-opencode-home:/home/dev/.config/opencode
      - devbox-shell-history:/home/dev/.history
      - devbox-go-cache:/home/dev/go
    working_dir: /workspace
    command: sleep infinity
    tty: true
    stdin_open: true

volumes:
  devbox-claude-home:
  devbox-codex-home:
  devbox-opencode-home:
  devbox-shell-history:
  devbox-go-cache:
```

`WORKSPACE_DIR` defaults to `../..`, which is `dev/devbox/../..` = the repository root. A migration to a new repository keeps the same relative layout if `dev/devbox/` is copied as a unit.

## .env Contract

`dev/devbox/.env.example` is committed. The implementation also adds `dev/devbox/.gitignore` containing `.env` so the secrets file is never committed regardless of what the repository root `.gitignore` currently does:

```bash
# AI agent API keys (host's ~/.claude is never mounted; agents read these env vars)
ANTHROPIC_API_KEY=
OPENAI_API_KEY=

# opencode supports multiple providers; populate only what you need
# OPENROUTER_API_KEY=
# GROQ_API_KEY=

# UID/GID matching the host so bind-mounted files are owned by you, not root
# Usual usage: export HOST_UID=$(id -u) HOST_GID=$(id -g) before docker compose
HOST_UID=1000
HOST_GID=1000

# Path to the repository root, relative to docker-compose.yml
WORKSPACE_DIR=../..
```

## Entrypoint Behaviour

`dev/devbox/entrypoint.sh` runs as PID 1 (root):

1. Reads `HOST_UID` and `HOST_GID` from the environment.
2. If `HOST_GID` differs from `dev`'s current GID, runs `groupmod -g "$HOST_GID" dev`.
3. If `HOST_UID` differs from `dev`'s current UID, runs `usermod -u "$HOST_UID" dev`.
4. `chown -R dev:dev /home/dev` to fix ownership of files generated during image build.
5. `exec gosu dev "$@"` to drop privileges and run the compose `command` (`sleep infinity`) as `dev`.

Subsequent `docker compose exec devbox bash` sessions land in a `dev` shell directly because compose's default exec user is the image's `USER`; the Dockerfile sets `USER dev` for the final layer (entrypoint runs before USER takes effect for the main command, because the entrypoint is invoked with the original root identity from the OCI runtime — implementation detail to verify; alternative is to leave `USER` unset and rely on the entrypoint to drop privileges, then have `docker compose exec --user dev devbox bash` in the README).

The implementation should choose whichever approach yields a `dev`-owned shell on plain `docker compose exec devbox bash` without flags. The plan can pick the exact mechanism after a short experimental check.

## Workflow

One-time setup:

```bash
cd dev/devbox
cp .env.example .env
$EDITOR .env                              # fill API keys
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose build
docker compose up -d
```

Daily use:

```bash
cd dev/devbox
docker compose exec devbox bash
# inside the container:
cd /workspace/go && go test ./...
claude        # uses ANTHROPIC_API_KEY from .env
codex         # uses OPENAI_API_KEY from .env
opencode      # uses provider keys from .env
```

Pause / resume across reboots: `docker compose stop` and `docker compose start` (volumes survive).

Reset agent state without rebuilding the image: `docker compose down && docker volume rm devbox-claude-home devbox-codex-home devbox-opencode-home && docker compose up -d`.

Full teardown: `docker compose down -v` (drops all named volumes; agent history, shell history, and Go module cache are gone).

Migration to a new repository:

```bash
cp -r /path/to/termix/dev/devbox /path/to/new-repo/dev/devbox
cd /path/to/new-repo/dev/devbox
cp .env.example .env && $EDITOR .env
docker compose up -d
```

If the new repository places `dev/devbox/` at a different relative depth from the project root, the user overrides `WORKSPACE_DIR` in `.env`.

## Validation Targets

After `docker compose up -d` and `docker compose exec devbox bash`, the following commands must succeed:

- `id` reports a UID/GID equal to the host user's UID/GID.
- `go version` reports `go1.25` or newer.
- `node -v` reports `v22.x`.
- `python3.12 --version` reports `3.12.x` and `uv --version` succeeds.
- `sqlc version`, `oapi-codegen --version`, `migrate -version`, `protoc --version`, `buf --version` all succeed.
- `claude --version`, `codex --version`, `opencode --version` all succeed.
- `cd /workspace/go && go test ./...` runs with the same outcome as on the host.
- `touch /workspace/.devbox-write-test && rm /workspace/.devbox-write-test` succeeds and the file is owned by the host user (not root) when observed from the host.
- `ls -la /home/dev/.claude` shows volume-backed contents that survive `docker compose down` followed by `docker compose up -d`.
- The host's `~/.claude` directory is unchanged after running Claude Code inside the container.

## Risks and Open Questions

- **`@latest` on Go tools**: a future surprise upgrade may break the build. The decision to defer pinning is intentional; the implementation plan should ensure the build is fast enough that re-running it against new tool versions is cheap.
- **opencode install path**: the npm package name is `opencode-ai`. If that package is not the user-intended `opencode`, the implementation step that installs it should pause and confirm before continuing. (The user explicitly approved adding "opencode" alongside Codex, and the only widely-distributed CLI by that name is `opencode-ai` from sst; this is recorded so the implementer can verify quickly.)
- **PostgreSQL inside the devbox**: not included. Termix integration tests that require a real Postgres are expected to use the user's existing host-side database or a separate `docker compose` stack. If that becomes friction, a follow-up design can add a `postgres` service to the same compose file, but it is out of scope here.
- **Image size**: the design accepts an image in the 1.5–2.5 GB range. No effort is being made in this slice to slim it (multi-stage builds that drop apt caches and Go build caches are encouraged but not required).

## Out of Scope and Follow-Ups

- A GPU-enabled variant of the image.
- Pinned tool versions (Go, Node, sqlc, etc.).
- A registry-published image.
- IDE-specific configuration (`devcontainer.json`, JetBrains Gateway).
- A bundled Postgres service for tests that need one.
- Android SDK support.

These are recorded so they are easy to schedule later if needed.
