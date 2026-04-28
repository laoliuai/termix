import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const terminalMock = vi.hoisted(() => ({
  instances: [] as Array<{
    options: Record<string, unknown>;
    open: ReturnType<typeof vi.fn>;
    write: ReturnType<typeof vi.fn>;
    onData: ReturnType<typeof vi.fn>;
    dispose: ReturnType<typeof vi.fn>;
  }>,
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn((options: Record<string, unknown>) => {
    const instance = {
      options: { ...options },
      open: vi.fn(),
      write: vi.fn(),
      onData: vi.fn(),
      dispose: vi.fn(),
    };
    terminalMock.instances.push(instance);
    return instance;
  }),
}));

import { mountTerminal } from "./terminal";

const COLS = 120;
const CELL_WIDTH_RATIO = 0.6;
const originalResizeObserver = window.ResizeObserver;

let resizeCallback: ResizeObserverCallback | null = null;
const resizeObservers: Array<{ observe: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }> = [];

class FakeResizeObserver {
  observe = vi.fn();
  disconnect = vi.fn();

  constructor(callback: ResizeObserverCallback) {
    resizeCallback = callback;
    resizeObservers.push(this);
  }
}

function setClientWidth(el: HTMLElement, width: number): void {
  Object.defineProperty(el, "clientWidth", { configurable: true, value: width });
}

function expectedGridWidth(fontSize: number): number {
  return COLS * fontSize * CELL_WIDTH_RATIO;
}

describe("mountTerminal", () => {
  beforeEach(() => {
    terminalMock.instances.length = 0;
    resizeCallback = null;
    resizeObservers.length = 0;
    Object.defineProperty(window, "ResizeObserver", {
      configurable: true,
      writable: true,
      value: FakeResizeObserver,
    });
  });

  afterEach(() => {
    Object.defineProperty(window, "ResizeObserver", {
      configurable: true,
      writable: true,
      value: originalResizeObserver,
    });
  });

  it("chooses a phone-width font size that fits the fixed 120-column grid", () => {
    const container = document.createElement("div");
    setClientWidth(container, 390);

    const ui = mountTerminal(container);

    const fontSize = terminalMock.instances[0].options.fontSize as number;
    expect(fontSize).toBeGreaterThan(4);
    expect(expectedGridWidth(fontSize)).toBeLessThanOrEqual(390);
    ui.dispose();
  });

  it("recomputes font size when the terminal container width changes", () => {
    const container = document.createElement("div");
    setClientWidth(container, 390);

    const ui = mountTerminal(container);
    expect(resizeCallback).toBeTypeOf("function");

    setClientWidth(container, 780);
    resizeCallback!([], resizeObservers[0] as unknown as ResizeObserver);

    const fontSize = terminalMock.instances[0].options.fontSize as number;
    expect(expectedGridWidth(fontSize)).toBeLessThanOrEqual(780);

    ui.dispose();
    expect(resizeObservers[0].disconnect).toHaveBeenCalled();
  });
});
