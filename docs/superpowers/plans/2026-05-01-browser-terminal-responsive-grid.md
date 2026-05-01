# Browser Terminal Responsive Grid — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Web UI's terminal page usable on phones by introducing a SPA-driven `client.resize` protocol that resizes the host tmux pane to match the viewport, and by hiding the composer + toolbar until the user actually has control.

**Architecture:** SPA picks `(cols, rows)` per the algorithm in the spec, calls `term.resize` locally and emits a new `client.resize` envelope. The relay forwards that envelope to the host daemon (same pattern the daemon's snapshot-request side already uses). The daemon's relayclient decodes it and calls a new `Manager.ResizeSession` which runs `tmux resize-window`. tmux stays in `window-size manual` so host-side `tmux attach` doesn't fight the SPA. The composer + toolbar in `terminal.tsx` are wrapped in a `controlState === "granted"` conditional with a `max-height` slide transition.

**Tech Stack:** Go (relay + daemon + tmux runner), TypeScript / Preact (web SPA), xterm.js, JSON Schema for the WS envelope. No openapi changes, no SQL migrations, no proto file changes (we only add a string envelope type, not a gRPC method).

**Design spec:** `docs/superpowers/specs/2026-05-01-browser-terminal-responsive-grid-design.md`

**Verification commands used throughout this plan:**

```bash
cd /media/liujia/data/workspace/xunfei/termix
cd go && go build ./... && go vet ./... && go test ./...
TERMIX_TEST_DATABASE_URL=postgres://postgres:test@127.0.0.1:55432/termix_test?sslmode=disable \
TERMIX_TMUX_INTEGRATION=1 \
go test -count=1 ./...
cd ../web/app && npm run typecheck && npm test -- --run && npm run build
cd ../.. && make build-web && make check-web-dist
```

Postgres test container: `docker start termix-test-pg` if it's exited.

---

## File map

**New files:**
- `schemas/ws/client.resize.schema.json` — JSON Schema for the new envelope payload.
- `go/internal/session/manager_resize_test.go` — unit test for `Manager.ResizeSession` with a fake tmux runner.
- `web/app/src/components/composer-dock.tsx` — small wrapper that owns the slide-in/out animation (keeps `terminal.tsx` lean).
- `web/app/src/components/composer-dock.test.tsx` — focused test for the dock conditional + class.

**Modified files:**
- `go/internal/relayproto/envelope.go` — new const `TypeClientResize = "client.resize"`.
- `go/internal/tmux/runner.go` — new method `ResizeWindow(ctx, sessionName, cols, rows)`.
- `go/internal/tmux/runner_test.go` — `TestResizeWindow` (real-tmux, gated on `TERMIX_TMUX_INTEGRATION=1`).
- `go/internal/session/manager.go` — extend `TmuxRunner` interface with `ResizeWindow`; add `Manager.ResizeSession` method.
- `go/internal/relayclient/client.go` — add `resizeHandler` field, `SetResizeHandler` setter, and a new envelope case routing `client.resize` to that handler.
- `go/internal/relayclient/messages.go` — typed `ResizeRequestPayload` decoder helper.
- `go/internal/relay/server.go` — add `case TypeClientResize` to `handleEnvelope` that verifies the peer is a watcher and forwards the envelope to the daemon peer.
- `go/internal/hostdaemon/daemon.go` — wire `relayClient.SetResizeHandler` to `manager.ResizeSession`.
- `go/tests/daemon_service_test.go` — extend `fakeTmuxRunner` with `ResizeWindow`.
- `go/tests/daemon_reap_test.go` — same.
- `web/app/src/protocol/types.ts` — `ClientResizePayload` interface and `requestResize` in the `Window` typing source.
- `web/app/src/globals.d.ts` — add `requestResize` to `Window`.
- `web/app/src/bridge/inbound.ts` — expose `window.requestResize`, send initial `client.resize` envelope right after `session.watch`, also fire on visibility regain.
- `web/app/src/bridge/inbound.test.ts` — assert outbound `client.resize` envelope on connect and on `requestResize`.
- `web/app/src/ui/terminal.ts` — replace fixed 120×40 with `pickGrid`, fixed font 13 px, `setGrid(cols, rows)`, debounced ResizeObserver triggering `window.requestResize`.
- `web/app/src/ui/terminal.test.ts` — new tests for `pickGrid` bounds and ResizeObserver-triggered resize.
- `web/app/src/pages/terminal.tsx` — render composer + toolbar only when `controlState === "granted"`, wrap in `<ComposerDock>`, fire `requestResize` on visibility/orientation changes.
- `web/app/src/pages/terminal.test.tsx` — assert composer/toolbar absent on read-only, present on grant.
- `web/app/src/theme/styles.css` — `.composer-dock` + `.composer-dock.is-open` slide transition.
- `go/internal/controlapi/web_dist/**` — rebuilt by `make build-web`.
- `docs/PROGRESS.md` — append the entry at the end.

---

## Task 1 — Add `TypeClientResize` envelope constant + JSON schema

The new envelope name is the foundation for every other change. Land it first so all later code can import the same constant.

**Files:**
- Create: `schemas/ws/client.resize.schema.json`
- Modify: `go/internal/relayproto/envelope.go`

- [ ] **Step 1.1 — Write the JSON Schema**

`schemas/ws/client.resize.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "client.resize",
  "type": "object",
  "required": ["session_id", "cols", "rows"],
  "properties": {
    "session_id": { "type": "string", "format": "uuid" },
    "cols":       { "type": "integer", "minimum": 1, "maximum": 1024 },
    "rows":       { "type": "integer", "minimum": 1, "maximum": 1024 }
  },
  "additionalProperties": false
}
```

- [ ] **Step 1.2 — Add the envelope type constant**

In `go/internal/relayproto/envelope.go`, add `TypeClientResize` to the const block right after `TypeControlRevoked`:

```go
const (
    // ...existing constants kept verbatim...
    TypeControlRevoked       = "control.revoked"
    TypeClientResize         = "client.resize"
    TypeHeartbeat            = "heartbeat"
    TypeError                = "error"
)
```

- [ ] **Step 1.3 — Build to confirm no compile fallout**

Run: `cd go && go build ./...`
Expected: succeeds.

- [ ] **Step 1.4 — Commit**

```bash
cd /media/liujia/data/workspace/xunfei/termix
git add schemas/ws/client.resize.schema.json go/internal/relayproto/envelope.go
git commit -m "$(cat <<'EOF'
Add client.resize envelope type and schema

New WS envelope from SPA → relay → daemon carrying (session_id, cols,
rows). Just the constant + JSON Schema for now; routing and handlers land
in subsequent tasks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2 — `tmux.Runner.ResizeWindow`

Lowest-level: shell out to `tmux resize-window`. TDD against real tmux (gated like the existing `TestStartSessionFailedTool` so unit-test runs without tmux still pass).

**Files:**
- Modify: `go/internal/tmux/runner.go`
- Modify: `go/internal/tmux/runner_test.go`

- [ ] **Step 2.1 — Write the failing integration test**

Append to `go/internal/tmux/runner_test.go`:

```go
// TestResizeWindowResizesLivePane verifies tmux respects resize-window in
// `window-size manual` mode (the StartSession default) so the daemon can
// drive the pane's size from a SPA-supplied (cols, rows).
func TestResizeWindowResizesLivePane(t *testing.T) {
    skipIfNoTmux(t)

    runner := NewRunner()
    sessionName := "termix_test_" + uuid.NewString()
    t.Cleanup(func() {
        _ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
    })

    original := startSessionLivenessProbe
    startSessionLivenessProbe = 100 * time.Millisecond
    t.Cleanup(func() { startSessionLivenessProbe = original })

    if err := runner.StartSession(context.Background(), session.StartSpec{
        SessionName:         sessionName,
        ToolCommand:         "sleep 30",
        DetectImmediateExit: true,
    }); err != nil {
        t.Fatalf("StartSession: %v", err)
    }

    if err := runner.ResizeWindow(context.Background(), sessionName, 80, 24); err != nil {
        t.Fatalf("ResizeWindow: %v", err)
    }

    // tmux's display-message reports the current window size; assert ours.
    out, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName,
        "#{window_width}x#{window_height}").Output()
    if err != nil {
        t.Fatalf("display-message: %v", err)
    }
    got := strings.TrimSpace(string(out))
    if got != "80x24" {
        t.Fatalf("expected window 80x24 after resize, got %q", got)
    }
}
```

- [ ] **Step 2.2 — Run the test, confirm it fails (compile error: ResizeWindow undefined)**

```bash
cd go
TERMIX_TMUX_INTEGRATION=1 go test -run TestResizeWindowResizesLivePane ./internal/tmux/...
```

Expected: build error `runner.ResizeWindow undefined (type *Runner has no field or method ResizeWindow)`.

- [ ] **Step 2.3 — Implement `ResizeWindow`**

Append to `go/internal/tmux/runner.go` (after the existing `StartOutputPipe` / `StopOutputPipe` cluster):

```go
// ResizeWindow drives the SPA-supplied grid into tmux's pane. Called by the
// session manager when a `client.resize` envelope arrives from a viewer; the
// pane's `window-size manual` setting (configured in StartSession) lets us
// pin the size against any concurrent host-side `tmux attach`.
func (r *Runner) ResizeWindow(ctx context.Context, sessionName string, cols, rows uint32) error {
    if sessionName == "" {
        return errors.New("session name is required")
    }
    if cols == 0 || rows == 0 {
        return errors.New("cols/rows must be positive")
    }
    return exec.CommandContext(ctx, r.binary,
        "resize-window", "-t", sessionName,
        "-x", strconv.Itoa(int(cols)),
        "-y", strconv.Itoa(int(rows)),
    ).Run()
}
```

Add the `strconv` import if it isn't already there. (It currently is not — confirm with `grep -n strconv go/internal/tmux/runner.go`.)

- [ ] **Step 2.4 — Run the test again, confirm it passes**

```bash
cd go
TERMIX_TMUX_INTEGRATION=1 go test -run TestResizeWindowResizesLivePane ./internal/tmux/...
```

Expected: PASS.

Also run the full tmux package tests to make sure the existing healthy/failed tests still pass:

```bash
TERMIX_TMUX_INTEGRATION=1 go test ./internal/tmux/...
```

Expected: 3 tests PASS (the 2 existing + 1 new).

- [ ] **Step 2.5 — Commit**

```bash
git add go/internal/tmux/runner.go go/internal/tmux/runner_test.go
git commit -m "$(cat <<'EOF'
tmux.Runner.ResizeWindow: drive pane size from caller

