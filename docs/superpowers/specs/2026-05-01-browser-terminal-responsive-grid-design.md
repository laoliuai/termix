# Browser-side terminal display: responsive grid + control-aware mobile layout

**Status:** Design approved 2026-05-01.
**Authors:** Claude (with LiuJia).
**Scope cap:** "B" — layout polish + responsive tmux grid via a new resize protocol. No live continuous reflow, no per-client viewport tracking.

## Problem

Today the browser terminal is unusable on phones. Two coupled issues:

1. **Fixed tmux grid.** `go/internal/tmux/runner.go` opens every pane at `-x 120 -y 40` and locks it with `set-option window-size manual`. The Web UI hardcodes the same `120 × 40` in `web/app/src/ui/terminal.ts`. There is no resize message in the relay protocol, so the SPA cannot ask for a different grid. On phone portrait the SPA's only escape is to shrink xterm's font-size to fit 120 cells in ~360 px → glyphs end up at 4–5 px, unreadable, and TUIs (Claude, Codex, opencode) render misaligned.
2. **Composer + Toolbar always rendered.** `web/app/src/pages/terminal.tsx` mounts both unconditionally and just toggles `disabled`. They eat ~150 px of vertical space on phones even when the user has no control and cannot type.

## Goals

- Phone portrait should render at a font-size where TUIs are actually readable, with the host pane sized to match so absolute-positioning TUIs are not garbled.
- Read-only sessions should not waste vertical space on input chrome the user cannot use.
- No tmux behavior surprises for the host user attaching via local `tmux attach`.

## Non-goals

- **Live continuous reflow** during arbitrary viewport changes (the C scope option). Resize fires on connect + on debounced orientation / large-width changes, nothing more.
- **Per-client viewport tracking.** Multi-client coexistence is last-wins; "largest-wins" is deferred.
- **Soft-keyboard-driven resize.** Keyboard shows / hides via `useKeyboardOffset`; the terminal grid stays the same and xterm renders what fits.
- **Header / control-bar redesign on mobile.** Hiding composer + toolbar already reclaims ~150 px; collapsing the 70 px of header chrome would clutter the mobile UI for marginal gain.
- **Pinch-to-zoom on the terminal.** Browsers already provide that natively; no in-app gesture.

## Design overview

The SPA owns grid policy. It computes a target `(cols, rows)` from its viewport, sets xterm to that grid locally, and sends a new `client.resize` frame to the daemon. The daemon translates that into `tmux resize-window`. tmux's `window-size manual` mode stays — daemon owns the pane size, host-side `tmux attach` does not fight the SPA.

```
┌─────────────────────────────────────────┐
│  Web SPA (xterm.js)                     │
│   ─ pickGrid(viewport) → (cols, rows)   │
│   ─ term.resize(cols, rows)             │
│   ─ window.requestResize(cols, rows)    │
└──────────────┬──────────────────────────┘
               │ client.resize WS frame
               ▼
┌─────────────────────────────────────────┐
│  termix-relay                           │
│   ─ forward as ClientResize gRPC        │
└──────────────┬──────────────────────────┘
               │ relay-control gRPC
               ▼
┌─────────────────────────────────────────┐
│  termixd (daemon, on host)              │
│   ─ Manager.ResizeSession               │
│   ─ tmux.Runner.ResizeWindow            │
│       → `tmux resize-window -t … -x -y` │
└─────────────────────────────────────────┘
```

## Resize protocol

### When the SPA computes

- on initial WS connect (always, before subscribing to output)
- on orientation change, **or** on viewport-width delta ≥ 10 % since the last sent grid, debounced 300 ms (iOS Safari fires a flurry on rotation)
- on tab-visibility regain (heals the multi-tab last-wins case automatically)
- **not** on soft-keyboard appearance — the keyboard pushes layout up, the terminal grid stays the same

### Grid algorithm

```
target_font_px = 13
cell_w = font * 0.6       // monospace approx
cell_h = font * 1.2
cols = clamp(80,  floor((vw - gutter) / cell_w),       120)
rows = clamp(20,  floor( available_h  / cell_h),        40)
```

