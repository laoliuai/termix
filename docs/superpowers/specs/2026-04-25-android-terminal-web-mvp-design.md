# Android Slice 1: `terminal-web` MVP Design

**Status:** Approved 2026-04-25.
**Phase:** Android UI, slice 1 of 2 (slice 2 is the Kotlin/Compose shell).
**Authoritative spec:** `docs/termix-v1-detailed-technical-spec.md` §5.5, §7.8, §17, §19.

## §1 Goal and Scope

### Goal
Build the `android/terminal-web/` static-asset bundle: an `xterm.js`-based WSS terminal client that the Android Compose shell will host inside a WebView. End-state of slice 1 is a `dist/` directory the Compose shell (slice 2) can drop into `android/app/src/main/assets/terminal-web/` and load via `file:///android_asset/terminal-web/index.html`.

### In scope
- WSS connection to `termix-relay` using a bearer access token supplied by the host shell.
- `session.watch` handshake: render initial snapshot, then live output via `xterm.js`.
- Control-lease state machine: `acquire`, auto-`renew` before expiry, `release`.
- Send terminal input frames (binary `TMX1`, `frame_type=2`) for typed text and special keys.
- 20 s heartbeat (spec §17.7).
- JS bridge contract, both directions, with concrete TS types.
- `dev.html` browser harness with form fields cached in `localStorage`. Used only via `vite dev`; not shipped in `dist/`.
- Unit tests covering the binary-frame codec, JSON-envelope encoder/decoder, WSS-client lifecycle, control-lease state machine, and snapshot reassembly.

### Out of scope (deferred to follow-ups)
- Auto-reconnect with backoff. On disconnect, terminal-web stops; the host re-calls `setSession` to recover.
- Compose / Kotlin shell, login UI, session-list UI (slice 2).
- Session preview pipeline. Preview is consumed by the session list, not by terminal-web; `onPreviewUpdate` is intentionally never a terminal-web concern.
- Selection and copy gestures beyond `xterm.js` defaults.
- Multi-session orchestration in a single WebView. Slice 2 owns navigation between sessions.
- Token refresh on 401. terminal-web surfaces the error; the host refreshes the token and re-calls `setSession`.

## §2 Architecture

### File tree

```text
android/terminal-web/
├── package.json
├── vite.config.ts
├── tsconfig.json
├── index.html               # production entry: <div id="terminal"> + entry/index.ts
├── dev.html                 # dev-only entry: form + status panel + entry/dev.ts (NOT in dist/)
└── src/
    ├── protocol/
    │   ├── frame.ts         # TMX1 binary frame encode/decode (types 1, 2, 3)
    │   ├── envelope.ts      # JSON text envelope encode/decode + type guards
    │   └── types.ts         # shared TS types: envelopes, frames, bridge contract, special keys
    ├── net/
    │   ├── wsClient.ts      # WebSocket lifecycle: open, close, sendText, sendBinary, onmessage demux
    │   └── heartbeat.ts     # 20 s timer that calls wsClient.sendText(envelope("heartbeat"))
    ├── session/
    │   ├── watcher.ts       # session.watch handshake, snapshot reassembly, live-output -> ui
    │   └── control.ts       # control-lease state machine + auto-renew timer
    ├── bridge/
    │   ├── inbound.ts       # registers window.setSession / sendText / sendSpecialKey / requestControl / releaseControl
    │   └── outbound.ts      # window.TermixBridge?.method(...) with console-fallback emitter
    ├── ui/
    │   └── terminal.ts      # xterm.js setup, write(), onData -> sendText, dispose
    └── entry/
        ├── index.ts         # production: install inbound bridge + outbound emitter
        └── dev.ts           # dev: read form, call setSession, render outbound events to status panel
```

### Module responsibilities