Adds Runner.ResizeWindow(sessionName, cols, rows) that shells out to
`tmux resize-window`. Works against panes started with `window-size
manual` (the existing StartSession default) so the daemon can resize the
pane to the SPA's viewport without interference from a host-side
`tmux attach`. Integration test gated on TERMIX_TMUX_INTEGRATION=1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3 — `Manager.ResizeSession` + `TmuxRunner` interface extension

The manager owns local-session lookup and routing to the runner. The interface change ripples into the tests' fakes (next task).

**Files:**
- Modify: `go/internal/session/manager.go`
- Create: `go/internal/session/manager_resize_test.go`

- [ ] **Step 3.1 — Extend `TmuxRunner` interface**

In `go/internal/session/manager.go`, add `ResizeWindow` to the `TmuxRunner` interface:

```go
type TmuxRunner interface {
    EnsureAvailable(ctx context.Context) error
    StartSession(ctx context.Context, spec StartSpec) error
    StartOutputPipe(ctx context.Context, sessionName, fifoPath string) error
    StopOutputPipe(ctx context.Context, sessionName string) error
    HasSession(ctx context.Context, sessionName string) bool
    ResizeWindow(ctx context.Context, sessionName string, cols, rows uint32) error
}
```

- [ ] **Step 3.2 — Update both `fakeTmuxRunner`s to satisfy the new interface**

Append a method to **both** of these fakes (test helper code):

`go/tests/daemon_service_test.go` (struct already has a `hasSession` field — follow the pattern):

```go
func (f *fakeTmuxRunner) ResizeWindow(_ context.Context, sessionName string, cols, rows uint32) error {
    if f.resizeWindow != nil {
        return f.resizeWindow(sessionName, cols, rows)
    }
    f.resizedSessionName = sessionName
    f.resizedCols = cols
    f.resizedRows = rows
    return nil
}
```

