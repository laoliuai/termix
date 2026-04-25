import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createOutboundEmitter } from "./outbound";

declare global {
  interface Window { TermixBridge?: {
    onConnectionState?(s: string, d?: string): void;
    onControlState?(s: string, d?: string): void;
    onError?(c: string, m: string): void;
  } }
}

describe("createOutboundEmitter", () => {
  beforeEach(() => {
    delete (globalThis as { TermixBridge?: unknown }).TermixBridge;
    delete (globalThis.window as { TermixBridge?: unknown }).TermixBridge;
  });
  afterEach(() => { vi.restoreAllMocks(); });

  it("calls window.TermixBridge methods when present", () => {
    const onConnectionState = vi.fn();
    const onControlState = vi.fn();
    const onError = vi.fn();
    (globalThis.window as Window).TermixBridge = { onConnectionState, onControlState, onError };

    const emitter = createOutboundEmitter();
    emitter.onConnectionState("connected");
    emitter.onControlState("granted", "ok");
    emitter.onError("auth", "bad token");

    expect(onConnectionState).toHaveBeenCalledWith("connected", undefined);
    expect(onControlState).toHaveBeenCalledWith("granted", "ok");
    expect(onError).toHaveBeenCalledWith("auth", "bad token");
  });

  it("falls back to console.info when window.TermixBridge is missing", () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    const emitter = createOutboundEmitter();
    emitter.onConnectionState("connecting");
    emitter.onControlState("none");
    emitter.onError("x", "y");
    expect(info).toHaveBeenCalledTimes(3);
    expect(info.mock.calls[0][0]).toContain("[TermixBridge]");
    expect(info.mock.calls[0][0]).toContain("onConnectionState");
    expect(info.mock.calls[0][0]).toContain("connecting");
  });

  it("falls back to console.info when only some bridge methods are defined", () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    const onConnectionState = vi.fn();
    (globalThis.window as Window).TermixBridge = { onConnectionState };
    const emitter = createOutboundEmitter();
    emitter.onConnectionState("connecting");
    emitter.onControlState("none");
    expect(onConnectionState).toHaveBeenCalledOnce();
    expect(info).toHaveBeenCalledOnce(); // for onControlState
  });
});
