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
