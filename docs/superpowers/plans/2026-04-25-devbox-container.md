# Termix Devbox Container Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-contained Ubuntu 22.04 Docker dev container under `dev/devbox/` so contributors can develop the Termix project using Claude Code, Codex, and opencode under their own API keys without polluting the host's `~/.claude` / `~/.codex` configuration.

**Architecture:** A single `Dockerfile` builds an `ubuntu:22.04`-based image carrying Go 1.25.0, Node.js 22 LTS, Python 3.12 + uv, the Termix Go developer tools (`sqlc`, `oapi-codegen`, `golang-migrate`, `protoc-gen-go`, `protoc-gen-go-grpc`, `buf`), and three AI agent CLIs (Claude Code, Codex, opencode). A non-root `dev` user with default UID/GID 1000 is created at build time; an `entrypoint.sh` running as root remaps the UID/GID to match the host at startup, then drops to `dev` via `gosu`. `docker-compose.yml` runs the container long-lived, bind-mounts the repository root onto `/workspace`, and persists per-agent state plus shell history in named docker volumes — no host directory other than the project root is ever mounted.

**Tech Stack:** Docker, docker compose v2, Ubuntu 22.04, Go 1.25.0, Node.js 22 LTS, Python 3.12 (deadsnakes), `uv`, `gosu`, npm.

**Spec:** `docs/superpowers/specs/2026-04-25-devbox-container-design.md`.

---

## File Structure

This plan creates the following files (all under `dev/devbox/`):

| File | Responsibility |
|------|----------------|
| `.gitignore` | Guarantees `.env` is never committed. |
| `.dockerignore` | Keeps build context small; excludes `.env`, `README.md`, `.gitignore`. |
| `.env.example` | Template for `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `HOST_UID`, `HOST_GID`, `WORKSPACE_DIR`. |
| `Dockerfile` | Image build: base apt packages → Go → Node.js → Python/uv → Go dev tools → AI agent CLIs → `dev` user → entrypoint. |
| `entrypoint.sh` | Runs as root: `groupmod`/`usermod` to align `dev` with `HOST_UID`/`HOST_GID`, `chown -R dev:dev /home/dev`, then `exec gosu dev "$@"`. |
| `docker-compose.yml` | One service `devbox`, `user: "0:0"` so entrypoint runs as root, named volumes for `~/.claude`, `~/.codex`, opencode config, shell history, GOPATH, and `command: sleep infinity` so the container stays up. |
| `README.md` | Setup, daily usage, validation, teardown, and migration to a new repository. |

This plan modifies:

- `docs/PROGRESS.md` — flip the devbox tasks from "in progress" / "pending" to "completed" once everything works.

Each task below produces a self-contained commit. Image layers are built incrementally so iteration stays fast (apt cache and prior layers cache between rebuilds).

---

## Task 1: Scaffold devbox directory and trivial config files

**Files:**
- Create: `dev/devbox/.gitignore`
- Create: `dev/devbox/.dockerignore`
- Create: `dev/devbox/.env.example`

- [ ] **Step 1: Create `dev/devbox/.gitignore`**

```
.env
```

- [ ] **Step 2: Create `dev/devbox/.dockerignore`**

```
.env
.gitignore
README.md
```

- [ ] **Step 3: Create `dev/devbox/.env.example`**

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

- [ ] **Step 4: Verify `.env` is git-ignored**

```bash
cd dev/devbox
cp .env.example .env
cd ../..
git status --porcelain dev/devbox/.env
```

Expected: empty output. If anything is printed, `.gitignore` is wrong.

Cleanup:
```bash
rm dev/devbox/.env
```

- [ ] **Step 5: Commit**

```bash
git add dev/devbox/.gitignore dev/devbox/.dockerignore dev/devbox/.env.example
git commit -m "Add devbox directory scaffold and .env template"
```

---

## Task 2: Bootstrap Dockerfile with Ubuntu 22.04 base and apt packages

**Files:**
- Create: `dev/devbox/Dockerfile`

- [ ] **Step 1: Create `dev/devbox/Dockerfile` with the base layer**

```dockerfile
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC

