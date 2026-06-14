import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/preact";

import { TerminalPage, initialToolbarExpanded } from "./terminal";
import { accessToken, accessTokenExpiresAt, userInfo, clearAuth } from "../auth/store";
import type { SpecialKey } from "../protocol/types";
import { setLocale } from "../i18n/store";

const setSessionSpy = vi.fn<[string, string, string, string], void>();
const sendTextSpy = vi.fn<[string], void>();
const sendSpecialKeySpy = vi.fn<[SpecialKey], void>();
const requestControlSpy = vi.fn<[], void>();
const releaseControlSpy = vi.fn<[], void>();

beforeEach(() => {
  cleanup();
  clearAuth();
  setLocale("en");
  localStorage.removeItem("termix_toolbar_expanded");
  // Force a deterministic platform default: matches=false => not desktop =>
  // toolbar expanded by default, so grant-state tests see the toolbar mount.
  Object.defineProperty(window, "matchMedia", {
    configurable: true, writable: true,
    value: (q: string) => ({
      matches: false, media: q, onchange: null,
      addListener() {}, removeListener() {},
      addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; },
    }),
  });
  accessToken.value = "tk";
  accessTokenExpiresAt.value = Date.now() + 600_000;
  userInfo.value = {
    user: { id: "u", email: "a@b", display_name: "A", role: "user" },
    device: { id: "dev-id", device_type: "web", platform: "web", label: "ua" },
  };
  setSessionSpy.mockReset();
  sendTextSpy.mockReset();
  sendSpecialKeySpy.mockReset();
  requestControlSpy.mockReset();
  releaseControlSpy.mockReset();
  window.setSession = setSessionSpy;
  window.sendText = sendTextSpy;
  window.sendSpecialKey = sendSpecialKeySpy;
  window.requestControl = requestControlSpy;
  window.releaseControl = releaseControlSpy;
  (import.meta as any).env = { ...((import.meta as any).env ?? {}), VITE_RELAY_WS_URL: "wss://relay.example.com/ws" };
});

