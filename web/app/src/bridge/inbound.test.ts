import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockWebSocket, mockFactory } from "@/test-utils/mockWebSocket";
import { installInboundBridge } from "./inbound";
import { encodeFrame } from "@/protocol/frame";
import { decodeEnvelope } from "@/protocol/envelope";
import type { TerminalUI } from "@/ui/terminal";
import type { ConnectionState, ControlState } from "@/protocol/types";
import { accessToken, accessTokenExpiresAt, clearAuth } from "@/auth/store";
import { __resetInflight } from "@/auth/refresh";
import * as refreshModule from "@/auth/refresh";

// Yield to the supervisor's async loop until the next WS instance shows up
// (or until `attempts` ticks elapse). Each iteration drains one microtask
// round so refreshToken + connect have a chance to run.
async function flushUntilWS(): Promise<MockWebSocket> {
  for (let i = 0; i < 20; i++) {
    if (MockWebSocket.instances.length > 0) return MockWebSocket.instances[MockWebSocket.instances.length - 1];
    await Promise.resolve();
  }
  throw new Error("WS never created");
}

// Drain any pending microtasks (used after triggerOpen/Close to let the
// supervisor advance through state transitions).
async function flush(): Promise<void> {
  for (let i = 0; i < 5; i++) await Promise.resolve();
}

interface StubUI extends TerminalUI {
  written: Uint8Array[];
  inputHandlers: ((text: string) => void)[];
  resetCalls: number;
  stubCols: number;
  stubRows: number;
}

function makeStubUI(initial: { cols: number; rows: number } = { cols: 100, rows: 30 }): StubUI {
  const ui: StubUI = {
    written: [],
    inputHandlers: [],
    resetCalls: 0,
    stubCols: initial.cols,
    stubRows: initial.rows,
    write(bytes) { ui.written.push(new Uint8Array(bytes)); },
    reset() { ui.resetCalls += 1; ui.written = []; },
    cols() { return ui.stubCols; },
    rows() { return ui.stubRows; },
    onInput(handler) { ui.inputHandlers.push(handler); },
    fit() {},
    setGrid(_cols: number, _rows: number) {},
    dispose() {},
  };
  return ui;
}

const w = window as unknown as {
  setSession?: (sId: string, rUrl: string, tok: string, dId: string) => void;
  sendText?: (t: string) => void;
  sendSpecialKey?: (k: string) => void;
  requestControl?: () => void;
  releaseControl?: () => void;
  requestResize?: (cols: number, rows: number) => void;
  retryRelay?: () => void;
  TermixBridge?: { onConnectionState?: (s: ConnectionState) => void; onControlState?: (s: ControlState) => void; onError?: (c: string, m: string) => void };
};

