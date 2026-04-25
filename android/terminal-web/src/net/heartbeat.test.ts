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