- **`protocol/`** is pure functions over `Uint8Array` and `string`. No `WebSocket`, no DOM. Every bug caught here cannot reach the wire.
- **`net/wsClient.ts`** is the only file that touches `WebSocket`. Everything else takes a small interface, so layer-2 tests inject a `MockWebSocket`.
- **`session/control.ts`** owns the lease state machine separately from `watcher.ts`, so its transitions can be unit-tested without WSS or terminal involvement.
- **`bridge/`** is the only place that touches `window.*` globals. `entry/index.ts` and `entry/dev.ts` differ only in *who calls inbound* — same core code path.

### Runtime wiring

```text
inbound.setSession(sessionId, relayUrl, accessToken, deviceId)
        │
        ▼
   wsClient ── opens WSS ──> heartbeat starts on open
        │                          │
        ├── sends hello.android ──> server
        ├── sends session.watch  ──> server
        │
        ▼
   watcher ── parses snapshot+output frames ──> ui.terminal.write()
   control ── parses control.* envelopes ──> outbound.onControlState()

inbound.sendText(text)         ──> control.guard ──> wsClient.sendBinary(frame.encodeInput(textBytes))
inbound.sendSpecialKey(key)    ──> control.guard ──> wsClient.sendBinary(frame.encodeInput(specialKeyBytes))
inbound.requestControl()       ──> control.acquire() ──> wsClient.sendText(envelope("control.acquire"))
inbound.releaseControl()       ──> control.release() ──> wsClient.sendText(envelope("control.release"))
```

## §3 Wire Contracts

### 3a. JS bridge

```typescript
// Native -> WebView. Installed as window.* by bridge/inbound.ts.
declare global {
  interface Window {
    setSession(sessionId: string, relayUrl: string, accessToken: string, deviceId: string): void;
    sendText(text: string): void;
    sendSpecialKey(key: SpecialKey): void;
    requestControl(): void;
    releaseControl(): void;
  }
}

type SpecialKey =
  | "Enter" | "Tab" | "Escape"
  | "Up" | "Down" | "Left" | "Right"
  | "C-c" | "C-d";

// WebView -> Native. Optional global; bridge/outbound.ts no-ops to console if absent.
interface TermixBridge {
  onConnectionState(state: ConnectionState, detail?: string): void;
  onControlState(state: ControlState, detail?: string): void;
  onError(code: string, message: string): void;
}
type ConnectionState = "connecting" | "connected" | "disconnected" | "error";
type ControlState = "none" | "requesting" | "granted" | "denied" | "revoked";
```

**Bridge invariants:**
- `setSession` is idempotent. Re-calling closes any existing socket cleanly first, then opens fresh. This is the reconnect path.
- `sendText` / `sendSpecialKey` silently no-op when control state is not `"granted"`. The Compose toolbar can stay dumb.
- `requestControl` / `releaseControl` no-op when there is no active connection.
- Calling `setSession("", "", "", "")` is treated as a graceful close. Used by `dev.html` "Disconnect" button.

### 3b. JSON envelope (spec §17.3)

```typescript
interface Envelope<T = unknown> {
  type: string;
  request_id: string | null;   // uuid v4, generated client-side; null for server-pushed messages
  payload: T;
}
```

**Sent by terminal-web (text frames):**

| Type | Payload |
| --- | --- |
| `hello.android` | `{ device_id: string }` — sent immediately after WSS open |
| `session.watch` | `{ session_id: string }` |
| `session.unwatch` | `{ session_id: string }` — sent on graceful close |
| `control.acquire` | `{ session_id: string }` |
| `control.renew` | `{ session_id: string, lease_version: number }` |
| `control.release` | `{ session_id: string, lease_version: number }` |
| `heartbeat` | `{}` |

**Received from relay (text frames):**

| Type | Payload |
| --- | --- |
| `hello.ok` | `{ connection_id: string }` |
| `session.joined` | `{ session_id: string }` |
| `session.snapshot.ready` | `{ session_id: string, total_chunks?: number }` (binary snapshot frames follow) |
| `control.granted` | `{ session_id: string, lease_version: number, expires_at: ISO8601, controller_device_id: string }` |
| `control.denied` | `{ session_id: string, reason: string }` |
| `control.revoked` | `{ session_id: string, reason: string }` |
| `error` | `{ code: string, message: string }` |
| `heartbeat` | `{}` (relay echo, ignored) |

