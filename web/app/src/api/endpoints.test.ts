import { describe, it, expect, beforeEach, vi } from "vitest";
import { login, listSessions, logout } from "./endpoints";
import { accessToken, clearAuth } from "../auth/store";
import { __resetInflight } from "../auth/refresh";

describe("api/endpoints", () => {
  beforeEach(() => {
    clearAuth();
    __resetInflight();
  });

  it("login posts cookie_mode=true with device_type=web", async () => {
    const fetchSpy = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({
      user: { id: "u1", email: "a@b", display_name: "A", role: "user" },
      device: { id: "d1", device_type: "web", platform: "web", label: "ua" },
      access_token: "t",
      expires_in_seconds: 1800,
    }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchSpy);

    const res = await login({ email: "a@b", password: "pw", device_label: "ua" });
    expect(res.ok).toBe(true);

    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    const body = JSON.parse(init.body as string);
    expect(body.device_type).toBe("web");
    expect(body.platform).toBe("web");
    expect(body.cookie_mode).toBe(true);
    expect(body.device_label).toBe("ua");
    expect(body.email).toBe("a@b");
    expect(body.password).toBe("pw");
    expect((init.headers as any)["Content-Type"]).toBe("application/json");
    vi.unstubAllGlobals();
  });

  it("login surfaces 401 with body.error", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ error: "invalid credentials" }),
      { status: 401, headers: { "content-type": "application/json" } })));
    const res = await login({ email: "a@b", password: "x", device_label: "ua" });
    expect(res.ok).toBe(false);
    if (!res.ok) {
      expect(res.status).toBe(401);
      expect(res.message).toBe("invalid credentials");
    }
    vi.unstubAllGlobals();
  });

  it("login network error returns status=0", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new TypeError("offline"); }));
    const res = await login({ email: "a@b", password: "x", device_label: "ua" });
    expect(res.ok).toBe(false);
    if (!res.ok) {
      expect(res.status).toBe(0);
    }
    vi.unstubAllGlobals();
  });

  it("listSessions sends Authorization and parses sessions array", async () => {
    accessToken.value = "tk";
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      const auth = (init?.headers as any)?.["Authorization"];
      expect(auth).toBe("Bearer tk");
      expect(url).toContain("/api/v1/sessions?status=running");
      return new Response(JSON.stringify({
        sessions: [{
          id: "s1", user_id: "u1", device_id: "d1",
          tool: "claude", name: "x", status: "running",
        }],
      }), { status: 200, headers: { "content-type": "application/json" } });
    }));

    const list = await listSessions("running");
    expect(list).toHaveLength(1);
    expect(list[0].tool).toBe("claude");
    vi.unstubAllGlobals();
  });

  it("listSessions throws on non-200", async () => {
    accessToken.value = "tk";
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 500 })));
    await expect(listSessions()).rejects.toThrow(/list sessions failed: 500/);
    vi.unstubAllGlobals();
  });

  it("logout swallows network errors silently", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("offline"); }));
    await expect(logout()).resolves.toBeUndefined();
    vi.unstubAllGlobals();
  });

  it("logout posts to /api/v1/auth/logout", async () => {
    const fetchSpy = vi.fn(async () => new Response("", { status: 204 }));
    vi.stubGlobal("fetch", fetchSpy);
    await logout();
    expect(fetchSpy).toHaveBeenCalledWith("/api/v1/auth/logout", expect.objectContaining({ method: "POST" }));
    vi.unstubAllGlobals();
  });
});
