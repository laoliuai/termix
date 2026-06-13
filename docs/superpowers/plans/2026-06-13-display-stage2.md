# Display Stage 2 (Multi-Client Sizing + Snapshot/Live Sync) Implementation Plan

> **For agentic workers:** Execute this plan with **superpowers:subagent-driven-development** (recommended — dispatch one subagent per task) or **superpowers:executing-plans**. Steps use `- [ ]` checkbox syntax for progress tracking; check each box as you complete it. Every task ends with a commit. TDD throughout: write the failing test first, then the implementation, then confirm green with the exact run command shown.

**Goal:** Make a tmux session render correctly across the host terminal + multiple browser viewers by making pane size host-authoritative (viewers never resize it) and restoring snapshot/live coherence (cursor restore + generation fence).

**Architecture:** Host terminal owns the pane size via `window-size latest`; the daemon publishes authoritative `cols`/`rows` + a per-session `generation` on `snapshot.ready`; viewers adopt that size and CSS-downscale-to-fit (`transform: scale()`, never upscale); snapshots carry cursor position and a generation fence drops stale live frames.

**Tech Stack:** Go (tmux runner/control, relayclient, CLI), TypeScript/Preact + xterm.js (`web/app`), gRPC/WS envelopes (`relayproto` + `protocol/types.ts`).

---

## File Structure

| File | Created/Modified | Responsibility in Stage 2 |
| --- | --- | --- |
| `go/internal/tmux/runner.go` | Modified | `StartSession`: `set-option window-size manual` → `latest` so the pane follows the host terminal. `initialPaneSize` unchanged (defaults 120×40, floors 40×10). |
| `go/internal/tmux/runner_test.go` | Modified | Assert `StartSession` argv sets `window-size latest` (replaces the prior `manual` assertion). |
| `go/internal/tmux/control.go` | Modified | New `PaneSize(ctx, sessionName)` (query `#{pane_width} #{pane_height}`); new pure `BuildSnapshot(content, cursorX, cursorY, cursorVisible)`; new `CaptureSnapshotWithCursor(ctx, sessionName)`. Keep `CaptureSnapshot`/`NormalizeSnapshot`/`SnapshotArgs` for back-compat. |
| `go/internal/tmux/control_test.go` | Created | Exact-bytes tests for `BuildSnapshot`; tmux-gated tests for `PaneSize` and `CaptureSnapshotWithCursor`. |
| `go/internal/relayclient/client.go` | Modified | `Client` struct gains `genMu`/`gen` map + `nextGeneration`/`currentGeneration`. `handleSnapshotRequest`: drop the resize branch, always single capture, emit `snapshot.ready` with `cols`/`rows`/`generation`. `handleResizeRequest`: drop resize + re-snapshot, keep parse/guard/debug-log only. |
| `go/internal/relayclient/messages.go` | Modified (optional) | No required shape change; `SnapshotRequestPayload.Cols/Rows` retained as fallback hint, `ResizeRequestPayload` retained for parse/guard. |
| `go/internal/relayclient/client_test.go` | Modified | Reverse existing assertions: handlers no longer call `resizeHandler`; `snapshot.ready` carries `cols`/`rows`/`generation`; generation increments per watch. |
| `go/cmd/termix/main.go` | Modified | `parseStartArgs` → `(tool, name, cols, rows, err)` with `--size COLSxROWS` parsing; `runStart` birth-size precedence `--size` > `hostWinsize()` > 0. |
| `go/cmd/termix/main_test.go` | Modified | `parseStartArgs` `--size` table test; update all existing call-sites to 5-value return. |
| `go/internal/hostdaemon/daemon.go` | Modified | Switch the `Snapshot` func from `tmux.CaptureSnapshot` to `tmux.CaptureSnapshotWithCursor`. |
| `go/internal/session/manager.go` | Modified | Wire `SetSizeHandler` (sessionID→tmux name→`PaneSize`) so `snapshot.ready` carries authoritative cols/rows (Correction A); add `PaneSize`+`RepushSnapshot` to the `RelayClient`/`TmuxRunner` interfaces; start a per-session host-resize monitor goroutine in `StartSession` (Task 8b). |
| `go/internal/tmux/runner.go` (PaneSize) | Modified | Add `Runner.PaneSize` delegating to the `tmux.PaneSize` free func, so the manager monitor can poll via the `TmuxRunner` interface (testable). |
| `go/tests/display_stage2_integration_test.go` | Created | Two different-size viewers do not change the pane; host-resize re-push carries new cols/rows + same generation; snapshot contains cursor-restore sequence. |
| `web/app/src/protocol/types.ts` | Modified | `SessionSnapshotReadyPayload` gains `cols?`/`rows?`/`generation?`. |
| `web/app/src/ui/terminal.ts` | Modified | `TerminalUI.setAuthoritativeGrid(cols, rows)`; scaler wrapper + `recomputeScale()`; `recompute()`/ResizeObserver/visualViewport only rescale in authoritative mode; pickGrid retained for fallback. |
| `web/app/src/ui/terminal.test.ts` | Modified | `setAuthoritativeGrid` → `term.resize`; scale math; never upscale; authoritative `recompute` does not call `requestResize`. |
| `web/app/src/bridge/inbound.ts` | Modified | On `snapshot.ready` with cols/rows → `setAuthoritativeGrid` + `authoritativeMode=true`; record `currentGeneration`; suppress `client.resize` in authoritative mode (fallback keeps Stage-1 path); drive the fence (snapshotPending + generation) coordinating with the watcher. |
| `web/app/src/bridge/inbound.test.ts` | Modified | snapshot.ready cols/rows → `setAuthoritativeGrid`; authoritative suppresses `client.resize`; fallback path still sends it; fence drops pre-snapshot output; stale-generation drop. |
| `web/app/src/session/watcher.ts` | Modified | `createWatcher` gains `setCurrentGeneration` / `setSnapshotPending`; `handleFrame` drops type-1 output while a snapshot is pending and clears pending on the final snapshot chunk (`is_last`). |
| `web/app/src/session/watcher.test.ts` | Created/Modified | Fence unit tests: output dropped while pending; flows after final snapshot chunk; stale-generation drop. |

---

## Phase 2a — Host-Authoritative Sizing + Protocol/Generation + Web Adopt

### Task 1: `window-size latest` in `StartSession`

Change tmux pane sizing so the pane follows the host terminal instead of being pinned at the manual birth size. This is the foundation of D1 (host-authoritative size).