# Base apt packages
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget gnupg lsb-release \
      git \
      build-essential pkg-config \
      tmux openssh-client \
      ripgrep jq vim less unzip xz-utils \
      postgresql-client \
      sudo \
      protobuf-compiler \
      software-properties-common \
  && rm -rf /var/lib/apt/lists/*

# gosu for the entrypoint privilege drop
ARG GOSU_VERSION=1.17
RUN ARCH="$(dpkg --print-architecture)" \
  && curl -fsSL -o /usr/local/bin/gosu \
       "https://github.com/tianon/gosu/releases/download/${GOSU_VERSION}/gosu-${ARCH}" \
  && chmod +x /usr/local/bin/gosu \
  && gosu --version

# GitHub CLI from official apt repo
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
       -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
  && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
       > /etc/apt/sources.list.d/github-cli.list \
  && apt-get update && apt-get install -y --no-install-recommends gh \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
```

- [ ] **Step 2: Build the base image**

```bash
cd dev/devbox
docker build -t termix-devbox:latest .
```

Expected: build completes; final stage emits `Successfully tagged termix-devbox:latest`.

- [ ] **Step 3: Verify base tools work**

```bash
docker run --rm termix-devbox:latest bash -c \
  'git --version && tmux -V && gh --version | head -1 && rg --version | head -1 && jq --version && gosu --version'
```

Expected: each line prints a version (e.g., `git version 2.x`, `tmux 3.x`, `gh version 2.x.x`, `ripgrep x.y.z`, `jq-1.x`, `1.17`).

- [ ] **Step 4: Commit**

```bash
git add dev/devbox/Dockerfile
git commit -m "Bootstrap devbox Dockerfile with Ubuntu 22.04 base packages and gosu"
```

---

## Task 3: Add Go 1.25.0 toolchain

**Files:**
- Modify: `dev/devbox/Dockerfile` (append a Go install layer before the trailing `WORKDIR /workspace`).

- [ ] **Step 1: Append the Go install layer**

Insert these lines into `dev/devbox/Dockerfile` immediately before the final `WORKDIR /workspace`:

```dockerfile
# Go 1.25.x
ARG GO_VERSION=1.25.0
RUN ARCH_GO="$(dpkg --print-architecture)" \
  && case "$ARCH_GO" in \
       amd64) GO_ARCH=amd64 ;; \
       arm64) GO_ARCH=arm64 ;; \
       *) echo "unsupported architecture: $ARCH_GO" >&2 && exit 1 ;; \
     esac \
  && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tgz \
  && tar -C /usr/local -xzf /tmp/go.tgz \
  && rm /tmp/go.tgz
ENV PATH=/usr/local/go/bin:$PATH
```

- [ ] **Step 2: Rebuild**

```bash
cd dev/devbox
docker build -t termix-devbox:latest .
```

Expected: build completes; the only newly-fetched layer is the Go tarball.

- [ ] **Step 3: Verify Go version**

```bash
docker run --rm termix-devbox:latest go version
```

Expected: `go version go1.25.0 linux/<arch>` (where `<arch>` is `amd64` on x86_64 hosts).

- [ ] **Step 4: Commit**

```bash
git add dev/devbox/Dockerfile
git commit -m "Add Go 1.25.0 to devbox image"
```

---

## Task 4: Add Node.js 22 LTS

**Files:**
- Modify: `dev/devbox/Dockerfile` (append after the Go layer).

- [ ] **Step 1: Append the Node.js install layer**

Insert before the final `WORKDIR /workspace`, after the Go layer:

```dockerfile
# Node.js 22 LTS via NodeSource
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
  && apt-get install -y --no-install-recommends nodejs \
  && rm -rf /var/lib/apt/lists/*
```

- [ ] **Step 2: Rebuild**

```bash
cd dev/devbox
docker build -t termix-devbox:latest .
```

Expected: build completes.

- [ ] **Step 3: Verify Node.js and npm**

```bash
docker run --rm termix-devbox:latest bash -c 'node -v && npm -v'
```

Expected: `v22.x.x` for node and `10.x.x` or `11.x.x` for npm.

- [ ] **Step 4: Commit**

```bash
git add dev/devbox/Dockerfile
git commit -m "Add Node.js 22 LTS to devbox image"
```

---

## Task 5: Add Python 3.12 and uv

**Files:**
- Modify: `dev/devbox/Dockerfile` (append after the Node.js layer).

- [ ] **Step 1: Append the Python + uv layer**

Insert before the final `WORKDIR /workspace`:

```dockerfile
# Python 3.12 via deadsnakes PPA (Ubuntu 22.04 archive only ships 3.10)
RUN add-apt-repository -y ppa:deadsnakes/ppa \
  && apt-get update && apt-get install -y --no-install-recommends \
       python3.12 python3.12-venv python3.12-dev python3-pip \
  && rm -rf /var/lib/apt/lists/* \
  && ln -sf /usr/bin/python3.12 /usr/local/bin/python3 \
  && ln -sf /usr/bin/python3.12 /usr/local/bin/python

# uv from Astral
RUN curl -LsSf https://astral.sh/uv/install.sh \
      | env UV_INSTALL_DIR=/usr/local/bin sh
```

- [ ] **Step 2: Rebuild**

```bash
cd dev/devbox
docker build -t termix-devbox:latest .
```

Expected: build completes; deadsnakes adds an apt source then installs `python3.12*`.

- [ ] **Step 3: Verify Python and uv**

```bash
docker run --rm termix-devbox:latest bash -c \
  'python3.12 --version && python --version && uv --version'
```

Expected: both `python3.12 --version` and `python --version` print `Python 3.12.x`; `uv --version` prints a `uv 0.x.y` line.

- [ ] **Step 4: Commit**

```bash
git add dev/devbox/Dockerfile
git commit -m "Add Python 3.12 (deadsnakes) and uv to devbox image"
```

---

## Task 6: Add Go developer tools (sqlc, oapi-codegen, migrate, buf, protoc plugins)

These tools must be installed system-wide (under `/usr/local/go-tools/bin`) because the runtime container mounts a named volume on `/home/dev/go`. If we put the binaries there, the volume mount would shadow them on first start.

**Files:**
- Modify: `dev/devbox/Dockerfile` (append after the Python layer).

- [ ] **Step 1: Append the Go tools layer**

Insert before the final `WORKDIR /workspace`:

```dockerfile
# Go developer tools, installed system-wide so the volume-mounted /home/dev/go
# does not shadow them at runtime.
ENV GOTOOLS=/usr/local/go-tools
ENV GOBIN=$GOTOOLS/bin
ENV PATH=$GOBIN:$PATH
RUN mkdir -p $GOBIN \
  && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest \
  && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
  && go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest \
  && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
  && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest \
  && go install github.com/bufbuild/buf/cmd/buf@latest \
  && rm -rf /root/.cache/go-build /root/go/pkg
```

- [ ] **Step 2: Rebuild**

```bash
cd dev/devbox
docker build -t termix-devbox:latest .
```

Expected: build succeeds. This step downloads several Go modules and may take 1–3 minutes on a cold build.

- [ ] **Step 3: Verify each tool is present and runs**

```bash
docker run --rm termix-devbox:latest bash -c '
  set -e
  sqlc version
  oapi-codegen --version
  migrate -version
  buf --version
  protoc --version
  ls /usr/local/go-tools/bin/protoc-gen-go /usr/local/go-tools/bin/protoc-gen-go-grpc
'
```

Expected: every command prints output without error and the two `protoc-gen-*` binaries exist.

- [ ] **Step 4: Commit**

```bash
git add dev/devbox/Dockerfile
git commit -m "Add Go developer tools (sqlc, oapi-codegen, migrate, buf, protoc plugins)"
```

---

## Task 7: Add AI agent CLIs (Claude Code, Codex, opencode)

**Files:**
- Modify: `dev/devbox/Dockerfile` (append after the Go tools layer).

- [ ] **Step 1: Append the npm global install layer**

Insert before the final `WORKDIR /workspace`:

```dockerfile
# AI coding agent CLIs (npm-distributed)
RUN npm install -g \
      @anthropic-ai/claude-code \
      @openai/codex \
      opencode-ai \
  && npm cache clean --force
```

- [ ] **Step 2: Rebuild**

```bash
cd dev/devbox
docker build -t termix-devbox:latest .
```

Expected: build completes. The npm install pulls three packages plus their dependencies and may take 30–90s.

- [ ] **Step 3: Verify each CLI is callable**

```bash
docker run --rm termix-devbox:latest bash -c \
  'claude --version && codex --version && opencode --version'
```

Expected: each command prints a version line.

If `opencode --version` fails because the binary name differs (some npm packages install a binary with a different name), debug:

```bash
docker run --rm termix-devbox:latest bash -c 'ls /usr/lib/node_modules && ls /usr/bin/ /usr/local/bin/ | grep -i opencode'
```

Then either rename the install or correct the binary name in the verification command. Do NOT proceed past this step until all three CLIs respond to `--version` (or whatever the correct version flag is for that binary).

- [ ] **Step 4: Commit**

```bash
git add dev/devbox/Dockerfile
git commit -m "Add Claude Code, Codex, and opencode CLIs to devbox"
```

---

## Task 8: Add the `dev` user, entrypoint, and final image stanza

**Files:**
- Create: `dev/devbox/entrypoint.sh`
- Modify: `dev/devbox/Dockerfile`

- [ ] **Step 1: Create `dev/devbox/entrypoint.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Default to UID/GID 1000 if the host did not pass them through.
TARGET_UID="${HOST_UID:-1000}"
TARGET_GID="${HOST_GID:-1000}"

current_uid="$(id -u dev)"
current_gid="$(id -g dev)"

if [ "$TARGET_GID" != "$current_gid" ]; then
  groupmod -g "$TARGET_GID" dev
fi

if [ "$TARGET_UID" != "$current_uid" ]; then
  usermod -u "$TARGET_UID" dev
fi

# Repair ownership of files that were written during image build with the
# old (build-time) UID/GID.
chown -R dev:dev /home/dev

exec gosu dev "$@"
```

- [ ] **Step 2: Replace the trailing `WORKDIR /workspace` in the Dockerfile**

Remove the existing `WORKDIR /workspace` at the end of the Dockerfile (the only one) and replace it with the following stanza:

```dockerfile
# Non-root dev user. UID/GID may be remapped by the entrypoint at runtime.
RUN groupadd -g 1000 dev \
  && useradd -m -u 1000 -g 1000 -s /bin/bash dev \
  && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/dev \
  && chmod 0440 /etc/sudoers.d/dev

# Pre-create the per-agent state dirs and the persistent shell-history dir,
# all owned by dev so the entrypoint chown is fast.
RUN install -d -o dev -g dev \
      /home/dev/.history \
      /home/dev/go \
      /home/dev/.claude \
      /home/dev/.codex \
      /home/dev/.config/opencode \
  && printf '%s\n' \
       'export HISTFILE=/home/dev/.history/bash_history' \
       'export HISTSIZE=10000' \
       'export HISTFILESIZE=20000' \
       'export GOPATH=/home/dev/go' \
       'export PATH=$PATH:/home/dev/go/bin' \
       'cd /workspace' \
     >> /home/dev/.bashrc \
  && chown dev:dev /home/dev/.bashrc

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /workspace
USER dev
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["sleep", "infinity"]
```

The Dockerfile must end with these directives — `USER dev` is what makes `docker compose exec devbox bash` (with no flags) drop the user into a `dev` shell. The compose service will set `user: "0:0"` to override that default for the **main** process so the entrypoint runs as root.

- [ ] **Step 3: Rebuild the full image**

```bash
cd dev/devbox
docker build -t termix-devbox:latest .
```

Expected: build completes.

- [ ] **Step 4: Verify UID remap with default IDs**

```bash
docker run --rm --user 0:0 -e HOST_UID=1000 -e HOST_GID=1000 \
  termix-devbox:latest id
```

Expected: `uid=1000(dev) gid=1000(dev) groups=1000(dev)` (no remap needed; the entrypoint should still produce dev's ID).

- [ ] **Step 5: Verify UID remap with non-default IDs**

```bash
docker run --rm --user 0:0 -e HOST_UID=4321 -e HOST_GID=8765 \
  termix-devbox:latest id
```

Expected: `uid=4321(dev) gid=8765(dev) groups=8765(dev)`.

- [ ] **Step 6: Verify `docker exec` defaults to dev shell**

```bash
docker run -d --rm --name devbox-test --user 0:0 \
  -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
  termix-devbox:latest
docker exec devbox-test id
docker stop devbox-test
```

Expected: `id` reports the host's UID/GID and the user `dev`. (The runtime override `--user 0:0` makes the entrypoint run as root for the main process; `docker exec` honors the image's `USER dev`, so the exec lands in the dev shell.)

If this step does not produce a dev shell, the implementer must investigate before continuing. Possible causes: a Docker version where `exec` inherits the runtime user override, in which case the fallback is to drop `USER dev` from the Dockerfile and ship a wrapper script. Update the spec and README accordingly if that fallback is needed.

- [ ] **Step 7: Commit**

```bash
git add dev/devbox/Dockerfile dev/devbox/entrypoint.sh
git commit -m "Add dev user, entrypoint UID/GID remap, and final image stanza"
```

---

## Task 9: Write `docker-compose.yml` and verify the long-running workflow

**Files:**
- Create: `dev/devbox/docker-compose.yml`

- [ ] **Step 1: Create `dev/devbox/docker-compose.yml`**

```yaml
services:
  devbox:
    build:
      context: .
    image: termix-devbox:latest
    container_name: termix-devbox
    env_file: .env
    user: "0:0"
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
    command: ["sleep", "infinity"]
    tty: true
    stdin_open: true

volumes:
  devbox-claude-home:
  devbox-codex-home:
  devbox-opencode-home:
  devbox-shell-history:
  devbox-go-cache:
```

The `user: "0:0"` line overrides the image's `USER dev` for the main container process so the entrypoint can run privileged operations (`usermod`, `groupmod`, `chown`). The image's `USER dev` still applies as the default for `docker compose exec`, so plain `docker compose exec devbox bash` lands in a dev shell.

- [ ] **Step 2: Bring up the stack**

```bash
cd dev/devbox
cp .env.example .env
export HOST_UID="$(id -u)" HOST_GID="$(id -g)"
docker compose up -d
docker compose ps
```

Expected: service `termix-devbox` shows status `running` and exposes no ports.

- [ ] **Step 3: Verify exec lands in a dev shell with the correct UID**

```bash
docker compose exec devbox id
```

Expected: `uid=<HOST_UID>(dev) gid=<HOST_GID>(dev) groups=<HOST_GID>(dev)`.

- [ ] **Step 4: Verify project bind-mount writeability and host-side ownership**

```bash
docker compose exec devbox bash -c 'cd /workspace && touch .devbox-write-test && ls -la .devbox-write-test'
ls -la /media/liujia/data/workspace/xunfei/termix/.devbox-write-test
rm /media/liujia/data/workspace/xunfei/termix/.devbox-write-test
```

Expected: the file's owner on the host is the user running `docker compose` (not `root`).

- [ ] **Step 5: Verify host `~/.claude` is unchanged**

```bash
ls -la ~/.claude 2>/dev/null | head
docker compose exec devbox bash -c 'ls -la /home/dev/.claude'
```

Expected: container's `/home/dev/.claude` is owned by `dev` and either empty or volume-backed; host `~/.claude` continues to look exactly as it did before this work started. Critically, no file written inside the container should appear on the host's `~/.claude`.

- [ ] **Step 6: Verify volume persistence across compose restart**

```bash
docker compose exec devbox bash -c 'echo persistent-marker > /home/dev/.claude/marker.txt'
docker compose down
export HOST_UID="$(id -u)" HOST_GID="$(id -g)"
docker compose up -d
docker compose exec devbox cat /home/dev/.claude/marker.txt
docker compose exec devbox rm /home/dev/.claude/marker.txt
```

Expected: the second `cat` prints `persistent-marker`, proving the named volume survived `down`/`up` (without `-v`).

- [ ] **Step 7: Tear down (volumes preserved)**

```bash
docker compose down
```

(Volumes intentionally remain so Task 11's e2e validation does not redo first-run state.)

- [ ] **Step 8: Commit**

```bash
git add dev/devbox/docker-compose.yml
git commit -m "Add devbox docker-compose service with persistent agent volumes"
```

---

## Task 10: Write `dev/devbox/README.md`

**Files:**
- Create: `dev/devbox/README.md`

- [ ] **Step 1: Create `dev/devbox/README.md`**

```markdown
# Termix Devbox

Self-contained Docker development container for the Termix project. The image carries Go 1.25, Node.js 22 LTS, Python 3.12 + `uv`, the Termix Go developer tools (`sqlc`, `oapi-codegen`, `migrate`, `protoc`, `buf`, the protoc plugins), and three AI coding-agent CLIs (Claude Code, Codex, opencode).

Per-agent state (`~/.claude`, `~/.codex`, opencode config) lives in named docker volumes, so the agents you run inside the container do not see — and cannot pollute — the host's logged-in account state.

## One-time setup

```bash
cd dev/devbox
cp .env.example .env
$EDITOR .env                              # fill in the API keys you want to use
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose build
docker compose up -d
```

## Daily use

```bash
cd dev/devbox
export HOST_UID=$(id -u) HOST_GID=$(id -g)   # if not already exported
docker compose up -d                          # idempotent
docker compose exec devbox bash               # land in a dev shell at /workspace
```

Inside the container:

```bash
cd /workspace/go && go test ./...
claude            # uses ANTHROPIC_API_KEY from .env
codex             # uses OPENAI_API_KEY from .env
opencode          # uses provider keys from .env
```

## Versions

```bash
docker compose exec devbox bash -c '
  go version
  node -v
  python3.12 --version
  uv --version
  sqlc version
  oapi-codegen --version
  migrate -version
  buf --version
  protoc --version
  claude --version
  codex --version
  opencode --version
'
```

## Pause and resume across reboots

```bash
docker compose stop          # keeps volumes
docker compose start         # resume; agent state, shell history, Go cache survive
```

## Reset agent state without rebuilding the image

```bash
docker compose down
docker volume rm \
  devbox_devbox-claude-home \
  devbox_devbox-codex-home \
  devbox_devbox-opencode-home
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose up -d
```

(Compose prefixes volume names with the project name, which defaults to the directory: `devbox`. If yours differs, list with `docker volume ls` to find the prefixed names.)

## Full teardown

```bash
docker compose down -v
```

This drops every named volume — agent histories, shell history, and the Go module cache are gone. The image itself remains; remove it with `docker rmi termix-devbox:latest`.

## Migrating to a different repository

```bash
cp -r /path/to/termix/dev/devbox /path/to/new-repo/dev/devbox
cd /path/to/new-repo/dev/devbox
cp .env.example .env && $EDITOR .env
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose up -d
```

If the new repository places `dev/devbox/` at a different relative depth from the project root, override `WORKSPACE_DIR` in `.env`. The default `WORKSPACE_DIR=../..` resolves to the repository root when `dev/devbox/` lives directly under it.

## How isolation works

- `.env` carries `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, and any opencode provider keys; `docker-compose.yml` wires them into the container as env vars.
- The host's `~/.claude` and `~/.codex` are **never** mounted into the container. Each agent reads its API key from the env vars above and writes its session state into a docker-managed volume (`devbox-claude-home`, `devbox-codex-home`, `devbox-opencode-home`).
- The repository root is bind-mounted onto `/workspace` so source edits made inside the container immediately reflect on the host (and vice versa).
- A non-root user `dev` is created in the image with default UID 1000. At container start, `entrypoint.sh` (running as root) remaps `dev` to `HOST_UID`/`HOST_GID` so files written through the bind-mount are owned by you, not by root.
```

- [ ] **Step 2: Commit**

```bash
git add dev/devbox/README.md
git commit -m "Add devbox README with setup, usage, and migration docs"
```

---

## Task 11: End-to-end validation against the spec checklist

This task walks through the spec's "Validation Targets" section. If any check fails, fix the underlying problem and re-run from Step 1. Do not commit until every check passes.

- [ ] **Step 1: Bring up the stack**

```bash
cd dev/devbox
export HOST_UID="$(id -u)" HOST_GID="$(id -g)"
docker compose up -d
```

Expected: `termix-devbox` is running.

- [ ] **Step 2: Verify UID/GID alignment**

```bash
docker compose exec devbox id
```

Expected: `uid=$(id -u)(dev) gid=$(id -g)(dev) groups=$(id -g)(dev)`.

- [ ] **Step 3: Verify Go version**

```bash
docker compose exec devbox go version
```

Expected: `go version go1.25.0` or newer.

- [ ] **Step 4: Verify Node.js version**

```bash
docker compose exec devbox node -v
```

Expected: `v22.x.x`.

- [ ] **Step 5: Verify Python and uv**

```bash
docker compose exec devbox bash -c 'python3.12 --version && uv --version'
```

Expected: `Python 3.12.x`; `uv 0.x.y`.

- [ ] **Step 6: Verify Go developer tools**

```bash
docker compose exec devbox bash -c '
  set -e
  sqlc version
  oapi-codegen --version
  migrate -version
  buf --version
  protoc --version
'
```

Expected: each command prints output without error.

- [ ] **Step 7: Verify AI agent CLIs**

```bash
docker compose exec devbox bash -c \
  'claude --version && codex --version && opencode --version'
```

Expected: each prints a version line.

- [ ] **Step 8: Run the project's Go test suite from inside the container**

```bash
docker compose exec devbox bash -c 'cd /workspace/go && go test ./... 2>&1 | tail -30'
```

Expected: same outcome as on the host. Tests that need a real PostgreSQL instance may be skipped or fail due to missing infra — that is acceptable as long as the failures match what the same suite produces on the host. Note any deltas in the commit message.

- [ ] **Step 9: Verify bind-mount file ownership**

```bash
docker compose exec devbox bash -c 'cd /workspace && touch .devbox-validate && ls -la .devbox-validate'
stat -c '%U:%G' /media/liujia/data/workspace/xunfei/termix/.devbox-validate
rm /media/liujia/data/workspace/xunfei/termix/.devbox-validate
```

Expected: `stat` reports `<your-username>:<your-group>`, not `root:root`.

- [ ] **Step 10: Verify host `~/.claude` is unchanged**

```bash
ls -la ~/.claude 2>/dev/null | head
```

Compare against the state before this work started. Expected: no new files written by the container appear on the host.

- [ ] **Step 11: Spot-check volume persistence**

```bash
docker compose exec devbox bash -c 'echo final-marker > /home/dev/.claude/__validation_marker'
docker compose restart devbox
sleep 2
docker compose exec devbox cat /home/dev/.claude/__validation_marker
docker compose exec devbox rm /home/dev/.claude/__validation_marker
```

Expected: marker survives restart.

- [ ] **Step 12: Tear down for the validation run**

```bash
docker compose down
```

(No commit yet — Task 12 produces the final commit.)

---

## Task 12: Update `docs/PROGRESS.md` and final commit

**Files:**
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Move devbox tasks to Completed**

Edit `docs/PROGRESS.md`:

1. Remove (or rewrite) the "In Progress" entry that says "Brainstorm and approve the developer devbox container design ...". Replace the In Progress section with `- [ ] No active in-progress tasks.` if no other in-progress work was added.
2. Remove the two devbox-related "Pending" entries:
   - "Write the devbox container implementation plan once the design is approved."
   - "Implement the `dev/devbox/` development container ...".
3. Append the following lines to the end of the **Completed** section, in chronological order:
   - `- [x] Brainstorm the developer devbox container design.`
   - `- [x] Approve the developer devbox container design.`
   - `- [x] Write the developer devbox container implementation plan.`
   - `- [x] Implement the developer devbox container under dev/devbox/ (Ubuntu 22.04 + Go 1.25 + Node 22 + Python 3.12 + uv + Go tooling + Claude Code/Codex/opencode, isolated agent state).`
4. In **Next Up**, remove the `Land the developer devbox container ...` item (now done) and re-number the remaining list.

- [ ] **Step 2: Commit**

```bash
git add docs/PROGRESS.md
git commit -m "Mark devbox container implementation complete in PROGRESS.md"
```

- [ ] **Step 3 (optional): Stop the validation container**

```bash
cd dev/devbox && docker compose down
```

(Volumes are kept for the next time the user uses the devbox.)

---

## Implementation Notes

- **Layer order matters for cache hits.** Each language runtime is its own RUN block so changing one (e.g., bumping Go) does not invalidate Node/Python layers. Tools that pull from the network (Go install, npm install) come *after* runtimes, so changes to those don't force runtime reinstall.
- **`@latest` pins are intentional for v1.** The spec accepts the risk of a future surprise upgrade. If a build breaks because a tool changed, that is the trigger to introduce explicit version pins; not before.
- **Image size budget.** The expected size is 1.5–2.5 GB; no extra effort is being spent in this slice to slim it. If size becomes a problem in CI later, options include moving Go-tool installs into a multi-stage build that copies just the binaries, and adding `apt-get clean` more aggressively.
- **Architecture support.** The Go install handles `amd64` and `arm64`. The other layers (NodeSource, deadsnakes, npm) work on both. If the host is something else (e.g., armv7), the build will fail on the Go architecture switch — fix that at build time, not in this plan.
- **No PostgreSQL inside the container.** Tests that need a real Postgres should use the user's host-side DB or a separate compose stack. Adding a Postgres service is a deliberate out-of-scope choice in the spec.
