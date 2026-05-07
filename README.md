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

### Troubleshooting

**Browser shows the session in the list, but clicking in says `disconnected`.**

The most common cause is an HTTP proxy in the daemon's environment. Termix's relay connection is a long-lived WebSocket — most HTTP-CONNECT proxies (including local Clash/mihomo/v2ray instances on `127.0.0.1:7890` / `:7897` / etc) close the tunnel after a short idle window, which the daemon sees as a `broken pipe` write. If you have a TUN-mode VPN or transparent proxy already active, the `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` env vars are pure overhead and should be unset:

```bash
unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY

# then restart the daemon
pkill -fx '(^|.*/)termix __daemon'
termix start <tool> --name <name>
```

Run `termix doctor` to confirm — a `proxy: warn (...)` line means the daemon will route through that proxy. Daemon log at `~/.local/state/termix/logs/termixd.log` (Linux) or `~/Library/Logs/Termix/termixd.log` (macOS) also prints the warning at boot.

If you legitimately need the proxy for your other apps, add the control/relay host to `NO_PROXY`:

```bash
export NO_PROXY=termix.cloud,localhost,127.0.0.1
```
