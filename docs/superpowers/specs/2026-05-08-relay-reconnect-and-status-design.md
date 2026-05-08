# Relay reconnect, SPA disconnect UX, and `termix status`

## Background

After v0.3.0 was released and deployed (2026-05-07), the user left a session
running overnight with the SPA terminal page open. Next morning:

- The browser's "Request Control" worked (control lease state machine is
  served by the control-plane REST API).
- Sending input from the browser had no effect; output from the host's tmux
  session never reached the browser.

Investigation on the host (PID 4143190, daemon up 11 hours):

```
2026/05/08 09:49:28 session cd264912... PublishOutput failed: failed to
write msg: ... write tcp 192.168.0.95:55164->43.156.83.27:443: write: broken pipe
```

The same line repeats every few seconds for hours. Kernel-level `ss` shows
the original WSS socket (local port `55164`) is gone — already closed,
cleaned up. The daemon's in-memory `relayclient` wrapper still holds a
reference to that dead socket and keeps trying to write.

The root cause is structural: `relayclient.Connect` dials once at daemon
boot and the rest of the daemon assumes the connection lives forever.
There is no read-error detection, no reconnect, no token re-negotiation,
no re-announce. When NAT idle, network blip, server restart, or any other
event severs the WSS, the daemon enters a permanently broken state with
no observable recovery signal.

The proxy policy fix in v0.3.0 reduced the rate of disconnect events
(no more mihomo HTTP-CONNECT idle cuts), but the underlying "no reconnect"
hole was always there. v0.3.0 just made it less obvious.

This is also a visibility hole. The user has no way to inspect "is my
daemon healthy", "am I logged in as the right user", "what is the proxy
policy", "are sessions actually being heartbeated" without running ad-hoc
shell commands (`ps`, `cat ~/.local/state/.../log`, `sqlc query`, etc).
A normal user would simply file a "Termix is broken" report. There needs
to be one well-known command that surfaces all of this.

## Goals

- The daemon's relay WSS recovers automatically from any transient
  disconnect (NAT idle, network blip, relay container restart, mihomo
  reload, etc) without user intervention.
- A SPA viewer that loses its WSS shows clear visual state (banner during
  brief outages, persistent modal after extended outages) and gives the
  user explicit choices — never silent failure pretending everything is
  fine.
- `termix status` shows logged-in user, daemon health, relay connection
  state, active sessions, and proxy policy in one human-readable output.

## Non-Goals

- No changes to the relay server (Go) — the protocol stays as-is. Only
  client-side reconnect.
- No in-band token refresh on a live WSS. Reconnect with a fresh token
  is the recovery model.
- No JSON output for `termix status` (only human-readable; JSON is a
  future iteration if a monitoring agent appears).
- No merge of `termix doctor` and `termix status`. They have distinct
  concerns: `doctor` checks "system requirements satisfied" (tmux on PATH,
  socket perms, ...), `status` reports "current runtime state". Keeping
  them separate keeps each concise.
- No application-layer ping/heartbeat. Existing Go TCP keepalive and
  write-error detection are sufficient signals.

## Architecture overview

```
┌──────────────── Host PC ────────────────┐    ┌──── termix.cloud ─────┐
│                                         │    │                       │
│  termix CLI ──────────► daemon socket   │    │                       │
│  (status, start, ...)        │          │    │                       │
│                              ▼          │    │                       │
│                     ┌─Manager────────┐  │    │                       │
│                     │ store          │  │    │                       │
│                     │ tmux           │  │    │                       │
│                     │ relay (iface) ─┼──┼────┼─reconnect loop─►relay │
│                     └─────┬──────────┘  │    │                       │
│                           ▼             │    │                       │
│              ┌─relayclient.Supervisor─┐ │    │                       │
│              │  state machine         │ │    │                       │
│              │  backoff loop          │ │    │                       │
│              │  reconnect callback    │ │    │                       │
│              │  ┌──────────────────┐  │ │    │                       │
│              │  │ relayclient.Client│ │ │    │                       │
│              │  │  (atomic.Pointer)│  │ │    │                       │
│              │  └──────────────────┘  │ │    │                       │
│              └────────────────────────┘ │    │                       │
└─────────────────────────────────────────┘    └───────────────────────┘
                                  ▲
                                  │ WSS
                                  │
                          ┌─SPA (browser)──┐
                          │ relay/supervisor│ ◄── mirror state machine
                          │ DisconnectModal │ ◄── 5-min give-up
                          └─────────────────┘
```

The new abstraction on both sides is a **Supervisor** that wraps a "dumb"
client and owns the reconnect lifecycle. From the consumer's perspective
the connection appears to live forever; internally the supervisor swaps
the underlying client whenever it dies.

