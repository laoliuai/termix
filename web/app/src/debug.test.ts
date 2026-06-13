import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { applyDebugParam, isDebugEnabled, setDebugEnabled } from "./debug";

describe("debug toggle", () => {
  beforeEach(() => localStorage.removeItem("termix_debug"));
  afterEach(() => localStorage.removeItem("termix_debug"));

  it("defaults to off", () => {
    expect(isDebugEnabled()).toBe(false);
  });

  it("setDebugEnabled flips the flag both ways", () => {
    setDebugEnabled(true);
    expect(isDebugEnabled()).toBe(true);
    setDebugEnabled(false);
    expect(isDebugEnabled()).toBe(false);
  });

  it("applyDebugParam enables on ?debug=1", () => {
    applyDebugParam("?debug=1");
    expect(isDebugEnabled()).toBe(true);
  });

  it("applyDebugParam disables on ?debug=0", () => {
    setDebugEnabled(true);
    applyDebugParam("?debug=0");
    expect(isDebugEnabled()).toBe(false);
  });

  it("applyDebugParam leaves the flag unchanged when the param is absent", () => {
    setDebugEnabled(true);
    applyDebugParam("?foo=bar");
    expect(isDebugEnabled()).toBe(true);
  });
});