### 3c. Binary frame (spec §17.4)

```text
0..3   "TMX1"
4      version = 0x01
5      frame_type = 1 (output) | 2 (input) | 3 (snapshot)
6..9   uint32 BE: header_json_len
10..N  UTF-8 header JSON
N..end raw payload bytes
```

Headers (TS):

```typescript
interface OutputHeader   { session_id: string; seq: number; stream: "stdout" | "stderr" }
interface InputHeader    { session_id: string; seq: number; encoding: "raw" }
interface SnapshotHeader { session_id: string; seq: number; is_last: boolean }
```

terminal-web sends only type=2 (input) and receives types 1 and 3.

## §4 State Machines

### 4a. Connection state (`net/wsClient.ts` and `entry/`)

```text
            setSession()
   idle ─────────────────────> connecting
                                   │ ws.onopen: send hello.android, send session.watch, start heartbeat
                                   v
                              connected
   connected ── ws.onclose ──> disconnected
   connected ── ws.onerror or protocol error ──> error
   connected ── setSession() again ──> connecting (close old, open new)
   disconnected ── setSession() ──> connecting
   error ──── setSession() ──────> connecting
```

**Side effects on transition:**
- `→ connected`: emit `onConnectionState("connected")`; start 20 s heartbeat; start watcher.
- `→ disconnected | error`: stop heartbeat; cancel control auto-renew; reset control state to `"none"`; emit `onConnectionState(...)`.
- Any transition leaving `connected` while control is `"granted"` does **not** send `control.release` (the socket is gone).

### 4b. Control-lease state (`session/control.ts`)

```text
                      requestControl() -> send control.acquire
   none ─────────────────────────────────────────────> requesting
   requesting ── control.granted -> schedule auto-renew ──> granted
   requesting ── control.denied ─────────────────────────> denied
   granted ──── auto control.renew -> control.granted (bumped lease_version) ──> granted (re-arm timer)
   granted ──── control.revoked ────────────────────────> revoked
   granted ──── releaseControl() -> send control.release ──> none
   denied ───── requestControl() ──> requesting
   revoked ──── requestControl() ──> requesting
   granted | requesting ── connection drop ──> none (no control.release sent)
```

**Auto-renew rule.** On entering `granted`, parse `expires_at`. Schedule a renew timer for `(expires_at - now) - 30000` ms, floored to 5000 ms. On fire, send `control.renew` with the current `lease_version`. The relay's ack is `control.granted` with an incremented `lease_version` — same transition as initial grant, just refreshes the timer. terminal-web does not synthesize a local timeout for the renew ack; the relay drives revocation via `control.revoked` and we react to that.

**Input gate.** `sendText` / `sendSpecialKey` are no-ops unless control state is `"granted"`. UI feedback for ungranted input is slice 2's job (the Compose toolbar can grey out).

### 4c. Special-key encoding (single source of truth, `protocol/types.ts`)

| `SpecialKey` | Bytes injected into input frame payload |
| --- | --- |
| `Enter`     | `0x0d` (CR) |
| `Tab`       | `0x09` |
| `Escape`    | `0x1b` |
| `Up`        | `0x1b 0x5b 0x41` |
| `Down`      | `0x1b 0x5b 0x42` |
| `Right`     | `0x1b 0x5b 0x43` |
| `Left`      | `0x1b 0x5b 0x44` |
| `C-c`       | `0x03` |
| `C-d`       | `0x04` |

Spec §17.5 says the daemon normalizes input via tmux symbolic forms. We send raw bytes because the daemon's existing Phase 2 contract accepts `encoding: "raw"` and translates to `tmux send-keys` server-side. terminal-web does not need symbolic-key encoding.

## §5 Build, Output, and Dev Server

### 5a. Tooling

- **Vite** as build tool and dev server.
- **Vitest** as unit-test runner (Vite-native, zero extra config).
- **TypeScript** strict mode.
- Dependencies: `xterm`, `xterm-addon-fit`. Both bundled — no CDN, no runtime fetches other than the WSS connection itself.

