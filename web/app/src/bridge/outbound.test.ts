import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createOutboundEmitter } from "./outbound";

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
    emitter.onConnectionState({ phase: "connected" });
    emitter.onControlState("granted", "ok");
    emitter.onError("auth", "bad token");

    expect(onConnectionState).toHaveBeenCalledWith({ phase: "connected" });
    expect(onControlState).toHaveBeenCalledWith("granted", "ok");
    expect(onError).toHaveBeenCalledWith("auth", "bad token");
  });

  it("forwards reconnect-aware connection states verbatim", () => {
    const onConnectionState = vi.fn();
    (globalThis.window as Window).TermixBridge = { onConnectionState };
    const emitter = createOutboundEmitter();
    emitter.onConnectionState({ phase: "reconnecting", attempt: 2, lastError: "ws close" });
    emitter.onConnectionState({ phase: "gave-up", attemptCount: 7, durationMs: 300_000, lastError: "ECONNREFUSED" });
    expect(onConnectionState).toHaveBeenNthCalledWith(1, { phase: "reconnecting", attempt: 2, lastError: "ws close" });
    expect(onConnectionState).toHaveBeenNthCalledWith(2, { phase: "gave-up", attemptCount: 7, durationMs: 300_000, lastError: "ECONNREFUSED" });
  });

  it("falls back to console.info when window.TermixBridge is missing", () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    const emitter = createOutboundEmitter();
    emitter.onConnectionState({ phase: "connecting" });
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
    emitter.onConnectionState({ phase: "connecting" });
    emitter.onControlState("none");
    expect(onConnectionState).toHaveBeenCalledOnce();
    expect(info).toHaveBeenCalledOnce(); // for onControlState
  });
});