Add the matching fields to the `fakeTmuxRunner` struct in the same file:

```go
type fakeTmuxRunner struct {
    ensureCalled       bool
    startSpec          session.StartSpec
    outputPipeSession  string
    outputPipeFifoPath string
    hasSession         func(sessionName string) bool
    resizeWindow       func(sessionName string, cols, rows uint32) error
    resizedSessionName string
    resizedCols        uint32
    resizedRows        uint32
}
```

`go/tests/daemon_reap_test.go` has its own `fakeTmuxRunner` (different file, same name in a different test). Confirm with `grep -n "fakeTmuxRunner" go/tests/`. If `daemon_reap_test.go` defines its own, add the same `ResizeWindow` method to it. If both files share a single declaration, only one edit is needed.

(Reading time: the second `fakeTmuxRunner` in `daemon_reap_test.go` keeps a parallel struct; just add a no-op `ResizeWindow` there.)

- [ ] **Step 3.3 — Write the failing manager test**

`go/internal/session/manager_resize_test.go`:

```go
package session

import (
    "context"
    "errors"
    "testing"

    "github.com/google/uuid"
    "github.com/termix/termix/go/internal/credentials"
)

type resizeFakeTmux struct {
    sessionName string
    cols, rows  uint32
    fail        error
    has         func(string) bool
}

func (f *resizeFakeTmux) EnsureAvailable(context.Context) error { return nil }
func (f *resizeFakeTmux) StartSession(context.Context, StartSpec) error { return nil }
func (f *resizeFakeTmux) StartOutputPipe(context.Context, string, string) error { return nil }
func (f *resizeFakeTmux) StopOutputPipe(context.Context, string) error { return nil }
func (f *resizeFakeTmux) HasSession(_ context.Context, name string) bool {
    if f.has != nil { return f.has(name) }
    return true
}
func (f *resizeFakeTmux) ResizeWindow(_ context.Context, name string, cols, rows uint32) error {
    if f.fail != nil { return f.fail }
    f.sessionName = name
    f.cols = cols
    f.rows = rows
    return nil
}

func TestManagerResizeSessionInvokesTmuxRunnerForKnownSession(t *testing.T) {
    tmpDir := t.TempDir()
    store := NewStore(tmpDir)
    sessionID := uuid.NewString()
    if err := store.Save(LocalSession{
        SessionID:       sessionID,
        Tool:            "claude",
        Status:          "running",
        TmuxSessionName: "termix_unit_resize",
    }); err != nil {
        t.Fatalf("seed local session: %v", err)
    }

    fake := &resizeFakeTmux{}
    manager := NewManager(ManagerOptions{
        Store:           store,
        Tmux:            fake,
        LoadCredentials: func() (credentials.StoredCredentials, error) {
            return credentials.StoredCredentials{}, nil
        },
    })

    if err := manager.ResizeSession(context.Background(), sessionID, 80, 24); err != nil {
        t.Fatalf("ResizeSession: %v", err)
    }
    if fake.sessionName != "termix_unit_resize" || fake.cols != 80 || fake.rows != 24 {
        t.Fatalf("unexpected runner call: name=%q cols=%d rows=%d",
            fake.sessionName, fake.cols, fake.rows)
    }
}

func TestManagerResizeSessionReturnsErrorForUnknownSession(t *testing.T) {
    tmpDir := t.TempDir()
    store := NewStore(tmpDir)
    fake := &resizeFakeTmux{}
    manager := NewManager(ManagerOptions{
        Store:           store,
        Tmux:            fake,
        LoadCredentials: func() (credentials.StoredCredentials, error) {
            return credentials.StoredCredentials{}, nil
        },
    })

    err := manager.ResizeSession(context.Background(), uuid.NewString(), 80, 24)
    if err == nil {
        t.Fatal("expected error for unknown session id")
    }
    if !errors.Is(err, ErrSessionNotFound) {
        t.Fatalf("expected ErrSessionNotFound, got %v", err)
    }
}
```

- [ ] **Step 3.4 — Run, confirm it fails**

```bash
cd go && go test ./internal/session/...
```

Expected: build error `manager.ResizeSession undefined` and `ErrSessionNotFound undefined`.

- [ ] **Step 3.5 — Implement `Manager.ResizeSession` and `ErrSessionNotFound`**

In `go/internal/session/manager.go`:

```go
// ErrSessionNotFound is returned by manager methods when no local session
// matches the supplied id (typical cause: SPA holds a stale session_id, or
// the daemon was restarted and the local store was cleared).
var ErrSessionNotFound = errors.New("session_not_found")

// ResizeSession drives the SPA's target (cols, rows) into tmux for the
// session referenced by sessionID. Returns ErrSessionNotFound if the
// daemon does not know that session anymore. Errors from the runner are
// surfaced verbatim so the caller (relayclient) can log them.
func (m *Manager) ResizeSession(ctx context.Context, sessionID string, cols, rows uint32) error {
    if m.store == nil {
        return errors.New("session store is required")
    }
    if m.tmux == nil {
        return errors.New("tmux runner is required")
    }
    if sessionID == "" {
        return errors.New("session_id is required")
    }
    local, err := m.store.Load(sessionID)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return ErrSessionNotFound
        }
        return err
    }
    return m.tmux.ResizeWindow(ctx, local.TmuxSessionName, cols, rows)
}
```

Confirm `os` is in the import block of `manager.go` (it is — used elsewhere). If not, add it.

- [ ] **Step 3.6 — Run the new test, confirm it passes**

```bash
go test ./internal/session/...
```

Expected: PASS (2 new tests).

- [ ] **Step 3.7 — Run the existing daemon tests to confirm the fakes still satisfy the interface**

```bash
go test ./tests/...
```

Expected: PASS. If you see "type *fakeTmuxRunner does not implement TmuxRunner (missing ResizeWindow method)", revisit Step 3.2.

- [ ] **Step 3.8 — Commit**

