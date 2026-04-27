import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/preact";
import { snackbar } from "../app/store";
import { Snackbar } from "./snackbar";

describe("Snackbar", () => {
  beforeEach(() => {
    snackbar.value = null;
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders nothing when signal is null", () => {
    const { container } = render(<Snackbar />);
    expect(container.textContent).toBe("");
  });

  it("renders message and kind class when signal set", () => {
    snackbar.value = { msg: "hello", kind: "info" };
    render(<Snackbar />);
    const el = screen.getByText("hello");
    expect(el).toBeTruthy();
    // The wrapping element should have kind-info class
    expect(el.closest(".snackbar")?.className).toContain("kind-info");
  });

  it("auto-dismisses info after 3s", () => {
    snackbar.value = { msg: "auto", kind: "info" };
    render(<Snackbar />);
    vi.advanceTimersByTime(3000);
    expect(snackbar.value).toBeNull();
  });

  it("auto-dismisses warn after 3s", () => {
    snackbar.value = { msg: "warn-msg", kind: "warn" };
    render(<Snackbar />);
    vi.advanceTimersByTime(3000);
    expect(snackbar.value).toBeNull();
  });

  it("does NOT auto-dismiss error", () => {
    snackbar.value = { msg: "boom", kind: "error" };
    render(<Snackbar />);
    vi.advanceTimersByTime(10000);
    expect(snackbar.value?.msg).toBe("boom");
  });

  it("renders action button and triggers callback when clicked", () => {
    const cb = vi.fn();
    snackbar.value = { msg: "update available", kind: "info", action: { label: "Refresh", cb } };
    render(<Snackbar />);
    fireEvent.click(screen.getByText("Refresh"));
    expect(cb).toHaveBeenCalledTimes(1);
  });
});
