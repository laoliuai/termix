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