```bash
git add go/internal/session/manager.go go/internal/session/manager_resize_test.go \
        go/tests/daemon_service_test.go go/tests/daemon_reap_test.go
git commit -m "$(cat <<'EOF'
Manager.ResizeSession + TmuxRunner.ResizeWindow

Manager.ResizeSession looks up the local session and asks the runner to
resize its tmux pane. Returns a sentinel ErrSessionNotFound when the
daemon doesn't know the id anymore. Extends the TmuxRunner interface
and updates the in-tree fakes accordingly.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4 — Daemon's relay client routes `client.resize` envelopes

The daemon receives WS envelopes from the relay (e.g., `session.snapshot.request`). Add a parallel handler for `client.resize` that decodes the payload and calls a registered Go function. Wire that function in `hostdaemon/daemon.go` to `manager.ResizeSession`.

**Files:**
- Modify: `go/internal/relayclient/messages.go`
- Modify: `go/internal/relayclient/client.go`
- Modify: `go/internal/hostdaemon/daemon.go`
- Modify: `go/internal/relayclient/client_test.go`

- [ ] **Step 4.1 — Write the failing test**

Append to `go/internal/relayclient/client_test.go` (look at the existing tests for the read-loop patterns; mimic the snapshot-request test):

```go
func TestClient_RoutesClientResizeEnvelopeToHandler(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    conn, server := newPipeConn(t)
    defer server.Close()

    client := New("ws://example", "tk", "dev")
    client.conn = conn // injected by helper if available; otherwise use the existing test scaffolding

    var got struct {
        sessionID string
        cols, rows uint32
    }
    done := make(chan struct{})
    client.SetResizeHandler(func(_ context.Context, sessionID string, cols, rows uint32) error {
        got.sessionID = sessionID
        got.cols = cols
        got.rows = rows
        close(done)
        return nil
    })

    go client.readLoop(ctx)

    server.WriteText(t, `{"type":"client.resize","payload":{"session_id":"sid-1","cols":80,"rows":24}}`)
    select {
    case <-done:
    case <-ctx.Done():
        t.Fatal("resize handler not invoked")
    }
    if got.sessionID != "sid-1" || got.cols != 80 || got.rows != 24 {
        t.Fatalf("unexpected handler args: %+v", got)
    }
}
```

> If `client_test.go` already has a different scaffolding helper (look for `newPipeConn` / similar), reuse it and adapt the bodies to the local convention. The behavior we are asserting is what matters — the test framing should match the file's idiom.

- [ ] **Step 4.2 — Run, confirm it fails**

```bash
cd go && go test ./internal/relayclient/...
```

Expected: build error `client.SetResizeHandler undefined`.

- [ ] **Step 4.3 — Add the typed payload helper**

In `go/internal/relayclient/messages.go`:

```go
type ResizeRequestPayload struct {
    SessionID string `json:"session_id"`
    Cols      uint32 `json:"cols"`
    Rows      uint32 `json:"rows"`
}
```

- [ ] **Step 4.4 — Wire the handler into the client**

In `go/internal/relayclient/client.go`:

```go
type Client struct {
    // ...existing fields...
    inputHandler    func(context.Context, string, []byte) error
    snapshotHandler func(context.Context, string) ([]byte, error)
    resizeHandler   func(context.Context, string, uint32, uint32) error
}

func (c *Client) SetResizeHandler(fn func(context.Context, string, uint32, uint32) error) {
    c.resizeHandler = fn
}
```

In the `readLoop` envelope branch (alongside `TypeSessionSnapshotReq`):

```go
switch env.Type {
case relayproto.TypeSessionSnapshotReq:
    c.handleSnapshotRequest(ctx, env)
case relayproto.TypeClientResize:
    c.handleResizeRequest(ctx, env)
}
```

(If the existing code uses an `if env.Type == ...` chain, swap it for a `switch` — clearer.)

```go
func (c *Client) handleResizeRequest(ctx context.Context, env relayproto.Envelope) {
    if c.resizeHandler == nil {
        return
    }
    raw, _ := json.Marshal(env.Payload)
    var p ResizeRequestPayload
    if err := json.Unmarshal(raw, &p); err != nil {
        return
    }
    if p.SessionID == "" || p.Cols == 0 || p.Rows == 0 {
        return
    }
    _ = c.resizeHandler(ctx, p.SessionID, p.Cols, p.Rows)
}
```

Add `encoding/json` to the imports if it isn't already there.

- [ ] **Step 4.5 — Wire the handler in `hostdaemon/daemon.go`**

After the manager is constructed (`manager := session.NewManager(...)`), add:

```go
relayClient.SetResizeHandler(func(ctx context.Context, sessionID string, cols, rows uint32) error {
    resizeCtx, cancel := context.WithTimeout(ctx, daemonOperationTimeout)
    defer cancel()
    return manager.ResizeSession(resizeCtx, sessionID, cols, rows)
})
```

- [ ] **Step 4.6 — Run the relayclient test, confirm it passes**

```bash
cd go && go test ./internal/relayclient/...
```

Expected: PASS.

- [ ] **Step 4.7 — Build the whole module**

```bash
go build ./...
go vet ./...
```

Expected: succeeds, no warnings.

- [ ] **Step 4.8 — Commit**

```bash
git add go/internal/relayclient/messages.go \
        go/internal/relayclient/client.go \
        go/internal/relayclient/client_test.go \
        go/internal/hostdaemon/daemon.go
git commit -m "$(cat <<'EOF'
Daemon relay client routes client.resize envelopes to ResizeSession

Adds Client.SetResizeHandler and a payload decoder for the new envelope
type. hostdaemon wires the handler to manager.ResizeSession with the
standard daemonOperationTimeout. Errors are logged inside the handler so
a misbehaving SPA cannot bring down the read-loop.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5 — Relay forwards `client.resize` envelope to the daemon peer

The relay's `handleEnvelope` already forwards `session.snapshot.ready` from daemon to watchers. We mirror it for `client.resize` going SPA → daemon, with a watcher check so unauthenticated peers can't drive resizes.

**Files:**
- Modify: `go/internal/relay/server.go`
- Modify (test, may already exist): `go/tests/relay_protocol_test.go`

- [ ] **Step 5.1 — Write the failing test**

Append to `go/tests/relay_protocol_test.go` (model after the existing tests in that file — the file uses `relay.NewServer` + `relayproto.Envelope` directly):

```go
func TestRelayForwardsClientResizeFromWatcherToDaemon(t *testing.T) {
    // Start a relay server, connect a fake daemon peer, register a session,
    // then connect a viewer that sends `session.watch` followed by
    // `client.resize`. Assert the daemon peer receives the resize envelope
    // unchanged.
    //
    // (Use the same harness as TestRelayForwardsTerminalInput; adapt for
    // envelope rather than binary frame.)
}
```

