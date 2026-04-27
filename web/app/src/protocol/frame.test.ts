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
