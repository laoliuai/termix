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

Termix's relay connection is a long-lived WebSocket. Most HTTP-CONNECT proxies (Clash / mihomo / v2ray on `127.0.0.1:7890` / `:7897` / etc., corporate gateways) idle-timeout long-lived tunnels and surface as `broken pipe` spam in the daemon log. To avoid that, the relay WSS uses a dedicated `http.Client` with `Proxy: nil` and **always dials direct, regardless of `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` in your shell**. No config knob, no env override — it's simply the only sane policy for a connection that needs to live for hours.

Every other endpoint (login, refresh, doctor, daemon heartbeats) honors your shell proxy env normally, so corporate-proxy users can still reach the control plane. Tools spawned via `termix start <tool>` (claude, codex, …) inherit your shell env, including `HTTPS_PROXY`, so they can route their own API traffic through your proxy.

`termix doctor` lists the proxy env vars currently visible to the process; the relay WSS line in `termix status` is informational only.