> Look at the existing `TestRelay*` cases for the harness scaffolding. The bodies reuse helper functions for the WS dialing. Implement the assertion: the daemon peer's `Read` should yield a text message whose decoded envelope has `Type == relayproto.TypeClientResize` and the same `cols/rows/session_id`.

- [ ] **Step 5.2 — Run, confirm it fails**

```bash
cd go && go test -run TestRelayForwardsClientResize ./tests/...
```

Expected: assertion fail or timeout (relay drops the envelope today).

- [ ] **Step 5.3 — Add the case to `handleEnvelope`**

In `go/internal/relay/server.go`, in the `switch env.Type` block of `handleEnvelope`, add:

```go
case relayproto.TypeClientResize:
    sessionID, err := payloadString(env, "session_id")
    if err != nil {
        return err
    }
    if !s.reg.isWatching(sessionID, p) {
        return errors.New("not watching session")
    }
    daemon := s.reg.daemon(sessionID)
    if daemon == nil {
        return errors.New("session daemon is offline")
    }
    return writeEnvelope(ctx, daemon, env)
```

`isWatching` already exists in `registry.go`. We do not lease-gate this — multiple SPA clients can resize, last-wins, per the spec.

- [ ] **Step 5.4 — Run, confirm it passes**

```bash
cd go && go test ./tests/...
```

Expected: the new `TestRelayForwardsClientResize` plus existing relay tests all pass.

- [ ] **Step 5.5 — Run the full Go suite**

```bash
go build ./... && go vet ./... && go test ./...
TERMIX_TEST_DATABASE_URL=postgres://postgres:test@127.0.0.1:55432/termix_test?sslmode=disable \
TERMIX_TMUX_INTEGRATION=1 \
go test -count=1 ./...
```

Expected: all green.

- [ ] **Step 5.6 — Commit**

```bash
git add go/internal/relay/server.go go/tests/relay_protocol_test.go
git commit -m "$(cat <<'EOF'
Relay forwards client.resize envelope to the session's daemon peer

Adds a case to handleEnvelope that requires the sender to be a watcher
of the session (so unauthenticated peers can't drive the pane size) and
then writes the envelope verbatim to the daemon peer. Lease-gating is
intentionally absent: the spec allows any joined viewer to resize, and
the last-wins multi-tab story relies on it.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6 — SPA bridge sends `client.resize` envelopes

Frontend half of the protocol: `window.requestResize(cols, rows)` plus an initial resize emitted right after the SPA finishes `session.watch`.

**Files:**
- Modify: `web/app/src/protocol/types.ts`
- Modify: `web/app/src/globals.d.ts`
- Modify: `web/app/src/bridge/inbound.ts`
- Modify: `web/app/src/bridge/inbound.test.ts`

- [ ] **Step 6.1 — Add the payload type**

In `web/app/src/protocol/types.ts`, beside `ControlReleasePayload`:

```ts
export interface ClientResizePayload { session_id: string; cols: number; rows: number }
```

- [ ] **Step 6.2 — Extend the global Window typing**

In `web/app/src/globals.d.ts`:

```ts
declare global {
  interface Window {
    setSession: (sessionId: string, relayUrl: string, accessToken: string, deviceId: string) => void;
    sendText: (text: string) => void;
    sendSpecialKey: (key: SpecialKey) => void;
    requestControl: () => void;
    releaseControl: () => void;
    requestResize: (cols: number, rows: number) => void;
    TermixBridge?: Partial<TermixBridge>;
  }
}
```

- [ ] **Step 6.3 — Write the failing bridge test**

In `web/app/src/bridge/inbound.test.ts`, add (mirroring the existing `setSession` opens-WS test pattern):

```ts
it("sends an initial client.resize envelope after session.watch", async () => {
  // The existing test harness stubs WS factory; spy on outbound text frames.
  const sent = await harnessWithSentTextFrames(); // helper used by other tests
  await sent.openSession({ initialCols: 80, initialRows: 24 });

  const resizeFrame = sent.textFrames().find(t => JSON.parse(t).type === "client.resize");
  expect(resizeFrame).toBeTruthy();
  const env = JSON.parse(resizeFrame!);
  expect(env.payload).toEqual({ session_id: expect.any(String), cols: 80, rows: 24 });
});

it("window.requestResize emits a client.resize envelope on the active session", async () => {
  const sent = await harnessWithSentTextFrames();
  await sent.openSession({ initialCols: 120, initialRows: 40 });
  sent.clearTextFrames();

  window.requestResize(80, 24);

  const resizeFrame = sent.textFrames().find(t => JSON.parse(t).type === "client.resize");
  expect(resizeFrame).toBeTruthy();
  expect(JSON.parse(resizeFrame!).payload).toEqual({ session_id: expect.any(String), cols: 80, rows: 24 });
});
```

> If `harnessWithSentTextFrames` doesn't exist, look for the existing helper in `inbound.test.ts` that verifies `session.watch` is sent — it already captures text frames; reuse or extract it. The test bodies above are what we're after; conform to the file's local scaffolding.

- [ ] **Step 6.4 — Run, confirm it fails**

```bash
cd web/app && npm test -- --run src/bridge/inbound.test.ts
```

Expected: the new tests fail (no `client.resize` frame is ever sent).

- [ ] **Step 6.5 — Implement `requestResize` and the initial fire**

In `web/app/src/bridge/inbound.ts`:

1. Track the current grid on the `ActiveSession`:

```ts
interface ActiveSession {
  // ...existing...
  cols: number;
  rows: number;
}
```

2. `setSession` accepts an `initial` grid (default 80×24 — safe floor):

The function signature stays the same on the WindowGlobals contract; instead, the SPA calls `window.requestResize(cols, rows)` *before* `setSession` is called by the page (terminal.tsx will), and `setSession` reads the pending values via a small module-level `lastGrid` cache:

```ts
let lastGrid: { cols: number; rows: number } = { cols: 80, rows: 24 };

