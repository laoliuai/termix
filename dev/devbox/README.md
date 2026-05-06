# Devbox

Self-contained Docker development container scoped to **base toolchains only**: Go 1.25, Node.js 22 LTS, Python 3.12 + `uv`, common shell utilities (`git`, `tmux`, `ripgrep`, `jq`, `gh`, `vim`, …), and three AI coding-agent CLIs (Claude Code, Codex, opencode). It carries no project-specific Go or Python libraries — install whatever your repo needs from inside the container.

Per-agent state (`~/.claude`, `~/.codex`, opencode config) lives in named docker volumes, so the agents you run inside the container do not see — and cannot pollute — the host's logged-in account state.

## One-time setup

```bash
cd dev/devbox
cp .env.example .env                          # optional; the file mainly exists for WORKSPACE_DIR overrides
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose build
docker compose up -d
```

The `dev` user inside the image is created at build time with the host's UID/GID baked in, so files written into the bind-mounted `/workspace` are owned by you, not by root. If your host's UID/GID change (for example, you copy this directory to a different machine), rebuild with `docker compose build`.

## Daily use

```bash
cd dev/devbox
export HOST_UID=$(id -u) HOST_GID=$(id -g)   # if not already exported
docker compose up -d                          # idempotent
docker compose exec devbox bash               # land in a dev shell at /workspace
```

Inside the container:

```bash
cd /workspace
claude            # interactive login on first launch; session persists in the named volume
codex
opencode
```

The AI CLIs handle their own auth — usually through their interactive login flow — and persist credentials into the per-agent named volumes. You do **not** need to put `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` into `.env`. If a particular workflow does require an API key, drop it into `.env` and `docker-compose.yml`'s `env_file: .env` will forward it.

## Versions

```bash
docker compose exec devbox bash -c '
  go version
  node -v
  python3.12 --version
  uv --version
  claude --version
  codex --version
  opencode --version
'
```

## Building behind a slow link (e.g. mainland China)

The Dockerfile accepts three optional build args that swap the upstream sources for faster regional mirrors. Defaults preserve the international sources so the image stays portable.

```bash
export HOST_UID=$(id -u) HOST_GID=$(id -g)
export APT_MIRROR=mirrors.tuna.tsinghua.edu.cn
export GOPROXY=https://goproxy.cn,direct
export NPM_REGISTRY=https://registry.npmmirror.com
docker compose build
docker compose up -d
```

The compose service forwards these from the shell environment to the build args. `GOPROXY` is also set as a runtime env var inside the image, so `go install` from inside the dev container also goes through the mirror.

`gosu` and `gh` are downloaded directly from `github.com` releases with `--retry`/`--speed-limit` flags, so transient stalls retry instead of hanging the build.

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

This drops every named volume — agent histories, shell history, and the Go module cache are gone. The image itself remains; remove it with `docker rmi ai-devbox:latest`.

## Migrating to a different repository

```bash
cp -r /path/to/this/repo/dev/devbox /path/to/new-repo/dev/devbox
cd /path/to/new-repo/dev/devbox
cp .env.example .env
export HOST_UID=$(id -u) HOST_GID=$(id -g)
docker compose up -d
```

If the new repository places `dev/devbox/` at a different relative depth from the project root, override `WORKSPACE_DIR` in `.env`. The default `WORKSPACE_DIR=../..` resolves to the repository root when `dev/devbox/` lives directly under it.

## How isolation works

- The host's `~/.claude` and `~/.codex` are **never** mounted into the container. Each agent writes its session state into a docker-managed volume (`devbox-claude-home`, `devbox-codex-home`, `devbox-opencode-home`).
- The repository root is bind-mounted onto `/workspace`, so source edits made inside the container reflect on the host immediately and vice versa.
- The image's `dev` user is created at build time with UID/GID baked in from the `HOST_UID`/`HOST_GID` build args (sourced from your shell environment). The shipped image has `USER dev` as its default, so plain `docker compose exec devbox bash` lands in a `dev` shell and bind-mounted files are owned by you on the host. There is no runtime entrypoint UID-remap; if your host UID/GID change, run `docker compose build` again with the new values.
- `.env` (forwarded via `env_file: .env`) is the place to drop any extra environment variables you want inside the container; nothing is required by default.

## Files in this directory

| File | Purpose |
|------|---------|
| `Dockerfile` | Image build: Ubuntu 22.04 → apt packages → gosu → gh → Go → Node.js → Python+uv → AI agent CLIs → dev user. |
| `docker-compose.yml` | Long-running service with bind-mounted `/workspace` and named volumes for agent state and dev caches. |
| `.env.example` | Template for `.env`. Copy and edit; never commit `.env`. |
| `.env` (gitignored) | Optional per-host overrides (e.g. `WORKSPACE_DIR`) and any extra env vars to forward into the container. |
| `.gitignore` | Ensures `.env` is not committed. |
| `.dockerignore` | Keeps `.env`, README, and `.gitignore` out of the build context. |
