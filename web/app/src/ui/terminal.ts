import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

// Termix V1: tmux pane is fixed at 120 cols × 40 rows (see runner.go:54-56).
// xterm must match — otherwise Claude's TUI uses absolute cursor positions for
// a 120-col canvas and renders correctly only at that width. Resize negotiation
// is a future feature; for now we lock both ends to the same grid.
const COLS = 120;
const ROWS = 40;

// Default font-size on a desktop browser. On narrower viewports (phones) we
// shrink so 120 cols fits inside the container width — otherwise xterm's
// element overflows and the user can only see one edge.
const DEFAULT_FONT_SIZE = 14;
const MIN_FONT_SIZE = 9;

// Approximate cell width as a fraction of font-size for a typical monospace
// font. Used only to pick an initial font-size that fits 120 cols.
const CELL_WIDTH_RATIO = 0.6;

const FONT_FAMILY =
  '"DejaVu Sans Mono", Menlo, Consolas, "Liberation Mono", "Courier New", monospace';

function pickFontSize(containerWidthPx: number): number {
  if (containerWidthPx <= 0) return DEFAULT_FONT_SIZE;
  const widthAtDefault = COLS * DEFAULT_FONT_SIZE * CELL_WIDTH_RATIO;
  if (containerWidthPx >= widthAtDefault) return DEFAULT_FONT_SIZE;
  const scaled = Math.floor(containerWidthPx / (COLS * CELL_WIDTH_RATIO));
  return Math.max(MIN_FONT_SIZE, scaled);
}

export interface TerminalUI {
  write(bytes: Uint8Array): void;
  onInput(handler: (text: string) => void): void;
  fit(): void;
  dispose(): void;
}

export function mountTerminal(container: HTMLElement): TerminalUI {
  const fontSize = pickFontSize(container.clientWidth);
  const term = new Terminal({
    cursorBlink: true,
    convertEol: false,
    fontSize,
    fontFamily: FONT_FAMILY,
    cols: COLS,
    rows: ROWS,
  });
  term.open(container);

  return {
    write(bytes) { term.write(bytes); },
    onInput(handler) { term.onData(handler); },
    // fit() kept for API compatibility but is a no-op while we lock to a
    // fixed 120x40 grid.
    fit() { /* no-op */ },
    dispose() { term.dispose(); },
  };
}
