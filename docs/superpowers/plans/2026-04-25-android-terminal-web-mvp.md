# Android `terminal-web` MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `android/terminal-web/` static-asset bundle — a TypeScript + Vite + xterm.js WSS client — so the Compose shell (slice 2) can host it inside an Android WebView.

**Architecture:** Five cohesive areas under `src/`: `protocol/` (pure binary-frame and JSON-envelope codecs), `net/` (WebSocket lifecycle + heartbeat with dependency-injected WebSocket factory for testability), `session/` (control-lease state machine + snapshot/live-output watcher), `bridge/` (only files touching `window.*` globals — inbound `window.setSession` etc., outbound `window.TermixBridge?.method(...)` with console fallback), `ui/` (xterm.js wrapper). Two HTML entries: `index.html` for production (in `dist/`), `dev.html` for browser dev harness (excluded from `dist/`).

**Tech Stack:** TypeScript 5 (strict), Vite 5 (build + dev server), Vitest 1 (test runner) with happy-dom, xterm.js (`@xterm/xterm` + `@xterm/addon-fit`).

**Spec:** `docs/superpowers/specs/2026-04-25-android-terminal-web-mvp-design.md`. All section numbers in this plan refer to that spec unless otherwise noted.

**Working directory for every command unless noted:** `android/terminal-web/`.

---

## Task 1: Bootstrap project skeleton

**Files:**
- Create: `android/terminal-web/package.json`
- Create: `android/terminal-web/tsconfig.json`
- Create: `android/terminal-web/vite.config.ts`
- Create: `android/terminal-web/.gitignore`

- [ ] **Step 1: Create directory and `package.json`**

```bash
mkdir -p android/terminal-web
```

`android/terminal-web/package.json`:

```json
{
  "name": "termix-terminal-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "vitest run",
    "test:watch": "vitest",
    "typecheck": "tsc --noEmit"
  },
  "dependencies": {
    "@xterm/xterm": "^5.5.0",
    "@xterm/addon-fit": "^0.10.0"
  },
  "devDependencies": {
    "@types/node": "^20.11.0",
    "happy-dom": "^13.0.0",
    "typescript": "^5.3.0",
    "vite": "^5.0.0",
    "vitest": "^1.2.0"
  }
}
```

- [ ] **Step 2: Create `tsconfig.json`**

`android/terminal-web/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "esModuleInterop": true,
    "isolatedModules": true,
    "resolveJsonModule": true,
    "skipLibCheck": true,
    "types": ["vitest/globals"],
    "baseUrl": ".",
    "paths": {
      "@/*": ["src/*"]
    }
  },
  "include": ["src", "vite.config.ts"]
}
```

- [ ] **Step 3: Create `vite.config.ts`**

`android/terminal-web/vite.config.ts`:

```typescript
import { defineConfig } from "vite";
import { resolve } from "path";

export default defineConfig(() => ({
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: resolve(__dirname, "index.html"),
    },
    sourcemap: false,
  },
  resolve: {
    alias: { "@": resolve(__dirname, "src") },
  },
  server: {
    open: "/dev.html",
  },
  test: {
    globals: true,
    environment: "happy-dom",
    include: ["src/**/*.test.ts"],
  },
}));
```

- [ ] **Step 4: Create local `.gitignore`**

`android/terminal-web/.gitignore`:

```
node_modules/
dist/
.vite/
*.log
```

- [ ] **Step 5: Install dependencies**

```bash
cd android/terminal-web
npm install
```

Expected: install completes; `node_modules/` and `package-lock.json` created.

- [ ] **Step 6: Add a placeholder smoke test so `npm test` is green**

`android/terminal-web/src/_smoke.test.ts`:

```typescript
import { describe, it, expect } from "vitest";

describe("smoke", () => {
  it("vitest is wired up", () => {
    expect(1 + 1).toBe(2);
  });
});
```

Run:

```bash
cd android/terminal-web
npm test
```

Expected: 1 test file, 1 test passing, exit 0.

- [ ] **Step 7: Commit**

```bash
git add android/terminal-web/.gitignore android/terminal-web/package.json android/terminal-web/package-lock.json android/terminal-web/tsconfig.json android/terminal-web/vite.config.ts android/terminal-web/src/_smoke.test.ts
git commit -m "Bootstrap android/terminal-web Vite + Vitest skeleton"
```

---

## Task 2: Shared TypeScript types

**Files:**
- Create: `android/terminal-web/src/protocol/types.ts`

These types are imported by every later module. Defining them once here is the DRY contract for slice 1; later tasks import from `@/protocol/types`.

- [ ] **Step 1: Create `src/protocol/types.ts`**

`android/terminal-web/src/protocol/types.ts`:

```typescript
// JS bridge contract — Native -> WebView (window.* globals).
export type SpecialKey =
  | "Enter" | "Tab" | "Escape"
  | "Up" | "Down" | "Left" | "Right"
  | "C-c" | "C-d";

// JS bridge contract — WebView -> Native (window.TermixBridge optional global).
export type ConnectionState = "connecting" | "connected" | "disconnected" | "error";
export type ControlState = "none" | "requesting" | "granted" | "denied" | "revoked";

export interface TermixBridge {
  onConnectionState(state: ConnectionState, detail?: string): void;
  onControlState(state: ControlState, detail?: string): void;
  onError(code: string, message: string): void;
}

// JSON text envelope (spec §17.3 / §3b).
export interface Envelope<T = unknown> {
  type: string;
  request_id: string | null;
  payload: T;
}

// Outgoing envelope payloads.
export interface HelloAndroidPayload     { device_id: string }
export interface SessionWatchPayload     { session_id: string }
export interface SessionUnwatchPayload   { session_id: string }
export interface ControlAcquirePayload   { session_id: string }
export interface ControlRenewPayload     { session_id: string; lease_version: number }
export interface ControlReleasePayload   { session_id: string; lease_version: number }
export interface HeartbeatPayload        { /* empty object */ }

// Incoming envelope payloads.
export interface HelloOkPayload          { connection_id: string }
export interface SessionJoinedPayload    { session_id: string }
export interface SessionSnapshotReadyPayload { session_id: string; total_chunks?: number }
export interface ControlGrantedPayload   { session_id: string; lease_version: number; expires_at: string; controller_device_id: string }
export interface ControlDeniedPayload    { session_id: string; reason: string }
export interface ControlRevokedPayload   { session_id: string; reason: string }
export interface ErrorPayload            { code: string; message: string }

// TMX1 binary frame (spec §17.4 / §3c).
export const FRAME_MAGIC = "TMX1";
export const FRAME_VERSION = 0x01;
export type FrameType = 1 | 2 | 3; // 1 output, 2 input, 3 snapshot

export interface OutputHeader   { session_id: string; seq: number; stream: "stdout" | "stderr" }
export interface InputHeader    { session_id: string; seq: number; encoding: "raw" }
export interface SnapshotHeader { session_id: string; seq: number; is_last: boolean }

export type FrameHeader = OutputHeader | InputHeader | SnapshotHeader;

export interface DecodedFrame {
  type: FrameType;
  header: FrameHeader;
  payload: Uint8Array;
}
```

- [ ] **Step 2: Confirm typecheck passes**

```bash
cd android/terminal-web
npx tsc --noEmit
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add android/terminal-web/src/protocol/types.ts
git commit -m "Define shared TS types for terminal-web protocol and bridge"
```

---

## Task 3: Binary frame codec

**Files:**
- Test: `android/terminal-web/src/protocol/frame.test.ts`
- Create: `android/terminal-web/src/protocol/frame.ts`

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/protocol/frame.test.ts`:

```typescript
import { describe, it, expect } from "vitest";
import { encodeFrame, decodeFrame, encodeInputFrame } from "./frame";
import type { OutputHeader, InputHeader, SnapshotHeader } from "./types";

const utf8 = (s: string) => new TextEncoder().encode(s);