## Daemon side: `relayclient.Supervisor`

### State machine

```
        ┌─── ctx canceled ───┐
        ▼                    │
   ┌─────────┐  Connect ok   ┌────────┐
   │connecting├──────────────►│connected│
   └─────────┘                └────┬───┘
        ▲                          │ read err / write err
        │ backoff sleep done       ▼
        │                     ┌─────────────┐
        └─────────────────────┤reconnecting │
        attempt++             │  (attempt N)│
                              └──────┬──────┘
                                     │ 3 consecutive 401s
                                     ▼
                              ┌──────────┐
                              │  closed  │ → requestShutdown()
                              └──────────┘
```

Four states: `connecting` (initial), `connected`, `reconnecting`, `closed`.
`closed` is reachable only via two paths:

1. **Normal shutdown**: Manager.Shutdown cancels the daemon ctx → supervisor
   exits its loop cleanly.
2. **Permanent auth failure**: 3 consecutive 401s during reconnect handshake
   indicate the refresh token is unrecoverable — the supervisor calls
   `requestShutdown()` so the daemon exits and the next CLI invocation either
   spawns a new daemon (via `EnsureFresh` retry) or surfaces the canonical
   `Not logged in. Run: termix login` error.

All other errors are treated as transient and retried indefinitely.

### Backoff

Schedule: `[1s, 2s, 5s, 10s, 30s, 30s, 30s, ...]`, 30s cap, ±20% jitter on
each delay (avoids thundering-herd reconnects when many daemons recover
simultaneously after a relay restart). About 14 attempts inside the first
5 minutes, then steady-state at one attempt every ~30s.

### Public interface

```go
type RelaySupervisor interface {
    // Forward to current client; return ErrNotConnected if not connected.
    AnnounceSession(ctx context.Context, s LocalSession) error
    PublishSnapshot(ctx context.Context, sessionID string, data []byte) error
    PublishOutput(ctx context.Context, sessionID string, data []byte) error
    SetSnapshotHandler(func(ctx context.Context, sessionID string) ([]byte, error))
    SetInputHandler(func(ctx context.Context, sessionID string, data []byte) error)

    // New:
    State() RelayState
    SetReconnectCallback(func(ctx context.Context))
    Run(ctx context.Context) error // owns the reconnect goroutine lifetime
}

type RelayState struct {
    Phase            string    // "connecting" / "connected" / "reconnecting" / "closed"
    Attempt          int       // current reconnect counter; 0 when connected
    LastConnectedAt  time.Time
    LastError        string
    NextRetryAt      time.Time // only meaningful in reconnecting
    AuthFailures     int       // consecutive 401 counter
}
```

### Caller semantics

`PublishOutput` / `AnnounceSession` / `PublishSnapshot` behavior by phase:

| Phase | Behavior |
|---|---|
| `connected` | Pass through to current client. |
| `connecting` / `reconnecting` | **Return `ErrNotConnected` immediately**. Do not block. |
| `closed` | Return `ErrClosed`. |

Non-blocking is essential. Each session has a goroutine reading the
pipe-pane FIFO and calling `PublishOutput` per chunk; if those calls
blocked during a multi-minute reconnect, goroutines would pile up holding
buffers and degrading the system in a non-obvious way.

Output dropped during reconnect is recovered by the fresh snapshot the
supervisor pushes after each successful reconnect — viewers re-render
xterm from current pane state. This is exactly the snapshot+stream
contract the v0.2.x relay protocol already defines.

### Re-announce on reconnect

Manager registers a callback once at boot:

```go
relay.SetReconnectCallback(func(ctx context.Context) {
    sessions, _ := manager.store.List()
    for _, s := range sessions {
        if s.Status != "running" && s.Status != "idle" {
            continue
        }
        if err := relay.AnnounceSession(ctx, s); err != nil {
            log.Printf("re-announce %s failed: %v", s.SessionID, err)
            continue
        }
        if data, err := manager.snapshot(ctx, s.TmuxSessionName); err == nil {
            _ = relay.PublishSnapshot(ctx, s.SessionID, data)
        }
    }
})
```

The supervisor invokes this immediately after each successful reconnect.
A failure for one session is logged and the loop continues — one bad
session must not block the rest from re-announcing.

The tmux pipe-pane → on-disk FIFO is unaffected. The session's output
forwarder goroutine reads from the FIFO and calls
`relay.PublishOutput`; when the supervisor swaps its internal client,
the next call routes to the new client automatically (via the
`atomic.Pointer` slot).

### Concurrency

