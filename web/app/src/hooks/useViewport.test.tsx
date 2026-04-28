import { describe, it, expect, beforeEach } from "vitest";
import { render } from "@testing-library/preact";
import { useKeyboardOffset } from "./useViewport";

let listeners: Record<string, EventListener[]>;

function setupVisualViewport(height: number, offsetTop: number, innerHeight: number) {
  const vv: any = {
    height,
    offsetTop,
    addEventListener: (e: string, f: EventListener) => {
      (listeners[e] ??= []).push(f);
    },
    removeEventListener: (e: string, f: EventListener) => {
      listeners[e] = (listeners[e] ?? []).filter(x => x !== f);
    },
  };
  Object.defineProperty(window, "visualViewport", { value: vv, configurable: true });
  Object.defineProperty(window, "innerHeight", { value: innerHeight, configurable: true });
  return vv;
}

let offset: any;

function Probe() {
  offset = useKeyboardOffset();
  return null;
}

describe("useKeyboardOffset", () => {
  beforeEach(() => {
    listeners = {};
    offset = null;
  });

  it("returns 0 when keyboard is hidden (full viewport)", () => {
    setupVisualViewport(800, 0, 800);
    render(<Probe />);
    expect(offset.value).toBe(0);
  });

  it("reflects keyboard offset on resize event", () => {
    const vv = setupVisualViewport(800, 0, 800);
    render(<Probe />);
    expect(offset.value).toBe(0);

    // Soft keyboard appears: visualViewport shrinks to 500
    vv.height = 500;
    listeners["resize"]?.forEach((l: EventListener) => l(new Event("resize")));

    expect(offset.value).toBe(300);
  });
});