describe("encodeFrame / decodeFrame", () => {
  it("round-trips an output frame with ASCII payload", () => {
    const header: OutputHeader = { session_id: "s1", seq: 12, stream: "stdout" };
    const buf = encodeFrame(1, header, utf8("hello"));
    const decoded = decodeFrame(buf);
    expect(decoded.type).toBe(1);
    expect(decoded.header).toEqual(header);
    expect(new TextDecoder().decode(decoded.payload)).toBe("hello");
  });

  it("round-trips an output frame with multi-byte UTF-8 payload", () => {
    const header: OutputHeader = { session_id: "s1", seq: 99, stream: "stderr" };
    const original = utf8("héllo 中文 🚀");
    const buf = encodeFrame(1, header, original);
    const decoded = decodeFrame(buf);
    expect(decoded.payload).toEqual(original);
  });

  it("round-trips a snapshot frame and preserves is_last", () => {
    const header: SnapshotHeader = { session_id: "s2", seq: 0, is_last: false };
    const buf = encodeFrame(3, header, utf8("snap"));
    const decoded = decodeFrame(buf);
    expect(decoded.type).toBe(3);
    expect(decoded.header).toEqual(header);

    const lastHeader: SnapshotHeader = { session_id: "s2", seq: 1, is_last: true };
    const lastBuf = encodeFrame(3, lastHeader, utf8(""));
    const lastDecoded = decodeFrame(lastBuf);
    expect((lastDecoded.header as SnapshotHeader).is_last).toBe(true);
    expect(lastDecoded.payload.byteLength).toBe(0);
  });

  it("encodeInputFrame produces a type=2 frame with encoding=raw", () => {
    const buf = encodeInputFrame("s3", 7, utf8("ls\n"));
    const decoded = decodeFrame(buf);
    expect(decoded.type).toBe(2);
    expect(decoded.header).toEqual({ session_id: "s3", seq: 7, encoding: "raw" } satisfies InputHeader);
    expect(new TextDecoder().decode(decoded.payload)).toBe("ls\n");
  });

  it("rejects buffers shorter than the fixed prefix", () => {
    expect(() => decodeFrame(new Uint8Array(5))).toThrow(/too short/i);
  });

  it("rejects bad magic", () => {
    const bad = encodeFrame(1, { session_id: "s", seq: 0, stream: "stdout" }, utf8("x"));
    bad[0] = 0x00; // corrupt magic
    expect(() => decodeFrame(bad)).toThrow(/magic/i);
  });

  it("rejects wrong version", () => {
    const bad = encodeFrame(1, { session_id: "s", seq: 0, stream: "stdout" }, utf8("x"));
    bad[4] = 0x02;
    expect(() => decodeFrame(bad)).toThrow(/version/i);
  });

  it("rejects header_json_len that overruns the buffer", () => {
    const bad = encodeFrame(1, { session_id: "s", seq: 0, stream: "stdout" }, utf8("x"));
    // overwrite header_json_len with a huge value
    const view = new DataView(bad.buffer, bad.byteOffset, bad.byteLength);
    view.setUint32(6, 0xffffffff, false);
    expect(() => decodeFrame(bad)).toThrow(/header_json_len/i);
  });

  it("rejects malformed header JSON", () => {
    const headerBytes = utf8("{not-json");
    const buf = new Uint8Array(10 + headerBytes.byteLength);
    buf.set(utf8("TMX1"), 0);
    buf[4] = 0x01;
    buf[5] = 0x01;
    new DataView(buf.buffer).setUint32(6, headerBytes.byteLength, false);
    buf.set(headerBytes, 10);
    expect(() => decodeFrame(buf)).toThrow(/json/i);
  });

  it("rejects unknown frame_type", () => {
    const buf = encodeFrame(1, { session_id: "s", seq: 0, stream: "stdout" }, utf8("x"));
    buf[5] = 9 as unknown as 1;
    expect(() => decodeFrame(buf)).toThrow(/frame_type/i);
  });

  it("rejects non-integer seq", () => {
    const headerJSON = utf8(JSON.stringify({ session_id: "s", seq: 1.5, stream: "stdout" }));
    const buf = new Uint8Array(10 + headerJSON.byteLength);
    buf.set(utf8("TMX1"), 0);
    buf[4] = 0x01;
    buf[5] = 0x01;
    new DataView(buf.buffer).setUint32(6, headerJSON.byteLength, false);
    buf.set(headerJSON, 10);
    expect(() => decodeFrame(buf)).toThrow(/seq/i);
  });
});
```

- [ ] **Step 2: Run tests, verify all FAIL**

```bash
cd android/terminal-web
npm test -- src/protocol/frame.test.ts
```

Expected: all tests fail with "Cannot find module './frame'" or similar.

- [ ] **Step 3: Implement `src/protocol/frame.ts`**

`android/terminal-web/src/protocol/frame.ts`:

```typescript
import {
  FRAME_MAGIC,
  FRAME_VERSION,
  type DecodedFrame,
  type FrameHeader,
  type FrameType,
  type InputHeader,
  type OutputHeader,
  type SnapshotHeader,
} from "./types";

const PREFIX_BYTES = 10; // 4 magic + 1 version + 1 type + 4 header_json_len

export function encodeFrame(type: FrameType, header: FrameHeader, payload: Uint8Array): Uint8Array {
  const headerJSON = new TextEncoder().encode(JSON.stringify(header));
  const buf = new Uint8Array(PREFIX_BYTES + headerJSON.byteLength + payload.byteLength);
  buf.set(new TextEncoder().encode(FRAME_MAGIC), 0);
  buf[4] = FRAME_VERSION;
  buf[5] = type;
  new DataView(buf.buffer, buf.byteOffset, buf.byteLength).setUint32(6, headerJSON.byteLength, false);
  buf.set(headerJSON, PREFIX_BYTES);
  buf.set(payload, PREFIX_BYTES + headerJSON.byteLength);
  return buf;
}

export function encodeInputFrame(sessionId: string, seq: number, payload: Uint8Array): Uint8Array {
  const header: InputHeader = { session_id: sessionId, seq, encoding: "raw" };
  return encodeFrame(2, header, payload);
}

export function decodeFrame(buf: Uint8Array): DecodedFrame {
  if (buf.byteLength < PREFIX_BYTES) {
    throw new Error(`frame too short: ${buf.byteLength} bytes`);
  }
  const magic = new TextDecoder().decode(buf.subarray(0, 4));
  if (magic !== FRAME_MAGIC) {
    throw new Error(`bad magic: ${magic}`);
  }
  if (buf[4] !== FRAME_VERSION) {
    throw new Error(`unsupported version: ${buf[4]}`);
  }
  const type = buf[5];
  if (type !== 1 && type !== 2 && type !== 3) {
    throw new Error(`unknown frame_type: ${type}`);
  }
  const headerLen = new DataView(buf.buffer, buf.byteOffset, buf.byteLength).getUint32(6, false);
  const headerEnd = PREFIX_BYTES + headerLen;
  if (headerEnd > buf.byteLength) {
    throw new Error(`header_json_len overruns buffer: ${headerLen}`);
  }
  let header: FrameHeader;
  try {
    header = JSON.parse(new TextDecoder().decode(buf.subarray(PREFIX_BYTES, headerEnd)));
  } catch (e) {
    throw new Error(`invalid header json: ${(e as Error).message}`);
  }
  validateHeader(type, header);
  const payload = buf.subarray(headerEnd);
  return { type, header, payload };
}

function validateHeader(type: FrameType, header: FrameHeader): void {
  if (typeof header !== "object" || header === null) {
    throw new Error("header must be an object");
  }
  if (typeof (header as { session_id?: unknown }).session_id !== "string") {
    throw new Error("header.session_id must be string");
  }
  const seq = (header as { seq?: unknown }).seq;
  if (typeof seq !== "number" || !Number.isInteger(seq) || seq < 0) {
    throw new Error("header.seq must be non-negative integer");
  }
  if (type === 3) {
    const isLast = (header as SnapshotHeader).is_last;
    if (typeof isLast !== "boolean") {
      throw new Error("snapshot header.is_last must be boolean");
    }
  }
  if (type === 1) {
    const stream = (header as OutputHeader).stream;
    if (stream !== "stdout" && stream !== "stderr") {
      throw new Error("output header.stream must be stdout|stderr");
    }
  }
  if (type === 2) {
    if ((header as InputHeader).encoding !== "raw") {
      throw new Error("input header.encoding must be 'raw'");
    }
  }
}
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/protocol/frame.test.ts
```

Expected: all 11 tests pass.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/protocol/frame.ts android/terminal-web/src/protocol/frame.test.ts
git commit -m "Implement TMX1 binary frame encode/decode with strict validation"
```

---

## Task 4: JSON envelope codec

**Files:**
- Test: `android/terminal-web/src/protocol/envelope.test.ts`
- Create: `android/terminal-web/src/protocol/envelope.ts`

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/protocol/envelope.test.ts`:

```typescript
import { describe, it, expect } from "vitest";
import {
  encodeEnvelope,
  decodeEnvelope,
  isControlGranted,
  isControlDenied,
  isControlRevoked,
  isSessionJoined,
  isSessionSnapshotReady,
  isErrorEnvelope,
} from "./envelope";

