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
      payload: { session_id: "s1", lease_version: 2, expires_at: "2026-04-25T10:01:30Z", controller_device_id: "dev-1" },
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
    ["Enter",     [0x0d]],
    ["Tab",       [0x09]],
    ["Escape",    [0x1b]],
    ["Up",        [0x1b, 0x5b, 0x41]],
    ["Down",      [0x1b, 0x5b, 0x42]],
    ["Right",     [0x1b, 0x5b, 0x43]],
    ["Left",      [0x1b, 0x5b, 0x44]],
    ["C-c",       [0x03]],
    ["C-d",       [0x04]],
    ["C-j",       [0x0a]],
    ["Backspace", [0x7f]],
  ] as const)("encodes %s correctly", (key, expected) => {
    expect(Array.from(encodeSpecialKey(key))).toEqual(expected);
  });
});
