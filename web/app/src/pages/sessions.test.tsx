import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, cleanup } from "@testing-library/preact";

// Mock API endpoints BEFORE importing the page
vi.mock("../api/endpoints", () => ({
  listSessions: vi.fn(),
  logout: vi.fn(),
}));

import { SessionsPage } from "./sessions";
import { accessToken, userInfo, clearAuth } from "../auth/store";
import { snackbar } from "../app/store";
import { listSessions, logout } from "../api/endpoints";

const mockList = listSessions as unknown as ReturnType<typeof vi.fn>;
const mockLogout = logout as unknown as ReturnType<typeof vi.fn>;

describe("SessionsPage", () => {
  beforeEach(() => {
    cleanup();
    clearAuth();
    accessToken.value = "tk";
    userInfo.value = {
      user: { id: "u", email: "a@b", display_name: "A", role: "user" },
      device: { id: "d", device_type: "web", platform: "web", label: "ua" },
    };
    snackbar.value = null;
    mockList.mockReset();
    mockLogout.mockReset();
  });

  it("renders empty state when list is empty", async () => {
    mockList.mockResolvedValueOnce([]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} />);
    await waitFor(() => expect(screen.getByText(/没有正在运行/)).toBeTruthy());
  });

  it("renders rows when list is non-empty", async () => {
    mockList.mockResolvedValueOnce([
      { id: "s1", user_id: "u", device_id: "d", tool: "claude", name: "demo", status: "running" },
    ]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} />);
    await waitFor(() => expect(screen.getByText(/claude/)).toBeTruthy());
    await waitFor(() => expect(screen.getByText(/demo/)).toBeTruthy());
  });

  it("clicking a row calls onOpen with session id", async () => {
    mockList.mockResolvedValueOnce([
      { id: "s1", user_id: "u", device_id: "d", tool: "claude", name: "demo", status: "running" },
    ]);
    const onOpen = vi.fn();
    render(<SessionsPage onOpen={onOpen} onLogout={() => {}} />);
    await waitFor(() => screen.getByText(/demo/));
    // Click the parent row, not just the inner text — so use the role/closest
    const row = screen.getByText(/demo/).closest("button") as HTMLElement;
    fireEvent.click(row);
    expect(onOpen).toHaveBeenCalledWith("s1");
  });

  it("Logout menu calls api.logout, clears auth, and invokes onLogout callback", async () => {
    mockList.mockResolvedValueOnce([]);
    mockLogout.mockResolvedValueOnce(undefined);
    const onLogout = vi.fn();
    render(<SessionsPage onOpen={() => {}} onLogout={onLogout} />);
    await waitFor(() => screen.getByText(/没有正在运行/));

    fireEvent.click(screen.getByLabelText("menu"));
    fireEvent.click(screen.getByText("Logout"));
    await waitFor(() => expect(mockLogout).toHaveBeenCalled());
    expect(accessToken.value).toBeNull();
    expect(userInfo.value).toBeNull();
    expect(onLogout).toHaveBeenCalled();
  });

  it("network error surfaces snackbar warn", async () => {
    mockList.mockRejectedValueOnce(new Error("net"));
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} />);
    await waitFor(() => expect(snackbar.value?.kind).toBe("warn"));
    expect(snackbar.value?.msg).toMatch(/加载失败/);
  });

  it("refresh button re-fetches the list", async () => {
    mockList.mockResolvedValueOnce([]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} />);
    await waitFor(() => screen.getByText(/没有正在运行/));
    expect(mockList).toHaveBeenCalledTimes(1);

    mockList.mockResolvedValueOnce([
      { id: "s2", user_id: "u", device_id: "d", tool: "codex", name: "spike", status: "running" },
    ]);
    fireEvent.click(screen.getByLabelText("refresh"));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByText(/codex/)).toBeTruthy());
  });
});
