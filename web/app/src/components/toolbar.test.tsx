import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/preact";
import { Toolbar } from "./toolbar";

describe("Toolbar", () => {
  beforeEach(() => {
    cleanup();
  });

  it("renders 10 digit buttons", () => {
    render(<Toolbar disabled={false} onDigit={() => {}} onSpecial={() => {}} />);
    for (const d of "0123456789") {
      expect(screen.getByText(d)).toBeTruthy();
    }
  });

  it("renders all 11 special keys with friendly glyphs", () => {
    render(<Toolbar disabled={false} onDigit={() => {}} onSpecial={() => {}} />);
    for (const label of ["Esc","Tab","↑","↓","←","→","⌫","^C","^D","^J","Enter"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
  });

  it("digit click calls onDigit with the literal", () => {
    const onDigit = vi.fn();
    render(<Toolbar disabled={false} onDigit={onDigit} onSpecial={() => {}} />);
    fireEvent.click(screen.getByText("3"));
    expect(onDigit).toHaveBeenCalledWith("3");
  });

  it("special-key click calls onSpecial with canonical SpecialKey value", () => {
    const onSpecial = vi.fn();
    render(<Toolbar disabled={false} onDigit={() => {}} onSpecial={onSpecial} />);

    fireEvent.click(screen.getByText("⌫"));
    expect(onSpecial).toHaveBeenCalledWith("Backspace");

    fireEvent.click(screen.getByText("^J"));
    expect(onSpecial).toHaveBeenCalledWith("C-j");

    fireEvent.click(screen.getByText("Enter"));
    expect(onSpecial).toHaveBeenCalledWith("Enter");

    fireEvent.click(screen.getByText("Esc"));
    expect(onSpecial).toHaveBeenCalledWith("Escape");

    fireEvent.click(screen.getByText("↑"));
    expect(onSpecial).toHaveBeenCalledWith("Up");
  });

  it("disabled state applies class and disables buttons", () => {
    const onDigit = vi.fn();
    const { container } = render(<Toolbar disabled={true} onDigit={onDigit} onSpecial={() => {}} />);
    expect(container.querySelector(".toolbar")?.className).toContain("is-disabled");
    const btn = screen.getByText("3") as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("Enter button has key-enter class", () => {
    render(<Toolbar disabled={false} onDigit={() => {}} onSpecial={() => {}} />);
    const enter = screen.getByText("Enter") as HTMLButtonElement;
    expect(enter.className).toContain("key-enter");
  });

  it("C-c and C-d buttons have key-danger class", () => {
    render(<Toolbar disabled={false} onDigit={() => {}} onSpecial={() => {}} />);
    expect((screen.getByText("^C") as HTMLButtonElement).className).toContain("key-danger");
    expect((screen.getByText("^D") as HTMLButtonElement).className).toContain("key-danger");
  });
});
