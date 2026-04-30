import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/preact";

import { HelpPage } from "./help";

describe("HelpPage", () => {
  beforeEach(() => {
    cleanup();
  });

  it("shows install command and platform downloads", () => {
    render(<HelpPage onBack={() => {}} />);

    expect(screen.getByRole("heading", { name: "Install Termix" })).toBeTruthy();
    expect(screen.getByRole("link", { name: /macOS Apple Silicon/ }).getAttribute("href")).toContain("termix_Darwin_arm64.tar.gz");
    expect(screen.getByRole("link", { name: /macOS Intel/ }).getAttribute("href")).toContain("termix_Darwin_x86_64.tar.gz");
    expect(screen.getByRole("link", { name: /Ubuntu x86_64/ }).getAttribute("href")).toContain("termix_Linux_x86_64.tar.gz");
    expect(screen.getByRole("link", { name: /Ubuntu arm64/ }).getAttribute("href")).toContain("termix_Linux_arm64.tar.gz");
    expect(screen.getByText(/curl -fsSL https:\/\/raw\.githubusercontent\.com\/termix\/termix\/main\/install\.sh \| sh/)).toBeTruthy();
  });

  it("shows the host workflow without mentioning termixd", () => {
    const { container } = render(<HelpPage onBack={() => {}} />);

    expect(screen.getByText("termix login")).toBeTruthy();
    expect(screen.getByText("termix start codex --name laoliu-codex-termix")).toBeTruthy();
    expect(screen.getByText("claude")).toBeTruthy();
    expect(screen.getByText("codex")).toBeTruthy();
    expect(screen.getByText("opencode")).toBeTruthy();
    expect(container.textContent).not.toContain("termixd");
  });

  it("calls onBack from the back button", () => {
    const onBack = vi.fn();
    render(<HelpPage onBack={onBack} />);

    fireEvent.click(screen.getByRole("button", { name: "Back" }));

    expect(onBack).toHaveBeenCalledOnce();
  });
});
