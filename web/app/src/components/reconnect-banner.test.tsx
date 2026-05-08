import { describe, it, expect, beforeEach } from "vitest";
import { render, screen } from "@testing-library/preact";
import { ReconnectBanner } from "./reconnect-banner";
import { setLocale } from "@/i18n/store";

describe("ReconnectBanner", () => {
  beforeEach(() => {
    localStorage.clear();
    setLocale("en");
  });

  it("renders nothing when not reconnecting", () => {
    const { container } = render(<ReconnectBanner phase="connected" attempt={0} />);
    expect(container.textContent ?? "").toBe("");
  });

  it("shows the attempt counter when reconnecting", () => {
    render(<ReconnectBanner phase="reconnecting" attempt={3} />);
    expect(screen.getByText(/Reconnecting/)).toBeTruthy();
    expect(screen.getByText(/3/)).toBeTruthy();
  });

  it("renders Chinese copy when locale is zh", () => {
    setLocale("zh-CN");
    render(<ReconnectBanner phase="reconnecting" attempt={2} />);
    expect(screen.getByText(/重新连接中/)).toBeTruthy();
  });
});