- **Cols** floor at `80`, ceiling at `120`. `80` is the readability-vs-fidelity pick from the brainstorm (B): keeps Claude/Codex/opencode TUI status lines intact, costs a small font on portrait phones; `120` is the current desktop grid, no need to grow further.
- **Rows** floor at `20`, ceiling at `40`. `20` is below the typical phone-portrait `24` so unusually short viewports still render — better a 20-row TUI than overflow. Tall phones / desktops grow naturally up to `40`.
- Font-size is fixed at 13 px. The previous "shrink the font to fit 120 cols" behavior is removed.

### New WS frame: `client.resize`

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
  }
}
```

Carried in the existing envelope (`schemas/ws/envelope.schema.json`) under `type: "client.resize"`.

### Relay → daemon plumbing

`proto/relay_control.proto`: add a `ClientResize` message in the relay-control stream the host's daemon already consumes for input + control acquisition. The relay forwards the SPA's `client.resize` frame as `ClientResize { session_id, cols, rows }`. The daemon's relay-control adapter (`go/internal/relaycontrol/`) routes it to `Manager.ResizeSession`.

### Daemon RPC

`proto/daemon.proto` adds:

```proto
message ResizeSessionRequest {
  string session_id = 1;
  uint32 cols = 2;
  uint32 rows = 3;
}
message ResizeSessionResponse {}

