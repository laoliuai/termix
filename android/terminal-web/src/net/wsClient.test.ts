import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockWebSocket, mockFactory } from "@/test-utils/mockWebSocket";
import { openWSClient } from "./wsClient";

describe("openWSClient", () => {
  beforeEach(() => { MockWebSocket.instances = []; });
  afterEach(() => { vi.restoreAllMocks(); });

  it("creates a WebSocket via the injected factory and exposes isOpen", () => {
    const { factory } = mockFactory();
    const client = openWSClient("wss://relay.example/ws", { onOpen: () => {}, onText: () => {}, onBinary: () => {}, onClose: () => {}, onError: () => {} }, factory);
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0].url).toBe("wss://relay.example/ws");
    expect(client.isOpen).toBe(false);
  });

  it("emits onOpen and flips isOpen when the socket opens", () => {
    const { factory } = mockFactory();
    const onOpen = vi.fn();
    const client = openWSClient("wss://x", { onOpen, onText: () => {}, onBinary: () => {}, onClose: () => {}, onError: () => {} }, factory);
    MockWebSocket.instances[0].triggerOpen();
    expect(onOpen).toHaveBeenCalledOnce();
    expect(client.isOpen).toBe(true);
  });

  it("dispatches text and binary frames to onText / onBinary", () => {
    const { factory } = mockFactory();
    const onText = vi.fn();
    const onBinary = vi.fn();
    openWSClient("wss://x", { onOpen: () => {}, onText, onBinary, onClose: () => {}, onError: () => {} }, factory);
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    ws.triggerText("{\"type\":\"hello.ok\"}");
    const buf = new ArrayBuffer(4);
    ws.triggerBinary(buf);
    expect(onText).toHaveBeenCalledWith("{\"type\":\"hello.ok\"}");
    expect(onBinary).toHaveBeenCalledOnce();
    expect(onBinary.mock.calls[0][0]).toBeInstanceOf(ArrayBuffer);
  });

  it("sendText delegates to socket.send only when open", () => {
    const { factory } = mockFactory();
    const client = openWSClient("wss://x", { onOpen: () => {}, onText: () => {}, onBinary: () => {}, onClose: () => {}, onError: () => {} }, factory);
    const ws = MockWebSocket.instances[0];
    client.sendText("before-open"); // dropped
    expect(ws.sentText).toEqual([]);
    ws.triggerOpen();
    client.sendText("after-open");
    expect(ws.sentText).toEqual(["after-open"]);
  });

  it("sendBinary delegates a Uint8Array to socket.send as ArrayBuffer view", () => {
    const { factory } = mockFactory();
    const client = openWSClient("wss://x", { onOpen: () => {}, onText: () => {}, onBinary: () => {}, onClose: () => {}, onError: () => {} }, factory);
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    client.sendBinary(new Uint8Array([1, 2, 3, 4]));
    expect(ws.sentBinary).toHaveLength(1);
    expect(new Uint8Array(ws.sentBinary[0])).toEqual(new Uint8Array([1, 2, 3, 4]));
  });

  it("close() calls socket.close and emits onClose with the code", () => {
    const { factory } = mockFactory();
    const onClose = vi.fn();
    const client = openWSClient("wss://x", { onOpen: () => {}, onText: () => {}, onBinary: () => {}, onClose, onError: () => {} }, factory);
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    client.close();
    expect(ws.closed).toBe(true);
    expect(onClose).toHaveBeenCalledOnce();
    expect(onClose.mock.calls[0][0].code).toBe(1000);
  });

  it("triggerClose from server side flips isOpen to false and fires onClose once", () => {
    const { factory } = mockFactory();
    const onClose = vi.fn();
    const client = openWSClient("wss://x", { onOpen: () => {}, onText: () => {}, onBinary: () => {}, onClose, onError: () => {} }, factory);
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    expect(client.isOpen).toBe(true);
    ws.triggerClose(1006);
    expect(client.isOpen).toBe(false);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("onError is invoked on the underlying error event", () => {
    const { factory } = mockFactory();
    const onError = vi.fn();
    openWSClient("wss://x", { onOpen: () => {}, onText: () => {}, onBinary: () => {}, onClose: () => {}, onError }, factory);
    const ws = MockWebSocket.instances[0];
    ws.triggerError();
    expect(onError).toHaveBeenCalledOnce();
  });
});
