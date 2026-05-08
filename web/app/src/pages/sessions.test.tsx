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
import { setLocale } from "../i18n/store";

const mockList = listSessions as unknown as ReturnType<typeof vi.fn>;
const mockLogout = logout as unknown as ReturnType<typeof vi.fn>;

describe("SessionsPage", () => {
  beforeEach(() => {
    cleanup();
    clearAuth();
    setLocale("en");
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
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => expect(screen.getByText(/No running sessions/)).toBeTruthy());
    expect(screen.getByText("Termix")).toBeTruthy();
    expect(screen.getAllByText("a@b")).toHaveLength(2);
    expect(screen.getByRole("heading", { name: "Sessions" })).toBeTruthy();
  });

  it("renders rows when list is non-empty", async () => {
    mockList.mockResolvedValueOnce([
      { id: "s1", user_id: "u", device_id: "d", tool: "claude", name: "demo", status: "running" },
    ]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => expect(screen.getAllByText(/claude · demo/).length).toBeGreaterThan(0));
  });

  it("clicking a row calls onOpen with session id", async () => {
    mockList.mockResolvedValueOnce([
      { id: "s1", user_id: "u", device_id: "d", tool: "claude", name: "demo", status: "running" },
    ]);
    const onOpen = vi.fn();
    render(<SessionsPage onOpen={onOpen} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => screen.getAllByText(/claude · demo/));
    // Click the parent row, not just the inner text — desktop and mobile both render
    const row = screen.getAllByText(/claude · demo/)[0].closest("button") as HTMLElement;
    fireEvent.click(row);
    expect(onOpen).toHaveBeenCalledWith("s1");
  });

  it("header exposes Help directly", async () => {
    mockList.mockResolvedValueOnce([]);
    const onHelp = vi.fn();
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={onHelp} />);
    await waitFor(() => screen.getByText(/No running sessions/));

    // Header icon button + empty-state button both expose "Help" — click the header icon (first).
    const helpButtons = screen.getAllByRole("button", { name: "Help" });
    fireEvent.click(helpButtons[0]);

    expect(onHelp).toHaveBeenCalledOnce();
  });

  it("account Logout calls api.logout, clears auth, and invokes onLogout callback", async () => {
    mockList.mockResolvedValueOnce([]);
    mockLogout.mockResolvedValueOnce(undefined);
    const onLogout = vi.fn();
    render(<SessionsPage onOpen={() => {}} onLogout={onLogout} onHelp={() => {}} />);
    await waitFor(() => screen.getByText(/No running sessions/));

    fireEvent.click(screen.getByRole("button", { name: "Account menu" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Logout" }));
    await waitFor(() => expect(mockLogout).toHaveBeenCalled());
    expect(accessToken.value).toBeNull();
    expect(userInfo.value).toBeNull();
    expect(onLogout).toHaveBeenCalled();
  });

  it("network error surfaces snackbar warn", async () => {
    mockList.mockRejectedValueOnce(new Error("net"));
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => expect(snackbar.value?.kind).toBe("warn"));
    expect(snackbar.value?.msg).toMatch(/failed to load/);
  });

  it("refresh button re-fetches the list", async () => {
    mockList.mockResolvedValueOnce([]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => screen.getByText(/No running sessions/));
    expect(mockList).toHaveBeenCalledTimes(1);

    mockList.mockResolvedValueOnce([
      { id: "s2", user_id: "u", device_id: "d", tool: "codex", name: "spike", status: "running" },
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getAllByText(/codex · spike/).length).toBeGreaterThan(0));
  });

  it("refresh button shows busy state while re-fetching", async () => {
    mockList.mockResolvedValueOnce([]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => screen.getByText(/No running sessions/));

    let resolveRefresh!: (value: unknown[]) => void;
    mockList.mockReturnValueOnce(new Promise(resolve => { resolveRefresh = resolve; }));
    const refresh = screen.getByRole("button", { name: "Refresh" });
    fireEvent.click(refresh);

    await waitFor(() => expect(refresh.getAttribute("aria-busy")).toBe("true"));
    expect((refresh as HTMLButtonElement).disabled).toBe(true);

    resolveRefresh([]);
    await waitFor(() => expect((refresh as HTMLButtonElement).disabled).toBe(false));
  });

  it("refresh button shows a success cue when a fast re-fetch completes", async () => {
    mockList.mockResolvedValueOnce([]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => screen.getByText(/No running sessions/));

    mockList.mockResolvedValueOnce([]);
    const refresh = screen.getByRole("button", { name: "Refresh" });
    fireEvent.click(refresh);

    await waitFor(() => expect(refresh.classList.contains("is-refreshed")).toBe(true));
    expect(refresh.textContent).toBe("✓");
  });

  it("renders desktop metadata and mobile compact markers", async () => {
    mockList.mockResolvedValueOnce([
      {
        id: "s1",
        user_id: "u",
        device_id: "d",
        tool: "codex",
        name: "main",
        status: "running",
        host_label: "MacBook Pro",
        last_activity_at: new Date().toISOString(),
        created_at: new Date(Date.now() - 60_000).toISOString(),
      },
    ]);
    const { container } = render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

    await waitFor(() => expect(screen.getAllByText(/codex · main/).length).toBeGreaterThan(0));
    expect(screen.getAllByText("MacBook Pro").length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: "Open codex main" }).length).toBe(2);
    expect(container.querySelector(".sessions-desktop-table")).toBeTruthy();
    expect(container.querySelector(".sessions-mobile-list")).toBeTruthy();
  });

  it("shows formatted creation time in desktop and mobile views", async () => {
    const createdAt = "2026-05-06T06:01:20.000Z";
    mockList.mockResolvedValueOnce([{
      id: "s1", user_id: "u", device_id: "d",
      tool: "claude", name: "demo", status: "running",
      created_at: createdAt,
    }]);
    const { container } = render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);
    await waitFor(() => screen.getAllByText(/claude · demo/));

    const cells = container.querySelectorAll(".session-created-at");
    expect(cells.length).toBeGreaterThan(0);
    const expected = new Intl.DateTimeFormat("en", {
      year: "numeric", month: "long", day: "numeric",
      hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
    }).format(new Date(createdAt));
    expect(cells[0].textContent).toBe(expected);
  });

  it("sorts most recently created sessions first", async () => {
    mockList.mockResolvedValueOnce([
      { id: "old", user_id: "u", device_id: "d", tool: "claude", name: "old", status: "running", created_at: "2026-04-29T00:00:00Z" },
      { id: "new", user_id: "u", device_id: "d", tool: "codex", name: "new", status: "running", created_at: "2026-04-30T00:00:00Z" },
    ]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

    await waitFor(() => screen.getAllByText(/codex · new/));
    const rows = screen.getAllByTestId("session-row");
    expect(rows[0].textContent).toContain("codex · new");
  });

  it("filters visible sessions by local search", async () => {
    mockList.mockResolvedValueOnce([
      { id: "s1", user_id: "u", device_id: "d", tool: "claude", name: "ui", status: "running", host_label: "Ubuntu" },
      { id: "s2", user_id: "u", device_id: "d", tool: "codex", name: "main", status: "running", host_label: "MacBook" },
    ]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

    await waitFor(() => screen.getAllByText(/claude · ui/));
    fireEvent.input(screen.getByPlaceholderText("Search by tool, name, host..."), { target: { value: "mac" } });

    expect(screen.queryByText(/claude · ui/)).toBeNull();
    expect(screen.getAllByText(/codex · main/).length).toBeGreaterThan(0);
  });

  it("loads all sessions when All filter is selected", async () => {
    mockList.mockResolvedValueOnce([]);
    mockList.mockResolvedValueOnce([
      { id: "s1", user_id: "u", device_id: "d", tool: "codex", name: "done", status: "exited" },
    ]);
    render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

    await waitFor(() => screen.getByRole("button", { name: "All" }));
    fireEvent.click(screen.getByRole("button", { name: "All" }));

    await waitFor(() => expect(mockList).toHaveBeenLastCalledWith("all"));
    await waitFor(() => expect(screen.getAllByText(/codex · done/).length).toBeGreaterThan(0));
  });
});