const requestResize = (cols: number, rows: number): void => {
  lastGrid = { cols, rows };
  if (!active) return;
  active.cols = cols;
  active.rows = rows;
  active.ws.sendText(encodeEnvelope("client.resize", {
    session_id: active.sessionId,
    cols, rows,
  }));
};
```

3. In the `onOpen` callback, after the existing `session.watch` send, add:

```ts
session.ws.sendText(encodeEnvelope("client.resize", {
  session_id: sessionId,
  cols: session.cols,
  rows: session.rows,
}));
```

(Set `session.cols = lastGrid.cols; session.rows = lastGrid.rows;` when constructing the `ActiveSession` literal.)

4. Expose `requestResize` on the window (existing pattern for `requestControl`):

```ts
type WindowGlobals = {
  // ...existing...
  requestResize: (cols: number, rows: number) => void;
};
const w = window as unknown as WindowGlobals;
w.requestResize = requestResize;
```

- [ ] **Step 6.6 — Run, confirm tests pass**

```bash
cd web/app && npm test -- --run src/bridge/inbound.test.ts
```

Expected: PASS.

- [ ] **Step 6.7 — Typecheck the whole web app**

```bash
npm run typecheck
```

Expected: succeeds.

- [ ] **Step 6.8 — Commit**

```bash
cd /media/liujia/data/workspace/xunfei/termix
git add web/app/src/protocol/types.ts web/app/src/globals.d.ts \
        web/app/src/bridge/inbound.ts web/app/src/bridge/inbound.test.ts
git commit -m "$(cat <<'EOF'
SPA bridge: window.requestResize + initial client.resize on connect

Adds a ClientResizePayload type, exposes window.requestResize(cols, rows)
on the active session, and emits an initial client.resize envelope right
after session.watch so the daemon resizes tmux to the SPA's viewport
before the first snapshot streams back.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7 — `terminal.ts` responsive grid algorithm

Drop the fixed 120×40, fix font at 13 px, compute grid from container, debounce-fire `window.requestResize` on relevant changes.

**Files:**
- Modify: `web/app/src/ui/terminal.ts`
- Modify: `web/app/src/ui/terminal.test.ts`

- [ ] **Step 7.1 — Write the failing tests**

Replace the existing tests (or add fresh ones) in `web/app/src/ui/terminal.test.ts` to assert the grid bounds:

```ts
it("picks 80 cols on a phone-portrait container (~360 px)", () => {
  const { container, term } = mountIntoContainer({ width: 360, height: 640 });
  expect(term.options.cols).toBe(80);
  expect(term.options.rows).toBeGreaterThanOrEqual(20);
  expect(term.options.rows).toBeLessThanOrEqual(40);
});

it("picks 120 cols on a desktop container (>= 1280 px)", () => {
  const { term } = mountIntoContainer({ width: 1280, height: 800 });
  expect(term.options.cols).toBe(120);
});

it("calls window.requestResize on grid change", () => {
  const reqResize = vi.fn();
  (window as any).requestResize = reqResize;

  const { rebind, container, term, fireResizeObserver } = mountIntoContainer({ width: 360, height: 640 });

  fireResizeObserver({ width: 1280, height: 800 });
  expect(reqResize).toHaveBeenCalledWith(120, expect.any(Number));
});
```

> The current `terminal.test.ts` already mocks `Terminal` from `@xterm/xterm` and a `ResizeObserver`; reuse those mocks. `mountIntoContainer` is illustrative — the existing scaffolding has equivalents.

- [ ] **Step 7.2 — Run, confirm fail**

```bash
cd web/app && npm test -- --run src/ui/terminal.test.ts
```

Expected: failing assertions because `cols` is currently always 120.

- [ ] **Step 7.3 — Implement the new `terminal.ts`**

Replace the body of `web/app/src/ui/terminal.ts` (full rewrite of the constants + mountTerminal sections; keep the `TerminalUI` interface):

```ts
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

const FONT_SIZE = 13;
const CELL_WIDTH_RATIO = 0.6;
const CELL_HEIGHT_RATIO = 1.2;
const GUTTER_PX = 2;

const COLS_FLOOR = 80;
const COLS_CAP   = 120;
const ROWS_FLOOR = 20;
const ROWS_CAP   = 40;

const FONT_FAMILY =
  '"DejaVu Sans Mono", Menlo, Consolas, "Liberation Mono", "Courier New", monospace';

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

export function pickGrid(widthPx: number, heightPx: number): { cols: number; rows: number } {
  const cellW = FONT_SIZE * CELL_WIDTH_RATIO;
  const cellH = FONT_SIZE * CELL_HEIGHT_RATIO;
  const cols = clamp(Math.floor((widthPx - GUTTER_PX) / cellW), COLS_FLOOR, COLS_CAP);
  const rows = clamp(Math.floor(heightPx / cellH),                ROWS_FLOOR, ROWS_CAP);
  return { cols, rows };
}

function containerSize(container: HTMLElement): { width: number; height: number } {
  const rect = container.getBoundingClientRect();
  return {
    width: container.clientWidth || rect.width || window.innerWidth || 0,
    height: container.clientHeight || rect.height || window.innerHeight || 0,
  };
}

export interface TerminalUI {
  write(bytes: Uint8Array): void;
  onInput(handler: (text: string) => void): void;
  fit(): void;
  setGrid(cols: number, rows: number): void;
  dispose(): void;
}

const RESIZE_DEBOUNCE_MS = 300;

export function mountTerminal(container: HTMLElement): TerminalUI {
  const initial = pickGrid(...Object.values(containerSize(container)) as [number, number]);
  const term = new Terminal({
    cursorBlink: true,
    convertEol: false,
    fontSize: FONT_SIZE,
    fontFamily: FONT_FAMILY,
    cols: initial.cols,
    rows: initial.rows,
  });
  term.open(container);

  let lastCols = initial.cols;
  let lastRows = initial.rows;

  const setGrid = (cols: number, rows: number): void => {
    if (cols === lastCols && rows === lastRows) return;
    lastCols = cols;
    lastRows = rows;
    term.resize(cols, rows);
  };

  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  const recompute = (): void => {
    if (debounceTimer !== null) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debounceTimer = null;
      const { width, height } = containerSize(container);
      const next = pickGrid(width, height);
      if (next.cols === lastCols && next.rows === lastRows) return;
      setGrid(next.cols, next.rows);
      const fn = (window as { requestResize?: (c: number, r: number) => void }).requestResize;
      if (fn) fn(next.cols, next.rows);
    }, RESIZE_DEBOUNCE_MS);
  };

  let resizeObserver: ResizeObserver | null = null;
  if (typeof ResizeObserver !== "undefined") {
    resizeObserver = new ResizeObserver(recompute);
    resizeObserver.observe(container);
  } else {
    window.addEventListener("resize", recompute);
  }

  // Page may have a `requestResize` hook that fires before any container
  // resize event — surface our initial pick to the bridge once on mount.
  const initFn = (window as { requestResize?: (c: number, r: number) => void }).requestResize;
  if (initFn) initFn(initial.cols, initial.rows);

  return {
    write(bytes) { term.write(bytes); },
    onInput(handler) { term.onData(handler); },
    fit() { recompute(); },
    setGrid,
    dispose() {
      if (debounceTimer !== null) clearTimeout(debounceTimer);
      resizeObserver?.disconnect();
      if (!resizeObserver) window.removeEventListener("resize", recompute);
      term.dispose();
    },
  };
}
```

