import { describe, it, expect, vi, beforeEach } from "vitest";
import { signal } from "@preact/signals";
import { createReconnectSupervisor } from "./reconnect";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe("createReconnectSupervisor", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("starts in connecting and transitions to connected on success", async () => {
    const state = signal<{ phase: string; attempt: number }>({ phase: "", attempt: 0 });
    const sup = createReconnectSupervisor({
      connect: async () => ({ disconnect: () => {} }),
      refreshToken: async () => "tok",
      onStateChange: (s) => (state.value = s),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    await sleep(10);
    expect(state.value.phase).toBe("connected");
  });

  it("retries with backoff after disconnect and increments attempt", async () => {
    vi.useFakeTimers();
    let calls = 0;
    const seenPhases: string[] = [];
    const sup = createReconnectSupervisor({
      connect: async () => {
        calls++;
        return {
          disconnect: () => {},
          // first call: simulate immediate close
          onCloseTrigger: calls === 1 ? () => sup.signalClose(new Error("server EOF")) : undefined,
        };
      },
      refreshToken: async () => "tok",
      onStateChange: (s) => seenPhases.push(s.phase),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    await vi.runAllTimersAsync();
    expect(calls).toBeGreaterThanOrEqual(2);
    expect(seenPhases).toContain("reconnecting");
    expect(seenPhases).toContain("connected");
  });

  it("transitions to gave-up after 5 minutes of failed attempts", async () => {
    vi.useFakeTimers();
    let phase = "";
    const sup = createReconnectSupervisor({
      connect: async () => {
        throw new Error("ECONNREFUSED");
      },
      refreshToken: async () => "tok",
      onStateChange: (s) => (phase = s.phase),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    // Advance 5 minutes + 1 second
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000 + 1000);
    expect(phase).toBe("gave-up");
  });

  it("retry() resets to reconnecting and tries again", async () => {
    vi.useFakeTimers();
    const startedAt = Date.now();
    let calls = 0;
    let phase = "";
    const sup = createReconnectSupervisor({
      connect: async () => {
        calls++;
        // Fail throughout the initial 5-minute window so we hit gave-up,
        // then succeed once retry() drives time past the deadline.
        if (Date.now() < startedAt + 5 * 60 * 1000) throw new Error("nope");
        return { disconnect: () => {} };
      },
      refreshToken: async () => "tok",
      onStateChange: (s) => (phase = s.phase),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000 + 1000);
    expect(phase).toBe("gave-up");

    sup.retry();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(phase).toBe("connected");
    expect(calls).toBeGreaterThanOrEqual(3);
  });
});
