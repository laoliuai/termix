import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/preact";

import { CommandBlock } from "./command-block";
import { setLocale } from "../i18n/store";

describe("CommandBlock", () => {
  let writeText: ReturnType<typeof vi.fn>;
  let originalClipboard: PropertyDescriptor | undefined;

  beforeEach(() => {
    cleanup();
    setLocale("en");
    writeText = vi.fn().mockResolvedValue(undefined);
    originalClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    if (originalClipboard) {
      Object.defineProperty(navigator, "clipboard", originalClipboard);
    } else {
      delete (navigator as { clipboard?: unknown }).clipboard;
    }
  });

  it("writes the command to the clipboard and shows a Copied state", async () => {
    render(<CommandBlock command="echo hello" />);

    const button = screen.getByRole("button", { name: "Copy" });
    fireEvent.click(button);

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("echo hello"));
    await waitFor(() => expect(screen.getByText("Copied")).toBeTruthy());
  });

  it("reverts to the idle Copy label after a short delay", async () => {
    render(<CommandBlock command="echo hi" />);

    fireEvent.click(screen.getByRole("button", { name: "Copy" }));
    await waitFor(() => expect(screen.getByText("Copied")).toBeTruthy());

    vi.advanceTimersByTime(2000);
    await waitFor(() => expect(screen.getByText("Copy")).toBeTruthy());
  });

  it("shows a failure state if clipboard.writeText rejects", async () => {
    writeText.mockRejectedValueOnce(new Error("nope"));
    // also block the legacy fallback so the failure path actually triggers
    const docAny = document as unknown as { execCommand?: () => boolean };
    const previous = docAny.execCommand;
    docAny.execCommand = () => false;

    render(<CommandBlock command="echo fail" />);
    fireEvent.click(screen.getByRole("button", { name: "Copy" }));

    await waitFor(() => expect(screen.getByText("Copy failed")).toBeTruthy());

    if (previous) docAny.execCommand = previous;
    else delete docAny.execCommand;
  });

  it("uses the localized Chinese label when locale is zh-CN", async () => {
    setLocale("zh-CN");
    render(<CommandBlock command="echo zh" />);

    const button = screen.getByRole("button", { name: "复制" });
    fireEvent.click(button);
    await waitFor(() => expect(screen.getByText("已复制")).toBeTruthy());
  });
});
