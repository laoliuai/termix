import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { isDebugEnabled, viewportDebug } from "@/debug";

const FONT_SIZE = 13;
const CELL_WIDTH_RATIO = 0.6;
const CELL_HEIGHT_RATIO = 1.2;
const GUTTER_PX = 2;

// Floors keep the TUI usable on narrow phone-portrait viewports where the raw
// container width would otherwise compute to ~45 cols; the resulting xterm
// overflows horizontally but TUIs like claude / codex render correctly. There
// is no upper cap — the previous 120×40 ceiling left a multi-hundred-pixel
// black band on desktop / 4K because xterm could only ever fill 936×624 px.
const COLS_FLOOR = 80;
const ROWS_FLOOR = 20;

const FONT_FAMILY =
  '"DejaVu Sans Mono", Menlo, Consolas, "Liberation Mono", "Courier New", monospace';

const RESIZE_DEBOUNCE_MS = 300;

// Default cell size, used as a fallback before xterm has rendered (so we have
// no measured metrics yet) and in unit tests where the terminal is mocked.
const DEFAULT_CELL_W = FONT_SIZE * CELL_WIDTH_RATIO; // 7.8 px
const DEFAULT_CELL_H = FONT_SIZE * CELL_HEIGHT_RATIO; // 15.6 px

export interface CellSize {
  w: number;
  h: number;
}

// pickGrid converts a container's pixel size into a terminal grid. The cell
// argument lets callers pass xterm's *actual measured* cell dimensions instead
// of the hardcoded 7.8/15.6 px assumption — critical on phones where the
// fallback monospace font (no DejaVu/Menlo installed) has a different advance
// width, so the assumed grid would not match what xterm renders. A missing or
// non-positive cell falls back to the default.
export function pickGrid(widthPx: number, heightPx: number, cell?: CellSize): { cols: number; rows: number } {
  const cellW = cell && cell.w > 0 ? cell.w : DEFAULT_CELL_W;
  const cellH = cell && cell.h > 0 ? cell.h : DEFAULT_CELL_H;
  const cols = Math.max(COLS_FLOOR, Math.floor((widthPx - GUTTER_PX) / cellW));
  const rows = Math.max(ROWS_FLOOR, Math.floor(heightPx / cellH));
  return { cols, rows };
}

function containerSize(container: HTMLElement): { width: number; height: number } {
  const rect = container.getBoundingClientRect();
  const width = container.clientWidth || rect.width || window.innerWidth || 0;
  let height = container.clientHeight || rect.height || window.innerHeight || 0;
  // Soft-keyboard correction. On phones the keyboard slides up and shrinks
  // window.visualViewport; in "pan-mode" browsers (some Android defaults, older
  // Safari) the layout viewport — and thus the container's clientHeight —
  // does NOT shrink, so the bottom of the terminal (where a TUI's input/cursor
  // lives) ends up hidden behind the keyboard. When the visible viewport ends
  // above the container's measured bottom, clamp the height to what's actually
  // visible so pickGrid yields rows that fit on-screen.
  const vv = window.visualViewport;
  if (vv) {
    const available = vv.height + (vv.offsetTop || 0) - rect.top;
    if (available > 0 && available < height) height = available;
  }
  return { width, height };
}

// measureCell reads xterm's *actual* rendered cell dimensions from its internal
// render service — the same private surface FitAddon uses — so pickGrid can size
// the grid to the font the browser really rendered (phone fallback fonts have a
// different advance width than the assumed 7.8px). Returns null when unavailable
// (before first render, or in unit tests where Terminal is mocked) so the caller
// falls back to the default cell.
function defaultMeasureCell(term: Terminal): CellSize | null {
  try {
    const cell = (term as unknown as {
      _core?: { _renderService?: { dimensions?: { css?: { cell?: { width?: number; height?: number } } } } };
    })._core?._renderService?.dimensions?.css?.cell;
    if (cell && typeof cell.width === "number" && typeof cell.height === "number" && cell.width > 0 && cell.height > 0) {
      return { w: cell.width, h: cell.height };
    }
  } catch {
    // fall through to null
  }
  return null;
}