- [ ] **Test (write first)** in `go/internal/tmux/runner_test.go` — locate the existing test that asserts the `set-option ... window-size manual` argv (the one covering `StartSession`'s final `set-option` call) and add/adjust an argv assertion. If the existing assertion is on a captured argv, flip its expectation from `"manual"` to `"latest"`; otherwise add:

```go
// TestStartSessionSetsWindowSizeLatest asserts the final set-option argv
// configures `window-size latest` so the pane follows the host terminal
// (not pinned to the CLI-supplied birth size). Replaces the prior `manual`
// expectation.
func TestStartSessionSetsWindowSizeLatest(t *testing.T) {
	skipIfNoTmux(t)

	runner := NewRunner()
	sessionName := "termix_test_" + uuid.NewString()
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sessionName).Run() })

	original := startSessionLivenessProbe
	startSessionLivenessProbe = 100 * time.Millisecond
	t.Cleanup(func() { startSessionLivenessProbe = original })

	if err := runner.StartSession(context.Background(), session.StartSpec{
		SessionName:         sessionName,
		ToolCommand:         "sleep 30",
		DetectImmediateExit: true,
		Cols:                120,
		Rows:                40,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	out, err := exec.Command("tmux", "show-options", "-t", sessionName, "window-size").Output()
	if err != nil {
		t.Fatalf("show-options: %v", err)
	}
	if opt := strings.TrimSpace(string(out)); !strings.Contains(opt, "latest") {
		t.Fatalf("expected window-size latest, got %q", opt)
	}
}
```

  Verify the test (or the flipped assertion) is RED first:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestStartSessionSetsWindowSize -v
```

  Expected before impl: FAIL (pane reports `manual`).

- [ ] **Implement** in `go/internal/tmux/runner.go`, `StartSession` final `set-option` call (lines 238–242): change `"window-size", "manual"` to `"window-size", "latest"`. Update the comment block above (lines 232–237) to explain that `latest` makes the pane track the most-recently-active local tmux client; capture-pane/pipe-pane viewers are not tmux clients and do not trigger a resize; detached sessions keep their last size. Leave `newSessionArgs` / `initialPaneSize` untouched.

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestStartSessionSetsWindowSize -v
```

  Expected: PASS (`window-size: latest`). On a host without tmux the test self-skips via `skipIfNoTmux`.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/internal/tmux/runner.go go/internal/tmux/runner_test.go && git commit -m "$(cat <<'EOF'
fix(tmux): use window-size latest so pane follows host terminal

Stage 2 D1: viewers (capture-pane/pipe-pane) are not tmux clients and
no longer drive pane size; the most-recently-active host attach is
authoritative.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: `PaneSize` query in `tmux/control.go`

Add a reusable helper to read the current pane dimensions, used by `snapshot.ready` (to publish authoritative size) and by host-resize re-push.

- [ ] **Test (write first)** in a new file `go/internal/tmux/control_test.go`:

```go
package tmux

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
)

func skipIfNoTmuxCtrl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// TestPaneSizeReturnsActualDimensions creates a session at a known size and
// asserts PaneSize reads it back.
func TestPaneSizeReturnsActualDimensions(t *testing.T) {
	skipIfNoTmuxCtrl(t)
	name := "termix_test_" + uuid.NewString()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "main", "-x", "140", "-y", "35", "sleep", "30").Run(); err != nil {
		t.Fatalf("new-session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	time.Sleep(50 * time.Millisecond)

	cols, rows, err := PaneSize(context.Background(), name)
	if err != nil {
		t.Fatalf("PaneSize: %v", err)
	}
	if cols != 140 || rows != 35 {
		t.Fatalf("PaneSize()=(%d,%d) want (140,35)", cols, rows)
	}
}
```

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestPaneSize -v
```

  Expected before impl: build failure (`undefined: PaneSize`).

- [ ] **Implement** in `go/internal/tmux/control.go` (add imports `context`, `fmt`, `strconv`):

```go
// PaneSize returns the current (cols, rows) of the session's main pane via
// `tmux display-message`. Used by snapshot.ready (authoritative size) and the
// host-resize re-push to detect when the pane size changes.
func PaneSize(ctx context.Context, sessionName string) (uint32, uint32, error) {
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p",
		"-t", sessionName+":main.0", "#{pane_width} #{pane_height}").Output()
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("PaneSize: expected two integers, got %q", string(out))
	}
	cols, err := strconv.ParseUint(fields[0], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("PaneSize parse cols %q: %w", fields[0], err)
	}
	rows, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("PaneSize parse rows %q: %w", fields[1], err)
	}
	return uint32(cols), uint32(rows), nil
}
```

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestPaneSize -v
```

  Expected: PASS (`(140,35)`); self-skips without tmux.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/internal/tmux/control.go go/internal/tmux/control_test.go && git commit -m "$(cat <<'EOF'
feat(tmux): add PaneSize to query current pane dimensions

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Generation tracking in `relayclient.Client`

Add per-session monotonic generation infrastructure used by the snapshot fence. `nextGeneration` increments on each fresh watch (`snapshot.req`); `currentGeneration` reads without incrementing (host-resize re-push reuses the current gen).

- [ ] **Test (write first)** in `go/internal/relayclient/client_test.go`. Note `nextGeneration`/`currentGeneration` are unexported, so this test must be in `package relayclient` (white-box). If the existing `client_test.go` is `package relayclient_test`, add this case in a new white-box file `go/internal/relayclient/generation_test.go` with `package relayclient`:

```go
package relayclient

import "testing"

// TestGenerationTrackingPerSession verifies the per-session counter increments
// independently and currentGeneration does not advance it.
func TestGenerationTrackingPerSession(t *testing.T) {
	c := New("ws://dummy", "token", "device")

	if got := c.nextGeneration("s1"); got != 1 {
		t.Fatalf("s1 first nextGeneration = %d, want 1", got)
	}
	if got := c.nextGeneration("s2"); got != 1 {
		t.Fatalf("s2 first nextGeneration = %d, want 1", got)
	}
	if got := c.nextGeneration("s1"); got != 2 {
		t.Fatalf("s1 second nextGeneration = %d, want 2", got)
	}
	if got := c.currentGeneration("s1"); got != 2 {
		t.Fatalf("currentGeneration(s1) = %d, want 2 (no increment)", got)
	}
	if got := c.currentGeneration("s1"); got != 2 {
		t.Fatalf("currentGeneration(s1) second read = %d, want 2", got)
	}
	if got := c.currentGeneration("s2"); got != 1 {
		t.Fatalf("currentGeneration(s2) = %d, want 1", got)
	}
}
```

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/relayclient/ -run TestGenerationTrackingPerSession -v
```

  Expected: build failure (`undefined: nextGeneration`).

- [ ] **Implement** in `go/internal/relayclient/client.go`. Add to the `Client` struct (after `closeErr`, ~line 81):

```go
	genMu sync.Mutex
	gen   map[string]uint64 // sessionID -> generation
```

  Initialize in `New()` (inside the returned `&Client{...}`):

```go
		gen: make(map[string]uint64),
```

  Add the two methods (place them near the handler functions):

```go
// nextGeneration increments and returns the generation for sessionID. Called
// on each fresh watch (snapshot.req) so the viewer can fence out frames that
// belong to a prior watch.
func (c *Client) nextGeneration(sessionID string) uint64 {
	c.genMu.Lock()
	defer c.genMu.Unlock()
	c.gen[sessionID]++
	return c.gen[sessionID]
}

// currentGeneration returns the current generation for sessionID without
// incrementing. Used by the host-resize re-push, which is not a new watch.
func (c *Client) currentGeneration(sessionID string) uint64 {
	c.genMu.Lock()
	defer c.genMu.Unlock()
	return c.gen[sessionID]
}
```

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/relayclient/ -run TestGenerationTrackingPerSession -v
```

  Expected: PASS.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/internal/relayclient/client.go go/internal/relayclient/generation_test.go && git commit -m "$(cat <<'EOF'
feat(relayclient): add per-session generation tracking

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: `handleSnapshotRequest` / `handleResizeRequest` — drop viewer resize, emit authoritative size + generation

Reverse the viewer-driven resize behavior and make `snapshot.ready` carry `cols`/`rows`/`generation`. This is the single client.go behavioral change; it must apply consecutively with Task 3 (same file).

- [ ] **Test (write first)** in `go/internal/relayclient/client_test.go`. Update existing assertions and add two cases. Use the test harness already present in this file (httptest WebSocket server pattern; mirror the existing snapshot/resize tests):

  - `TestHandleSnapshotRequestEmitsAuthoritativeSizeAndGeneration`: server sends `session.snapshot.req` for a session; assert the `snapshot.ready` payload contains `session_id`, `cols`, `rows`, and `generation == 1`; a second `snapshot.req` yields `generation == 2`. Assert `resizeHandler` is NOT invoked even when the req payload includes `cols`/`rows`. (For `cols`/`rows` to be non-zero the test must inject a snapshot handler whose session resolves to a real or stubbed `PaneSize`; since `PaneSize` shells out to tmux, gate the cols/rows assertion on `skipIfNoTmux` OR assert only that the keys are present. Prefer asserting the keys exist and `generation` increments — the exact cols/rows values are covered by the integration test in Task 11.)
  - `TestHandleResizeRequestIsNoOp`: server sends `client.resize` with positive cols/rows; assert neither `resizeHandler` nor `snapshotHandler` is called and no `snapshot.ready`/binary frame follows (wait ~100ms, confirm no further frames).