- Supervisor holds the current `*relayclient.Client` in
  `atomic.Pointer[Client]`. `PublishOutput` does an atomic load, no lock.
- `RelayState` is guarded by `sync.RWMutex`. Reads (Status RPC) take
  the read lock, the supervisor goroutine takes the write lock when
  transitioning state.
- Manager-side code is unchanged — it has no awareness of reconnect.

### Disconnect detection

Two complementary signals:

1. **Read-side**: relayclient's existing read-loop goroutine returns on
   server FIN / read error; supervisor selects on its exit channel.
2. **Write-side**: `PublishOutput` and friends return write errors;
   supervisor's wrapper records the error and triggers state transition.

Both signals converge through a `sync.Once` guard so the state machine
transitions exactly once per disconnect event.

### Token refresh integration

Each reconnect attempt's first step is `refresher.EnsureFresh(ctx)`. The
refresher already decides whether to refresh based on token expiry, so no
extra logic is needed. The freshly-returned access token is used to build
a new `relayclient.Client`. Auth-failure responses (401) on the connect
itself increment `AuthFailures`; 3 consecutive failures push the
supervisor to `closed`.

## SPA side: `relay/supervisor.ts` + `DisconnectModal`

### State machine — mirror with one variation

```
connecting → connected → reconnecting(N) → connected
                ▲              │ >5min
                │              ▼
                │         gave-up ◄── terminal until user acts
                └── retry click ─┘
```

Differences from daemon side:

- **`gave-up` instead of `closed`**: after 5 minutes in the `reconnecting`
  phase without a single successful reconnection, the supervisor stops the
  backoff loop and shows the user the modal. The 5-minute clock resets
  whenever a reconnect succeeds, so a flapping connection that briefly
  recovers does not trigger the modal. The user picks Reload (full page
  refresh) or Retry (resets to `reconnecting(0)`).
- **No daemon-style self-shutdown**: there is no equivalent — closing the
  page is the user's job.

Same `[1s, 2s, 5s, 10s, 30s..., ±20% jitter]` backoff schedule.

### UI state mapping

| Disconnect duration | Visual | User can |
|---|---|---|
| < 3s | silent | normal use |
| 3s – 60s | thin yellow banner at top: `Reconnecting… (attempt N)` | normal use; input may publish-fail until reconnect |
| 60s – 5min | same banner with elapsed counter | same |
| > 5min (`gave-up`) | centered modal + backdrop locks the terminal page | must click a button |

The 3s threshold suppresses one-second WiFi/NAT blips. The 5-minute
threshold separates "transient network event" from "real problem" with
plenty of headroom.

### `DisconnectModal` shape

```
   ┌────────────────────────────────────────┐
   │  连接断开                              │
   │                                        │
   │  无法重新连接到 termix.cloud           │
   │  已尝试 14 次，断开 5m 23s             │
   │                                        │
   │  ▸ 显示详情                            │
   │                                        │
   │      ┌──────────────┐ ┌────────────┐   │
   │      │ 重新加载页面 │ │ 重试连接   │   │
   │      └──────────────┘ └────────────┘   │
   └────────────────────────────────────────┘
```

- **Reload** (primary): `window.location.reload()`. Cleanest path —
  fetches fresh access token, rebuilds SPA, reacquires control lease.
- **Retry** (secondary): `supervisor.retry()`. State machine returns to
  `reconnecting(0)`, runs another full backoff cycle without page
  refresh. Useful when the user knows the network just recovered.
- **Details** (collapsible): last 3 attempts, timestamps, error strings.
- **Never auto-dismiss**, even if a background reconnect succeeds while
  the modal is on screen. The user already saw a problem; the decision
  is theirs.

### Token refresh integration

```
on reconnect attempt:
  1. POST /api/v1/auth/refresh   (refresh token comes via HttpOnly cookie)
     - 200 → store fresh access_token in memory
     - 401 → navigate to /login (do not enter gave-up — refresh token is dead)
  2. Open WSS with the new access_token in query string
  3. On any failure, increment attempt counter and back off
```

A 401 on `/auth/refresh` is the SPA equivalent of the daemon's
"3 consecutive 401s → closed". There is no path to recover without
re-authenticating, so the supervisor short-circuits to `/login` rather
than presenting a useless gave-up modal.

### Components and integration

- `web/app/src/relay/supervisor.ts` (new): state machine + reconnect loop
  + state signals (uses `@preact/signals`, matching existing style).
- `web/app/src/components/disconnect-modal.tsx` (new): the modal.
- `web/app/src/components/reconnect-banner.tsx` (new): the inline banner.
- `web/app/src/pages/terminal.tsx` (edit): subscribe to supervisor state,
  render the appropriate UI element.