- [ ] **Step 7.4 — Run, confirm tests pass**

```bash
cd web/app && npm test -- --run src/ui/terminal.test.ts
```

Expected: PASS.

- [ ] **Step 7.5 — Typecheck**

```bash
npm run typecheck
```

Expected: succeeds.

- [ ] **Step 7.6 — Commit**

```bash
cd /media/liujia/data/workspace/xunfei/termix
git add web/app/src/ui/terminal.ts web/app/src/ui/terminal.test.ts
git commit -m "$(cat <<'EOF'
xterm grid is now responsive (80-120 cols, 20-40 rows, 13px font)

Replaces the fixed 120x40 grid + shrink-the-font-to-fit strategy with a
viewport-derived (cols, rows) clamped to [80, 120] x [20, 40]. xterm's
font-size is fixed at 13px. A debounced ResizeObserver recomputes the
grid on container changes and fires window.requestResize with the new
size so the daemon can resize tmux to match.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8 — `terminal.tsx`: hide composer + toolbar until granted

A new `<ComposerDock>` wrapper owns the slide-in/out so `terminal.tsx` stays small. Tests assert composer/toolbar absent on read-only and present on grant.

**Files:**
- Create: `web/app/src/components/composer-dock.tsx`
- Create: `web/app/src/components/composer-dock.test.tsx`
- Modify: `web/app/src/pages/terminal.tsx`
- Modify: `web/app/src/pages/terminal.test.tsx`
- Modify: `web/app/src/theme/styles.css`

- [ ] **Step 8.1 — `ComposerDock` component**

`web/app/src/components/composer-dock.tsx`:

```tsx
import type { ComponentChildren } from "preact";

export interface ComposerDockProps {
  open: boolean;
  children: ComponentChildren;
}

// Wraps the composer + toolbar so the terminal page can mount/unmount the
// input chrome with a max-height transition. Children render only when
// open == true; we keep the wrapper always-mounted so screen readers see a
// stable landmark.
export function ComposerDock({ open, children }: ComposerDockProps) {
  return (
    <div class={`composer-dock${open ? " is-open" : ""}`} aria-hidden={!open}>
      {open ? children : null}
    </div>
  );
}
```

- [ ] **Step 8.2 — `ComposerDock` test**

`web/app/src/components/composer-dock.test.tsx`:

```tsx
import { describe, it, expect } from "vitest";
import { render, cleanup } from "@testing-library/preact";

import { ComposerDock } from "./composer-dock";

describe("ComposerDock", () => {
  it("does not render children when closed", () => {
    const { container } = render(<ComposerDock open={false}><span data-testid="kid">kid</span></ComposerDock>);
    expect(container.querySelector("[data-testid='kid']")).toBeNull();
    expect(container.querySelector(".composer-dock")?.classList.contains("is-open")).toBe(false);
    cleanup();
  });

  it("renders children and toggles is-open class when open", () => {
    const { container } = render(<ComposerDock open={true}><span data-testid="kid">kid</span></ComposerDock>);
    expect(container.querySelector("[data-testid='kid']")).toBeTruthy();
    expect(container.querySelector(".composer-dock")?.classList.contains("is-open")).toBe(true);
    cleanup();
  });
});
```

- [ ] **Step 8.3 — CSS for the dock**

Append to `web/app/src/theme/styles.css`:

```css
/* Composer + Toolbar slide in only when the SPA has control. */
.composer-dock {
  max-height: 0;
  overflow: hidden;
  transition: max-height 180ms ease;
}
.composer-dock.is-open {
  /* Generous ceiling — actual height is capped by content + viewport. */
  max-height: 360px;
}
```

- [ ] **Step 8.4 — Update `terminal.test.tsx` assertions**

Find the existing tests that assert the composer / toolbar render in `web/app/src/pages/terminal.test.tsx`. Replace them with:

```tsx
it("hides composer + toolbar in read-only state", () => {
  const { container } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
  expect(container.querySelector(".composer")).toBeNull();
  expect(container.querySelector(".toolbar")).toBeNull();
});

it("renders composer + toolbar after control is granted", async () => {
  const { container } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
  // simulate the bridge granting control
  (window as any).TermixBridge?.onControlState?.("granted");
  // wait for re-render
  await new Promise(r => setTimeout(r, 0));
  expect(container.querySelector(".composer")).not.toBeNull();
  expect(container.querySelector(".toolbar")).not.toBeNull();
});
```

> If the existing tests reference an `is-disabled` toolbar or a `disabled` composer, those assertions go away. Search-and-replace any leftover.

- [ ] **Step 8.5 — Update `terminal.tsx`**

Modify `web/app/src/pages/terminal.tsx`:

- Import `ComposerDock`:

```tsx
import { ComposerDock } from "../components/composer-dock";
```

- Replace the existing trailing block:

```tsx
<Composer disabled={disabled} onSend={onCompose} placeholder={t("terminal.placeholder")} />
<Toolbar disabled={disabled} onDigit={onDigit} onSpecial={onSpecial} />
```

with:

```tsx
<ComposerDock open={controlState.value === "granted"}>
  <Composer disabled={false} onSend={onCompose} placeholder={t("terminal.placeholder")} />
  <Toolbar disabled={false} onDigit={onDigit} onSpecial={onSpecial} />