describe("encodeEnvelope / decodeEnvelope", () => {
  it("encodes a hello.android envelope with a request_id", () => {
    const text = encodeEnvelope("hello.android", { device_id: "dev-1" }, "req-42");
    expect(JSON.parse(text)).toEqual({ type: "hello.android", request_id: "req-42", payload: { device_id: "dev-1" } });
  });

  it("encodes with null request_id when none provided", () => {
    const text = encodeEnvelope("heartbeat", {}, null);
    expect(JSON.parse(text)).toEqual({ type: "heartbeat", request_id: null, payload: {} });
  });

  it("decodes a control.granted envelope", () => {
    const env = decodeEnvelope(JSON.stringify({
      type: "control.granted",
      request_id: null,
      payload: { session_id: "s1", lease_version: 3, expires_at: "2026-04-25T10:00:00Z", controller_device_id: "dev-1" },
    }));
    expect(env.type).toBe("control.granted");
    expect(isControlGranted(env)).toBe(true);
    if (isControlGranted(env)) {
      expect(env.payload.lease_version).toBe(3);
    }
  });

  it("rejects malformed JSON", () => {
    expect(() => decodeEnvelope("{not-json")).toThrow();
  });

  it("rejects envelope with missing type", () => {
    const text = JSON.stringify({ request_id: null, payload: {} });
    expect(() => decodeEnvelope(text)).toThrow(/type/i);
  });

  it("rejects envelope where payload is not an object", () => {
    const text = JSON.stringify({ type: "x", request_id: null, payload: "string" });
    expect(() => decodeEnvelope(text)).toThrow(/payload/i);
  });

  it("rejects envelope with non-string request_id", () => {
    const text = JSON.stringify({ type: "x", request_id: 42, payload: {} });
    expect(() => decodeEnvelope(text)).toThrow(/request_id/i);
  });

  it("type guards narrow correctly", () => {
    const denied = decodeEnvelope(JSON.stringify({ type: "control.denied", request_id: null, payload: { session_id: "s1", reason: "busy" } }));
    expect(isControlDenied(denied)).toBe(true);
    expect(isControlGranted(denied)).toBe(false);

    const revoked = decodeEnvelope(JSON.stringify({ type: "control.revoked", request_id: null, payload: { session_id: "s1", reason: "expired" } }));
    expect(isControlRevoked(revoked)).toBe(true);

    const joined = decodeEnvelope(JSON.stringify({ type: "session.joined", request_id: null, payload: { session_id: "s1" } }));
    expect(isSessionJoined(joined)).toBe(true);

    const snap = decodeEnvelope(JSON.stringify({ type: "session.snapshot.ready", request_id: null, payload: { session_id: "s1", total_chunks: 4 } }));
    expect(isSessionSnapshotReady(snap)).toBe(true);

    const err = decodeEnvelope(JSON.stringify({ type: "error", request_id: null, payload: { code: "boom", message: "oops" } }));
    expect(isErrorEnvelope(err)).toBe(true);
  });
});
```

- [ ] **Step 2: Run tests, verify FAIL**

```bash
cd android/terminal-web
npm test -- src/protocol/envelope.test.ts
```

Expected: tests fail because `./envelope` does not exist yet.

- [ ] **Step 3: Implement `src/protocol/envelope.ts`**

`android/terminal-web/src/protocol/envelope.ts`:

```typescript
import type {
  ControlDeniedPayload,
  ControlGrantedPayload,
  ControlRevokedPayload,
  Envelope,
  ErrorPayload,
  SessionJoinedPayload,
  SessionSnapshotReadyPayload,
} from "./types";

export function encodeEnvelope<T>(type: string, payload: T, requestId: string | null = null): string {
  const env: Envelope<T> = { type, request_id: requestId, payload };
  return JSON.stringify(env);
}

export function decodeEnvelope(text: string): Envelope {
  const raw = JSON.parse(text) as unknown;
  if (typeof raw !== "object" || raw === null) {
    throw new Error("envelope must be an object");
  }
  const obj = raw as Record<string, unknown>;
  if (typeof obj.type !== "string") {
    throw new Error("envelope.type must be a string");
  }
  if (obj.request_id !== null && typeof obj.request_id !== "string") {
    throw new Error("envelope.request_id must be string or null");
  }
  if (typeof obj.payload !== "object" || obj.payload === null) {
    throw new Error("envelope.payload must be an object");
  }
  return obj as unknown as Envelope;
}

export function isSessionJoined(env: Envelope): env is Envelope<SessionJoinedPayload> {
  return env.type === "session.joined";
}

export function isSessionSnapshotReady(env: Envelope): env is Envelope<SessionSnapshotReadyPayload> {
  return env.type === "session.snapshot.ready";
}

export function isControlGranted(env: Envelope): env is Envelope<ControlGrantedPayload> {
  return env.type === "control.granted";
}

export function isControlDenied(env: Envelope): env is Envelope<ControlDeniedPayload> {
  return env.type === "control.denied";
}

export function isControlRevoked(env: Envelope): env is Envelope<ControlRevokedPayload> {
  return env.type === "control.revoked";
}

export function isErrorEnvelope(env: Envelope): env is Envelope<ErrorPayload> {
  return env.type === "error";
}
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/protocol/envelope.test.ts
```

Expected: all 8 tests pass.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/protocol/envelope.ts android/terminal-web/src/protocol/envelope.test.ts
git commit -m "Implement JSON envelope encode/decode with type guards"
```

---

## Task 5: Mock WebSocket test utility

**Files:**
- Create: `android/terminal-web/src/test-utils/mockWebSocket.ts`

This is shared test infrastructure used by Tasks 6, 7, 9, 10, 12, and the inbound bridge test in Task 13. The wsClient will accept a factory `(url: string) => WebSocket`, defaulting to `(url) => new WebSocket(url)` in production. In tests we pass a `MockWebSocket` factory.

- [ ] **Step 1: Implement the mock**

`android/terminal-web/src/test-utils/mockWebSocket.ts`:

```typescript
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
```

- [ ] **Step 2: Confirm typecheck passes**

```bash
cd android/terminal-web
npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add android/terminal-web/src/test-utils/mockWebSocket.ts
git commit -m "Add MockWebSocket test utility for wsClient and downstream tests"
```

---

## Task 6: WSS client (`net/wsClient`)

**Files:**
- Test: `android/terminal-web/src/net/wsClient.test.ts`
- Create: `android/terminal-web/src/net/wsClient.ts`

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/net/wsClient.test.ts`:

```typescript
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
```

- [ ] **Step 2: Run tests, verify FAIL**

```bash
cd android/terminal-web
npm test -- src/net/wsClient.test.ts
```

Expected: tests fail with "Cannot find module './wsClient'".

- [ ] **Step 3: Implement `src/net/wsClient.ts`**

`android/terminal-web/src/net/wsClient.ts`:

```typescript
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
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/net/wsClient.test.ts
```

Expected: all 8 tests pass.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/net/wsClient.ts android/terminal-web/src/net/wsClient.test.ts
git commit -m "Implement WSClient lifecycle wrapper with injected WebSocket factory"
```

---

## Task 7: Heartbeat sender

**Files:**
- Test: `android/terminal-web/src/net/heartbeat.test.ts`
- Create: `android/terminal-web/src/net/heartbeat.ts`

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/net/heartbeat.test.ts`:

```typescript
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { startHeartbeat } from "./heartbeat";

describe("startHeartbeat", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it("calls send() at every interval", () => {
    const send = vi.fn();
    const stop = startHeartbeat(send, 20000);
    expect(send).not.toHaveBeenCalled();
    vi.advanceTimersByTime(20000);
    expect(send).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(20000);
    expect(send).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(20000);
    expect(send).toHaveBeenCalledTimes(3);
    stop();
  });

  it("stop() halts further calls", () => {
    const send = vi.fn();
    const stop = startHeartbeat(send, 1000);
    vi.advanceTimersByTime(1000);
    expect(send).toHaveBeenCalledTimes(1);
    stop();
    vi.advanceTimersByTime(5000);
    expect(send).toHaveBeenCalledTimes(1);
  });

  it("stop() is idempotent", () => {
    const send = vi.fn();
    const stop = startHeartbeat(send, 1000);
    stop();
    expect(() => stop()).not.toThrow();
  });
});
```

- [ ] **Step 2: Run tests, verify FAIL**

```bash
cd android/terminal-web
npm test -- src/net/heartbeat.test.ts
```

Expected: tests fail with "Cannot find module './heartbeat'".

- [ ] **Step 3: Implement `src/net/heartbeat.ts`**

`android/terminal-web/src/net/heartbeat.ts`:

```typescript
export function startHeartbeat(send: () => void, intervalMs: number): () => void {
  const handle = setInterval(send, intervalMs);
  let stopped = false;
  return () => {
    if (stopped) return;
    stopped = true;
    clearInterval(handle);
  };
}
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/net/heartbeat.test.ts
```

Expected: all 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/net/heartbeat.ts android/terminal-web/src/net/heartbeat.test.ts
git commit -m "Add heartbeat timer with idempotent stop"
```