### 5b. Vite config sketch

```typescript
// vite.config.ts
import { defineConfig } from "vite";
import { resolve } from "path";

export default defineConfig(() => ({
  base: "./",                          // relative paths — required for WebView file:// loading
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: resolve(__dirname, "index.html"),   // production entry only; dev.html excluded
    },
    sourcemap: false,
  },
  server: {
    open: "/dev.html",                 // `vite dev` opens dev harness automatically
  },
  test: {
    environment: "happy-dom",          // any DOM-touching tests; protocol/ tests need none
    include: ["src/**/*.test.ts"],
  },
}));
```

### 5c. Output shape after `vite build`

```text
dist/
├── index.html
└── assets/
    ├── index-<hash>.js
    └── index-<hash>.css
```

Slice 2 copies `dist/` into `android/app/src/main/assets/terminal-web/`. The WebView loads `file:///android_asset/terminal-web/index.html`; relative-path script tags resolve correctly because `base: "./"`.

### 5d. Dev workflow

```bash
# in android/terminal-web/
npm install
npm run dev          # vite dev, opens http://localhost:5173/dev.html
npm test             # vitest run (CI-friendly)
npm run build        # produces dist/
```

`dev.html` provides:
- Four text inputs (sessionId, relayUrl, accessToken, deviceId) backed by `localStorage`.
- "Connect" button → `window.setSession(...)`.
- "Disconnect" button → `window.setSession("", "", "", "")`.
- "Request Control" / "Release Control" buttons.
- "Send Text" textarea + button.
- Special-key buttons (Enter, Tab, Esc, Up/Down/Left/Right, Ctrl+C, Ctrl+D).
- A status panel that installs `window.TermixBridge = {...}` and prints every outbound event as a log line.

### 5e. Verification commands run in slice 1

- `npm run build` succeeds; `dist/index.html` and `dist/assets/*` exist and contain no references to `dev.html`.
- `npm test` — all unit tests pass.
- Manual: with the Go stack running (control + relay + termixd + a started session), open `dev.html`, paste session details, see the snapshot render, request control, type, release. Capture a short note in the slice-completion commit message.

## §6 Test Strategy

Layer-by-layer. Layers 1 + 2 are unit-tested; layer 3 is a manual smoke checklist via `dev.html`.

### 6a. Layer 1 — pure protocol (no DOM, no socket)

`src/protocol/frame.test.ts`
- Round-trip: `encodeInput("hello")` → decode → header `{seq, encoding: "raw"}`, payload `"hello"` bytes.
- Round-trip: snapshot output frame with multi-byte UTF-8 payload preserves bytes exactly.
- Reject: bad magic (not `TMX1`), wrong version, `header_json_len` overruns buffer, malformed JSON header, unknown `frame_type`.
- Field validation: `seq` is non-negative integer; `is_last` is boolean.

`src/protocol/envelope.test.ts`
- Round-trip every envelope type from §3b, both directions.
- Reject: missing `type`, `payload` not an object, non-string `request_id`.
- Type guards: `isControlGranted(env)` narrows correctly for downstream code.

### 6b. Layer 2 — net + session (mock socket)

A tiny `MockWebSocket` exposing `triggerOpen()`, `triggerMessage(data)`, `triggerClose()`, capturing `.send(data)` calls. Constructed and injected into `wsClient`.

`src/net/wsClient.test.ts`
- On `setSession`, sends `hello.android` then `session.watch` text envelopes in order.
- Heartbeat fires every 20 s (Vitest fake timers); stops on close.
- Re-`setSession` while connected closes the old socket before opening the new one.
- Binary `send()` round-trips through the mock as `Uint8Array`.

