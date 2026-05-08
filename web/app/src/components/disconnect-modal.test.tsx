import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/preact";
import { DisconnectModal } from "./disconnect-modal";
import { setLocale } from "@/i18n/store";

const sampleProps = {
  open: true,
  serverUrl: "https://termix.cloud",
  attempts: 14,
  durationMs: 323_000,
  lastError: "broken pipe",
  onReload: vi.fn(),
  onRetry: vi.fn(),
};

describe("DisconnectModal", () => {
  beforeEach(() => {
    cleanup();
    localStorage.clear();
    setLocale("en");
    sampleProps.onReload.mockClear();
    sampleProps.onRetry.mockClear();
  });

  it("renders nothing when open is false", () => {
    const { container } = render(<DisconnectModal {...sampleProps} open={false} />);
    expect(container.textContent ?? "").toBe("");
  });

  it("shows attempts, duration, and a reload button", () => {
    render(<DisconnectModal {...sampleProps} />);
    expect(screen.getByText(/14/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Reload page/ })).toBeTruthy();
  });

  it("calls onReload when reload is clicked", () => {
    render(<DisconnectModal {...sampleProps} />);
    fireEvent.click(screen.getByRole("button", { name: /Reload page/ }));
    expect(sampleProps.onReload).toHaveBeenCalledTimes(1);
  });

  it("calls onRetry when retry is clicked", () => {
    render(<DisconnectModal {...sampleProps} />);
    fireEvent.click(screen.getByRole("button", { name: /Retry connection/ }));
    expect(sampleProps.onRetry).toHaveBeenCalledTimes(1);
  });

  it("renders Chinese copy when locale is zh", () => {
    setLocale("zh-CN");
    render(<DisconnectModal {...sampleProps} />);
    expect(screen.getByText(/连接断开/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /重新加载/ })).toBeTruthy();
  });

  it("shows lastError in details when no attemptHistory is provided", () => {
    render(<DisconnectModal {...sampleProps} />);
    fireEvent.click(screen.getByRole("button", { name: /Show details/ }));
    expect(screen.getByText(/broken pipe/)).toBeTruthy();
  });

  it("shows attemptHistory entries when provided and details are open", () => {
    const history = [
      { at: new Date("2026-05-08T10:01:02Z"), error: "ws close" },
      { at: new Date("2026-05-08T10:02:15Z"), error: "ws error before open" },
      { at: new Date("2026-05-08T10:03:45Z"), error: "refresh failed" },
    ];
    render(<DisconnectModal {...sampleProps} attemptHistory={history} />);
    fireEvent.click(screen.getByRole("button", { name: /Show details/ }));
    const list = screen.getByRole("list", { name: /attempt history/ });
    expect(list).toBeTruthy();
    expect(list.querySelectorAll("li")).toHaveLength(3);
    expect(list.textContent).toMatch(/ws close/);
    expect(list.textContent).toMatch(/ws error before open/);
    expect(list.textContent).toMatch(/refresh failed/);
  });

  it("falls back to lastError when attemptHistory is empty", () => {
    render(<DisconnectModal {...sampleProps} attemptHistory={[]} />);
    fireEvent.click(screen.getByRole("button", { name: /Show details/ }));
    expect(screen.getByText(/broken pipe/)).toBeTruthy();
  });
});