---

## Task 8: Outbound bridge emitter

**Files:**
- Test: `android/terminal-web/src/bridge/outbound.test.ts`
- Create: `android/terminal-web/src/bridge/outbound.ts`

The outbound bridge wraps `window.TermixBridge?.method(...)` calls. If `TermixBridge` is absent (running in dev/browser without native), it logs to `console.info` so a developer can see events in devtools. This module is the *only* place that touches `window.TermixBridge`; all other code calls into the typed emitter object returned here.

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/bridge/outbound.test.ts`:

```typescript
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
```

- [ ] **Step 2: Run tests, verify FAIL**

```bash
cd android/terminal-web
npm test -- src/bridge/outbound.test.ts
```

Expected: tests fail with "Cannot find module './outbound'".

- [ ] **Step 3: Implement `src/bridge/outbound.ts`**

`android/terminal-web/src/bridge/outbound.ts`:

```typescript
import type { ConnectionState, ControlState, TermixBridge } from "@/protocol/types";

export function createOutboundEmitter(): TermixBridge {
  const log = (method: string, ...args: unknown[]) => {
    console.info(`[TermixBridge] ${method}`, ...args);
  };

  const bridge = (): Partial<TermixBridge> | undefined => {
    if (typeof window === "undefined") return undefined;
    return (window as { TermixBridge?: Partial<TermixBridge> }).TermixBridge;
  };

  return {
    onConnectionState(state: ConnectionState, detail?: string) {
      const fn = bridge()?.onConnectionState;
      if (fn) fn(state, detail);
      else log("onConnectionState", state, detail);
    },
    onControlState(state: ControlState, detail?: string) {
      const fn = bridge()?.onControlState;
      if (fn) fn(state, detail);
      else log("onControlState", state, detail);
    },
    onError(code: string, message: string) {
      const fn = bridge()?.onError;
      if (fn) fn(code, message);
      else log("onError", code, message);
    },
  };
}
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/bridge/outbound.test.ts
```

Expected: all 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/bridge/outbound.ts android/terminal-web/src/bridge/outbound.test.ts
git commit -m "Implement outbound bridge with window.TermixBridge + console fallback"
```

---

## Task 9: Control-lease state machine

**Files:**
- Test: `android/terminal-web/src/session/control.test.ts`
- Create: `android/terminal-web/src/session/control.ts`

The control module owns: state (`none|requesting|granted|denied|revoked`), the auto-renew timer, and the special-key encoding table from spec §4c. It exposes the input gate (`canSendInput()`) so callers don't reimplement the rule.

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/session/control.test.ts`:

```typescript
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createControl, encodeSpecialKey } from "./control";
import { decodeEnvelope } from "@/protocol/envelope";
import type { ControlState } from "@/protocol/types";

describe("createControl", () => {
  beforeEach(() => { vi.useFakeTimers({ now: new Date("2026-04-25T10:00:00Z") }); });
  afterEach(() => { vi.useRealTimers(); });

  it("starts in 'none' state", () => {
    const sent: string[] = [];
    const states: ControlState[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: (s) => states.push(s) });
    expect(ctrl.state).toBe("none");
    expect(states).toEqual([]);
  });

  it("requestControl moves to 'requesting' and sends control.acquire", () => {
    const sent: string[] = [];
    const states: ControlState[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: (s) => states.push(s) });
    ctrl.requestControl();
    expect(ctrl.state).toBe("requesting");
    expect(states).toEqual(["requesting"]);
    expect(sent).toHaveLength(1);
    const env = decodeEnvelope(sent[0]);
    expect(env.type).toBe("control.acquire");
    expect(env.payload).toEqual({ session_id: "s1" });
  });

  it("control.granted moves to 'granted' and schedules auto-renew", () => {
    const sent: string[] = [];
    const states: ControlState[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: (s) => states.push(s) });
    ctrl.requestControl();
    sent.length = 0; states.length = 0;

    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 1, expires_at: "2026-04-25T10:01:00Z", controller_device_id: "dev-1" },
    })));
    expect(ctrl.state).toBe("granted");
    expect(states).toEqual(["granted"]);

    // Auto-renew at expires_at - 30s = 30s from now.
    vi.advanceTimersByTime(30_000);
    expect(sent).toHaveLength(1);
    const env = decodeEnvelope(sent[0]);
    expect(env.type).toBe("control.renew");
    expect(env.payload).toEqual({ session_id: "s1", lease_version: 1 });
  });

  it("renew with bumped lease_version re-arms the timer", () => {
    const sent: string[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: () => {} });
    ctrl.requestControl();
    sent.length = 0;
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 1, expires_at: "2026-04-25T10:01:00Z", controller_device_id: "dev-1" },
    })));
    vi.advanceTimersByTime(30_000);
    expect(sent.length).toBe(1); // first renew
    sent.length = 0;
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 2, expires_at: "2026-04-25T10:02:00Z", controller_device_id: "dev-1" },
    })));
    // Now timer should fire again 30s before new expiry, i.e. 30s from now.
    vi.advanceTimersByTime(30_000);
    expect(sent.length).toBe(1);
    const env = decodeEnvelope(sent[0]);
    expect(env.payload).toEqual({ session_id: "s1", lease_version: 2 });
  });

  it("control.denied moves to 'denied'", () => {
    const states: ControlState[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: () => {}, onState: (s) => states.push(s) });
    ctrl.requestControl();
    states.length = 0;
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.denied", request_id: null,
      payload: { session_id: "s1", reason: "another device holds control" },
    })));
    expect(ctrl.state).toBe("denied");
    expect(states).toEqual(["denied"]);
  });

  it("control.revoked moves granted -> revoked and clears the timer", () => {
    const sent: string[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: () => {} });
    ctrl.requestControl();
    sent.length = 0;
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 1, expires_at: "2026-04-25T10:01:00Z", controller_device_id: "dev-1" },
    })));
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.revoked", request_id: null, payload: { session_id: "s1", reason: "expired" },
    })));
    expect(ctrl.state).toBe("revoked");
    sent.length = 0;
    vi.advanceTimersByTime(60_000);
    expect(sent).toHaveLength(0); // no renew after revoke
  });

  it("releaseControl from granted sends control.release and resets to none", () => {
    const sent: string[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: () => {} });
    ctrl.requestControl();
    sent.length = 0;
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 5, expires_at: "2026-04-25T10:01:00Z", controller_device_id: "dev-1" },
    })));
    ctrl.releaseControl();
    expect(ctrl.state).toBe("none");
    expect(sent).toHaveLength(1);
    const env = decodeEnvelope(sent[0]);
    expect(env.type).toBe("control.release");
    expect(env.payload).toEqual({ session_id: "s1", lease_version: 5 });
  });

  it("connection drop while granted resets to none WITHOUT sending control.release", () => {
    const sent: string[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: () => {} });
    ctrl.requestControl();
    sent.length = 0;
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 1, expires_at: "2026-04-25T10:01:00Z", controller_device_id: "dev-1" },
    })));
    ctrl.onConnectionDropped();
    expect(ctrl.state).toBe("none");
    expect(sent).toHaveLength(0);
    sent.length = 0;
    vi.advanceTimersByTime(60_000);
    expect(sent).toHaveLength(0);
  });

  it("canSendInput is true only when granted", () => {
    const ctrl = createControl({ sessionId: "s1", sendText: () => {}, onState: () => {} });
    expect(ctrl.canSendInput()).toBe(false);
    ctrl.requestControl();
    expect(ctrl.canSendInput()).toBe(false);
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 1, expires_at: "2026-04-25T10:01:00Z", controller_device_id: "dev-1" },
    })));
    expect(ctrl.canSendInput()).toBe(true);
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.revoked", request_id: null, payload: { session_id: "s1", reason: "x" },
    })));
    expect(ctrl.canSendInput()).toBe(false);
  });

  it("requestControl from denied or revoked moves back to requesting", () => {
    const ctrl = createControl({ sessionId: "s1", sendText: () => {}, onState: () => {} });
    ctrl.requestControl();
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.denied", request_id: null, payload: { session_id: "s1", reason: "busy" },
    })));
    expect(ctrl.state).toBe("denied");
    ctrl.requestControl();
    expect(ctrl.state).toBe("requesting");
  });

  it("auto-renew timer floors at 5 seconds when expiry is closer than 35s", () => {
    const sent: string[] = [];
    const ctrl = createControl({ sessionId: "s1", sendText: (t) => sent.push(t), onState: () => {} });
    ctrl.requestControl();
    sent.length = 0;
    ctrl.handleEnvelope(decodeEnvelope(JSON.stringify({
      type: "control.granted", request_id: null,
      payload: { session_id: "s1", lease_version: 1, expires_at: "2026-04-25T10:00:10Z", controller_device_id: "dev-1" }, // 10s from "now"
    })));
    // 10 - 30 = -20, floored to 5s.
    vi.advanceTimersByTime(4_999);
    expect(sent).toHaveLength(0);
    vi.advanceTimersByTime(2);
    expect(sent).toHaveLength(1);
  });
});