```go
// TestHandleResizeRequestIsNoOp verifies a viewer client.resize parses + guards
// but never resizes the pane and never re-snapshots (Stage 2 D2).
func TestHandleResizeRequestIsNoOp(t *testing.T) {
	resizeCalled := false
	snapshotCalled := false
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, nil)
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // hello.daemon
		req, _ := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type:    relayproto.TypeClientResize,
			Payload: map[string]any{"session_id": "s1", "cols": 200, "rows": 50},
		})
		_ = conn.Write(ctx, websocket.MessageText, req)
		time.Sleep(100 * time.Millisecond) // no follow-up frame should arrive
		close(done)
	}))
	defer server.Close()

	c := New("ws"+server.URL[len("http"):], "tok", "dev")
	c.SetSnapshotHandler(func(context.Context, string) ([]byte, error) { snapshotCalled = true; return []byte("x"), nil })
	c.SetResizeHandler(func(context.Context, string, uint32, uint32) error { resizeCalled = true; return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Connect(ctx)
	<-done
	if resizeCalled {
		t.Fatal("resizeHandler must not be called for client.resize")
	}
	if snapshotCalled {
		t.Fatal("snapshotHandler must not be called for client.resize")
	}
}
```

  Also flip any existing test that asserted `client.resize` triggers a resize + snapshot (search `client_test.go` for assertions around `resizeHandler` being invoked and the resize-driven snapshot.ready) so they now assert the no-op behavior.

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/relayclient/ -run 'TestHandleResizeRequestIsNoOp|TestHandleSnapshotRequestEmitsAuthoritative' -v
```

  Expected before impl: FAIL.

- [ ] **Implement** in `go/internal/relayclient/client.go`:

  `handleSnapshotRequest` (lines 308–331): remove the resize branch (lines 308–323 — the comment block plus the `if req.Cols > 0 && req.Rows > 0 && c.resizeHandler != nil { ... } else { ... }`), leaving a single unconditional capture:

```go
	snapshot, err := c.snapshotHandler(ctx, req.SessionID)
	if err != nil {
		return
	}
	gen := c.nextGeneration(req.SessionID)
	cols, rows, _ := PaneSize(ctx, req.SessionID) // best-effort; 0/0 if tmux query fails
	_ = c.writeEnvelope(ctx, relayproto.Envelope{
		Type: relayproto.TypeSessionSnapshotReady,
		Payload: map[string]any{
			"session_id": req.SessionID,
			"cols":       cols,
			"rows":       rows,
			"generation": gen,
		},
	})
	_ = c.PublishSnapshot(ctx, req.SessionID, snapshot)
```

  Note: `req.SessionID` is the daemon-local session id used for `PublishSnapshot`; `PaneSize` expects the tmux session name. If they differ in this codebase, resolve the tmux name via the same path the snapshot handler uses (the snapshot handler in `hostdaemon` maps session id → tmux name); if a mapping is not available inside the relay client, pass `0, 0` (the viewer falls back) and rely on the host-resize re-push / integration coverage. Keep the cols/rows best-effort and never fatal.

  `handleResizeRequest` (lines 334–377): keep the parse + guard (lines 334–347) and the debug log (lines 352–357). **Remove** the `resizeHandler` call (lines 349–351) and the entire trailing `captureStable` + `snapshot.ready` + `PublishSnapshot` block (lines 359–376). The guard `if c.resizeHandler == nil { return }` at the top can be dropped (no longer needed) — replace with a `if c.snapshotHandler == nil && c.resizeHandler == nil` no-op guard only if a nil-deref risk exists; otherwise simply parse, guard `p.SessionID`, debug-log, return. Final body:

```go
func (c *Client) handleResizeRequest(ctx context.Context, env relayproto.Envelope) {
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		return
	}
	var p ResizeRequestPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	if p.SessionID == "" {
		return
	}
	// Stage 2 D2: viewer resize requests never change the pane and never
	// trigger a re-snapshot — the viewport adapts via CSS scale on the SPA.
	// We keep parsing the envelope (back-compat with old SPAs) and DEBUG-log only.
	if p.Debug != nil {
		log.Printf("client.resize debug (ignored, Stage 2): session=%s client=%v", p.SessionID, p.Debug)
	}
}
```

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/relayclient/ -v
```

  Expected: PASS (all relayclient tests, including the flipped existing ones).

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/internal/relayclient/client.go go/internal/relayclient/client_test.go && git commit -m "$(cat <<'EOF'
feat(relayclient): host-authoritative size on snapshot.ready; drop viewer resize

handleSnapshotRequest now always single-captures and emits cols/rows/generation;
handleResizeRequest is a parse+log no-op (viewer never resizes the pane).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: `--size COLSxROWS` CLI flag

Add explicit birth-size control with precedence `--size` > host tty (`hostWinsize()`) > 0 (daemon default). Changes `parseStartArgs`'s signature, so update all call-sites in the same task.

- [ ] **Test (write first)** in `go/cmd/termix/main_test.go`:

```go
func TestParseStartArgsParsesSizeFlag(t *testing.T) {
	cases := []struct {
		name                   string
		args                   []string
		wantTool, wantName     string
		wantCols, wantRows     int
		wantErr                string
	}{
		{name: "tool only", args: []string{"claude"}, wantTool: "claude"},
		{name: "tool with name", args: []string{"codex", "-n", "fix bug"}, wantTool: "codex", wantName: "fix bug"},
		{name: "size flag", args: []string{"claude", "--size", "220x50"}, wantTool: "claude", wantCols: 220, wantRows: 50},
		{name: "size and name", args: []string{"codex", "--size", "100x24", "-n", "s"}, wantTool: "codex", wantName: "s", wantCols: 100, wantRows: 24},
		{name: "no x separator", args: []string{"claude", "--size", "220,50"}, wantErr: "invalid --size"},
		{name: "non-numeric", args: []string{"claude", "--size", "abcxdef"}, wantErr: "invalid --size"},
		{name: "missing value", args: []string{"claude", "--size"}, wantErr: "missing value for --size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, name, cols, rows, err := parseStartArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStartArgs: %v", err)
			}
			if tool != tc.wantTool || name != tc.wantName || cols != tc.wantCols || rows != tc.wantRows {
				t.Fatalf("got (%q,%q,%d,%d) want (%q,%q,%d,%d)", tool, name, cols, rows, tc.wantTool, tc.wantName, tc.wantCols, tc.wantRows)
			}
		})
	}
}
```

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./cmd/termix/ -run TestParseStartArgsParsesSizeFlag -v
```

  Expected before impl: build failure (return-arity mismatch) and/or compile error in existing call-sites.

- [ ] **Implement** in `go/cmd/termix/main.go`:

  Rewrite `parseStartArgs` (currently `func parseStartArgs(args []string) (string, string, error)` at line 525) to:

```go
func parseStartArgs(args []string) (tool string, name string, cols int, rows int, err error) {
	if len(args) == 0 {
		return "", "", 0, 0, errors.New("usage: termix start <tool> [-n name] [--size COLSxROWS]")
	}
	tool = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-n", "--name":
			i++
			if i >= len(args) {
				return "", "", 0, 0, errors.New("missing value for --name")
			}
			name = args[i]
		case "--size":
			i++
			if i >= len(args) {
				return "", "", 0, 0, errors.New("missing value for --size")
			}
			parts := strings.Split(args[i], "x")
			if len(parts) != 2 {
				return "", "", 0, 0, fmt.Errorf("invalid --size format %q: expected COLSxROWS", args[i])
			}
			cv, cerr := strconv.Atoi(parts[0])
			rv, rerr := strconv.Atoi(parts[1])
			if cerr != nil || rerr != nil || cv <= 0 || rv <= 0 {
				return "", "", 0, 0, fmt.Errorf("invalid --size format %q: cols and rows must be positive integers", args[i])
			}
			cols, rows = cv, rv
		default:
			return "", "", 0, 0, fmt.Errorf("unknown start argument: %s", args[i])
		}
	}
	return tool, name, cols, rows, nil
}
```

  (Preserve whatever the current function actually handles — re-read lines around 525 and keep any existing flags it parsed; the above assumes `-n/--name` only. If other flags exist, fold them into the new signature unchanged.)

  Update `runStart` (lines 205–245): change line 206 to `tool, name, sizeCols, sizeRows, err := parseStartArgs(args)` and replace the birth-size block (lines 231–234) with the precedence logic:

```go
	// Birth size precedence: --size > host tty (hostWinsize) > 0 (daemon default).
	cols, rows := sizeCols, sizeRows
	if cols <= 0 || rows <= 0 {
		if deps.hostWinsize != nil {
			cols, rows = deps.hostWinsize()
		}
	}
