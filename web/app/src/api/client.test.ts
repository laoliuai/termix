import { describe, it, expect, beforeEach, vi } from "vitest";
import { accessToken, accessTokenExpiresAt, clearAuth } from "../auth/store";
import { __resetInflight } from "../auth/refresh";
import { fetchWithAuth } from "./client";

describe("api/client", () => {
  beforeEach(() => {
    clearAuth();
    __resetInflight();
  });

  it("attaches Authorization header from accessToken signal", async () => {
    accessToken.value = "tk-1";
    const fetchSpy = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response("ok", { status: 200 }));
    vi.stubGlobal("fetch", fetchSpy);

    await fetchWithAuth("/api/v1/sessions");
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe("Bearer tk-1");
    vi.unstubAllGlobals();
  });

  it("does not attach Authorization when accessToken is null", async () => {
    const fetchSpy = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response("ok", { status: 200 }));
    vi.stubGlobal("fetch", fetchSpy);

    await fetchWithAuth("/api/v1/sessions");
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    const headers = (init.headers ?? {}) as Record<string, string>;
    expect(headers["Authorization"]).toBeUndefined();
    vi.unstubAllGlobals();
  });

  it("on 401 runs single-flight refresh and retries once", async () => {
    accessToken.value = "stale";
    accessTokenExpiresAt.value = Date.now() + 60_000;  // outside 60s threshold so freshAccessToken would NOT refresh on its own
    const calls: Array<{ url: string; auth?: string }> = [];
    const fetchSpy = vi.fn(async (url: string, init?: RequestInit) => {
      const auth = (init?.headers as any)?.["Authorization"];
      calls.push({ url, auth });
      if (url === "/api/v1/auth/refresh") {
        return new Response(JSON.stringify({ access_token: "fresh", expires_in_seconds: 1800 }),
          { status: 200, headers: { "content-type": "application/json" } });
      }
      // first call to /sessions has stale token → 401; second has fresh → 200
      if (auth === "Bearer stale") return new Response("", { status: 401 });
      return new Response("ok", { status: 200 });
    });
    vi.stubGlobal("fetch", fetchSpy);

    const res = await fetchWithAuth("/api/v1/sessions");
    expect(res.status).toBe(200);
    expect(calls.map(c => c.url)).toEqual([
      "/api/v1/sessions",
      "/api/v1/auth/refresh",
      "/api/v1/sessions",
    ]);
    expect(accessToken.value).toBe("fresh");
    vi.unstubAllGlobals();
  });

  it("on 401 + refresh failure, surfaces original 401", async () => {
    accessToken.value = "stale";
    accessTokenExpiresAt.value = Date.now() + 60_000;
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/v1/auth/refresh") return new Response("", { status: 401 });
      return new Response("", { status: 401 });
    }));
    const res = await fetchWithAuth("/api/v1/sessions");
    expect(res.status).toBe(401);
    vi.unstubAllGlobals();
  });

  it("non-401 responses pass through without refresh attempt", async () => {
    accessToken.value = "tk";
    const fetchSpy = vi.fn(async () => new Response("server error", { status: 500 }));
    vi.stubGlobal("fetch", fetchSpy);
    const res = await fetchWithAuth("/api/v1/sessions");
    expect(res.status).toBe(500);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
  });
});