export interface MountOptions {
  // Override how xterm's cell size is measured. Tests inject a fake; production
  // uses defaultMeasureCell (xterm render-service introspection).
  measureCell?: (term: Terminal) => CellSize | null;
}

export interface TerminalUI {
  write(bytes: Uint8Array): void;
  // Hard-reset the xterm buffer (RIS, ESC c) so the next bytes draw onto a
  // blank screen with cleared scrollback. Called by the bridge on every
  // `session.snapshot.ready` envelope so a re-watch (page revisit, WS
  // reconnect) doesn't stack the new snapshot below the previous one.
  reset(): void;
  // Current grid size, in cells. The bridge reads these at install time so
  // the very first `client.resize` it sends to the daemon matches what
  // xterm is actually rendering — instead of the stale 80×24 default that
  // used to leave the daemon's tmux pane permanently undersized.
  cols(): number;
  rows(): number;
  onInput(handler: (text: string) => void): void;
  fit(): void;
  setGrid(cols: number, rows: number): void;
  setAuthoritativeGrid(cols: number, rows: number): void;
  // Move keyboard focus into xterm's hidden textarea so the user types straight
  // at the cursor (and the mobile soft keyboard rises). Called by the bridge on
  // control-grant. No-op-safe in tests where Terminal is mocked.
  focus(): void;
  dispose(): void;
}