```

  Leave the `StartSessionRequest{... Cols: int32(cols), Rows: int32(rows) ...}` unchanged (lines 243–244).

  Update any other call-site of `parseStartArgs` (search the package) to the 5-value form.

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./cmd/termix/ -v
```

  Expected: PASS (new `--size` table test + all existing main_test.go cases compile and pass).

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/cmd/termix/main.go go/cmd/termix/main_test.go && git commit -m "$(cat <<'EOF'
feat(cli): add --size COLSxROWS for explicit birth pane size

Precedence: --size > host tty winsize > daemon default (120x40).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Web — protocol types for authoritative size + generation

- [ ] **Implement** in `web/app/src/protocol/types.ts`, the `SessionSnapshotReadyPayload` interface (line 53):

```typescript
export interface SessionSnapshotReadyPayload {
  session_id: string;
  total_chunks?: number;
  cols?: number;        // authoritative pane width (new daemon only)
  rows?: number;        // authoritative pane height (new daemon only)
  generation?: number;  // per-session generation for the snapshot fence
}
```

- [ ] **Verify** (type-check; fields are optional so nothing breaks):

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npm run -s build >/dev/null 2>&1 && echo TYPECHECK_OK || (npx tsc --noEmit && echo TSC_OK)
```

  Expected: no type errors (`TYPECHECK_OK` or `TSC_OK`). Use whichever type-check command the repo defines — confirm via `package.json` scripts if `build` is not a typecheck.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add web/app/src/protocol/types.ts && git commit -m "$(cat <<'EOF'
feat(web): add cols/rows/generation to SessionSnapshotReadyPayload

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Web — `setAuthoritativeGrid` + CSS scale in `terminal.ts`

Add an authoritative-grid path that resizes xterm to the daemon-provided size and CSS-downscales to fit (never upscales). In authoritative mode, `recompute()` only rescales — it never changes the grid or calls `requestResize`. pickGrid stays for fallback/old-daemon.

- [ ] **Test (write first)** in `web/app/src/ui/terminal.test.ts` — add a describe block (mirror the existing test setup helpers `setContainerSize`, `terminalMock`, `mountTerminal`, the DEBUG overlay via `localStorage.termix_debug`):

```typescript
describe("setAuthoritativeGrid + scale", () => {
  it("resizes xterm to the authoritative size", () => {
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);
    const ui = mountTerminal(container);
    const term = terminalMock.instances[0];
    term.resize.mockClear();
    ui.setAuthoritativeGrid(220, 50);
    expect(term.resize).toHaveBeenCalledWith(220, 50);
    expect(ui.cols()).toBe(220);
    expect(ui.rows()).toBe(50);
    ui.dispose();
  });

  it("computes scale = min(1, containerW / (cols*cellW)) and never upscales", () => {
    // Enable DEBUG so the overlay exposes the scale for assertion.
    localStorage.setItem("termix_debug", "1");
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);
    const ui = mountTerminal(container, { measureCell: () => ({ w: 8, h: 16 }) });

    ui.setAuthoritativeGrid(220, 50); // 1280/(220*8)=0.727 -> 0.73
    let overlay = (container.parentElement ?? container).querySelector("[data-termix-debug]");
    expect(overlay?.textContent ?? "").toContain("scale 0.73");

    ui.setAuthoritativeGrid(80, 24); // 1280/(80*8)=2.0 -> clamp 1.00
    overlay = (container.parentElement ?? container).querySelector("[data-termix-debug]");
    expect(overlay?.textContent ?? "").toContain("scale 1.00");

    ui.dispose();
    localStorage.removeItem("termix_debug");
  });

  it("recompute only rescales in authoritative mode (no requestResize, no grid change)", () => {
    vi.useFakeTimers();
    const requestResize = vi.fn();
    (window as { requestResize?: (c: number, r: number) => void }).requestResize = requestResize;
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);
    const ui = mountTerminal(container);
    const term = terminalMock.instances[0];

    ui.setAuthoritativeGrid(220, 50);
    requestResize.mockClear();
    term.resize.mockClear();

    setContainerSize(container, 800, 600);
    ui.fit(); // drives recompute()
    vi.advanceTimersByTime(350);

    expect(requestResize).not.toHaveBeenCalled();
    expect(term.resize).not.toHaveBeenCalled(); // grid unchanged; only transform updates

    ui.dispose();
    delete (window as { requestResize?: (c: number, r: number) => void }).requestResize;
    vi.useRealTimers();
  });
});
```

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/ui/terminal.test.ts
```

  Expected before impl: FAIL (`setAuthoritativeGrid` undefined).

- [ ] **Implement** in `web/app/src/ui/terminal.ts`:

  1. Add to the `TerminalUI` interface (after `setGrid`, line 107):

```typescript
  setAuthoritativeGrid(cols: number, rows: number): void;
```

  2. After `term.open(container)` (line 125), wrap xterm's element in a scaler div:

```typescript
  const scaler = document.createElement("div");
  scaler.style.cssText = "display:inline-block;transform-origin:top left;";
  const el = term.element;
  if (el && el.parentElement) {
    el.parentElement.insertBefore(scaler, el);
    scaler.appendChild(el);
  }
```

  3. Add state (after `let lastRows = initial.rows;`, line 128):

```typescript
  let authoritativeMode = false;
  let currentScale = 1;
```

  4. Add `recomputeScale()` (before `recompute`, ~line 175) and `setAuthoritativeGrid` (after `setGrid`, ~line 135):

```typescript
  const recomputeScale = (): void => {
    if (!authoritativeMode) return;
    const { width } = containerSize(container);
    const cell = measureCell(term);
    const cellW = cell && cell.w > 0 ? cell.w : DEFAULT_CELL_W;
    const raw = Math.min(1, width / (lastCols * cellW));
    currentScale = Math.round(raw * 100) / 100;
    scaler.style.transform = `scale(${currentScale})`;
  };

  const setAuthoritativeGrid = (cols: number, rows: number): void => {
    authoritativeMode = true;
    setGrid(cols, rows);
    recomputeScale();
    updateOverlay();
  };
```

  5. Modify `recompute()` (lines 176–192) so that in authoritative mode it only rescales:

```typescript
  const recompute = (): void => {
    if (debounceTimer !== null) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debounceTimer = null;
      if (authoritativeMode) {
        recomputeScale();
      } else {
        const { width, height } = containerSize(container);
        const cell = measureCell(term) ?? undefined;
        const next = pickGrid(width, height, cell);
        if (next.cols !== lastCols || next.rows !== lastRows) {
          setGrid(next.cols, next.rows);
          const fn = (window as { requestResize?: (c: number, r: number) => void }).requestResize;
          if (fn) fn(next.cols, next.rows);
        }
      }
      updateOverlay();
    }, RESIZE_DEBOUNCE_MS);
  };
```

  6. Extend the DEBUG overlay text (line 171) to append pane/scale in authoritative mode:

```typescript
      `grid ${lastCols}×${lastRows}` +
      (authoritativeMode ? ` · pane ${lastCols}×${lastRows} · scale ${currentScale.toFixed(2)}` : "");
```

  7. Return `setAuthoritativeGrid` from the returned object (line 219 region, alongside `setGrid`).

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/ui/terminal.test.ts
```

  Expected: PASS (new describe block + existing terminal tests).

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add web/app/src/ui/terminal.ts web/app/src/ui/terminal.test.ts && git commit -m "$(cat <<'EOF'
feat(web): adopt authoritative grid + CSS downscale-to-fit (never upscale)

setAuthoritativeGrid resizes xterm to the daemon size; a scaler wrapper applies
transform: scale(min(1, containerW/(cols*cellW))). recompute() only rescales in
authoritative mode; pickGrid retained for old-daemon fallback.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Web — `inbound.ts` adopts authoritative grid + suppresses `client.resize`

On `snapshot.ready` with cols/rows, adopt the authoritative grid and enter authoritative mode (record generation). In authoritative mode, `requestResize` no longer sends `client.resize`; without cols/rows (old daemon) it keeps the Stage-1 path. (This is the inbound adopt edit; Task 10 adds the fence to the same file — keep them consecutive.)

- [ ] **Test (write first)** in `web/app/src/bridge/inbound.test.ts`. Add `setAuthoritativeGrid` to the stub UI (`makeStubUI` / the inline stub around line 50), then add cases:

```typescript
it("snapshot.ready with cols/rows calls setAuthoritativeGrid", async () => {
  const { factory } = mockFactory();
  const ui = makeStubUI({ cols: 100, rows: 30 });
  const spy = vi.spyOn(ui, "setAuthoritativeGrid");
  installInboundBridge({ ui, factory });
  w.setSession!("s1", "wss://relay/ws", "tok", "dev-1");
  const ws = await flushUntilWS();
  ws.triggerOpen(); await flush();
  ws.triggerText(JSON.stringify({ type: "session.snapshot.ready", request_id: null,
    payload: { session_id: "s1", cols: 220, rows: 50, generation: 1 } }));
  expect(spy).toHaveBeenCalledWith(220, 50);
});

it("snapshot.ready without cols/rows stays in Stage-1 fallback", async () => {
  const { factory } = mockFactory();
  const ui = makeStubUI({ cols: 100, rows: 30 });
  const spy = vi.spyOn(ui, "setAuthoritativeGrid");
  installInboundBridge({ ui, factory });
  w.setSession!("s1", "wss://relay/ws", "tok", "dev-1");
  const ws = await flushUntilWS();
  ws.triggerOpen(); await flush();
  ws.triggerText(JSON.stringify({ type: "session.snapshot.ready", request_id: null,
    payload: { session_id: "s1" } }));
  expect(spy).not.toHaveBeenCalled();
});

it("requestResize is suppressed in authoritative mode", async () => {
  const { factory } = mockFactory();
  const ui = makeStubUI({ cols: 100, rows: 30 });
  installInboundBridge({ ui, factory });
  w.setSession!("s1", "wss://relay/ws", "tok", "dev-1");
  const ws = await flushUntilWS();
  ws.triggerOpen(); await flush();
  ws.triggerText(JSON.stringify({ type: "session.snapshot.ready", request_id: null,
    payload: { session_id: "s1", cols: 220, rows: 50 } }));
  ws.sentText = [];
  w.requestResize!(130, 40);
  const resizes = ws.sentText.filter((m) => { try { return decodeEnvelope(m).type === "client.resize"; } catch { return false; } });
  expect(resizes).toHaveLength(0);
});

it("requestResize sends client.resize in Stage-1 fallback", async () => {
  const { factory } = mockFactory();
  const ui = makeStubUI({ cols: 100, rows: 30 });
  installInboundBridge({ ui, factory });
  w.setSession!("s1", "wss://relay/ws", "tok", "dev-1");
  const ws = await flushUntilWS();
  ws.triggerOpen(); await flush();
  ws.triggerText(JSON.stringify({ type: "session.snapshot.ready", request_id: null,
    payload: { session_id: "s1" } }));
  ws.sentText = [];
  w.requestResize!(130, 40);
  const env = decodeEnvelope(ws.sentText[0]);
  expect(env.type).toBe("client.resize");
  const p = env.payload as { cols: number; rows: number };
  expect(p.cols).toBe(130);
  expect(p.rows).toBe(40);
});
```

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/bridge/inbound.test.ts
```

  Expected before impl: FAIL.

- [ ] **Implement** in `web/app/src/bridge/inbound.ts`:

  1. Add state after `let lastGrid` (line 44):

```typescript
  let authoritativeMode = false;
  let currentGeneration = 0;
```

  2. In the `onText` `session.snapshot.ready` branch (lines 136–138), after `cfg.ui.reset();`:

```typescript
                if (env.type === "session.snapshot.ready") {
                  cfg.ui.reset();
                  const p = env.payload as { cols?: number; rows?: number; generation?: number };
                  if (typeof p.cols === "number" && typeof p.rows === "number") {
                    authoritativeMode = true;
                    currentGeneration = p.generation ?? 0;
                    cfg.ui.setAuthoritativeGrid(p.cols, p.rows);
                  }
                }
```

  3. In `requestResize` (lines 253–269), short-circuit in authoritative mode (keep `lastGrid` updated so a later fallback re-watch still carries a size):

```typescript
  const requestResize = (cols: number, rows: number): void => {
    lastGrid = { cols, rows };
    if (!active) return;
    if (authoritativeMode) return; // viewer never resizes the pane in Stage 2
    active.cols = cols;
    active.rows = rows;
    const payload: Record<string, unknown> = { session_id: active.sessionId, cols, rows };
    if (isDebugEnabled()) {
      payload.debug = { ...viewportDebug(), cols, rows };
    }
    active.ws.sendText(encodeEnvelope("client.resize", payload));
  };
```

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/bridge/inbound.test.ts
```

  Expected: PASS.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add web/app/src/bridge/inbound.ts web/app/src/bridge/inbound.test.ts && git commit -m "$(cat <<'EOF'
feat(web): adopt authoritative grid on snapshot.ready; suppress client.resize

Old daemon (no cols/rows) keeps the Stage-1 pickGrid + client.resize path.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

## Phase 2b — Cursor Restore + Generation Fence

### Task 9: `BuildSnapshot` (cursor restore) — pure function, exact bytes

Append cursor positioning (and hide-only visibility) to the normalized snapshot so the SPA's xterm cursor lands where Claude/Ink expects, fixing the relative-redraw drift.

- [ ] **Test (write first)** in `go/internal/tmux/control_test.go` (same package, append):

```go
import "bytes" // add if not present

func TestBuildSnapshotVisibleCursor(t *testing.T) {
	content := []byte("line1\nline2\nline3")
	got := BuildSnapshot(content, 10, 5, true)
	// NormalizeSnapshot: reset prefix + CRLF; then CUP at row=y+1, col=x+1.
	want := "\x1b[3J\x1b[2J\x1b[H" + "line1\r\nline2\r\nline3" + "\x1b[6;11H"
	if string(got) != want {
		t.Fatalf("visible:\n want %q\n got  %q", want, string(got))
	}
}

func TestBuildSnapshotHiddenCursor(t *testing.T) {
	got := BuildSnapshot([]byte("a"), 0, 0, false)
	want := "\x1b[3J\x1b[2J\x1b[H" + "a" + "\x1b[1;1H" + "\x1b[?25l"
	if string(got) != want {
		t.Fatalf("hidden:\n want %q\n got  %q", want, string(got))
	}
}
```

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestBuildSnapshot -v
```

  Expected before impl: build failure (`undefined: BuildSnapshot`).

- [ ] **Implement** in `go/internal/tmux/control.go` (ensure `fmt` imported):

