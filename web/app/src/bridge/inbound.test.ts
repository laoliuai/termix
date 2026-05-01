import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockWebSocket, mockFactory } from "@/test-utils/mockWebSocket";
import { installInboundBridge } from "./inbound";
import { encodeFrame } from "@/protocol/frame";
import { decodeEnvelope } from "@/protocol/envelope";
import type { TerminalUI } from "@/ui/terminal";
import type { ConnectionState, ControlState } from "@/protocol/types";

function makeStubUI(): TerminalUI & { written: Uint8Array[]; inputHandlers: ((text: string) => void)[] } {
  const ui: TerminalUI & { written: Uint8Array[]; inputHandlers: ((text: string) => void)[] } = {
    written: [],
    inputHandlers: [],
    write(bytes) { ui.written.push(new Uint8Array(bytes)); },
    onInput(handler) { ui.inputHandlers.push(handler); },
    fit() {},
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
  TermixBridge?: { onConnectionState?: (s: ConnectionState) => void; onControlState?: (s: ControlState) => void; onError?: (c: string, m: string) => void };
};

describe("installInboundBridge", () => {
  let ui: ReturnType<typeof makeStubUI>;

  beforeEach(() => {
    MockWebSocket.instances = [];
    delete w.setSession; delete w.sendText; delete w.sendSpecialKey;
    delete w.requestControl; delete w.releaseControl; delete w.requestResize;
    delete w.TermixBridge;
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

  it("setSession opens a WS to the relay URL with token + device_id query string", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    expect(MockWebSocket.instances).toHaveLength(1);
    const url = new URL(MockWebSocket.instances[0].url);
    expect(url.searchParams.get("access_token")).toBe("tok");
    expect(url.searchParams.get("device_id")).toBe("dev-1");
    expect(url.searchParams.get("session_id")).toBe("sess-1");
  });

  it("on open, sends hello.android then session.watch then client.resize and emits connected", () => {
    const onConnectionState = vi.fn();
    w.TermixBridge = { onConnectionState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    expect(ws.sentText).toHaveLength(3);
    expect(decodeEnvelope(ws.sentText[0])).toEqual(expect.objectContaining({ type: "hello.android", payload: { device_id: "dev-1" } }));
    expect(decodeEnvelope(ws.sentText[1])).toEqual(expect.objectContaining({ type: "session.watch", payload: { session_id: "sess-1" } }));
    expect(decodeEnvelope(ws.sentText[2])).toEqual(expect.objectContaining({ type: "client.resize", payload: { session_id: "sess-1", cols: 80, rows: 24 } }));
    expect(onConnectionState).toHaveBeenCalledWith("connected", undefined);
  });

  it("incoming output frames are forwarded to ui.write", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    const frame = encodeFrame(1, { session_id: "sess-1", seq: 0, stream: "stdout" }, new TextEncoder().encode("hi"));
    ws.triggerBinary(frame.buffer.slice(frame.byteOffset, frame.byteOffset + frame.byteLength) as ArrayBuffer);
    expect(ui.written).toHaveLength(1);
    expect(new TextDecoder().decode(ui.written[0])).toBe("hi");
  });

  it("requestControl + control.granted -> sendText is gated until granted, then frames flow", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
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

  it("sendSpecialKey('Enter') after granted produces a 0x0d binary frame", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
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

  it("re-calling setSession closes the old socket and opens a new one", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const first = MockWebSocket.instances[0];
    first.triggerOpen();
    w.setSession!("sess-2", "wss://r/", "tok2", "dev-1");
    expect(first.closed).toBe(true);
    expect(MockWebSocket.instances).toHaveLength(2);
  });

  it("setSession with empty sessionId acts as graceful close", () => {
    const onConnectionState = vi.fn();
    w.TermixBridge = { onConnectionState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    onConnectionState.mockClear();
    w.setSession!("", "", "", "");
    expect(ws.closed).toBe(true);
    expect(MockWebSocket.instances).toHaveLength(1); // no new socket opened
  });

  it("ui.onInput pipes typed text into sendText and through the input gate", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();

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

  it("on connect, emits initial client.resize with cached grid (default 80x24)", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    const resizeEnv = decodeEnvelope(ws.sentText[2]);
    expect(resizeEnv.type).toBe("client.resize");
    const p = resizeEnv.payload as { session_id: string; cols: number; rows: number };
    expect(p.session_id).toBe("sess-1");
    expect(p.cols).toBe(80);
    expect(p.rows).toBe(24);
  });

  it("requestResize while session is open sends a client.resize envelope", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
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

  it("onError cleans up heartbeat, control, and active session", () => {
    const onConnectionState = vi.fn();
    const onControlState = vi.fn();
    w.TermixBridge = { onConnectionState, onControlState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    // Acquire control to start the renew timer.
    w.requestControl!();
    ws.triggerText(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "sess-1", lease_version: 1, expires_at: "2099-01-01T00:00:00Z", controller_device_id: "dev-1" },
    }));
    // Now fire error WITHOUT a subsequent close (this is what the bug guards against).
    ws.triggerError();

    // The error state was emitted.
    expect(onConnectionState).toHaveBeenCalledWith("error", undefined);
    // Control was reset to none (without sending control.release on the dead socket).
    expect(onControlState).toHaveBeenCalledWith("none", undefined);
    // sendText after error is a no-op since active is null.
    ws.sentBinary = [];
    w.sendText!("post-error");
    expect(ws.sentBinary).toHaveLength(0);
  });
});