describe("encodeSpecialKey", () => {
  it.each([
    ["Enter",  [0x0d]],
    ["Tab",    [0x09]],
    ["Escape", [0x1b]],
    ["Up",     [0x1b, 0x5b, 0x41]],
    ["Down",   [0x1b, 0x5b, 0x42]],
    ["Right",  [0x1b, 0x5b, 0x43]],
    ["Left",   [0x1b, 0x5b, 0x44]],
    ["C-c",    [0x03]],
    ["C-d",    [0x04]],
  ] as const)("encodes %s correctly", (key, expected) => {
    expect(Array.from(encodeSpecialKey(key))).toEqual(expected);
  });
});
```

- [ ] **Step 2: Run tests, verify FAIL**

```bash
cd android/terminal-web
npm test -- src/session/control.test.ts
```

Expected: tests fail with "Cannot find module './control'".

- [ ] **Step 3: Implement `src/session/control.ts`**

`android/terminal-web/src/session/control.ts`:

```typescript
import { encodeEnvelope, isControlDenied, isControlGranted, isControlRevoked } from "@/protocol/envelope";
import type { ControlState, Envelope, SpecialKey } from "@/protocol/types";

const RENEW_LEAD_MS = 30_000;
const RENEW_FLOOR_MS = 5_000;

export interface Control {
  readonly state: ControlState;
  requestControl(): void;
  releaseControl(): void;
  handleEnvelope(env: Envelope): void;
  onConnectionDropped(): void;
  canSendInput(): boolean;
}

export interface ControlConfig {
  sessionId: string;
  sendText: (text: string) => void;
  onState: (state: ControlState, detail?: string) => void;
}

export function createControl(cfg: ControlConfig): Control {
  let state: ControlState = "none";
  let leaseVersion: number | null = null;
  let renewHandle: ReturnType<typeof setTimeout> | null = null;

  const setState = (next: ControlState, detail?: string) => {
    if (state === next) return;
    state = next;
    cfg.onState(next, detail);
  };

  const clearRenew = () => {
    if (renewHandle !== null) {
      clearTimeout(renewHandle);
      renewHandle = null;
    }
  };

  const scheduleRenew = (expiresAtIso: string) => {
    clearRenew();
    const expiresAtMs = Date.parse(expiresAtIso);
    const delay = Math.max(RENEW_FLOOR_MS, expiresAtMs - Date.now() - RENEW_LEAD_MS);
    renewHandle = setTimeout(() => {
      if (state !== "granted" || leaseVersion === null) return;
      cfg.sendText(encodeEnvelope("control.renew", { session_id: cfg.sessionId, lease_version: leaseVersion }));
    }, delay);
  };

  return {
    get state() { return state; },

    requestControl() {
      cfg.sendText(encodeEnvelope("control.acquire", { session_id: cfg.sessionId }));
      setState("requesting");
    },

    releaseControl() {
      if (state !== "granted" || leaseVersion === null) return;
      cfg.sendText(encodeEnvelope("control.release", { session_id: cfg.sessionId, lease_version: leaseVersion }));
      clearRenew();
      leaseVersion = null;
      setState("none");
    },

    handleEnvelope(env: Envelope) {
      if (isControlGranted(env)) {
        leaseVersion = env.payload.lease_version;
        setState("granted");
        scheduleRenew(env.payload.expires_at);
      } else if (isControlDenied(env)) {
        clearRenew();
        leaseVersion = null;
        setState("denied", env.payload.reason);
      } else if (isControlRevoked(env)) {
        clearRenew();
        leaseVersion = null;
        setState("revoked", env.payload.reason);
      }
    },

    onConnectionDropped() {
      clearRenew();
      leaseVersion = null;
      setState("none");
    },

    canSendInput() { return state === "granted"; },
  };
}

const SPECIAL_KEY_BYTES: Record<SpecialKey, number[]> = {
  Enter:  [0x0d],
  Tab:    [0x09],
  Escape: [0x1b],
  Up:     [0x1b, 0x5b, 0x41],
  Down:   [0x1b, 0x5b, 0x42],
  Right:  [0x1b, 0x5b, 0x43],
  Left:   [0x1b, 0x5b, 0x44],
  "C-c":  [0x03],
  "C-d":  [0x04],
};

export function encodeSpecialKey(key: SpecialKey): Uint8Array {
  return new Uint8Array(SPECIAL_KEY_BYTES[key]);
}
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/session/control.test.ts
```

Expected: all 12 tests pass (10 control + 9 encodeSpecialKey via `it.each`, vitest counts each parametric case).

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/session/control.ts android/terminal-web/src/session/control.test.ts
git commit -m "Implement control-lease state machine and special-key encoder"
```

---

## Task 10: Session watcher (snapshot reassembly + live output)

**Files:**
- Test: `android/terminal-web/src/session/watcher.test.ts`
- Create: `android/terminal-web/src/session/watcher.ts`

The watcher receives decoded binary frames from wsClient and forwards bytes into a `write(bytes)` callback (xterm.js will be the production sink). Snapshot frames arrive as type=3 with `is_last`; live output as type=1; per spec §7a we trust arrival order and forward in order.

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/session/watcher.test.ts`:

```typescript
import { describe, expect, it, vi } from "vitest";
import { createWatcher } from "./watcher";
import type { DecodedFrame, OutputHeader, SnapshotHeader } from "@/protocol/types";

const utf8 = (s: string) => new TextEncoder().encode(s);

function snap(seq: number, isLast: boolean, bytes: Uint8Array): DecodedFrame {
  const header: SnapshotHeader = { session_id: "s1", seq, is_last: isLast };
  return { type: 3, header, payload: bytes };
}
function out(seq: number, bytes: Uint8Array): DecodedFrame {
  const header: OutputHeader = { session_id: "s1", seq, stream: "stdout" };
  return { type: 1, header, payload: bytes };
}

describe("createWatcher", () => {
  it("writes snapshot bytes in arrival order then live output", () => {
    const writes: string[] = [];
    const watcher = createWatcher({ sessionId: "s1", write: (b) => writes.push(new TextDecoder().decode(b)) });
    watcher.handleFrame(snap(0, false, utf8("AAA")));
    watcher.handleFrame(snap(1, false, utf8("BBB")));
    watcher.handleFrame(snap(2, true,  utf8("CCC")));
    watcher.handleFrame(out(0, utf8("LIVE")));
    expect(writes).toEqual(["AAA", "BBB", "CCC", "LIVE"]);
  });

  it("ignores frames whose session_id does not match", () => {
    const writes: string[] = [];
    const watcher = createWatcher({ sessionId: "expected", write: (b) => writes.push(new TextDecoder().decode(b)) });
    const wrong: DecodedFrame = { type: 1, header: { session_id: "other", seq: 0, stream: "stdout" }, payload: utf8("X") };
    watcher.handleFrame(wrong);
    expect(writes).toEqual([]);
  });

  it("forwards multi-byte UTF-8 payloads as raw bytes", () => {
    const writes: Uint8Array[] = [];
    const watcher = createWatcher({ sessionId: "s1", write: (b) => writes.push(new Uint8Array(b)) });
    const bytes = utf8("héllo 中文 🚀");
    watcher.handleFrame(out(0, bytes));
    expect(writes).toHaveLength(1);
    expect(writes[0]).toEqual(bytes);
  });

  it("write is called once per frame even with empty payload", () => {
    const write = vi.fn();
    const watcher = createWatcher({ sessionId: "s1", write });
    watcher.handleFrame(snap(0, true, new Uint8Array(0)));
    expect(write).toHaveBeenCalledOnce();
    expect(write.mock.calls[0][0].byteLength).toBe(0);
  });
});
```

- [ ] **Step 2: Run tests, verify FAIL**

```bash
cd android/terminal-web
npm test -- src/session/watcher.test.ts
```

Expected: tests fail with "Cannot find module './watcher'".

- [ ] **Step 3: Implement `src/session/watcher.ts`**

`android/terminal-web/src/session/watcher.ts`:

```typescript
import type { DecodedFrame } from "@/protocol/types";

