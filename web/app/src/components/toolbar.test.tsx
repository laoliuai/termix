import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/preact";
import { Toolbar } from "./toolbar";

describe("Toolbar", () => {
  beforeEach(() => {
    cleanup();
  });

  it("renders all 11 special keys with friendly glyphs", () => {
    render(<Toolbar disabled={false} onSpecial={() => {}} />);
    for (const label of ["Esc","Tab","↑","↓","←","→","⌫","^C","^D","^J","Enter"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
  });

  it("does not render a digit row", () => {
    const { container } = render(<Toolbar disabled={false} onSpecial={() => {}} />);
    expect(container.querySelector(".row.digits")).toBeNull();
    expect(screen.queryByText("3")).toBeNull();
  });

  it("special-key click calls onSpecial with canonical SpecialKey value", () => {
    const onSpecial = vi.fn();
    render(<Toolbar disabled={false} onSpecial={onSpecial} />);

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

  it("special-key button preventDefaults mousedown (keeps focus) but still sends on click", () => {
    const onSpecial = vi.fn();
    render(<Toolbar disabled={false} onSpecial={onSpecial} />);
    const btn = screen.getByText("Esc");
    // mousedown is cancelled so the button never steals focus from the terminal,
    // and a bare mousedown must NOT send the key...
    const ev = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
    btn.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
    expect(onSpecial).not.toHaveBeenCalled();
    // ...the key fires on click (which mousedown-cancel does not suppress).
    fireEvent.click(btn);
    expect(onSpecial).toHaveBeenCalledWith("Escape");
  });

  it("disabled state applies class and disables buttons", () => {
    const { container } = render(<Toolbar disabled={true} onSpecial={() => {}} />);
    expect(container.querySelector(".toolbar")?.className).toContain("is-disabled");
    const btn = screen.getByText("Esc") as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("Enter button has key-enter class", () => {
    render(<Toolbar disabled={false} onSpecial={() => {}} />);
    const enter = screen.getByText("Enter") as HTMLButtonElement;
    expect(enter.className).toContain("key-enter");
  });

  it("C-c and C-d buttons have key-danger class", () => {
    render(<Toolbar disabled={false} onSpecial={() => {}} />);
    expect((screen.getByText("^C") as HTMLButtonElement).className).toContain("key-danger");
    expect((screen.getByText("^D") as HTMLButtonElement).className).toContain("key-danger");
  });
});
