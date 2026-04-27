export interface WSClient {
  readonly isOpen: boolean;
  sendText(text: string): void;
  sendBinary(data: Uint8Array): void;
  close(code?: number): void;
}

export interface WSClientHandlers {
  onOpen: () => void;
  onText: (msg: string) => void;
  onBinary: (data: ArrayBuffer) => void;
  onClose: (event: { code: number; reason: string }) => void;
  onError: (err: Event) => void;
}

export type WebSocketLike = Pick<WebSocket, "addEventListener" | "removeEventListener" | "send" | "close"> & {
  binaryType: BinaryType;
  readyState: number;
};

export type WebSocketFactory = (url: string) => WebSocketLike;

const defaultFactory: WebSocketFactory = (url) => {
  const ws = new WebSocket(url);
  ws.binaryType = "arraybuffer";
  return ws;
};

export function openWSClient(url: string, handlers: WSClientHandlers, factory: WebSocketFactory = defaultFactory): WSClient {
  const ws = factory(url);
  ws.binaryType = "arraybuffer";

  let open = false;

  ws.addEventListener("open", () => {
    open = true;
    handlers.onOpen();
  });

  ws.addEventListener("message", (ev) => {
    const data = (ev as MessageEvent).data;
    if (typeof data === "string") {
      handlers.onText(data);
    } else if (data instanceof ArrayBuffer) {
      handlers.onBinary(data);
    }
  });

  ws.addEventListener("close", (ev) => {
    open = false;
    const ce = ev as CloseEvent;
    handlers.onClose({ code: ce.code, reason: ce.reason });
  });

  ws.addEventListener("error", (ev) => {
    handlers.onError(ev);
  });

  return {
    get isOpen() { return open; },
    sendText(text) { if (open) ws.send(text); },
    sendBinary(data) { if (open) ws.send(data); },
    close(code = 1000) { if (!open) return; ws.close(code); },
  };
}