service DaemonService {
  rpc ResizeSession(ResizeSessionRequest) returns (ResizeSessionResponse);
}
```

`Manager.ResizeSession`:

1. Look up the local session by `session_id`. Return `session_not_found` if missing.
2. Call `tmux.Runner.ResizeWindow(ctx, sessionName, cols, rows)`.
3. Emit a `terminal.snapshot` frame for the SPA so xterm can immediately redraw at the new dimensions (avoids a brief flicker race against tmux's own re-render).

### tmux runner

`go/internal/tmux/runner.go`:

```go
func (r *Runner) ResizeWindow(ctx context.Context, sessionName string, cols, rows uint32) error {
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

`StartSession` keeps `set-option window-size manual` — `resize-window` works in manual mode and the daemon's value is what tmux honors against any concurrent host-side attach.

## SPA changes

### `web/app/src/ui/terminal.ts`

- Drop `COLS = 120; ROWS = 40` constants. Replace with `pickGrid(container)` returning `(cols, rows)` per the algorithm above.
- `mountTerminal(container)` constructs `Terminal({ cols, rows, fontSize: 13 })`.
- `TerminalUI` gains `setGrid(cols, rows)` which calls `term.resize(cols, rows)`.
- A debounced `ResizeObserver` callback recomputes grid; if `(cols, rows)` differ from the last sent values, calls `setGrid` locally and `window.requestResize(cols, rows)` over the bridge.
- `useVisibility`-style hook re-fires `requestResize` on tab-visibility regain.

### `web/app/src/bridge/`

- New outbound bridge function `requestResize(cols, rows)` that emits a `client.resize` envelope.
- `setSession(...)` triggers an initial `requestResize` before subscribing to output frames.

### `web/app/src/pages/terminal.tsx`

- `Composer` and `Toolbar` rendered only when `controlState.value === "granted"`. Read-only mode shows just header + control-bar + terminal. The existing control-bar's `Request control` button remains the sole CTA.
- A `max-height` 0 → auto, 180 ms transition wraps composer + toolbar so they slide in/out instead of popping.
- The `disabled` prop on `Composer`/`Toolbar` is removed (no longer needed — they only mount when active).

### `web/app/src/protocol/types.ts`

- Add `ClientResizeFrame { type: "client.resize"; session_id: string; cols: number; rows: number }`.

## Multi-client coexistence

Last-wins. If two SPA tabs are open at different sizes, whichever sent `client.resize` most recently sets the pane. The "loser" tab heals automatically on visibility regain (the SPA fires `requestResize` from `useVisibility`).

Pathological 2-tab simultaneous case is rare. "Largest-wins" / per-client viewport tracking is explicitly out of scope.

## Edge cases

- **Daemon resize fails or times out.** SPA already set xterm to the new grid locally before sending the frame; logs once, continues. The host TUI may render at a stale tmux grid until the next resize or reconnect — strictly better than blocking input on the ack.
- **`tmux resize-window` errors** (usually because the session died between lookup and call). Manager returns `session_not_found`; SPA treats it the same as any other session-end signal.
- **Reaper sweep concurrent with resize.** No conflict — the reaper only deletes sessions whose tmux pane is gone; resize on a live pane is independent.
- **Host user attaches locally from a 184-col shell.** `window-size manual` plus daemon-driven `resize-window` keep the pane at whatever the SPA last requested. Host shell sees wasted real estate but TUIs render correctly. Preserves existing intent.
- **Initial-connect race.** SPA sends `client.resize` before subscribing to output frames; daemon resizes tmux first; the first snapshot the SPA receives is already at the new grid. No flicker.
- **Backwards compatibility.** New SPA against old daemon: relay forwards `ClientResize`; old daemon returns `Unimplemented`; SPA logs once and falls back to font-shrink behavior. Old SPA against new daemon: SPA never sends the frame; daemon never resizes; default `120 × 40` stands. Both directions degrade safely. **No protocol break.**

## Mobile layout

Read-only state on a phone (with these changes) lays out as:

```
[ ‹  claude · main          live ]   <- term-header   (~40 px)
[ ● Read-only   [Request control] ]  <- control-bar   (~30 px)
[                                 ]
[       terminal pane             ]  <- xterm 80×24   (rest)
[       (80 cols × 20–24 rows)    ]
[                                 ]
```

Granted state inserts composer + toolbar slide-up below the terminal, ~150 px.

The header (back + name + connection badge) and control bar (state + button) keep their 2-row layout on mobile. Collapsing them buys ~40 px for non-trivial UI churn; deferred.

## Tests

### Web

- `terminal.test.tsx`:
  - read-only state: no composer / toolbar in the DOM
  - granted state: composer + toolbar present
- `terminal.ts` unit test: `mountTerminal` with a 360-px container picks `cols = 80`; with a 1280-px container picks `cols = 120`. Mocks `ResizeObserver`.
- Bridge test: `requestResize(80, 24)` produces a `client.resize` frame with the matching payload.

### Go

- `tmux/runner_test.go`: `TestResizeWindow` (gated on real tmux + `TERMIX_TMUX_INTEGRATION=1`) — start a session at `120 × 40`, call `ResizeWindow(80, 24)`, capture-pane size, assert.
- `session/manager_test.go`: unit test with fake tmux — `ResizeSession` calls runner with the right args; returns `session_not_found` for unknown id.
- Relay-control adapter test: forwarding `ClientResize` invokes `Manager.ResizeSession`.

### Manual smoke

- Open Web on phone-portrait → grid auto-becomes `80 × 24`; Claude TUI re-renders at the new grid.
- Rotate to landscape → grid grows to ~`100 × 28`.
- Release control → composer + toolbar slide out, terminal expands.
- Same session on desktop tab → `120 × 40`. Phone tab regains focus → grid renegotiates back to `80 × 24` (last-wins demo).

## Verification matrix

```
make generate                                         # protos + sqlc + openapi
cd go && go build ./... && go vet ./... && go test ./...
TERMIX_TEST_DATABASE_URL=…                            # control-plane integration
TERMIX_TMUX_INTEGRATION=1                             # tmux integration
cd web/app && npm run typecheck && npm test -- --run && npm run build
make build-web && make check-web-dist
```

## Out of scope, explicit

- Live continuous reflow (option C).
- Per-client viewport tracking / largest-wins.
- Soft-keyboard-triggered resize.
- Header + control-bar redesign on mobile.
- Pinch-to-zoom in-app gesture.
- Cleanup of `<LogDir>/sessions/*.err` (from the prior daemon-log fix).
