import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

describe("i18n store", () => {
  beforeEach(() => {
    vi.resetModules();
    localStorage.clear();
    Object.defineProperty(navigator, "language", {
      value: "en-US",
      configurable: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("defaults to English for an English browser", async () => {
    const { locale, t } = await import("./store");
    expect(locale.value).toBe("en");
    expect(t("nav.signIn")).toBe("Sign in");
  });

  it("defaults to Chinese for a Chinese browser", async () => {
    Object.defineProperty(navigator, "language", {
      value: "zh-CN",
      configurable: true,
    });
    const { locale, t } = await import("./store");
    expect(locale.value).toBe("zh-CN");
    expect(t("nav.signIn")).toBe("登录");
  });

  it("persists explicit locale selection", async () => {
    const { locale, setLocale, t } = await import("./store");
    setLocale("zh-CN");
    expect(locale.value).toBe("zh-CN");
    expect(localStorage.getItem("termix.locale")).toBe("zh-CN");
    expect(t("nav.help")).toBe("帮助");
  });

  it("falls back to English for an unsupported saved locale", async () => {
    localStorage.setItem("termix.locale", "fr");
    Object.defineProperty(navigator, "language", {
      value: "zh-CN",
      configurable: true,
    });
    const { locale, t } = await import("./store");
    expect(locale.value).toBe("en");
    expect(t("common.refresh")).toBe("Refresh");
  });

  it("falls back to browser locale when saved locale lookup fails", async () => {
    Object.defineProperty(navigator, "language", {
      value: "zh-CN",
      configurable: true,
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("storage unavailable");
    });

    const { locale, t } = await import("./store");

    expect(locale.value).toBe("zh-CN");
    expect(t("nav.help")).toBe("帮助");
  });

  it("updates in-memory locale when persistence fails", async () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("storage unavailable");
    });
    const { locale, setLocale, t } = await import("./store");

    expect(() => setLocale("zh-CN")).not.toThrow();
    expect(locale.value).toBe("zh-CN");
    expect(t("nav.help")).toBe("帮助");
  });
});
