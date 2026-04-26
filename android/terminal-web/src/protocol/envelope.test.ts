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

  it("treats omitted request_id as null (relay omits it via json:omitempty)", () => {
    const text = JSON.stringify({ type: "session.joined", payload: { session_id: "s1" } });
    const env = decodeEnvelope(text);
    expect(env.request_id).toBeNull();
    expect(env.type).toBe("session.joined");
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
