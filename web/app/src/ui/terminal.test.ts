import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const terminalMock = vi.hoisted(() => ({
  instances: [] as Array<{
    options: Record<string, unknown>;
    open: ReturnType<typeof vi.fn>;
    write: ReturnType<typeof vi.fn>;
    onData: ReturnType<typeof vi.fn>;
    dispose: ReturnType<typeof vi.fn>;
    resize: ReturnType<typeof vi.fn>;
    reset: ReturnType<typeof vi.fn>;
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
      resize: vi.fn(),
      reset: vi.fn(),
    };
    terminalMock.instances.push(instance);
    return instance;
  }),
}));

import { mountTerminal } from "./terminal";

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

function setContainerSize(el: HTMLElement, width: number, height: number): void {
  Object.defineProperty(el, "clientWidth", { configurable: true, value: width });
  Object.defineProperty(el, "clientHeight", { configurable: true, value: height });
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
    vi.useRealTimers();
    Object.defineProperty(window, "ResizeObserver", {
      configurable: true,
      writable: true,
      value: originalResizeObserver,
    });
  });

  it("phone-portrait container floors at 80 cols / 20 rows", () => {
    const container = document.createElement("div");
    setContainerSize(container, 360, 200);

    const ui = mountTerminal(container);

    const opts = terminalMock.instances[0].options;
    expect(opts.cols).toBe(80);
    expect(opts.rows).toBe(20);
    expect(opts.fontSize).toBe(13);

    ui.dispose();
  });

  it("desktop container fills viewport without cap", () => {
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);

    const ui = mountTerminal(container);

    const opts = terminalMock.instances[0].options;
    // (1280 - 2 gutter) / (13 * 0.6) = 163.84 → 163
    expect(opts.cols).toBe(163);
    // 800 / (13 * 1.2) = 51.28 → 51
    expect(opts.rows).toBe(51);

    ui.dispose();
  });

  it("4K container also fills viewport without cap", () => {
    const container = document.createElement("div");
    setContainerSize(container, 3840, 2160);

    const ui = mountTerminal(container);

    const opts = terminalMock.instances[0].options;
    // (3840 - 2) / 7.8 = 492.05 → 492
    expect(opts.cols).toBe(492);
    // 2160 / 15.6 = 138.46 → 138
    expect(opts.rows).toBe(138);

    ui.dispose();
  });

  it("ResizeObserver triggers window.requestResize after debounce", () => {
    vi.useFakeTimers();

    const requestResizeMock = vi.fn();
    (window as { requestResize?: (c: number, r: number) => void }).requestResize = requestResizeMock;

    const container = document.createElement("div");
    setContainerSize(container, 360, 200);

    const ui = mountTerminal(container);

    // Clear the initial requestResize call made at mount time
    requestResizeMock.mockClear();
    const termInstance = terminalMock.instances[0];
    termInstance.resize.mockClear();

    expect(resizeCallback).toBeTypeOf("function");

    // Update container to desktop size and fire ResizeObserver
    setContainerSize(container, 1280, 800);
    resizeCallback!([], resizeObservers[0] as unknown as ResizeObserver);

    // Should not have fired yet (debounce pending)
    expect(requestResizeMock).not.toHaveBeenCalled();

    // Advance past the 300ms debounce
    vi.advanceTimersByTime(350);

    expect(termInstance.resize).toHaveBeenCalledWith(163, 51);
    expect(requestResizeMock).toHaveBeenCalledWith(163, 51);

    ui.dispose();
    delete (window as { requestResize?: (c: number, r: number) => void }).requestResize;
  });
});