export interface Watcher {
  handleFrame(frame: DecodedFrame): void;
}

export interface WatcherConfig {
  sessionId: string;
  write: (bytes: Uint8Array) => void;
}

export function createWatcher(cfg: WatcherConfig): Watcher {
  return {
    handleFrame(frame) {
      if (frame.header.session_id !== cfg.sessionId) return;
      if (frame.type === 1 || frame.type === 3) {
        cfg.write(frame.payload);
      }
      // type 2 is input; never received from server.
    },
  };
}
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/session/watcher.test.ts
```

Expected: all 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/session/watcher.ts android/terminal-web/src/session/watcher.test.ts
git commit -m "Implement session watcher: route output and snapshot frames in arrival order"
```

---

## Task 11: xterm.js terminal UI wrapper

**Files:**
- Create: `android/terminal-web/src/ui/terminal.ts`

This is a thin wrapper. xterm.js is well-tested upstream; we don't re-test rendering. We just need a stable `init / write / onInput / dispose` shape to keep the entry/index code clean.

- [ ] **Step 1: Implement `src/ui/terminal.ts`**

`android/terminal-web/src/ui/terminal.ts`:

```typescript
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

export interface TerminalUI {
  write(bytes: Uint8Array): void;
  onInput(handler: (text: string) => void): void;
  fit(): void;
  dispose(): void;
}

export function mountTerminal(container: HTMLElement): TerminalUI {
  const term = new Terminal({ cursorBlink: true, convertEol: false, fontSize: 14 });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(container);
  fit.fit();

  return {
    write(bytes) { term.write(bytes); },
    onInput(handler) { term.onData(handler); },
    fit() { fit.fit(); },
    dispose() { term.dispose(); },
  };
}
```

- [ ] **Step 2: Confirm typecheck passes**

```bash
cd android/terminal-web
npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add android/terminal-web/src/ui/terminal.ts
git commit -m "Add xterm.js terminal wrapper (init, write, onInput, fit, dispose)"
```

---

## Task 12: Inbound bridge (orchestrator)

**Files:**
- Test: `android/terminal-web/src/bridge/inbound.test.ts`
- Create: `android/terminal-web/src/bridge/inbound.ts`

The inbound bridge is the only place that wires `wsClient` + `control` + `watcher` + `outbound` + `terminal-ui` together. It exposes `installInboundBridge({ ui, factory })` which installs the `window.*` functions. Tests verify the wiring with mock socket + a stub UI; ui is injected so the unit test doesn't need a real DOM terminal.

- [ ] **Step 1: Write the failing tests**

`android/terminal-web/src/bridge/inbound.test.ts`:

```typescript
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
  TermixBridge?: { onConnectionState?: (s: ConnectionState) => void; onControlState?: (s: ControlState) => void; onError?: (c: string, m: string) => void };
};

describe("installInboundBridge", () => {
  let ui: ReturnType<typeof makeStubUI>;

  beforeEach(() => {
    MockWebSocket.instances = [];
    delete w.setSession; delete w.sendText; delete w.sendSpecialKey;
    delete w.requestControl; delete w.releaseControl;
    delete w.TermixBridge;
    ui = makeStubUI();
  });
  afterEach(() => { vi.useRealTimers(); });

  it("installs all five window functions", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    expect(typeof w.setSession).toBe("function");
    expect(typeof w.sendText).toBe("function");
    expect(typeof w.sendSpecialKey).toBe("function");
    expect(typeof w.requestControl).toBe("function");
    expect(typeof w.releaseControl).toBe("function");
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

  it("on open, sends hello.android then session.watch and emits connected", () => {
    const onConnectionState = vi.fn();
    w.TermixBridge = { onConnectionState };
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    expect(ws.sentText).toHaveLength(2);
    expect(decodeEnvelope(ws.sentText[0])).toEqual(expect.objectContaining({ type: "hello.android", payload: { device_id: "dev-1" } }));
    expect(decodeEnvelope(ws.sentText[1])).toEqual(expect.objectContaining({ type: "session.watch", payload: { session_id: "sess-1" } }));
    expect(onConnectionState).toHaveBeenCalledWith("connected", undefined);
  });

  it("incoming output frames are forwarded to ui.write", () => {
    const { factory } = mockFactory();
    installInboundBridge({ ui, factory });
    w.setSession!("sess-1", "wss://relay.example/ws", "tok", "dev-1");
    const ws = MockWebSocket.instances[0];
    ws.triggerOpen();
    const frame = encodeFrame(1, { session_id: "sess-1", seq: 0, stream: "stdout" }, new TextEncoder().encode("hi"));
    ws.triggerBinary(frame.buffer.slice(frame.byteOffset, frame.byteOffset + frame.byteLength));
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
});
```

- [ ] **Step 2: Run tests, verify FAIL**

```bash
cd android/terminal-web
npm test -- src/bridge/inbound.test.ts
```

Expected: tests fail with "Cannot find module './inbound'".

- [ ] **Step 3: Implement `src/bridge/inbound.ts`**

`android/terminal-web/src/bridge/inbound.ts`:

```typescript
import { decodeFrame, encodeInputFrame } from "@/protocol/frame";
import { decodeEnvelope, encodeEnvelope } from "@/protocol/envelope";
import { openWSClient, type WSClient, type WebSocketFactory } from "@/net/wsClient";
import { startHeartbeat } from "@/net/heartbeat";
import { createControl, encodeSpecialKey, type Control } from "@/session/control";
import { createWatcher } from "@/session/watcher";
import { createOutboundEmitter } from "./outbound";
import type { SpecialKey } from "@/protocol/types";
import type { TerminalUI } from "@/ui/terminal";

interface ActiveSession {
  sessionId: string;
  ws: WSClient;
  control: Control;
  stopHeartbeat: () => void;
  inputSeq: number;
  sendInput: (bytes: Uint8Array) => void;
}

export interface InboundConfig {
  ui: TerminalUI;
  factory?: WebSocketFactory;
}

export function installInboundBridge(cfg: InboundConfig): void {
  const outbound = createOutboundEmitter();
  let active: ActiveSession | null = null;

  const closeActive = () => {
    if (!active) return;
    active.stopHeartbeat();
    active.ws.close();
    active = null;
  };

  const setSession = (sessionId: string, relayUrl: string, accessToken: string, deviceId: string): void => {
    closeActive();
    if (!sessionId || !relayUrl) return; // graceful-close path

    outbound.onConnectionState("connecting");

    const url = new URL(relayUrl);
    url.searchParams.set("access_token", accessToken);
    url.searchParams.set("device_id", deviceId);
    url.searchParams.set("session_id", sessionId);

    const session: ActiveSession = {
      sessionId,
      ws: undefined as unknown as WSClient, // assigned below before any handler can fire
      control: undefined as unknown as Control,
      stopHeartbeat: () => {},
      inputSeq: 0,
      sendInput: (bytes) => {
        if (!session.control.canSendInput()) return;
        session.ws.sendBinary(encodeInputFrame(session.sessionId, session.inputSeq++, bytes));
      },
    };

    session.control = createControl({
      sessionId,
      sendText: (text) => session.ws.sendText(text),
      onState: (state, detail) => outbound.onControlState(state, detail),
    });

    const watcher = createWatcher({ sessionId, write: (b) => cfg.ui.write(b) });

    session.ws = openWSClient(url.toString(), {
      onOpen: () => {
        session.ws.sendText(encodeEnvelope("hello.android", { device_id: deviceId }));
        session.ws.sendText(encodeEnvelope("session.watch", { session_id: sessionId }));
        session.stopHeartbeat = startHeartbeat(
          () => session.ws.sendText(encodeEnvelope("heartbeat", {})),
          20_000,
        );
        outbound.onConnectionState("connected");
      },
      onText: (text) => {
        try {
          const env = decodeEnvelope(text);
          session.control.handleEnvelope(env);
          if (env.type === "error") {
            const p = env.payload as { code?: string; message?: string };
            outbound.onError(p.code ?? "error", p.message ?? "");
          }
        } catch (e) {
          outbound.onError("decode", (e as Error).message);
        }
      },
      onBinary: (data) => {
        try {
          watcher.handleFrame(decodeFrame(new Uint8Array(data)));
        } catch (e) {
          outbound.onError("frame", (e as Error).message);
        }
      },
      onClose: () => {
        session.control.onConnectionDropped();
        session.stopHeartbeat();
        if (active === session) active = null;
        outbound.onConnectionState("disconnected");
      },
      onError: () => outbound.onConnectionState("error"),
    }, cfg.factory);

    active = session;
  };

  cfg.ui.onInput((text) => {
    if (!active) return;
    active.sendInput(new TextEncoder().encode(text));
  });

  type WindowGlobals = {
    setSession: typeof setSession;
    sendText: (text: string) => void;
    sendSpecialKey: (key: SpecialKey) => void;
    requestControl: () => void;
    releaseControl: () => void;
  };
  const w = window as unknown as WindowGlobals;
  w.setSession = setSession;
  w.sendText = (text) => active?.sendInput(new TextEncoder().encode(text));
  w.sendSpecialKey = (key) => active?.sendInput(encodeSpecialKey(key));
  w.requestControl = () => active?.control.requestControl();
  w.releaseControl = () => active?.control.releaseControl();
}
```

