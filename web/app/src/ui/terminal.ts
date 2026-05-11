import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

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

export function pickGrid(widthPx: number, heightPx: number): { cols: number; rows: number } {
  const cellW = FONT_SIZE * CELL_WIDTH_RATIO;
  const cellH = FONT_SIZE * CELL_HEIGHT_RATIO;
  const cols = Math.max(COLS_FLOOR, Math.floor((widthPx - GUTTER_PX) / cellW));
  const rows = Math.max(ROWS_FLOOR, Math.floor(heightPx / cellH));
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
  dispose(): void;
}

export function mountTerminal(container: HTMLElement): TerminalUI {
  const init = containerSize(container);
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

  // The previous incarnation of mountTerminal called window.requestResize
  // here to surface the initial grid to the bridge — but main.tsx invokes
  // mountTerminal *before* installInboundBridge, so the call always
  // landed on a stale (or undefined) requestResize. The bridge now reads
  // cols()/rows() directly off the returned TerminalUI instead, which
  // makes that hack unnecessary.

  return {
    write(bytes) { term.write(bytes); },
    reset() { term.reset(); },
    cols() { return lastCols; },
    rows() { return lastRows; },
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
