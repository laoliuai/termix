import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/preact";

import { HomePage } from "./home";
import { accessToken, clearAuth } from "../auth/store";
import { setLocale } from "../i18n/store";

const installCommand = "curl -fsSL https://termix.cloud/install.sh | sh";

describe("HomePage", () => {
  beforeEach(() => {
    cleanup();
    clearAuth();
    localStorage.clear();
    setLocale("en");
  });

  it("renders logged-out product positioning, install command, long-running scripts copy, and sign-in action", () => {
    render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={() => {}} />);

    expect(screen.getByRole("heading", { name: "Take over host AI coding sessions from your browser or phone" })).toBeTruthy();
    expect(screen.getByText(/long-running scripts/i)).toBeTruthy();
    expect(screen.getAllByText(installCommand).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Install Termix" })).toBeTruthy();
  });

  it("shows Open Sessions CTA when already authenticated", () => {
    const onSessions = vi.fn();
    accessToken.value = "tk";

    render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={onSessions} />);

    fireEvent.click(screen.getAllByRole("button", { name: "Open Sessions" })[0]);

    expect(onSessions).toHaveBeenCalledTimes(1);
  });

  it("labels the language switcher for assistive technology", () => {
    render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={() => {}} />);

    expect(screen.getByRole("button", { name: "Language" })).toBeTruthy();
  });

  it("calls help and login callbacks from header actions", () => {
    const onHelp = vi.fn();
    const onLogin = vi.fn();

    render(<HomePage onLogin={onLogin} onHelp={onHelp} onSessions={() => {}} />);

    fireEvent.click(screen.getByRole("button", { name: "Help" }));
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(onHelp).toHaveBeenCalledTimes(1);
    expect(onLogin).toHaveBeenCalledTimes(1);
  });

  it("renders the footer with contact email and owner avatar, no GitHub link", () => {
    const { container } = render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={() => {}} />);

    const email = screen.getByRole("link", { name: /liujia\.gl@gmail\.com/ });
    expect(email.getAttribute("href")).toBe("mailto:liujia.gl@gmail.com");

    const avatar = container.querySelector(".site-footer-avatar") as HTMLImageElement | null;
    expect(avatar).toBeTruthy();
    expect(avatar?.getAttribute("src")).toBe("/icons/owner-avatar-96.png");

    expect(screen.queryByText(/github\.com\/laoliuai\/termix/)).toBeNull();
  });

  it("renders Chinese copy after locale switch", () => {
    setLocale("zh-CN");

    render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={() => {}} />);

    expect(screen.getByRole("heading", { name: "用浏览器或手机接管主机上的 AI coding session" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "登录" })).toBeTruthy();
  });
});
