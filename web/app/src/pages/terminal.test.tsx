import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/preact";

import { TerminalPage } from "./terminal";
import { accessToken, accessTokenExpiresAt, userInfo, clearAuth } from "../auth/store";

declare global {
  interface Window {
    setSession?: (id: string, url: string, tok: string, dev: string) => void;
    sendText?: (s: string) => void;
    sendSpecialKey?: (k: string) => void;
    requestControl?: () => void;
    releaseControl?: () => void;
    TermixBridge?: any;
  }
}

const setSessionSpy = vi.fn();
const sendTextSpy = vi.fn();
const sendSpecialKeySpy = vi.fn();
const requestControlSpy = vi.fn();
const releaseControlSpy = vi.fn();

beforeEach(() => {
  cleanup();
  clearAuth();
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
  (window as any).setSession = setSessionSpy;
  (window as any).sendText = sendTextSpy;
  (window as any).sendSpecialKey = sendSpecialKeySpy;
  (window as any).requestControl = requestControlSpy;
  (window as any).releaseControl = releaseControlSpy;
  (import.meta as any).env = { ...((import.meta as any).env ?? {}), VITE_RELAY_WS_URL: "wss://relay.example.com/ws" };
});

describe("TerminalPage", () => {
  it("calls setSession on mount with sessionId, relay URL, token, deviceId", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
    expect(setSessionSpy).toHaveBeenCalledWith("s1", "wss://relay.example.com/ws", "tk", "dev-id");
  });

  it("toolbar digit click sends sendText with the digit (when control granted)", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => screen.getByText("3"));
    // Simulate control granted via the bridge
    (window as any).TermixBridge?.onControlState?.("granted", null);
    await waitFor(() => {
      const btn = screen.getByText("3") as HTMLButtonElement;
      expect(btn.disabled).toBe(false);
    });
    fireEvent.click(screen.getByText("3"));
    expect(sendTextSpy).toHaveBeenCalledWith("3");
  });

  it("toolbar special-key click sends sendSpecialKey", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => screen.getByText("^J"));
    (window as any).TermixBridge?.onControlState?.("granted", null);
    await waitFor(() => {
      const btn = screen.getByText("^J") as HTMLButtonElement;
      expect(btn.disabled).toBe(false);
    });
    fireEvent.click(screen.getByText("^J"));
    expect(sendSpecialKeySpy).toHaveBeenCalledWith("C-j");
  });

  it("composer Send sends text and clears the input", async () => {
    render(<TerminalPage sessionId="s1" onBack={() => {}} />);
    await waitFor(() => screen.getByPlaceholderText(/type/i));
    (window as any).TermixBridge?.onControlState?.("granted", null);

    const ta = await waitFor(() => screen.getByPlaceholderText(/type/i)) as HTMLTextAreaElement;
    fireEvent.input(ta, { target: { value: "hello\nworld" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(sendTextSpy).toHaveBeenCalledWith("hello\nworld");
    await waitFor(() => expect((ta as HTMLTextAreaElement).value).toBe(""));
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
    (window as any).TermixBridge?.onControlState?.("granted", null);
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
});
