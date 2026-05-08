## termix

**Anywhere, Anytime, Your PC Terminal, Reimagined.**

Termix lets you view and control AI coding CLI sessions from a mobile or desktop browser while the real session keeps running on your Mac or Ubuntu host.

### Install the host client

```bash
curl -fsSL https://termix.cloud/install.sh | sh
```

The installer detects macOS or Ubuntu/Linux, downloads the matching binary, and installs the single `termix` binary into `~/.local/bin`.

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

### Proxy policy

Termix's relay connection is a long-lived WebSocket. Most HTTP-CONNECT proxies (including local Clash/mihomo/v2ray instances on `127.0.0.1:7890` / `:7897` / etc) close the tunnel after a short idle window, which surfaces as `broken pipe` write errors in the daemon log. To make this a non-issue out of the box, **the CLI and daemon ignore the system's `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` / `NO_PROXY` env vars by default** — they are unset internally before any HTTP/gRPC/WSS client is constructed. If you are on a TUN-mode VPN (most users), this is the right behavior and you do not need to do anything.

If you genuinely need a proxy to reach the control plane (e.g. a corporate network with no direct egress), opt in once at first login:

```bash
TERMIX_ENABLE_PROXY=1 termix login
```

The CLI persists this preference into `~/.config/termix/host.json` (Linux) / `~/Library/Application Support/Termix/host.json` (macOS) as `"enable_proxy": true`, so subsequent `termix start` / `termix sessions ...` invocations honor the proxy without the env var.

To flip it later, edit `host.json` and set `"enable_proxy": false` (or `true`) and run any `termix` command — the CLI handshake detects the policy change and automatically restarts the daemon under the new policy:

```text
termix: daemon proxy config mismatch (was=<hash>, now=<hash>), restarting...
```

Run `termix doctor` to confirm the effective state — `proxy: ok (enable_proxy disabled; relay WSS dials directly)` is what you want for the default case.
