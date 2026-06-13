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

import { mountTerminal, pickGrid } from "./terminal";

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

  it("corrects the grid using the measured cell size after xterm renders", () => {
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);

    const ui = mountTerminal(container, { measureCell: () => ({ w: 8, h: 16 }) });

    const term = terminalMock.instances[0];
    // constructed at the 7.8px default (163) then corrected to the measured 8px: (1280-2)/8 = 159
    expect(term.resize).toHaveBeenCalledWith(159, 50);
    expect(ui.cols()).toBe(159);
    expect(ui.rows()).toBe(50);

    ui.dispose();
  });

  it("recompute uses the measured cell size, not the hardcoded default", () => {
    vi.useFakeTimers();
    const requestResizeMock = vi.fn();
    (window as { requestResize?: (c: number, r: number) => void }).requestResize = requestResizeMock;

    const container = document.createElement("div");
    setContainerSize(container, 360, 200);
    const ui = mountTerminal(container, { measureCell: () => ({ w: 8, h: 16 }) });

    requestResizeMock.mockClear();
    setContainerSize(container, 1280, 800);
    resizeCallback!([], resizeObservers[0] as unknown as ResizeObserver);
    vi.advanceTimersByTime(350);

    expect(requestResizeMock).toHaveBeenCalledWith(159, 50);

    ui.dispose();
    delete (window as { requestResize?: (c: number, r: number) => void }).requestResize;
  });

  it("recomputes when the visual viewport shrinks (soft keyboard, pan-mode)", () => {
    vi.useFakeTimers();
    const requestResizeMock = vi.fn();
    (window as { requestResize?: (c: number, r: number) => void }).requestResize = requestResizeMock;

    const listeners: Record<string, Array<() => void>> = {};
    const fakeVV = {
      height: 800,
      offsetTop: 0,
      addEventListener: (ev: string, cb: () => void) => { (listeners[ev] ??= []).push(cb); },
      removeEventListener: vi.fn(),
    };
    Object.defineProperty(window, "visualViewport", { configurable: true, value: fakeVV });

    const container = document.createElement("div");
    setContainerSize(container, 360, 800);
    const ui = mountTerminal(container);

    requestResizeMock.mockClear();
    // Keyboard slides up: the visual viewport shrinks but the container's
    // clientHeight stays 800 (pan-mode). The visualViewport resize event must
    // still drive a recompute, and the grid must use the reduced height.
    fakeVV.height = 400;
    (listeners["resize"] ?? []).forEach((cb) => cb());
    vi.advanceTimersByTime(350);

    expect(requestResizeMock).toHaveBeenCalled();
    const lastCall = requestResizeMock.mock.calls[requestResizeMock.mock.calls.length - 1];
    // 400 / 15.6 = 25.6 → 25 rows (down from 800/15.6=51)
    expect(lastCall[1]).toBe(25);

    ui.dispose();
    delete (window as { requestResize?: (c: number, r: number) => void }).requestResize;
    Object.defineProperty(window, "visualViewport", { configurable: true, value: undefined });
  });
});

describe("pickGrid cell metrics", () => {
  it("uses the provided measured cell size instead of the hardcoded default", () => {
    // cell 8px wide → (1280 - 2 gutter) / 8 = 159.75 → 159; 800 / 16 = 50
    const grid = pickGrid(1280, 800, { w: 8, h: 16 });
    expect(grid.cols).toBe(159);
    expect(grid.rows).toBe(50);
  });

  it("falls back to the 7.8 / 15.6 default when no cell size is given", () => {
    const grid = pickGrid(1280, 800);
    expect(grid.cols).toBe(163);
    expect(grid.rows).toBe(51);
  });

  it("ignores a zero/invalid cell size and uses the default", () => {
    const grid = pickGrid(1280, 800, { w: 0, h: 0 });
    expect(grid.cols).toBe(163);
    expect(grid.rows).toBe(51);
  });

  it("still applies the 80x20 floor with a measured cell on a narrow phone", () => {
    const grid = pickGrid(360, 200, { w: 9, h: 18 });
    expect(grid.cols).toBe(80);
    expect(grid.rows).toBe(20);
  });
});

describe("debug overlay", () => {
  afterEach(() => localStorage.removeItem("termix_debug"));

  it("mounts an on-screen overlay when debug mode is enabled", () => {
    localStorage.setItem("termix_debug", "1");
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);
    const ui = mountTerminal(container);
    expect(container.querySelector("[data-termix-debug]")).not.toBeNull();
    ui.dispose();
  });

  it("does not mount an overlay when debug mode is off (default)", () => {
    localStorage.removeItem("termix_debug");
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);
    const ui = mountTerminal(container);
    expect(container.querySelector("[data-termix-debug]")).toBeNull();
    ui.dispose();
  });

  it("removes the overlay on dispose", () => {
    localStorage.setItem("termix_debug", "1");
    const container = document.createElement("div");
    setContainerSize(container, 1280, 800);
    const ui = mountTerminal(container);
    ui.dispose();
    expect(container.querySelector("[data-termix-debug]")).toBeNull();
  });
});