export function mountTerminal(container: HTMLElement, opts: MountOptions = {}): TerminalUI {
  const measureCell = opts.measureCell ?? defaultMeasureCell;
  const init = containerSize(container);
  // Initial grid uses the default cell — xterm has not rendered yet, so no
  // measured metrics exist. Corrected below right after open().
  const initial = pickGrid(init.width, init.height);
  const term = new Terminal({
    cursorBlink: true,
    convertEol: false,
    fontSize: FONT_SIZE,
    fontFamily: FONT_FAMILY,
    cols: initial.cols,
    rows: initial.rows,
  });
  term.open(container);

  // Wrap xterm's root element in a scaler div so authoritative mode can apply a
  // CSS transform: scale(...) to downscale the daemon-sized grid into the
  // container without resizing the grid itself. In unit tests Terminal is mocked
  // and has no `element`, so this is a no-op there (guarded below).
  const scaler = document.createElement("div");
  scaler.style.cssText = "display:inline-block;transform-origin:top left;";
  const el = term.element;
  if (el && el.parentElement) {
    el.parentElement.insertBefore(scaler, el);
    scaler.appendChild(el);
  }

  let lastCols = initial.cols;
  let lastRows = initial.rows;
  let authoritativeMode = false;
  let currentScale = 1;

  const setGrid = (cols: number, rows: number): void => {
    if (cols === lastCols && rows === lastRows) return;
    lastCols = cols;
    lastRows = rows;
    term.resize(cols, rows);
  };

  // Now that xterm has rendered once, re-measure the real cell size and correct
  // the grid. The bridge reads cols()/rows() immediately after mountTerminal
  // returns, so this must run synchronously here for the first session.watch to
  // carry the right size.
  const correctForMeasuredCell = (): void => {
    const { width, height } = containerSize(container);
    const cell = measureCell(term) ?? undefined;
    const next = pickGrid(width, height, cell);
    setGrid(next.cols, next.rows);
  };
  correctForMeasuredCell();

  // DEBUG overlay (opt-in via localStorage `termix_debug`). Renders live
  // viewport / measured-cell / grid numbers directly on the device — the most
  // practical way to diagnose phone-only garble, where remote console access is
  // awkward. Default OFF → never created, zero effect on normal rendering.
  let overlay: HTMLElement | null = null;
  const updateOverlay = (): void => {
    if (!isDebugEnabled()) return;
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.setAttribute("data-termix-debug", "");
      overlay.style.cssText =
        "position:absolute;top:4px;right:4px;z-index:9999;font:11px/1.35 monospace;" +
        "color:#7CFC00;background:rgba(0,0,0,.72);padding:4px 6px;white-space:pre;" +
        "pointer-events:none;border-radius:4px;max-width:60vw;";
      (container.parentElement ?? container).appendChild(overlay);
    }
    const cell = measureCell(term);
    const vp = viewportDebug();
    overlay.textContent =
      `vp ${vp.vw}×${vp.vh}  vv ${vp.vvw ?? "-"}×${vp.vvh ?? "-"}  dpr ${vp.dpr}\n` +
      `cell ${cell ? cell.w.toFixed(2) : "?"}×${cell ? cell.h.toFixed(2) : "?"} ` +
      `(assumed ${DEFAULT_CELL_W}×${DEFAULT_CELL_H})\n` +
      `grid ${lastCols}×${lastRows}` +
      (authoritativeMode ? ` · pane ${lastCols}×${lastRows} · scale ${currentScale.toFixed(2)}` : "");
  };
  updateOverlay();

  // recomputeScale CSS-downscales the authoritative grid to fit the container
  // width, never upscaling (scale capped at 1). No-op outside authoritative
  // mode. Rounded to 2 dp so the DEBUG overlay and the applied transform agree.
  const recomputeScale = (): void => {
    if (!authoritativeMode) return;
    const { width } = containerSize(container);
    const cell = measureCell(term);
    const cellW = cell && cell.w > 0 ? cell.w : DEFAULT_CELL_W;
    const raw = Math.min(1, width / (lastCols * cellW));
    currentScale = Math.round(raw * 100) / 100;
    scaler.style.transform = `scale(${currentScale})`;
  };

  // setAuthoritativeGrid adopts the daemon-provided pane size: resize xterm to
  // exactly that grid and CSS-downscale to fit. From here recompute() only
  // rescales — it never changes the grid or sends client.resize back upstream.
  const setAuthoritativeGrid = (cols: number, rows: number): void => {
    // Defense in depth against a non-positive grid (daemon emits 0/0 on a size-
    // query failure). term.resize(0,0) would throw / corrupt xterm — ignore it
    // and stay in whatever mode we were in.
    if (cols <= 0 || rows <= 0) return;
    authoritativeMode = true;
    setGrid(cols, rows);
    recomputeScale();
    updateOverlay();
  };

  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  const recompute = (): void => {
    if (debounceTimer !== null) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debounceTimer = null;
      if (authoritativeMode) {
        // Authoritative mode: the grid is owned by the daemon. A container
        // resize only changes the downscale factor — never the grid, and we
        // never echo a client.resize back upstream.
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
      // Refresh the debug overlay on every recompute — even when the grid is
      // unchanged — so viewport / keyboard changes are visible live.
      updateOverlay();
    }, RESIZE_DEBOUNCE_MS);
  };

  let resizeObserver: ResizeObserver | null = null;
  if (typeof ResizeObserver !== "undefined") {
    resizeObserver = new ResizeObserver(recompute);
    resizeObserver.observe(container);
  } else {
    window.addEventListener("resize", recompute);
  }

  // The visual viewport shrinks when the mobile soft keyboard opens. In
  // pan-mode browsers this does NOT resize the container, so ResizeObserver
  // never fires — listen on visualViewport directly so the keyboard still
  // triggers a recompute (containerSize then clamps to the visible height).
  const vv = window.visualViewport;
  if (vv) {
    vv.addEventListener("resize", recompute);
    vv.addEventListener("scroll", recompute);
  }

  return {
    write(bytes) { term.write(bytes); },
    reset() { term.reset(); },
    cols() { return lastCols; },
    rows() { return lastRows; },
    onInput(handler) { term.onData(handler); },
    fit() { recompute(); },
    setGrid,
    setAuthoritativeGrid,
    focus() { term.focus(); },
    dispose() {
      if (debounceTimer !== null) clearTimeout(debounceTimer);
      resizeObserver?.disconnect();
      if (!resizeObserver) window.removeEventListener("resize", recompute);
      if (vv) {
        vv.removeEventListener("resize", recompute);
        vv.removeEventListener("scroll", recompute);
      }
      overlay?.remove();
      overlay = null;
      term.dispose();
    },
  };
}
