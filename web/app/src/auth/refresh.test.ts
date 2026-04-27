import { describe, it, expect, beforeEach, vi } from "vitest";
import { accessToken, accessTokenExpiresAt, clearAuth } from "./store";
import { doRefreshOnce, freshAccessToken, __resetInflight } from "./refresh";

describe("auth/refresh", () => {
  beforeEach(() => {
    clearAuth();
    __resetInflight();
  });

  it("ten concurrent calls coalesce to one fetch", async () => {
    const fetchSpy = vi.fn(async () => new Response(JSON.stringify({
      access_token: "new-tok",
      expires_in_seconds: 1800,
    }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchSpy);

    const tokens = await Promise.all(Array.from({ length: 10 }, () => doRefreshOnce()));
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(tokens.every(t => t === "new-tok")).toBe(true);
    expect(accessToken.value).toBe("new-tok");

    vi.unstubAllGlobals();
  });

  it("returns null on refresh failure (401)", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 401 })));
    const t = await doRefreshOnce();
    expect(t).toBeNull();
    expect(accessToken.value).toBeNull();
    vi.unstubAllGlobals();
  });

  it("returns null when fetch throws", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new TypeError("offline"); }));
    const t = await doRefreshOnce();
    expect(t).toBeNull();
    vi.unstubAllGlobals();
  });

  it("freshAccessToken returns cached token if not near expiry", async () => {
    accessToken.value = "still-good";
    accessTokenExpiresAt.value = Date.now() + 5 * 60_000; // 5 min — well outside 60s threshold
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    const t = await freshAccessToken();
    expect(t).toBe("still-good");
    expect(fetchSpy).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("freshAccessToken triggers refresh if within 60s of expiry", async () => {
    accessToken.value = "stale";
    accessTokenExpiresAt.value = Date.now() + 3_000; // 3s -> within both 5s and 60s thresholds → forces fetch
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({
      access_token: "rotated",
      expires_in_seconds: 1800,
    }), { status: 200, headers: { "content-type": "application/json" } })));
    const t = await freshAccessToken();
    expect(t).toBe("rotated");
    vi.unstubAllGlobals();
  });

  it("inflight is cleared after resolution so a second refresh runs a fresh fetch", async () => {
    let callCount = 0;
    vi.stubGlobal("fetch", vi.fn(async () => {
      callCount++;
      return new Response(JSON.stringify({
        access_token: `tok-${callCount}`,
        expires_in_seconds: 1800,
      }), { status: 200, headers: { "content-type": "application/json" } });
    }));

    // First refresh → resolves to "tok-1"
    const first = await doRefreshOnce();
    expect(first).toBe("tok-1");

    // Set expiry to now (expired) so doRefreshOnce will trigger a real refresh
    accessTokenExpiresAt.value = Date.now();

    const second = await doRefreshOnce();
    expect(second).toBe("tok-2");
    expect(callCount).toBe(2);

    vi.unstubAllGlobals();
  });
});