```go
// BuildSnapshot normalizes pane content (reset prefix + CRLF, via
// NormalizeSnapshot) then appends a CUP escape positioning the cursor at the
// captured location. Coordinates are 0-based (tmux display-message) and
// converted to 1-based CUP (\x1b[{cursorY+1};{cursorX+1}H). If the cursor is
// hidden it also appends \x1b[?25l. Position only — it never emits \x1b[?25h
// (Ink re-shows on its next frame). Pure and unit-tested.
func BuildSnapshot(content []byte, cursorX, cursorY int, cursorVisible bool) []byte {
	buf := bytes.NewBuffer(NormalizeSnapshot(content))
	fmt.Fprintf(buf, "\x1b[%d;%dH", cursorY+1, cursorX+1)
	if !cursorVisible {
		buf.WriteString("\x1b[?25l")
	}
	return buf.Bytes()
}
```

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestBuildSnapshot -v
```

  Expected: PASS (both cases, exact bytes).

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/internal/tmux/control.go go/internal/tmux/control_test.go && git commit -m "$(cat <<'EOF'
feat(tmux): add BuildSnapshot to restore cursor position in snapshots

Position-only CUP; hide-only visibility (never unconditional show).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: `CaptureSnapshotWithCursor` + daemon wiring

Capture pane bytes + cursor (position/visibility) and pair with `BuildSnapshot`; switch the daemon's `Snapshot` func to it.

- [ ] **Test (write first)** in `go/internal/tmux/control_test.go`:

```go
func TestCaptureSnapshotWithCursor(t *testing.T) {
	skipIfNoTmuxCtrl(t)
	name := "termix_test_" + uuid.NewString()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "main", "-x", "80", "-y", "24", "sh", "-c", "printf 'line1\\nline2\\n'; sleep 30").Run(); err != nil {
		t.Fatalf("new-session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	time.Sleep(150 * time.Millisecond)

	snap, err := CaptureSnapshotWithCursor(context.Background(), name)
	if err != nil {
		t.Fatalf("CaptureSnapshotWithCursor: %v", err)
	}
	if !bytes.HasPrefix(snap, []byte("\x1b[3J\x1b[2J\x1b[H")) {
		t.Fatalf("missing reset prefix; got %q", snap[:min(24, len(snap))])
	}
	// Must contain a CUP escape (cursor restore) ending in 'H'.
	if !bytes.Contains(snap, []byte("\x1b[")) || snap[len(snap)-1] != 'H' && !bytes.Contains(snap, []byte("H\x1b[?25l")) {
		t.Fatalf("missing cursor-restore CUP; got tail %q", snap[max(0, len(snap)-12):])
	}
}
```

  (Add small `min`/`max` helpers in the test file if the Go toolchain version lacks the builtins.)

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestCaptureSnapshotWithCursor -v
```

  Expected before impl: build failure (`undefined: CaptureSnapshotWithCursor`).

- [ ] **Implement** in `go/internal/tmux/control.go`:

```go
// CaptureSnapshotWithCursor captures pane content (raw capture-pane via
// SnapshotArgs) plus the cursor position/visibility from display-message, then
// returns BuildSnapshot(content, x, y, visible). Cursor flag: 1 = visible.
// On any cursor-query failure it falls back to the cursor-less CaptureSnapshot.
func CaptureSnapshotWithCursor(ctx context.Context, sessionName string) ([]byte, error) {
	raw, err := exec.CommandContext(ctx, "tmux", SnapshotArgs(sessionName)...).Output()
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p",
		"-t", sessionName+":main.0", "#{cursor_x} #{cursor_y} #{cursor_flag}").Output()
	if err != nil {
		return NormalizeSnapshot(raw), nil
	}
	var x, y, flag int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d %d", &x, &y, &flag); err != nil {
		return NormalizeSnapshot(raw), nil
	}
	return BuildSnapshot(raw, x, y, flag == 1), nil
}
```

  Then in `go/internal/hostdaemon/daemon.go` (lines 119–120), switch:

```go
		Snapshot: func(ctx context.Context, sessionName string) ([]byte, error) {
			return tmux.CaptureSnapshotWithCursor(ctx, sessionName)
		},
```

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./internal/tmux/ -run TestCaptureSnapshotWithCursor -v && go build ./...
```

  Expected: PASS (self-skips without tmux) and the whole module builds.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/internal/tmux/control.go go/internal/tmux/control_test.go go/internal/hostdaemon/daemon.go && git commit -m "$(cat <<'EOF'
feat(tmux,daemon): capture cursor with snapshot and wire it into the daemon

Daemon snapshot func switches CaptureSnapshot -> CaptureSnapshotWithCursor.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 11: Go integration test — multi-viewer pane stability + cursor in snapshot

End-to-end regression for the two core Stage 2 invariants on the Go side.

- [ ] **Test (write first)** create `go/tests/display_stage2_integration_test.go`. Gate on tmux (`skip` if absent). Cover:

  1. `TestTwoDifferentSizeViewersDoNotChangePane`: create a tmux session at a known size; invoke the snapshot-request path for two simulated viewers (drive the relay client's `handleSnapshotRequest` via the same httptest WS harness used in `client_test.go`, or call the snapshot handler + `PaneSize` directly); assert `PaneSize` is unchanged after both, and that no `resizeHandler` was registered/called. Assert each emitted `snapshot.ready` carries the same `cols`/`rows` (the pane size) and that `generation` increments per watch (viewer1 → 1, viewer2 → 2).
  2. `TestSnapshotContainsCursorRestore`: write multi-line content into the pane, call `tmux.CaptureSnapshotWithCursor`, assert reset prefix + content + a trailing CUP escape (proves cursor restore is present end-to-end).

  Follow the existing `go/tests` package conventions (check a sibling test for the package name and helpers). Keep assertions concrete (exact prefix bytes, exact generation values).

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./tests/ -run 'TestTwoDifferentSizeViewers|TestSnapshotContainsCursorRestore' -v
```

  Expected before impl: FAIL or build failure until the test compiles against the real APIs.

- [ ] **Implement / iterate** the test against the real `relayclient` + `tmux` APIs until it passes (no production code change expected — Tasks 1–10 already provide the behavior; this task is the integration assertion). If the test reveals that `PaneSize` inside `handleSnapshotRequest` cannot resolve the tmux name from `req.SessionID` (see Task 4 note), document the actual mapping used and assert against it; if cols/rows are 0 by design in that path, assert generation-increment + pane-unchanged only and record the cols/rows-source limitation in the test comment.

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go test ./tests/ -run 'TestTwoDifferentSizeViewers|TestSnapshotContainsCursorRestore' -v
```

  Expected: PASS (self-skips without tmux).

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add go/tests/display_stage2_integration_test.go && git commit -m "$(cat <<'EOF'
test(integration): multi-viewer pane stability + cursor-in-snapshot

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: Web — generation fence in `watcher.ts`

The fence drops live output frames that arrive before the snapshot completes (and from a stale generation), preventing pre-snapshot output from layering onto a fresh snapshot.

- [ ] **Test (write first)** in `web/app/src/session/watcher.test.ts` (create if absent; mirror existing frame-encoding test helpers, or build minimal `out(seq, payload)` / `snap(seq, isLast, payload)` `DecodedFrame` factories):

```typescript
import { describe, it, expect } from "vitest";
import { createWatcher } from "@/session/watcher";
import type { DecodedFrame } from "@/protocol/types";

const utf8 = (s: string) => new TextEncoder().encode(s);
const out = (seq: number, payload: Uint8Array): DecodedFrame =>
  ({ type: 1, header: { session_id: "s1", seq, stream: "stdout" }, payload } as unknown as DecodedFrame);
const snap = (seq: number, isLast: boolean, payload: Uint8Array): DecodedFrame =>
  ({ type: 3, header: { session_id: "s1", seq, is_last: isLast }, payload } as unknown as DecodedFrame);

describe("watcher generation fence", () => {
  it("drops output frames while a snapshot is pending", () => {
    const writes: string[] = [];
    const w = createWatcher({ sessionId: "s1", write: (b) => writes.push(new TextDecoder().decode(b)) });
    w.setCurrentGeneration(1);
    w.setSnapshotPending(true);
    w.handleFrame(out(0, utf8("EARLY")));      // dropped: pending
    expect(writes).toEqual([]);
    w.handleFrame(snap(0, true, utf8("SNAP"))); // final chunk -> pending=false
    w.handleFrame(out(1, utf8("LIVE")));        // flows
    expect(writes).toEqual(["SNAP", "LIVE"]);
  });

  it("keeps dropping until the final snapshot chunk", () => {
    const writes: string[] = [];
    const w = createWatcher({ sessionId: "s1", write: (b) => writes.push(new TextDecoder().decode(b)) });
    w.setCurrentGeneration(1);
    w.setSnapshotPending(true);
    w.handleFrame(snap(0, false, utf8("AAA")));  // non-final chunk: still pending
    w.handleFrame(out(0, utf8("STALE")));        // dropped
    w.handleFrame(snap(1, true, utf8("BBB")));   // final chunk -> pending=false
    w.handleFrame(out(1, utf8("LIVE")));
    expect(writes).toEqual(["AAA", "BBB", "LIVE"]);
  });
});
```

  (Confirm the actual `DecodedFrame` shape/field names in `protocol/types.ts` and adjust the factories; `is_last` may be named differently — match the real header.)

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/session/watcher.test.ts
```

  Expected before impl: FAIL (`setCurrentGeneration`/`setSnapshotPending` undefined).

- [ ] **Implement** in `web/app/src/session/watcher.ts`:

```typescript
import type { DecodedFrame } from "@/protocol/types";

export interface Watcher {
  handleFrame(frame: DecodedFrame): void;
  setCurrentGeneration(gen: number): void;
  setSnapshotPending(pending: boolean): void;
}

export interface WatcherConfig {
  sessionId: string;
  write: (bytes: Uint8Array) => void;
}

// Generation fence: inbound calls setCurrentGeneration(N) + setSnapshotPending(true)
// when it receives snapshot.ready(gen=N). Until the final snapshot chunk
// (type 3, is_last) arrives, live output frames (type 1) are dropped — they
// belong to the previous snapshot/generation and would layer onto the new one.
export function createWatcher(cfg: WatcherConfig): Watcher {
  let snapshotPending = false;
  // currentGeneration is recorded for parity with inbound; arrival-order +
  // pending flag are the operative fence (frame headers do not carry a gen).
  let currentGeneration = 0;

  return {
    setCurrentGeneration(gen) { currentGeneration = gen; },
    setSnapshotPending(pending) { snapshotPending = pending; },
    handleFrame(frame) {
      if (frame.header.session_id !== cfg.sessionId) return;
      if (frame.type === 3) {
        cfg.write(frame.payload);
        if ((frame.header as { is_last?: boolean }).is_last) snapshotPending = false;
        return;
      }
      if (frame.type === 1) {
        if (snapshotPending) return; // fence: drop pre-snapshot live output
        cfg.write(frame.payload);
      }
      // type 2 is input; never received from server.
    },
  };
}
```

  (Reference `currentGeneration` in a comment/no-op if the linter flags it as unused, or wire it where inbound provides a per-frame gen if the frame header is later extended — for now it is recorded to mirror inbound's contract without a frame-header protocol change.)

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/session/watcher.test.ts
```

  Expected: PASS.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add web/app/src/session/watcher.ts web/app/src/session/watcher.test.ts && git commit -m "$(cat <<'EOF'
feat(web): generation fence in watcher — drop live output until snapshot lands

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 13: Web — `inbound.ts` drives the fence + integration test

Wire inbound to open the fence on `snapshot.ready` (set generation + pending on the watcher) and close it when the final snapshot chunk arrives via the watcher. (Same file as Task 8 — apply after it.)

- [ ] **Test (write first)** in `web/app/src/bridge/inbound.test.ts` — full-cycle case asserting pre-snapshot output is dropped and post-snapshot live output flows:

```typescript
it("fence: snapshot.ready -> chunks -> live; pre-snapshot output dropped", async () => {
  const { factory } = mockFactory();
  const written: string[] = [];
  const ui = makeStubUI({ cols: 100, rows: 30 });
  ui.write = (b: Uint8Array) => { written.push(new TextDecoder().decode(b)); };
  installInboundBridge({ ui, factory });
  w.setSession!("s1", "wss://relay/ws", "tok", "dev-1");
  const ws = await flushUntilWS();
  ws.triggerOpen(); await flush();
  written.length = 0;

  ws.triggerText(JSON.stringify({ type: "session.snapshot.ready", request_id: null,
    payload: { session_id: "s1", cols: 100, rows: 30, generation: 1 } }));
  await flush();
  // EARLY output before snapshot chunk -> dropped
  ws.triggerBinary(encodeFrame(1, { session_id: "s1", seq: 0, stream: "stdout" }, utf8("EARLY")).buffer);
  // snapshot chunk (final) -> written, fence closes
  ws.triggerBinary(encodeFrame(3, { session_id: "s1", seq: 0, is_last: true }, utf8("SNAP")).buffer);
  // live -> written
  ws.triggerBinary(encodeFrame(1, { session_id: "s1", seq: 1, stream: "stdout" }, utf8("LIVE")).buffer);

  expect(written).toEqual(["SNAP", "LIVE"]);
});
```

  (Match `encodeFrame` to the real frame encoder used elsewhere in the test suite; adapt header field names.)

  Verify RED:

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/bridge/inbound.test.ts -t fence
```

  Expected before impl: FAIL (EARLY not dropped — inbound does not yet open the fence).

- [ ] **Implement** in `web/app/src/bridge/inbound.ts`. The `watcher` is created per-connect inside the `connect` closure (line 80). To let `onText` reach it, hoist a reference: declare `let watcher = createWatcher({...})` (already there) and, in the `session.snapshot.ready` branch, call the fence setters on it. Concretely, in the branch added in Task 8:

```typescript
                if (env.type === "session.snapshot.ready") {
                  cfg.ui.reset();
                  const p = env.payload as { cols?: number; rows?: number; generation?: number };
                  watcher.setCurrentGeneration(p.generation ?? 0);
                  watcher.setSnapshotPending(true);
                  if (typeof p.cols === "number" && typeof p.rows === "number") {
                    authoritativeMode = true;
                    currentGeneration = p.generation ?? 0;
                    cfg.ui.setAuthoritativeGrid(p.cols, p.rows);
                  }
                }
```

  The watcher closes the fence itself on the final snapshot chunk (`is_last`, Task 12), so no extra wiring is needed on the binary path. Ensure `watcher` is in scope of `onText` (it is declared just above `openWSClient` in the same closure — confirm and, if needed, move its declaration so both `onText` and `onBinary` capture the same instance).

- [ ] **Verify GREEN:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run src/bridge/inbound.test.ts
```

  Expected: PASS (fence case + all Task 8 cases).

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add web/app/src/bridge/inbound.ts web/app/src/bridge/inbound.test.ts && git commit -m "$(cat <<'EOF'
feat(web): drive the generation fence from inbound on snapshot.ready

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 14: Full suite + progress ledger

- [ ] **Run the full Go + web suites:**

```bash
cd /media/liujia/data/workspace/xunfei/termix/go && go build ./... && go test ./...
cd /media/liujia/data/workspace/xunfei/termix/web/app && npx vitest run
```

  Expected: all green (tmux-gated Go tests self-skip where tmux is unavailable; do not treat skips as failures).

- [ ] **Update `docs/PROGRESS.md`** (project task ledger — mandatory per CLAUDE.md): add a Display Stage 2 entry marked `Completed` for Phase 2a + 2b, listing the shipped invariants (host-authoritative size, viewer no-resize, cursor restore, generation fence) and noting the documented limitation from Task 4/11 (cols/rows source on the `snapshot.ready` path) if it remained.

- [ ] **Commit:**

```bash
cd /media/liujia/data/workspace/xunfei/termix && git add docs/PROGRESS.md && git commit -m "$(cat <<'EOF'
docs(progress): log Display Stage 2 (multi-client sizing + snapshot/live sync)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Corrections (MUST apply — fix two real gaps the draft left)

The drafted Tasks 4/11 + the deferred-monitor note would, if executed literally, ship a **non-working** Stage 2: (a) `cols/rows` can't be resolved from `req.SessionID` inside `client.go` → `snapshot.ready` always carries `0/0` → viewers never enter authoritative mode; (b) the host-resize monitor was deferred → host terminal resizes never propagate to viewers. Both are core Phase 2a. Apply these corrections.

### Correction A — `cols/rows` come from a `sizeHandler` (the daemon side that knows the tmux name), NOT `PaneSize(req.SessionID)`

`req.SessionID` in `client.go` is the daemon-local session id; `tmux.PaneSize` needs the tmux session name (`localSession.TmuxSessionName`), which only the **manager's** snapshot/size wiring has. Mirror the existing `snapshotHandler`/`inputHandler` injection with a `sizeHandler`.

**In Task 3** (relayclient infra), additionally add to `Client` (alongside `gen`):
```go
	sizeHandler func(context.Context, string) (uint32, uint32, error)
```
and a setter:
```go
// SetSizeHandler injects the daemon's session-id -> (cols, rows) lookup
// (resolves the tmux name and calls tmux.PaneSize). Used by handleSnapshotRequest
// to put the authoritative pane size on snapshot.ready.
func (c *Client) SetSizeHandler(fn func(context.Context, string) (uint32, uint32, error)) {
	c.sizeHandler = fn
}
```

**In Task 4** (`handleSnapshotRequest`), replace the `cols, rows, _ := PaneSize(ctx, req.SessionID)` line with:
```go
	var cols, rows uint32
	if c.sizeHandler != nil {
		cols, rows, _ = c.sizeHandler(ctx, req.SessionID) // best-effort; 0/0 -> viewer falls back
	}
```
Delete the Task 4 note about `PaneSize`/`req.SessionID` mismatch — it is resolved here.

**New wiring in `go/internal/session/manager.go`** (do it in the same task as Correction B): in `NewManager`, next to the existing `opts.Relay.SetSnapshotHandler(...)` (line ~169) and guarded by `opts.Relay != nil`:
```go
	if opts.Relay != nil {
		opts.Relay.SetSizeHandler(func(ctx context.Context, sessionID string) (uint32, uint32, error) {
			if opts.Store == nil {
				return 0, 0, errors.New("session store is required")
			}
			ls, err := opts.Store.Load(sessionID)
			if err != nil {
				return 0, 0, err
			}
			return tmux.PaneSize(ctx, ls.TmuxSessionName)
		})
	}
```
Add `SetSizeHandler(func(context.Context, string) (uint32, uint32, error))` and `RepushSnapshot(ctx context.Context, sessionID string, snapshot []byte, cols, rows uint32) error` (Correction B) to the `RelayClient` interface in `manager.go` (the interface `m.relay` implements). Update any fake `RelayClient` in manager tests to satisfy the new methods (no-op is fine).

Task 11's integration test can now assert real `cols/rows` on `snapshot.ready` (drop the "0 by design" hedge).

### Correction B — Host-resize monitor is a REQUIRED task (insert as Task 8b, after Task 8, before Phase 2b)

**### Task 8b: Host-resize monitor + authoritative-size re-push (daemon)**

Poll the pane size per session; when the host terminal resizes (pane size changes and then settles), re-push a fresh snapshot + new authoritative `cols/rows` (reusing the current generation) to all viewers so they re-adopt.

- [ ] **Step 1 — `PaneSize` on the `TmuxRunner` interface (testability).** In `go/internal/session/manager.go`, add to the `TmuxRunner` interface: `PaneSize(ctx context.Context, sessionName string) (uint32, uint32, error)`. In `go/internal/tmux/runner.go`, add the method delegating to the free func from Task 2:
```go
func (r *Runner) PaneSize(ctx context.Context, sessionName string) (uint32, uint32, error) {
	return PaneSize(ctx, sessionName)
}
```
Add `PaneSize` to any fake `TmuxRunner` in manager tests.

- [ ] **Step 2 — `RepushSnapshot` on the relay client.** In `go/internal/relayclient/client.go`:
```go
// RepushSnapshot re-publishes a snapshot to viewers after a host-driven pane
// resize: emits snapshot.ready with the new authoritative size and the CURRENT
// generation (a host resize is not a new watch, so it does NOT increment), then
// publishes the snapshot bytes.
func (c *Client) RepushSnapshot(ctx context.Context, sessionID string, snapshot []byte, cols, rows uint32) error {
	if err := c.writeEnvelope(ctx, relayproto.Envelope{
		Type: relayproto.TypeSessionSnapshotReady,
		Payload: map[string]any{
			"session_id": sessionID,
			"cols":       cols,
			"rows":       rows,
			"generation": c.currentGeneration(sessionID),
		},
	}); err != nil {
		return err
	}
	return c.PublishSnapshot(ctx, sessionID, snapshot)
}
```

- [ ] **Step 3 — Monitor goroutine in `manager.go`** (write the test first against a fake `TmuxRunner` whose `PaneSize` returns a scripted sequence + a fake/no-op `RelayClient` recording `RepushSnapshot` calls; assert: a size change that then stays stable for one extra tick → exactly one `RepushSnapshot` with the new cols/rows; no change → zero calls). Implement:
```go
// startPaneSizeMonitor watches the pane size and re-pushes a fresh snapshot +
// authoritative size to viewers when the host terminal resizes. Double-tick
// debounce: only re-push once the new size has been stable for one interval
// (covers the async SIGWINCH->TUI repaint). Exits when ctx is done.
func (m *Manager) startPaneSizeMonitor(ctx context.Context, sessionID, tmuxName string) {
	const interval = 500 * time.Millisecond
	go func() {
		lastStable, _, _ := m.tmux.PaneSize(ctx, tmuxName)
		var pending uint32 // 0 = none; encodes pending cols (rows tracked alongside)
		var pendingCols, pendingRows uint32
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cols, rows, err := m.tmux.PaneSize(ctx, tmuxName)
				if err != nil {
					continue
				}
				if cols == lastStable { // unchanged (use cols+rows pair in real impl)
					pending = 0
					continue
				}
				if pending != 0 && cols == pendingCols && rows == pendingRows {
					// stable for one tick -> re-push
					lastStable = cols
					pending = 0
					snap, serr := m.snapshot(ctx, tmuxName)
					if serr != nil {
						continue
					}
					_ = m.relay.RepushSnapshot(ctx, sessionID, snap, cols, rows)
					continue
				}
				pending, pendingCols, pendingRows = cols, cols, rows
			}
		}
	}()
}
```
(Track the full `(cols,rows)` pair in the real impl — the sketch uses `cols` as the change sentinel; compare both dimensions.) Wire it in `StartSession` right after `startOutputPipe` (manager.go ~line 332), guarded by `m.relay != nil && m.snapshot != nil`, using the daemon lifetime context. Pass `createResp.SessionId.String()` + `createResp.TmuxSessionName`.

- [ ] **Step 4 — Verify + commit.** `cd .../go && go test ./internal/session/ ./internal/relayclient/ -v` (monitor + RepushSnapshot tests green). Commit: `feat(daemon): host-resize monitor re-pushes authoritative size to viewers`.

> The tmux `client-resized` hook (spec §4.A) remains a deferred *optimization* to make this responsive without the 500ms poll latency — see Deferred below.

---

## Deferred (tracked, not implemented here)

- **`client-resized` hook (host-resize latency optimization):** spec §4.A's preferred fast path. Task 8b ships the polling monitor (correct, ~500ms latency); the hook (`set-hook client-resized 'run-shell ...'` waking the daemon) is a follow-up to cut latency. Record in `docs/PROGRESS.md` as deferred.
- **`ReannounceAllSessions` (manager.go):** emits `PublishSnapshot` without a `snapshot.ready` envelope today. After Stage 2 it should also carry `cols/rows/generation` (it can use the new `sizeHandler`/`RepushSnapshot` path). Minor; fold into a follow-up if reconnect re-adoption needs it.
- **Native pinch-zoom / coordinate mapping (S4):** CSS-scale rendering relies on browser-native pinch-zoom and xterm's `getBoundingClientRect()`-based click mapping. Real-device verification (control-state viewer click accuracy after scale) is a manual test step.