- [ ] **Step 4: Run tests, verify PASS**

```bash
cd android/terminal-web
npm test -- src/bridge/inbound.test.ts
```

Expected: all 9 tests pass.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/src/bridge/inbound.ts android/terminal-web/src/bridge/inbound.test.ts
git commit -m "Implement inbound bridge: install window.* globals, orchestrate WS+control+watcher"
```

---

## Task 13: Production entry + `index.html`

**Files:**
- Create: `android/terminal-web/index.html`
- Create: `android/terminal-web/src/entry/index.ts`
- Delete: `android/terminal-web/src/_smoke.test.ts` (no longer needed)

- [ ] **Step 1: Create `index.html`**

`android/terminal-web/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Termix Terminal</title>
    <style>
      html, body { margin: 0; padding: 0; height: 100%; background: #000; }
      #terminal { position: absolute; inset: 0; }
    </style>
  </head>
  <body>
    <div id="terminal"></div>
    <script type="module" src="/src/entry/index.ts"></script>
  </body>
</html>
```

- [ ] **Step 2: Create `src/entry/index.ts`**

`android/terminal-web/src/entry/index.ts`:

```typescript
import { mountTerminal } from "@/ui/terminal";
import { installInboundBridge } from "@/bridge/inbound";

const container = document.getElementById("terminal");
if (!container) {
  throw new Error("missing #terminal container");
}
const ui = mountTerminal(container);
installInboundBridge({ ui });
```

- [ ] **Step 3: Delete the bootstrap smoke test**

```bash
git rm android/terminal-web/src/_smoke.test.ts
```

- [ ] **Step 4: Confirm typecheck and tests pass**

```bash
cd android/terminal-web
npx tsc --noEmit
npm test
```

Expected: typecheck clean; full test suite green.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/index.html android/terminal-web/src/entry/index.ts
git commit -m "Add production entry: mount xterm.js + install inbound bridge"
```

---

## Task 14: Dev harness (`dev.html` + `entry/dev.ts`)

**Files:**
- Create: `android/terminal-web/dev.html`
- Create: `android/terminal-web/src/entry/dev.ts`

`dev.html` is dev-only. It is **not** included in `vite build` output (Task 15 verifies this).

- [ ] **Step 1: Create `dev.html`**

`android/terminal-web/dev.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Termix Terminal — Dev Harness</title>
    <style>
      html, body { margin: 0; padding: 0; height: 100%; background: #111; color: #eee; font-family: monospace; }
      .layout { display: grid; grid-template-rows: auto 1fr auto; height: 100%; }
      .controls, .status { background: #181818; padding: 8px; }
      .controls label { display: inline-block; min-width: 120px; }
      .controls input { width: 360px; padding: 4px; background: #222; color: #eee; border: 1px solid #333; }
      .row { display: flex; gap: 8px; align-items: center; margin-bottom: 4px; }
      button { padding: 4px 10px; background: #333; color: #eee; border: 1px solid #444; cursor: pointer; }
      button:hover { background: #444; }
      #terminal { background: #000; }
      #log { max-height: 120px; overflow-y: auto; font-size: 12px; }
      #log .line { padding: 2px 4px; border-bottom: 1px solid #222; }
    </style>
  </head>
  <body>
    <div class="layout">
      <div class="controls">
        <div class="row"><label>Session ID</label><input id="sessionId" /></div>
        <div class="row"><label>Relay URL</label><input id="relayUrl" placeholder="wss://relay.example/ws" /></div>
        <div class="row"><label>Access token</label><input id="accessToken" /></div>
        <div class="row"><label>Device ID</label><input id="deviceId" /></div>
        <div class="row">
          <button id="btnConnect">Connect</button>
          <button id="btnDisconnect">Disconnect</button>
          <button id="btnRequest">Request Control</button>
          <button id="btnRelease">Release Control</button>
        </div>
        <div class="row">
          <input id="text" placeholder="text to send" style="flex:1; width:auto;" />
          <button id="btnSendText">Send Text</button>
        </div>
        <div class="row">
          <button data-key="Enter">Enter</button>
          <button data-key="Tab">Tab</button>
          <button data-key="Escape">Esc</button>
          <button data-key="Up">Up</button>
          <button data-key="Down">Down</button>
          <button data-key="Left">Left</button>
          <button data-key="Right">Right</button>
          <button data-key="C-c">Ctrl-C</button>
          <button data-key="C-d">Ctrl-D</button>
        </div>
      </div>
      <div id="terminal"></div>
      <div class="status">
        <div id="conn">connection: idle</div>
        <div id="ctrl">control: none</div>
        <div id="log"></div>
      </div>
    </div>
    <script type="module" src="/src/entry/dev.ts"></script>
  </body>
</html>
```

- [ ] **Step 2: Create `src/entry/dev.ts`**

`android/terminal-web/src/entry/dev.ts`:

```typescript
import { mountTerminal } from "@/ui/terminal";
import { installInboundBridge } from "@/bridge/inbound";
import type { ConnectionState, ControlState, SpecialKey, TermixBridge } from "@/protocol/types";

type WindowGlobals = {
  setSession?: (s: string, r: string, t: string, d: string) => void;
  sendText?: (t: string) => void;
  sendSpecialKey?: (k: SpecialKey) => void;
  requestControl?: () => void;
  releaseControl?: () => void;
  TermixBridge?: TermixBridge;
};
const w = window as unknown as WindowGlobals & Window;

const $ = (id: string) => document.getElementById(id) as HTMLInputElement | HTMLButtonElement;
const log = (text: string) => {
  const div = document.createElement("div");
  div.className = "line";
  div.textContent = `[${new Date().toLocaleTimeString()}] ${text}`;
  const el = document.getElementById("log")!;
  el.prepend(div);
};

// Restore form values from localStorage.
const fields = ["sessionId", "relayUrl", "accessToken", "deviceId"] as const;
for (const f of fields) {
  const v = localStorage.getItem(`terminal-web.${f}`);
  if (v) (document.getElementById(f) as HTMLInputElement).value = v;
}
for (const f of fields) {
  (document.getElementById(f) as HTMLInputElement).addEventListener("input", (e) => {
    localStorage.setItem(`terminal-web.${f}`, (e.target as HTMLInputElement).value);
  });
}

// Install bridge before mounting buttons so window.* globals exist.
const ui = mountTerminal(document.getElementById("terminal")!);
installInboundBridge({ ui });

// Install dev TermixBridge to surface state changes in the status panel.
w.TermixBridge = {
  onConnectionState(state: ConnectionState, detail?: string) {
    document.getElementById("conn")!.textContent = `connection: ${state}${detail ? ` (${detail})` : ""}`;
    log(`connection: ${state}${detail ? ` (${detail})` : ""}`);
  },
  onControlState(state: ControlState, detail?: string) {
    document.getElementById("ctrl")!.textContent = `control: ${state}${detail ? ` (${detail})` : ""}`;
    log(`control: ${state}${detail ? ` (${detail})` : ""}`);
  },
  onError(code: string, message: string) {
    log(`error: ${code} — ${message}`);
  },
};

const val = (id: string) => (document.getElementById(id) as HTMLInputElement).value;

document.getElementById("btnConnect")!.addEventListener("click", () => {
  w.setSession!(val("sessionId"), val("relayUrl"), val("accessToken"), val("deviceId"));
});
document.getElementById("btnDisconnect")!.addEventListener("click", () => {
  w.setSession!("", "", "", "");
});
document.getElementById("btnRequest")!.addEventListener("click", () => w.requestControl!());
document.getElementById("btnRelease")!.addEventListener("click", () => w.releaseControl!());
document.getElementById("btnSendText")!.addEventListener("click", () => {
  w.sendText!(val("text"));
  (document.getElementById("text") as HTMLInputElement).value = "";
});
for (const btn of Array.from(document.querySelectorAll<HTMLButtonElement>("button[data-key]"))) {
  btn.addEventListener("click", () => {
    const key = btn.dataset.key as SpecialKey;
    w.sendSpecialKey!(key);
  });
}
```

- [ ] **Step 3: Verify dev server starts (smoke check, ctrl-C after launch)**

```bash
cd android/terminal-web
timeout 5 npm run dev || true
```

Expected: Vite logs `Local: http://localhost:5173/dev.html` then exits (timeout). No build errors.

- [ ] **Step 4: Confirm typecheck still passes**

```bash
cd android/terminal-web
npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add android/terminal-web/dev.html android/terminal-web/src/entry/dev.ts
git commit -m "Add dev harness: dev.html form + entry/dev.ts wiring"
```

---

## Task 15: Vite production build verification

**Files:**
- (Build artifacts only — `dist/` is gitignored.)

- [ ] **Step 1: Run the production build**

```bash
cd android/terminal-web
rm -rf dist
npm run build
```

Expected: success, `dist/index.html` and `dist/assets/*.js`, `dist/assets/*.css` present. Bundle size < 500 kB total uncompressed.

- [ ] **Step 2: Verify `dev.html` is NOT in `dist/`**

```bash
test ! -f android/terminal-web/dist/dev.html && echo "OK: dev.html excluded" || (echo "FAIL: dev.html leaked into dist" && exit 1)
```

Expected: `OK: dev.html excluded`.

- [ ] **Step 3: Verify `dist/index.html` references only relative paths**

```bash
grep -E '(href|src)="(/|http)' android/terminal-web/dist/index.html && (echo "FAIL: absolute path found in dist/index.html" && exit 1) || echo "OK: only relative paths"
```

Expected: `OK: only relative paths`. (The `grep` must NOT find any matches.)

- [ ] **Step 4: Smoke-load `dist/index.html` in a browser**

```bash
cd android/terminal-web
# Serve the built bundle from a static server to mimic file:// loading.
npx serve -p 5174 dist &
sleep 2
curl -s -o /tmp/index.html http://localhost:5174/index.html
grep -q "TermixBridge\|terminal" /tmp/index.html && echo "OK" || (echo "FAIL" && exit 1)
kill %1
```

Expected: `OK`. Page loads, terminal container present.

- [ ] **Step 5: Commit (no files to add — verification only)**

```bash
git status --short
# Expected: working tree clean. Move to Task 16.
```

If accidentally `dist/` got staged, `git restore --staged android/terminal-web/dist`.

---

## Task 16: README and manual smoke checklist

**Files:**
- Create: `android/terminal-web/README.md`

The README captures how to run the dev stack so the §6c smoke test (spec) is reproducible by anyone.

- [ ] **Step 1: Create `android/terminal-web/README.md`**

`android/terminal-web/README.md`:

````markdown
# `android/terminal-web`

Static-asset bundle that the Termix Android Compose shell loads inside a WebView. It owns the WSS protocol, terminal rendering (xterm.js), and the JS bridge contract used by the native shell.

See `docs/superpowers/specs/2026-04-25-android-terminal-web-mvp-design.md` for the full design.

## Commands

```bash
npm install        # one-time
npm run dev        # opens http://localhost:5173/dev.html with hot reload
npm test           # vitest run (unit tests)
npm run build      # produces dist/ for the Compose shell to copy in
npm run typecheck  # tsc --noEmit
```

## Manual smoke checklist (spec §6c)

1. From the repo root, start the Go stack against the running Postgres test container:

   ```bash
   export TERMIX_TEST_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:55432/termix?sslmode=disable"
   cd go
   go run ./cmd/termix-control &
   go run ./cmd/termix-relay &
   go run ./cmd/termixd &
   ```

2. Log in and start a session via the CLI:

   ```bash
   ./bin/termix login
   ./bin/termix start claude --name "smoke"
   ./bin/termix sessions list
   ```

   Note the `session_id`, the relay URL, and your access token (from `~/.config/termix/credentials.json` or the daemon log).

3. In another shell, run the dev harness:

   ```bash
   cd android/terminal-web
   npm run dev
   ```

4. In the browser tab that opens (`dev.html`), paste session_id, relay URL, access token, and device_id. Click **Connect**. Expected:
   - Status panel: `connection: connected`.
   - Snapshot of the existing terminal renders.

5. Click **Request Control**. Expected: `control: granted` within ~1 s.

6. Type a command (e.g. `echo hi`) into the **Send Text** box, click **Enter**. Expected: command echoes in the terminal output stream.

7. Click **Release Control**. Expected: `control: none`. Subsequent **Send Text** clicks produce no output (input gate).
````

- [ ] **Step 2: Commit**

```bash
git add android/terminal-web/README.md
git commit -m "Document terminal-web dev workflow and manual smoke checklist"
```

---

## Task 17: Run the manual smoke checklist and update PROGRESS.md

**Files:**
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Run the smoke checklist from the README**

Execute every step in `android/terminal-web/README.md` § "Manual smoke checklist". Record any deviations or issues observed; if a step fails, stop and fix the underlying code rather than working around it.

- [ ] **Step 2: Update `docs/PROGRESS.md`**

Move the in-progress bullet to Completed and remove the matching Pending entry. Mark slice 1 implementation complete.

Concrete edits (apply each via Edit tool):

1. Under `## Completed`, append:

   ```
   - [x] Implement Android slice 1: `terminal-web` MVP per `docs/superpowers/plans/2026-04-25-android-terminal-web-mvp.md` (Vite + TS + xterm.js bundle, JS bridge, WSS protocol, control-lease state machine, dev harness, full unit-test coverage).
   ```

2. Under `## In Progress`, remove the line:

   ```
   - [ ] Android slice 1 (`terminal-web` MVP): spec written; awaiting user spec review before plan write-up.
   ```

   Replace it with `- [ ] No active in-progress tasks.` if the in-progress list would otherwise be empty *for the Android track*. (Devbox container task remains.)

3. Under `## Pending`, remove the lines:

   ```
   - [ ] Write the Android slice 1 (`terminal-web` MVP) implementation plan (after user approves the design doc).
   - [ ] Implement Android slice 1: `terminal-web` MVP (per the approved plan).
   ```

4. Under `## Next Up`, replace items 2–4 with:

   ```
   2. Brainstorm and design Android slice 2: `android/app/` Kotlin+Compose shell (login, session list, WebView host, toolbar, reconnect).
   3. Implement Android slice 2: Kotlin+Compose shell.
   ```

   Renumber subsequent items.

- [ ] **Step 3: Commit**

```bash
git add docs/PROGRESS.md
git commit -m "Mark Android slice 1 (terminal-web MVP) complete in PROGRESS.md"
```

- [ ] **Step 4: Verification — full test + build pass**

```bash
cd android/terminal-web
npm test && npm run build && echo "ALL GREEN"
```

Expected: tests green, build green, `ALL GREEN` printed.

---

## Self-review notes (for the implementer / reviewer)

- Test count expectation: ~65 unit tests across `protocol/`, `net/`, `bridge/`, `session/` modules.
- Type-only files are not directly tested; their consumers exercise them.
- The `dist/` directory must remain gitignored (`.gitignore` from Task 1 covers this).
- `happy-dom` provides `URL`, `TextEncoder`, `TextDecoder`, and `localStorage` by default; no test-environment setup beyond `vite.config.ts`.
