# Single Binary Install and Help Page Design

Date: 2026-04-29
Status: Approved for implementation planning

## Problem

The current implementation still exposes too much of Termix's internal runtime shape to users. The product goal is simple: install Termix, log in once, then start AI coding sessions from a project directory with one command.

Users should not need to know that Termix has a local daemon. They should not manually start `termixd`, install multiple host-side binaries, or read separate daemon setup instructions before creating a session.

The Web UI also needs a product help/download page so a new user can land in the browser, install the right host client, and follow the shortest working flow.

## Goals

- Publish one user-facing host client binary named `termix`.
- Keep daemon behavior as an internal runtime mode of `termix`.
- Make `termix start <tool> --name <name>` the normal session creation command.
- Add a repository-root install script that supports a one-line install command.
- Default the install script to user-level installation under `~/.local/bin`.
- Add a Web help/download page for macOS and Ubuntu host clients.
- Keep the flow aligned with the existing tmux-backed architecture and relay model.

## Non-Goals

- Remove the daemon architecture.
- Put Python or the Web UI on the terminal byte path.
- Replace tmux or allow remote clients to talk to tmux directly.
- Build package-manager integrations such as Homebrew, apt, or snap in this slice.
- Add Windows host support in this slice.

## Product Model

The user-facing host workflow is:

```bash
curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh
termix login
termix start codex --name laoliu-codex-termix
```

The Web UI then shows the running session and lets the user view or control it from the browser.

`termixd` is removed from the normal product vocabulary. Advanced troubleshooting may describe a "local Termix daemon", but users are not asked to run a `termixd` command.

## CLI and Daemon Architecture

The published client artifact contains a single executable:

```text
termix
```

Internally, `termix start` keeps the current daemon lifecycle shape:

1. Check the local daemon IPC socket.
2. If healthy, call `StartSession` over local IPC.
3. If missing or unhealthy, launch the same binary in a hidden daemon mode.
4. Wait for health.
5. Request session creation.
6. Attach the user's foreground terminal to the tmux session.

The hidden daemon mode is:

```bash
termix __daemon
```

This command is internal and should not appear in normal help output.

The daemon mode keeps the current `termixd` responsibilities:

- Load host config and credentials.
- Refresh access tokens.
- Maintain the host-to-relay WSS connection.
- Create and manage tmux sessions.
- Publish snapshots and live output.
- Inject remote input.
- Send session heartbeats.
- Reap dead or stale local sessions.
- Serve local daemon IPC.

The repository may keep `go/cmd/termixd` temporarily during migration, but release packaging should stop presenting it as a user-installed command once single-binary mode is ready.

## Install Script

Add `install.sh` at the repository root.

Default usage:

```bash
curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh
```

Default behavior:

1. Detect OS:
   - `darwin` -> macOS
   - `linux` -> Ubuntu/Linux
2. Detect architecture:
   - `x86_64` / `amd64`
   - `arm64` / `aarch64`
3. Resolve the release version:
   - default: latest GitHub release
   - override: `TERMIX_VERSION=v0.1.0`
4. Download the matching release artifact.
5. Extract or copy the `termix` binary.
6. Install to:
   ```text
   ~/.local/bin/termix
   ```
7. Create `~/.local/bin` if needed.
8. Verify:
   ```bash
   termix --version
   ```
9. If `~/.local/bin` is not on `PATH`, print the shell export line:
   ```bash
   export PATH="$HOME/.local/bin:$PATH"
   ```

Advanced override:

```bash
TERMIX_INSTALL_DIR=/usr/local/bin sh install.sh
```

The script should not default to `sudo`. If a user chooses `/usr/local/bin` and lacks write permission, the script can explain the failure and suggest rerunning with a writable directory or an explicit privileged install flow.

## Release Artifacts

The release process should publish one archive per supported host target:

```text
termix_Darwin_x86_64.tar.gz
termix_Darwin_arm64.tar.gz
termix_Linux_x86_64.tar.gz
termix_Linux_arm64.tar.gz
```

Each archive contains:

```text
termix
LICENSE
README.md
```

The install script should keep the artifact-name mapping small and explicit. Unsupported OS or architecture combinations should fail with a clear message.

## Web Help and Download Page

Add a help/download route to the Web UI, for example:

```text
/help
```

The page should be reachable from the login/session UI header or menu without requiring users to already understand the CLI.

Content:

- Two primary download options:
  - macOS
  - Ubuntu
- One-line install command.
- Short usage flow:
  ```bash
  termix login
  termix start codex --name laoliu-codex-termix
  ```
- Supported tools:
  - `claude`
  - `codex`
  - `opencode`
- A note that Termix starts a local background service automatically.
- A troubleshooting section:
  - `termix doctor`
  - confirm `~/.local/bin` is on `PATH`
  - confirm `tmux` is installed

The page should avoid mentioning `termixd` in the main flow.

## Error Handling

`termix start` should fail with actionable messages:

- Not logged in:
  ```text
  Not logged in. Run: termix login
  ```
- Daemon failed to start:
  ```text
  Termix background service did not become healthy. Run: termix doctor
  ```
- `tmux` missing:
  ```text
  tmux is required. Install tmux, then run termix doctor.
  ```
- Unsupported tool:
  ```text
  unsupported tool "x"; expected claude, codex, or opencode
  ```

The install script should fail early for unsupported platforms, download failures, missing archive contents, or unwritable install directories.

## Testing

CLI and daemon:

- Unit test that `termix start` launches the hidden daemon mode when IPC is unavailable.
- Unit test that daemon mode is hidden from normal usage/help.
- Integration or smoke test that only the `termix` binary is needed to start a session.
- Regression test that existing daemon responsibilities still work after moving `termixd` logic behind `termix`.

Install script:

- Shellcheck if available.
- Test OS/arch mapping with overridable env fixtures.
- Test default install path resolution.
- Test `TERMIX_VERSION` and `TERMIX_INSTALL_DIR` overrides.
- Test unsupported OS/arch errors.

Web UI:

- Route test for `/help`.
- Test download/help page renders macOS and Ubuntu options.
- Test page includes the one-line install command and core CLI flow.
- Build embedded SPA assets after adding the page.

## Rollout

1. Add the design and implementation plan.
2. Add `install.sh` and script tests.
3. Refactor CLI packaging so `termix` can run the daemon internally.
4. Update build/release targets to produce single-binary client artifacts.
5. Add the Web help/download page.
6. Update smoke docs and manual checklist to remove user-facing `termixd`.
7. Keep `termixd` available only as an internal/dev artifact until follow-up cleanup removes or fully hides it.

## Decisions

- The internal daemon command is `termix __daemon`.
- The initial release artifact names are the four names listed above. If release tooling later needs a naming change, the public one-line install command must remain stable.
