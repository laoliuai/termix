import { describe, it, expect } from "vitest";
import type { SessionSnapshotReadyPayload } from "./types";

describe("SessionSnapshotReadyPayload", () => {
  it("carries authoritative cols/rows/generation as optional fields", () => {
    // Compile-time contract: the authoritative-size fields added in Stage 2
    // must exist as optional members. This object would not type-check if
    // cols/rows/generation were absent from the interface.
    const payload: SessionSnapshotReadyPayload = {
      session_id: "s1",
      total_chunks: 3,
      cols: 220,
      rows: 50,
      generation: 7,
    };
    expect(payload.cols).toBe(220);
    expect(payload.rows).toBe(50);
    expect(payload.generation).toBe(7);
  });

  it("still accepts a minimal payload (fields are optional)", () => {
    const payload: SessionSnapshotReadyPayload = { session_id: "s2" };
    expect(payload.cols).toBeUndefined();
    expect(payload.rows).toBeUndefined();
    expect(payload.generation).toBeUndefined();
  });
});
