import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/preact";

import { Header } from "./header";
import { setLocale } from "../i18n/store";
import { userInfo } from "../auth/store";

describe("Header", () => {
  beforeEach(() => {
    cleanup();
    setLocale("en");
    userInfo.value = null;
  });

  it("uses the real app icon asset for the brand mark", () => {
    const { container } = render(<Header />);

    const icon = container.querySelector(".brand-mark");
    expect(icon?.getAttribute("src")).toBe("/icons/termix.svg?v=tmx");

    const svg = readFileSync(resolve(process.cwd(), "public/icons/termix.svg"), "utf8");
    expect(svg).toContain("<svg");
    expect(svg).toContain('id="termix-tmx-logo"');
    expect(svg).toContain("TMX");
    expect(svg).not.toContain(">_");
  });

  it("keeps the browser tab icon aligned with the header app icon", () => {
    const html = readFileSync(resolve(process.cwd(), "index.html"), "utf8");

    expect(html).toContain('<link rel="icon" type="image/svg+xml" href="/icons/termix.svg?v=tmx">');
  });

  it("exposes Help directly instead of hiding it in the account menu", () => {
    const onHelp = vi.fn();
    render(<Header onHelp={onHelp} />);

    fireEvent.click(screen.getByRole("button", { name: "Help" }));

    expect(onHelp).toHaveBeenCalledOnce();
  });

  it("opens account menu with language and logout actions", () => {
    const onLogout = vi.fn();
    render(<Header onLogout={onLogout} />);

    const accountMenu = screen.getByRole("button", { name: "Account menu" });
    expect(accountMenu.getAttribute("aria-haspopup")).toBe("menu");
    expect(accountMenu.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(accountMenu);
    expect(accountMenu.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByRole("menuitem", { name: "Language: English" })).toBeTruthy();
    expect(screen.getAllByRole("menuitem")).toHaveLength(2);

    fireEvent.click(screen.getByRole("menuitem", { name: "Language: English" }));
    expect(screen.getByRole("menuitem", { name: "语言：中文" })).toBeTruthy();

    fireEvent.click(screen.getByRole("menuitem", { name: "退出登录" }));
    expect(onLogout).toHaveBeenCalledOnce();
  });

  it("keeps the account menu email label presentational", () => {
    userInfo.value = {
      user: { id: "u1", email: "user@example.com", display_name: "User", role: "user" },
      device: { id: "d1", device_type: "web", platform: "web", label: "browser" },
    };

    render(<Header onLogout={() => {}} />);

    fireEvent.click(screen.getByRole("button", { name: "Account menu" }));

    const menuLabel = document.querySelector(".menu-label");
    expect(menuLabel?.textContent).toBe("user@example.com");
    expect(menuLabel?.getAttribute("role")).toBe("presentation");
    expect(screen.getAllByRole("menuitem")).toHaveLength(2);
  });

  it("bounds the account menu width so long email labels can ellipsize", () => {
    const css = readFileSync(resolve(process.cwd(), "src/theme/styles.css"), "utf8");

    expect(css).toContain("width: min(280px, calc(100vw - 32px));");
    expect(css).toContain("max-width: calc(100vw - 32px);");
    expect(css).toContain("min-width: 0;");
    expect(css).toContain("text-overflow: ellipsis;");
    expect(css).toContain("white-space: nowrap;");
  });

  it("closes the account menu with Escape and outside clicks", () => {
    const onLogout = vi.fn();
    render(<Header onLogout={onLogout} />);

    const accountMenu = screen.getByRole("button", { name: "Account menu" });
    fireEvent.click(accountMenu);
    expect(screen.getByRole("menu")).toBeTruthy();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(accountMenu.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(accountMenu);
    expect(screen.getByRole("menu")).toBeTruthy();

    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(accountMenu.getAttribute("aria-expanded")).toBe("false");
  });

  it("keeps refresh as a direct action when provided", () => {
    const onRefresh = vi.fn();
    render(<Header onRefresh={onRefresh} />);

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    expect(onRefresh).toHaveBeenCalledOnce();
  });
});