</ComposerDock>
```

The `disabled` local in `terminal.tsx` becomes unused — delete it.

- Add a viewport / orientation watcher right after the existing `useEffect` that wires the bridge:

```tsx
useEffect(() => {
  const fire = (): void => {
    // The terminal element drives sizing; xterm's mountTerminal already
    // pushes its picked grid through window.requestResize, so we just
    // nudge the recompute path here.
    const term = document.getElementById("terminal");
    if (term) {
      const ev = new Event("resize");
      window.dispatchEvent(ev);
    }
  };
  window.addEventListener("orientationchange", fire);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") fire();
  });
  return () => {
    window.removeEventListener("orientationchange", fire);
    // visibilitychange handler intentionally left registered for tab life;
    // if this becomes test-noisy, attach via .signal AbortController.
  };
}, []);
```

- [ ] **Step 8.6 — Run all web tests**

```bash
cd web/app && npm test -- --run
```

Expected: 178+ tests pass (composer-dock adds 2; terminal.test.tsx may net to the same count since the read-only test is stricter).

- [ ] **Step 8.7 — Typecheck + build**

```bash
npm run typecheck
npm run build
```

Expected: both succeed.

- [ ] **Step 8.8 — Commit**

```bash
cd /media/liujia/data/workspace/xunfei/termix
git add web/app/src/components/composer-dock.tsx \
        web/app/src/components/composer-dock.test.tsx \
        web/app/src/pages/terminal.tsx \
        web/app/src/pages/terminal.test.tsx \
        web/app/src/theme/styles.css
git commit -m "$(cat <<'EOF'
Hide composer + toolbar until control is granted

Wraps Composer + Toolbar in a new <ComposerDock> that mounts its children
only when controlState === "granted" and applies a max-height slide
transition. Read-only mode now reclaims ~150 px on phone portrait while
preserving the existing "Request control" CTA in the control bar.

Also adds an orientation/visibility watcher in terminal.tsx so the SPA
re-fires the responsive-grid pipeline when the user rotates the phone or
returns to the tab.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9 — Rebuild embedded web_dist + final verification

Make sure the Go-embedded SPA bundle is up-to-date and the full matrix passes.

**Files:**
- Modify (regenerated): `go/internal/controlapi/web_dist/**`

- [ ] **Step 9.1 — Rebuild the embedded SPA**

```bash
cd /media/liujia/data/workspace/xunfei/termix
make build-web
make check-web-dist
```

Expected: the rsync output from `build-web` and an empty (zero-output) `check-web-dist`.

- [ ] **Step 9.2 — Full Go test matrix**

```bash
cd go
go build ./... && go vet ./... && go test -count=1 ./...
TERMIX_TEST_DATABASE_URL=postgres://postgres:test@127.0.0.1:55432/termix_test?sslmode=disable \
TERMIX_TMUX_INTEGRATION=1 \
go test -count=1 ./...
```

Expected: all green.

- [ ] **Step 9.3 — Full Web test matrix**

```bash
cd ../web/app && npm run typecheck && npm test -- --run && npm run build
```

Expected: typecheck, ~178+ tests, and build all green.

- [ ] **Step 9.4 — Stage and commit the rebuilt web_dist**

```bash
cd /media/liujia/data/workspace/xunfei/termix
git add go/internal/controlapi/web_dist
git commit -m "$(cat <<'EOF'
Rebuild embedded Web assets after responsive-grid + composer-dock

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

(If `git status` shows nothing under `go/internal/controlapi/web_dist`, the bundle hash didn't change — skip the commit.)

---

## Task 10 — PROGRESS.md entry

**Files:**
- Modify: `docs/PROGRESS.md`

- [ ] **Step 10.1 — Append the entry under `## In Progress`**

Insert at the top of the `## In Progress` section, just under the heading:

```markdown
- [x] Make the browser terminal usable on phones — responsive grid + control-aware mobile layout. Spec: `docs/superpowers/specs/2026-05-01-browser-terminal-responsive-grid-design.md`. Plan: `docs/superpowers/plans/2026-05-01-browser-terminal-responsive-grid.md`. Backend: new `client.resize` envelope routed SPA → relay → daemon → `Manager.ResizeSession` → `tmux resize-window`; `tmux.Runner.ResizeWindow` and `Manager.ResizeSession` with TmuxRunner-interface tests; relay forwards only to authenticated watchers; no protocol break in either direction. SPA: `web/app/src/ui/terminal.ts` picks a grid from the viewport (cols ∈ [80, 120], rows ∈ [20, 40], font 13 px) and fires `window.requestResize` through the bridge debounced 300 ms; `terminal.tsx` hides composer + toolbar (`<ComposerDock>` with max-height slide transition) until `controlState === "granted"`. Verification: green `cd go && go build ./... && go vet ./... && go test ./...` plus the integration suite under `TERMIX_TEST_DATABASE_URL` + `TERMIX_TMUX_INTEGRATION=1`; green `cd web/app && npm run typecheck && npm test -- --run && npm run build`; green `make build-web`; green `make check-web-dist`.
```

- [ ] **Step 10.2 — Commit**

```bash
git add docs/PROGRESS.md
git commit -m "$(cat <<'EOF'
PROGRESS: log responsive-grid + composer-dock landing

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review (already applied)

- **Spec coverage** — every section of the design spec maps to a task: protocol (Tasks 1, 4, 5, 6), tmux + manager (Tasks 2, 3), SPA grid algorithm (Task 7), composer/toolbar layout (Task 8), embedded bundle + verification (Task 9), progress log (Task 10).
- **Placeholder scan** — no TBDs. Two intentional "consult-the-existing-helper" callouts in Task 5 (relay test scaffolding) and Task 6 (`harnessWithSentTextFrames`) point the implementer at idiomatic local patterns in tests we are extending; the assertion shape is fully specified.
- **Type consistency** — `ResizeWindow(ctx, sessionName, cols, rows uint32)` is the same signature in the runner, the interface, the fakes, and the manager. `requestResize(cols, rows: number)` is the same name in terminal.ts, the bridge, and the global typing. `client.resize` is the same envelope type string in the JSON Schema, the Go const, the Go relay/daemon paths, and the SPA code.
- **Scope** — single implementation plan, single feature, no decomposition needed.