- `web/app/src/i18n/messages.ts` (edit): ~8 new keys (`relay.reconnecting`,
  `relay.attempts`, `relay.disconnected.title`, `.body`, `.reload`, `.retry`,
  `.details`, `.lastError`) in both English and Chinese.

## `termix status` — RPC + CLI

### Daemon RPC

```proto
service DaemonService {
  ...
  rpc Status(StatusRequest) returns (StatusResponse);
}

message StatusRequest {}

message StatusResponse {
  string version = 1;
  string revision = 2;
  bool   modified = 3;
  int64  uptime_seconds = 4;

  RelayState relay = 5;
  repeated SessionSummary sessions = 6;
  string proxy_fingerprint = 7;
}

message RelayState {
  string phase = 1;             // "connecting" / "connected" / "reconnecting" / "closed"
  int32  attempt = 2;
  int64  last_connected_at = 3; // unix seconds; 0 = never
  string last_error = 4;
  int64  next_retry_at = 5;
  int32  auth_failures = 6;
}
```

`SessionSummary` is reused from the v0.3.0 `ListSessions` extension.

### CLI

`cmd/termix/main.go::run` adds case `"status"` → `runStatus`:

1. Local read of `credentials.json` for user email + server URL.
2. Local read of `host.json` for `enable_proxy`.
3. Local compute of `proxyenv.Fingerprint()` and current effective env.
4. `ensureDaemon` → dial → Status RPC.
5. Compose into the section-block format below.

### Output format

Connected:

```
USER
  liujia@kaini.org
  https://termix.cloud

DAEMON
  pid 4143190, up 3h22m, version v0.4.0 (rev abc123def456)
  socket /run/user/1000/termix/daemon.sock

RELAY
  connected (since 2026-05-08 09:47:29 UTC)
  reconnects this session: 2

SESSIONS  (2 active)
  cd264912-4c06-4d62-9be2-74aa5cc56f3e  claude  termix_test  live  pid 1274769
  11e36199-e9c3-4d97-a0c3-c422278c97c1  codex   fix-auth     live  pid 4567

PROXY
  enable_proxy: false
  effective:    bypassed (HTTP_PROXY / HTTPS_PROXY / ALL_PROXY all unset)
  fingerprint:  837885c8f809
```

Reconnecting:

```
RELAY
  reconnecting (attempt 4, next try in 8s, last connected 2026-05-07 22:58:42 UTC)
  last error: write tcp 192.168.0.95:55164->43.156.83.27:443: write: broken pipe
```

Timestamps render as RFC3339 in UTC for consistency with `termix sessions list`.

## Error handling and edge cases

### Daemon

| Scenario | Behavior |
|---|---|
| Manager.Shutdown during reconnect loop | ctx cancellation makes the next `select` unblock; supervisor exits the loop, transitions to `closed`, daemon exits cleanly. |
| Per-session re-announce failure | Logged with the session ID; the loop continues. The session may be unreachable from viewers until the next reaper sweep cleans up. |
| 3 consecutive 401s | `requestShutdown()` → daemon exits → next CLI invocation runs full credential bootstrap; either succeeds or surfaces "Not logged in. Run: termix login". |
| `refresher.EnsureFresh` failure (refresh token dead) | Counts as a 401; falls into the same self-shutdown path. |
| Concurrent PublishOutput during reconnect | All return `ErrNotConnected` immediately; each call site logs and continues. The atomic.Pointer guarantees no torn reads. |
| Reconnect succeeds with stale viewer subscriptions on relay | Re-announce restores the routing table; relay's per-session-ID broadcast topic preserves viewer subscriptions; the snapshot push triggers viewer re-render. Seamless. |
| Read-error and write-error fire simultaneously | Both signal the supervisor through a `sync.Once`; only one transition happens. Idempotent. |

### SPA

| Scenario | Behavior |
|---|---|
| `/auth/refresh` returns 401 | Skip `gave-up`; navigate to `/login` directly. The refresh token is unrecoverable. |
| 5-minute timer fires while user is typing | Modal takes focus. Acceptable — the user must decide. |
| Laptop wakes from 8 hours of sleep | Supervisor is already in `gave-up`; modal is up; user sees the explicit choice. |
| User reloads during reconnecting | Reload behaves identically to clicking the modal's Reload button. No special handling. |
| Multiple tabs | Each tab has its own supervisor; they reconnect independently. Server-side broadcast is per session_id, all tabs receive the same stream. |
| WSS dial fails because the cookie expired and `/login` is reached mid-terminal-page | Existing redirect mechanism preserves the path via `?next=/sessions/<id>`; on successful re-login the user lands back on the same terminal. |

