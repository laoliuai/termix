type Listener<T extends Event = Event> = (ev: T) => void;

export class MockWebSocket {
  static OPEN = 1 as const;
  static CLOSED = 3 as const;
  static instances: MockWebSocket[] = [];

  url: string;
  readyState: number = 0; // CONNECTING
  binaryType: BinaryType = "arraybuffer";

  sentText: string[] = [];
  sentBinary: ArrayBuffer[] = [];
  closed = false;
  closeCode: number | undefined;

  private openListeners: Listener[] = [];
  private messageListeners: Listener<MessageEvent>[] = [];
  private closeListeners: Listener<CloseEvent>[] = [];
  private errorListeners: Listener[] = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  addEventListener(name: string, fn: Listener<Event>): void {
    if (name === "open") this.openListeners.push(fn);
    else if (name === "message") this.messageListeners.push(fn as Listener<MessageEvent>);
    else if (name === "close") this.closeListeners.push(fn as Listener<CloseEvent>);
    else if (name === "error") this.errorListeners.push(fn);
  }

  removeEventListener(): void { /* not needed for these tests */ }

  send(data: string | ArrayBuffer | ArrayBufferView | Blob): void {
    if (typeof data === "string") {
      this.sentText.push(data);
    } else if (data instanceof ArrayBuffer) {
      this.sentBinary.push(data);
    } else if (ArrayBuffer.isView(data)) {
      const view = data as ArrayBufferView;
      const buf = new ArrayBuffer(view.byteLength);
      new Uint8Array(buf).set(new Uint8Array(view.buffer, view.byteOffset, view.byteLength));
      this.sentBinary.push(buf);
    } else {
      throw new Error("MockWebSocket: Blob payloads not supported");
    }
  }

  close(code: number = 1000): void {
    if (this.closed) return;
    this.closed = true;
    this.closeCode = code;
    this.readyState = MockWebSocket.CLOSED;
    const ev = { code, reason: "", wasClean: true } as CloseEvent;
    for (const fn of this.closeListeners) fn(ev);
  }

  // Test helpers — drive transitions explicitly.
  triggerOpen(): void {
    this.readyState = MockWebSocket.OPEN;
    for (const fn of this.openListeners) fn(new Event("open"));
  }

  triggerText(text: string): void {
    const ev = { data: text } as MessageEvent;
    for (const fn of this.messageListeners) fn(ev);
  }

  triggerBinary(data: ArrayBuffer): void {
    const ev = { data } as MessageEvent;
    for (const fn of this.messageListeners) fn(ev);
  }

  triggerClose(code: number = 1006): void {
    if (this.closed) return;
    this.closed = true;
    this.closeCode = code;
    this.readyState = MockWebSocket.CLOSED;
    const ev = { code, reason: "", wasClean: false } as CloseEvent;
    for (const fn of this.closeListeners) fn(ev);
  }

  triggerError(): void {
    for (const fn of this.errorListeners) fn(new Event("error"));
  }
}

export function mockFactory(): { factory: (url: string) => MockWebSocket; reset: () => void } {
  MockWebSocket.instances = [];
  return {
    factory: (url: string) => new MockWebSocket(url),
    reset: () => { MockWebSocket.instances = []; },
  };
}