describe("TerminalPage", () => {
  it("calls setSession on mount with sessionId, relay URL, token, deviceId", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    expect(setSessionSpy).toHaveBeenCalledWith("s1", "wss://relay.example.com/ws", "tk", "dev-id");
  });

  it("read-only state: toolbar is not in the DOM (and no composer)", async () => {
    const { container } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    expect(container.querySelector(".composer")).toBeNull();
    expect(container.querySelector(".toolbar")).toBeNull();
  });

  it("granted (expanded by default in test env): toolbar appears, no composer", async () => {
    const { container } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    window.TermixBridge?.onControlState?.("granted");
    await waitFor(() => expect(container.querySelector(".toolbar")).toBeTruthy());
    expect(container.querySelector(".composer")).toBeNull();
  });

  it("granted: toggle collapses then re-expands the toolbar, persisting each choice", async () => {
    const { container } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    window.TermixBridge?.onControlState?.("granted");
    await waitFor(() => expect(container.querySelector(".toolbar")).toBeTruthy());

    // The toggle is focus-preserving (mousedown cancelled) so a mobile tap
    // doesn't dismiss the soft keyboard.
    const hideBtn = screen.getByRole("button", { name: /hide keys/i });
    const md = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
    hideBtn.dispatchEvent(md);
    expect(md.defaultPrevented).toBe(true);

    fireEvent.click(hideBtn);
    await waitFor(() => expect(container.querySelector(".toolbar")).toBeNull());
    expect(localStorage.getItem("termix_toolbar_expanded")).toBe("0");

    // Re-expand: the toggle (now "show keys") brings the toolbar back.
    fireEvent.click(screen.getByRole("button", { name: /show keys/i }));
    await waitFor(() => expect(container.querySelector(".toolbar")).toBeTruthy());
    expect(localStorage.getItem("termix_toolbar_expanded")).toBe("1");
  });

  it("desktop default (matchMedia matches): toolbar starts collapsed, toggle expands it", async () => {
    // Override the touch-default stub from beforeEach: desktop => collapsed.
    Object.defineProperty(window, "matchMedia", {
      configurable: true, writable: true,
      value: (q: string) => ({
        matches: true, media: q, onchange: null,
        addListener() {}, removeListener() {},
        addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; },
      }),
    });
    const { container } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    window.TermixBridge?.onControlState?.("granted");
    // Collapsed by default on desktop: the toggle shows, the toolbar does not.
    await waitFor(() => expect(screen.getByRole("button", { name: /show keys/i })).toBeTruthy());
    expect(container.querySelector(".toolbar")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /show keys/i }));
    await waitFor(() => expect(container.querySelector(".toolbar")).toBeTruthy());
  });

  it("toolbar special-key click sends sendSpecialKey", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    // Grant control first — toolbar is not mounted until then
    window.TermixBridge?.onControlState?.("granted");
    await waitFor(() => {
      const btn = screen.getByText("^J") as HTMLButtonElement;
      expect(btn.disabled).toBe(false);
    });
    fireEvent.click(screen.getByText("^J"));
    expect(sendSpecialKeySpy).toHaveBeenCalledWith("C-j");
  });

  it("Request Control calls requestControl when control state is none", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => screen.getByRole("button", { name: /request control/i }));
    fireEvent.click(screen.getByRole("button", { name: /request control/i }));
    expect(requestControlSpy).toHaveBeenCalled();
  });

  it("Release Control calls releaseControl when control state is granted", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => screen.getByRole("button", { name: /request control/i }));
    window.TermixBridge?.onControlState?.("granted");
    await waitFor(() => screen.getByRole("button", { name: /release/i }));
    fireEvent.click(screen.getByRole("button", { name: /release/i }));
    expect(releaseControlSpy).toHaveBeenCalled();
  });

  it("calls setSession('','','','') on unmount", async () => {
    const { unmount } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    setSessionSpy.mockClear();
    unmount();
    expect(setSessionSpy).toHaveBeenCalledWith("", "", "", "");
  });

  it("reconnects the terminal session when the page returns visible after a disconnect", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalledWith("s1", "wss://relay.example.com/ws", "tk", "dev-id"));

    window.TermixBridge?.onConnectionState?.({ phase: "disconnected" });
    setSessionSpy.mockClear();
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));

    await waitFor(() => expect(setSessionSpy).toHaveBeenCalledWith("s1", "wss://relay.example.com/ws", "tk", "dev-id"));
  });

  it("renders Chinese terminal control labels", async () => {
    setLocale("zh-CN");
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);

    await waitFor(() => expect(screen.getByText(/只读/)).toBeTruthy());
    expect(screen.getByRole("button", { name: "请求控制" })).toBeTruthy();
  });
});

describe("initialToolbarExpanded", () => {
  const realMM = window.matchMedia;
  const setMM = (matches: boolean) => {
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      writable: true,
      value: (q: string) => ({
        matches, media: q, onchange: null,
        addListener() {}, removeListener() {},
        addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; },
      }),
    });
  };
  beforeEach(() => localStorage.removeItem("termix_toolbar_expanded"));
  afterEach(() => {
    Object.defineProperty(window, "matchMedia", { configurable: true, writable: true, value: realMM });
    localStorage.removeItem("termix_toolbar_expanded");
  });

  it("desktop (hover + fine pointer) defaults collapsed", () => {
    setMM(true);
    expect(initialToolbarExpanded()).toBe(false);
  });
  it("touch (no hover/fine pointer) defaults expanded", () => {
    setMM(false);
    expect(initialToolbarExpanded()).toBe(true);
  });
  it("stored '1' overrides the desktop default", () => {
    setMM(true);
    localStorage.setItem("termix_toolbar_expanded", "1");
    expect(initialToolbarExpanded()).toBe(true);
  });
  it("stored '0' overrides the touch default", () => {
    setMM(false);
    localStorage.setItem("termix_toolbar_expanded", "0");
    expect(initialToolbarExpanded()).toBe(false);
  });
});
