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

const RESIZE_DEBOUNCE_MS = 300;

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

  // Surface the initial pick to the bridge so the daemon resizes tmux even
  // when the container hasn't changed since mount.
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
