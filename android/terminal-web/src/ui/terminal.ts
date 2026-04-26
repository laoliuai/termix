import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

export interface TerminalUI {
  write(bytes: Uint8Array): void;
  onInput(handler: (text: string) => void): void;
  fit(): void;
  dispose(): void;
}

export function mountTerminal(container: HTMLElement): TerminalUI {
  const term = new Terminal({ cursorBlink: true, convertEol: false, fontSize: 14 });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(container);
  fit.fit();

  return {
    write(bytes) { term.write(bytes); },
    onInput(handler) { term.onData(handler); },
    fit() { fit.fit(); },
    dispose() { term.dispose(); },
  };
}