## Backwards compatibility

- **v0.4.0 CLI ↔ v0.3.0 daemon**: existing version handshake forces the
  daemon to respawn at v0.4.0 before any RPC is made. New CLI always
  reaches a v0.4.0 daemon.
- **v0.3.0 CLI ↔ v0.4.0 daemon**: shouldn't happen in practice (the CLI
  upgrades the daemon), but if it does, the v0.3.0 CLI lacks a `status`
  subcommand and the version handshake replaces the daemon back to v0.3.0
  on next start. No data loss.
- **Server side**: relay does not change. SPA bundle deploy is
  side-by-side compatible — old SPA + new server keeps working; new SPA
  + old server keeps working; any combination is fine because the
  protocol is unchanged.

## Testing strategy

### Layer matrix

| Layer | File | Coverage |
|---|---|---|
| daemon supervisor | `internal/relayclient/supervisor_test.go` (new) | state-machine truth table; backoff + jitter; 3-401-self-shutdown; ctx cancel exits cleanly; concurrent PublishOutput non-blocking |
| relay client interface | `internal/session/manager.go` (test files) | inject `fakeRelaySupervisor`; verify Manager.Status assembly; reconnect callback re-announce path |
| status RPC | `internal/session/manager_status_test.go` (new) | three RelayState phases; empty / N sessions; proxy fingerprint passthrough |
| CLI status | `cmd/termix/main_test.go` (extend) | three rendering branches; fake daemon Status response; runs without tmux |
| SPA supervisor | `web/app/src/relay/supervisor.test.ts` (new) | state machine mirror; fake WebSocket; `vi.useFakeTimers()` for the 5-min timer; retry/reload paths |
| SPA modal/banner | `web/app/src/components/disconnect-modal.test.tsx`, `reconnect-banner.test.tsx` (new) | render, click handlers, no auto-dismiss, details expand, EN+ZH |
| integration | `go/tests/` | (best-effort) end-to-end "relay container restart triggers daemon reconnect + Status reflects each phase" |

### Disconnect simulation

- Write errors: `fakeRelayClient.PublishOutput` configurable to fail
  after N successful calls.
- Read errors: expose `KillRead()` on the fake to close its read goroutine
  mid-stream.
- Time: inject a `clock` interface; supervisor's `backoffSleep` uses
  `clock.After(d)`. Tests advance the clock manually.

### Expected counts

- Go: +18 tests (supervisor ~10 + status manager ~3 + CLI status ~3
  + integration 1–2). v0.3.0 baseline: 235.
- Web: +12 tests (supervisor ~7 + modal ~3 + banner ~2). v0.3.0 baseline:
  174.

### Manual smoke

- Daemon: rebuild, let CLI handshake-respawn the running v0.3.0 daemon.
  `termix status` reports `connected`. `iptables -A OUTPUT -p tcp
  --dport 443 -j DROP` simulates network loss → `status` shifts to
  `reconnecting`, daemon log shows backoff lines. Remove the rule →
  `status` returns to `connected`, browser viewers re-render via the
  fresh snapshot.
- SPA: DevTools → Offline → banner appears → wait 5 minutes (or use
  the dev-only timer-fast-forward hook) → modal appears → Retry path
  exercises another backoff cycle, Reload path refreshes the page.

## Out of scope (deliberate)

- `termix status --json` for machine consumption.
- In-band token refresh on a live WSS (reconnect-with-fresh-token is
  the recovery model).
- Prometheus-style metric / health endpoints (status command is
  sufficient for diagnostic; monitoring is its own slice).
- Application-layer ping/heartbeat. Go TCP keepalive on the listener side
  + write-error detection on our side cover the relevant failure modes.
- Merging `termix doctor` and `termix status`. Different concerns —
  doctor checks system requirements (tmux, socket perms), status reports
  runtime state.

## Implementation slice shape

This design becomes one slice with three roughly independent components:

1. **Daemon supervisor** (`internal/relayclient/supervisor.go` + tests,
   `internal/session/manager.go` integration). ~400 lines.
2. **SPA supervisor + modal + banner** (`web/app/src/relay/`,
   `web/app/src/components/disconnect-modal.tsx`, terminal page wiring).
   ~250 lines.
3. **`termix status` RPC + CLI** (`proto/daemon.proto`,
   `internal/session/manager.go`, `cmd/termix/main.go`). ~150 lines.

Total: ~800 lines + ~30 tests. Single worktree, single PR; the components
are independent enough that the implementation plan can structure them as
parallel tasks.
