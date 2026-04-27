import { describe, it, expect, beforeEach, vi } from "vitest";
import { accessToken, accessTokenExpiresAt, clearAuth } from "./store";
import { splashing } from "../app/store";
import { bootstrap } from "./bootstrap";

describe("auth/bootstrap", () => {
  beforeEach(() => {
    clearAuth();
    splashing.value = true;
  });

  it("sets accessToken and clears splash on 200", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({
      access_token: "boot-tok",
      expires_in_seconds: 1800,
    }), { status: 200, headers: { "content-type": "application/json" } })));
    const result = await bootstrap();
    expect(result).toBe("authed");
    expect(accessToken.value).toBe("boot-tok");
    expect(accessTokenExpiresAt.value).toBeGreaterThan(Date.now());
    expect(splashing.value).toBe(false);
    vi.unstubAllGlobals();
  });

  it("returns 'unauthed' and clears splash on 401", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 401 })));
    const result = await bootstrap();
    expect(result).toBe("unauthed");
    expect(accessToken.value).toBeNull();
    expect(splashing.value).toBe(false);
    vi.unstubAllGlobals();
  });

  it("returns 'network-error' and clears splash on fetch throw", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new TypeError("offline"); }));
    const result = await bootstrap();
    expect(result).toBe("network-error");
    expect(accessToken.value).toBeNull();
    expect(splashing.value).toBe(false);
    vi.unstubAllGlobals();
  });

  it("splash is reset to true at the start of bootstrap", async () => {
    splashing.value = false; // simulate a re-bootstrap edge case
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 401 })));
    const p = bootstrap();
    // splashing should flip to true synchronously (before await)
    expect(splashing.value).toBe(true);
    await p;
    expect(splashing.value).toBe(false);
    vi.unstubAllGlobals();
  });
});
