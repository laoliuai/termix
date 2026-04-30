## termix

**Anywhere, Anytime, Your PC Terminal, Reimagined.**

Termix lets you view and control AI coding CLI sessions from a mobile or desktop browser while the real session keeps running on your Mac or Ubuntu host.

### Install the host client

```bash
curl -fsSL https://raw.githubusercontent.com/laoliuai/termix/main/install.sh | sh
```

The installer detects macOS or Ubuntu/Linux, downloads the matching GitHub release, and installs the single `termix` binary into `~/.local/bin`.

If `~/.local/bin` is not on your `PATH`, add:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Start a session

Log in once:

```bash
termix login
```

Start an AI coding session from a project directory:

```bash
termix start codex --name laoliu-codex-termix
```

Supported tools:

- `claude`
- `codex`
- `opencode`

Termix starts its local background service automatically. You do not need to run a separate daemon command.

### Diagnose host setup

```bash
termix doctor
```