describe("installInboundBridge", () => {
  let ui: ReturnType<typeof makeStubUI>;

  beforeEach(() => {
    MockWebSocket.instances = [];
    delete w.setSession; delete w.sendText; delete w.sendSpecialKey;
    delete w.requestControl; delete w.releaseControl; delete w.requestResize;
    delete w.retryRelay;
    delete w.TermixBridge;
    clearAuth();
    __resetInflight();
    // Seed a non-null cached access-token so freshAccessToken returns it
    // synchronously when the supervisor reconnects (instead of trying to
    // hit /api/v1/auth/refresh through happy-dom's fetch).
    accessToken.value = "tok";
    accessTokenExpiresAt.value = Date.now() + 600_000;
    ui = makeStubUI();
  });
  afterEach(() => { vi.useRealTimers(); });

  it("installs all six window functions", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    expect(typeof w.setSession).toBe("function");
    expect(typeof w.sendText).toBe("function");
    expect(typeof w.sendSpecialKey).toBe("function");
    expect(typeof w.requestControl).toBe("function");
    expect(typeof w.releaseControl).toBe("function");
    expect(typeof w.requestResize).toBe("function");
  });

  it("setSession opens a WS to the relay URL with token + device_id query string", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    expect(MockWebSocket.instances).toHaveLength(1);
    const url = new URL(ws.url);
    expect(url.searchParams.get("access_token")).toBe("tok");
    expect(url.searchParams.get("device_id")).toBe("dev-1");
    expect(url.searchParams.get("session_id")).toBe("sess-1");
  });

  it("on open, sends hello.android then client.resize then session.watch and emits connecting + connected", async () => {
    const onConnectionState = vi.fn();
    w.TermixBridge = { onConnectionState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    expect(ws.sentText).toHaveLength(3);
    // Order matters: resize must precede watch so the daemon sizes its
    // tmux pane before running capture-pane for the snapshot.
    expect(decodeEnvelope(ws.sentText[0])).toEqual(expect.objectContaining({ type: "hello.android", payload: { device_id: "dev-1" } }));
    expect(decodeEnvelope(ws.sentText[1])).toEqual(expect.objectContaining({ type: "client.resize", payload: { session_id: "sess-1", cols: 100, rows: 30 } }));
    expect(decodeEnvelope(ws.sentText[2])).toEqual(expect.objectContaining({ type: "session.watch", payload: { session_id: "sess-1" } }));
    expect(onConnectionState).toHaveBeenCalledWith({ phase: "connecting" });
    expect(onConnectionState).toHaveBeenCalledWith({ phase: "connected" });
  });

  it("incoming output frames are forwarded to ui.write", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    const frame = encodeFrame(1, { session_id: "sess-1", seq: 0, stream: "stdout" }, new TextEncoder().encode("hi"));
    ws.triggerBinary(frame.buffer.slice(frame.byteOffset, frame.byteOffset + frame.byteLength) as ArrayBuffer);
    expect(ui.written).toHaveLength(1);
    expect(new TextDecoder().decode(ui.written[0])).toBe("hi");
  });

  it("requestControl + control.granted -> sendText is gated until granted, then frames flow", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    ws.sentText = [];
    ws.sentBinary = [];

    // sendText before granted is dropped
    w.sendText!("nope");
    expect(ws.sentBinary).toHaveLength(0);

    w.requestControl!();
    expect(decodeEnvelope(ws.sentText[ws.sentText.length - 1]).type).toBe("control.acquire");

    ws.triggerText(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "sess-1", lease_version: 1, expires_at: "2099-01-01T00:00:00Z", controller_device_id: "dev-1" },
    }));

    w.sendText!("ls\n");
    expect(ws.sentBinary).toHaveLength(1);
    // Verify it's a TMX1 input frame.
    const buf = new Uint8Array(ws.sentBinary[0]);
    expect(new TextDecoder().decode(buf.subarray(0, 4))).toBe("TMX1");
    expect(buf[5]).toBe(2); // type=2 input
  });

  it("sendSpecialKey('Enter') after granted produces a 0x0d binary frame", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    w.requestControl!();
    ws.triggerText(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "sess-1", lease_version: 1, expires_at: "2099-01-01T00:00:00Z", controller_device_id: "dev-1" },
    }));
    ws.sentBinary = [];
    w.sendSpecialKey!("Enter");
    expect(ws.sentBinary).toHaveLength(1);
    const buf = new Uint8Array(ws.sentBinary[0]);
    // payload begins after 10-byte prefix + header_json_len.
    const headerLen = new DataView(buf.buffer, buf.byteOffset, buf.byteLength).getUint32(6, false);
    const payload = buf.subarray(10 + headerLen);
    expect(payload[0]).toBe(0x0d);
  });

  it("re-calling setSession closes the old socket and opens a new one", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const first = await flushUntilWS();
    first.triggerOpen();
    await flush();
    w.setSession!("sess-2", "wss://r/", "tok2", "dev-1");
    expect(first.closed).toBe(true);
    await flushUntilWS(); // wait for the new WS
    expect(MockWebSocket.instances).toHaveLength(2);
  });

  it("setSession with empty sessionId acts as graceful close", async () => {
    const onConnectionState = vi.fn();
    w.TermixBridge = { onConnectionState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    onConnectionState.mockClear();
    w.setSession!("", "", "", "");
    expect(ws.closed).toBe(true);
    expect(MockWebSocket.instances).toHaveLength(1); // no new socket opened
  });

  it("ui.onInput pipes typed text into sendText and through the input gate", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();

    // Without grant: typed input is dropped.
    ws.sentBinary = [];
    ui.inputHandlers[0]("typed");
    expect(ws.sentBinary).toHaveLength(0);

    // After grant: typed input becomes a binary input frame.
    w.requestControl!();
    ws.triggerText(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "sess-1", lease_version: 1, expires_at: "2099-01-01T00:00:00Z", controller_device_id: "dev-1" },
    }));
    ws.sentBinary = [];
    ui.inputHandlers[0]("typed");
    expect(ws.sentBinary).toHaveLength(1);
  });

  it("on connect, emits initial client.resize with the grid xterm reports via TerminalUI", async () => {
    const customUI = makeStubUI({ cols: 117, rows: 38 });
    const { factory } = mockFactory();
    installInboundBridge({ ui: customUI, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    const resizeEnv = decodeEnvelope(ws.sentText[1]);
    expect(resizeEnv.type).toBe("client.resize");
    const p = resizeEnv.payload as { session_id: string; cols: number; rows: number };
    expect(p.session_id).toBe("sess-1");
    expect(p.cols).toBe(117);
    expect(p.rows).toBe(38);
  });

  it("requestResize while session is open sends a client.resize envelope", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    ws.sentText = []; // clear hello + watch + initial resize
    w.requestResize!(120, 40);
    expect(ws.sentText).toHaveLength(1);
    const env = decodeEnvelope(ws.sentText[0]);
    expect(env.type).toBe("client.resize");
    const p = env.payload as { session_id: string; cols: number; rows: number };
    expect(p.session_id).toBe("sess-1");
    expect(p.cols).toBe(120);
    expect(p.rows).toBe(40);
  });

  it("on ws close after open, supervisor surfaces reconnecting", async () => {
    const onConnectionState = vi.fn();
    const onControlState = vi.fn();
    w.TermixBridge = { onConnectionState, onControlState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    // Acquire control so the renew timer is active.
    w.requestControl!();
    ws.triggerText(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "sess-1", lease_version: 1, expires_at: "2099-01-01T00:00:00Z", controller_device_id: "dev-1" },
    }));
    onConnectionState.mockClear();
    onControlState.mockClear();
    // Drop the WS — supervisor should transition to reconnecting.
    ws.triggerClose(1006);
    await flush();
    expect(onConnectionState).toHaveBeenCalledWith(expect.objectContaining({ phase: "reconnecting", attempt: 1 }));
    // Control was reset to none.
    expect(onControlState).toHaveBeenCalledWith("none", undefined);
    // sendText after close+before-next-connect is a no-op since active is null.
    ws.sentBinary = [];
    w.sendText!("post-close");
    expect(ws.sentBinary).toHaveLength(0);
  });

  it("session.snapshot.ready triggers ui.reset before snapshot frames are written", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();

    // Simulate a stale screen from a previous watch — the bridge should
    // forward the bytes to xterm.
    const stale = encodeFrame(1, { session_id: "sess-1", seq: 0, stream: "stdout" }, new TextEncoder().encode("STALE"));
    ws.triggerBinary(stale.buffer.slice(stale.byteOffset, stale.byteOffset + stale.byteLength) as ArrayBuffer);
    expect(ui.written.length).toBeGreaterThan(0);
    expect(ui.resetCalls).toBe(0);

    // Daemon sends session.snapshot.ready before the snapshot frame; bridge
    // must reset xterm so the upcoming snapshot draws onto a blank screen.
    ws.triggerText(JSON.stringify({
      type: "session.snapshot.ready", request_id: null,
      payload: { session_id: "sess-1" },
    }));
    expect(ui.resetCalls).toBe(1);
    // makeStubUI clears `written` on reset() so the test asserts the
    // post-reset stream — the snapshot frame is the first thing drawn.
    expect(ui.written).toHaveLength(0);

    const snap = encodeFrame(3, { session_id: "sess-1", seq: 1, is_last: true }, new TextEncoder().encode("FRESH"));
    ws.triggerBinary(snap.buffer.slice(snap.byteOffset, snap.byteOffset + snap.byteLength) as ArrayBuffer);
    expect(ui.written).toHaveLength(1);
    expect(new TextDecoder().decode(ui.written[0])).toBe("FRESH");
  });

  it("retryRelay window global is exposed after setSession", async () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    await flushUntilWS();
    expect(typeof w.retryRelay).toBe("function");
  });

  it("401 from /auth/refresh sets window.location.href to /login and aborts the connect", async () => {
    vi.useFakeTimers();

    // Expire the cached token so the supervisor must call freshAccessTokenWithStatus.
    accessToken.value = null;
    accessTokenExpiresAt.value = Date.now() + 600_000; // still "valid" for freshAccessToken cache check
    // But we want the supervisor to call freshAccessTokenWithStatus after firstToken
    // is consumed. We mock the function directly, so cache state doesn't matter.

    // Stub freshAccessTokenWithStatus to return a 401.
    const spy = vi.spyOn(refreshModule, "freshAccessTokenWithStatus").mockResolvedValue({
      accessToken: null,
      status: 401,
    });

    // Capture any write to window.location.href.
    let capturedHref = "";
    const mockLocation = {
      pathname: "/terminal/sess-1",
      search: "",
      host: "localhost",
      protocol: "http:",
      reload: vi.fn(),
    };
    Object.defineProperty(mockLocation, "href", {
      get() { return capturedHref; },
      set(v: string) { capturedHref = v; },
      configurable: true,
    });
    Object.defineProperty(window, "location", {
      value: mockLocation,
      writable: true,
      configurable: true,
    });

    const onConnectionState = vi.fn();
    w.TermixBridge = { onConnectionState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });

    // setSession seeds firstToken = "tok", consumed on the first WS attempt.
    // Let the first attempt succeed; then drop the WS to force a reconnect
    // that calls freshAccessTokenWithStatus (the real refresh path).
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const ws = await flushUntilWS();
    ws.triggerOpen();
    await flush();
    // Drop the WS — supervisor transitions to reconnecting.
    ws.triggerClose(1006);
    await flush();
    // Advance fake timers past the first backoff delay (1s with 0.5 rng factor).
    await vi.advanceTimersByTimeAsync(2000);
    // Drain microtasks so the supervisor calls refreshToken.
    for (let i = 0; i < 20; i++) await Promise.resolve();

    expect(spy).toHaveBeenCalled();
    expect(capturedHref).toMatch(/\/login\?next=/);

    spy.mockRestore();
  });
});