`src/session/control.test.ts`
- `requestControl()` → state `"requesting"`, `control.acquire` envelope sent.
- Receiving `control.granted` → state `"granted"`, auto-renew scheduled.
- Auto-renew sends `control.renew` with the current `lease_version` at `expires_at − 30 s`.
- Receiving second `control.granted` with bumped `lease_version` re-arms the timer.
- Receiving `control.revoked` → state `"revoked"`, timer cleared.
- `releaseControl()` → sends `control.release`, state `"none"`, timer cleared.
- Connection drop while `"granted"` → state resets to `"none"`, no `control.release` sent.
- `sendText`/`sendSpecialKey` are no-ops when state ≠ `"granted"` (assertion: nothing sent).
- Special-key encoding table from §4c: each key produces the exact byte sequence.

`src/session/watcher.test.ts`
- Snapshot reassembly: receive 3 snapshot chunks (`is_last` only on chunk 3), then 1 output frame; xterm `write` called with concatenated snapshot bytes first, then output bytes.
- Late `session.snapshot.ready` after first output frame: out-of-order arrival surfaces snapshot bytes after live bytes (per §7a).

### 6c. Layer 3 — manual smoke checklist via `dev.html`

Run on the slice-completion commit and recorded in the commit message:

1. Open `dev.html` against the running stack → connect → see `onConnectionState("connected")`.
2. Snapshot renders in the xterm panel.
3. `requestControl()` → `onControlState("granted")` within 1 s.
4. Type a command, click Enter button → command echoed in output stream.
5. `releaseControl()` → `onControlState("none")`; subsequent `sendText` produces no output.

Write a short `android/terminal-web/README.md` with the exact dev-stack-startup steps, so the smoke checklist is reproducible. One-time write, not a perpetual doc.

### 6d. Coverage target

No numeric threshold gate. The bar is: every state-machine transition in §4 has a test; every envelope type in §3b has a round-trip test; every binary frame type has a round-trip test.

## §7 Known Risks and Deliberate Punts

### 7a. Out-of-order `seq` from relay
The spec gives `seq` as an int but does not promise ordering across multiple snapshot chunks racing live output. Slice 1 trusts arrival order and writes everything to xterm in receive order. If garbled output appears in §6c, add a small ordering buffer keyed on `seq` as a follow-up.

### 7b. xterm.js sizing vs tmux
Spec §168 / §494: Android viewport must not resize tmux. We use `xterm-addon-fit` to fit cols/rows to the WebView container, but this only affects local rendering. The tmux session keeps whatever size the daemon set. Mismatched sizes can produce visually-odd wrapping; that is per-spec correctness, not a bug.

### 7c. Token expiry
A 4xx WSS close (e.g. 401) drives state to `"error"` and `onError("auth", message)` fires. terminal-web does not refresh tokens. Slice 2 catches the error event, refreshes the token, and re-calls `setSession`.

### 7d. TLS / cert pinning
Use the browser's (later WebView's) default trust store. No pinning in slice 1. Revisit during Phase 3 hardening if needed.

### 7e. Backpressure (spec §17.8)
Relay-side backpressure is the relay's concern. If the relay closes us with a "slow consumer" error code, we surface it via `onError` and transition to `"disconnected"`. No client-side flow control in slice 1.

### 7f. IME / paste / selection
xterm.js defaults handle in-WebView selection and text input. Native paste flows through `sendText(text)` from Compose's paste handler in slice 2.

## §8 Slice 2 Preview (informational)

Slice 2 (`android/app/`) is out of scope for this design but its boundary is fixed by §3:

- Login screen → `POST /v1/auth/login` → access token + device record.
- Session list → `GET /v1/sessions?status=running` (REST poll) or relay subscription (decided in slice 2's brainstorm).
- Open session → instantiate `WebView`, load `file:///android_asset/terminal-web/index.html`, then call `window.setSession(sessionId, relayUrl, accessToken, deviceId)` once `pageFinished` fires.
- Toolbar buttons → `window.sendSpecialKey(...)`.
- Compose hosts a `JavascriptInterface` named `TermixBridge` with the three outbound methods to receive state changes.
- Reconnect button → re-call `setSession` with a (possibly refreshed) token.

The slice 2 design will brainstorm REST polling vs WSS subscription for the session list, multi-session navigation/recents, and reconnect / token refresh UX.
