import { describe, it, expect, beforeEach } from "vitest";
import { accessToken, accessTokenExpiresAt, userInfo, clearAuth } from "./store";

describe("auth/store", () => {
  beforeEach(() => clearAuth());

  it("starts empty", () => {
    expect(accessToken.value).toBeNull();
    expect(accessTokenExpiresAt.value).toBe(0);
    expect(userInfo.value).toBeNull();
  });

  it("clearAuth resets all signals", () => {
    accessToken.value = "abc";
    accessTokenExpiresAt.value = Date.now() + 60_000;
    userInfo.value = {
      user: { id: "u", email: "a@b", display_name: "A", role: "user" },
      device: { id: "d", device_type: "web", platform: "web", label: "ua" },
    };
    clearAuth();
    expect(accessToken.value).toBeNull();
    expect(accessTokenExpiresAt.value).toBe(0);
    expect(userInfo.value).toBeNull();
  });
});
