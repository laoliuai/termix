import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/preact";
import { useVisibility } from "./useVisibility";

function Probe({ onShow }: { onShow: () => void }) {
  useVisibility(onShow);
  return null;
}

describe("useVisibility", () => {
  let originalState: string;

  beforeEach(() => {
    originalState = document.visibilityState;
    Object.defineProperty(document, "visibilityState", {
      value: "visible",
      configurable: true,
    });
  });

  afterEach(() => {
    Object.defineProperty(document, "visibilityState", {
      value: originalState,
      configurable: true,
    });
  });

  it("calls onShow when visibility flips from hidden to visible", () => {
    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
    const onShow = vi.fn();
    render(<Probe onShow={onShow} />);

    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(onShow).toHaveBeenCalled();
  });

  it("does NOT call onShow when visibility flips to hidden", () => {
    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    const onShow = vi.fn();
    render(<Probe onShow={onShow} />);

    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(onShow).not.toHaveBeenCalled();
  });
});
