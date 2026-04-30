import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/preact";

// Mock the API module BEFORE importing LoginPage
vi.mock("../api/endpoints", () => ({
  login: vi.fn(),
}));

import { LoginPage } from "./login";
import { accessToken, accessTokenExpiresAt, userInfo, clearAuth } from "../auth/store";
import { login as loginApi } from "../api/endpoints";

const mockedLogin = loginApi as unknown as ReturnType<typeof vi.fn>;

describe("LoginPage", () => {
  beforeEach(() => {
    cleanup();
    clearAuth();
    localStorage.clear();
    mockedLogin.mockReset();
  });

  it("submits email/password and writes auth signals on success", async () => {
    const onSuccess = vi.fn();
    mockedLogin.mockResolvedValueOnce({
      ok: true,
      data: {
        user: { id: "u", email: "a@b", display_name: "A", role: "user" },
        device: { id: "d", device_type: "web", platform: "web", label: "ua" },
        access_token: "tk",
        expires_in_seconds: 1800,
      },
    });
    render(<LoginPage onSuccess={onSuccess} onHelp={() => {}} />);

    fireEvent.input(screen.getByLabelText(/email/i), { target: { value: "a@b" } });
    fireEvent.input(screen.getByLabelText(/password/i), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
    expect(accessToken.value).toBe("tk");
    expect(accessTokenExpiresAt.value).toBeGreaterThan(Date.now());
    expect(userInfo.value?.user.email).toBe("a@b");
    expect(userInfo.value?.device.id).toBe("d");
    expect(mockedLogin).toHaveBeenCalledWith(expect.objectContaining({
      email: "a@b",
      password: "pw",
    }));
  });

  it("shows 401 message on bad credentials", async () => {
    mockedLogin.mockResolvedValueOnce({ ok: false, status: 401, message: "" });
    render(<LoginPage onSuccess={() => {}} onHelp={() => {}} />);
    fireEvent.input(screen.getByLabelText(/email/i), { target: { value: "x" } });
    fireEvent.input(screen.getByLabelText(/password/i), { target: { value: "y" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    await waitFor(() => expect(screen.getByText("邮箱或密码错误")).toBeTruthy());
  });

  it("shows 429 message on rate-limit", async () => {
    mockedLogin.mockResolvedValueOnce({ ok: false, status: 429, message: "" });
    render(<LoginPage onSuccess={() => {}} onHelp={() => {}} />);
    fireEvent.input(screen.getByLabelText(/email/i), { target: { value: "x" } });
    fireEvent.input(screen.getByLabelText(/password/i), { target: { value: "y" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    await waitFor(() => expect(screen.getByText("尝试过于频繁，请稍候")).toBeTruthy());
  });

  it("shows network-error message when api returns status=0", async () => {
    mockedLogin.mockResolvedValueOnce({ ok: false, status: 0, message: "" });
    render(<LoginPage onSuccess={() => {}} onHelp={() => {}} />);
    fireEvent.input(screen.getByLabelText(/email/i), { target: { value: "x" } });
    fireEvent.input(screen.getByLabelText(/password/i), { target: { value: "y" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    await waitFor(() => expect(screen.getByText("无法连接服务器")).toBeTruthy());
  });

  it("disables button while busy", async () => {
    let resolve: (v: any) => void = () => {};
    mockedLogin.mockImplementationOnce(() => new Promise(r => { resolve = r; }));
    render(<LoginPage onSuccess={() => {}} onHelp={() => {}} />);
    fireEvent.input(screen.getByLabelText(/email/i), { target: { value: "a" } });
    fireEvent.input(screen.getByLabelText(/password/i), { target: { value: "b" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    await waitFor(() => {
      const btn = screen.getByRole("button", { name: /sign/i }) as HTMLButtonElement;
      expect(btn.disabled).toBe(true);
    });
    // Resolve so the test exits cleanly
    resolve({ ok: false, status: 0, message: "" });
  });

  it("restores and persists the last email without storing the password", () => {
    localStorage.setItem("termix.login.email", "saved@example.com");

    render(<LoginPage onSuccess={() => {}} onHelp={() => {}} />);

    const email = screen.getByLabelText(/email/i) as HTMLInputElement;
    const password = screen.getByLabelText(/password/i) as HTMLInputElement;
    expect(email.value).toBe("saved@example.com");
    expect(password.value).toBe("");

    fireEvent.input(email, { target: { value: "next@example.com" } });
    fireEvent.input(password, { target: { value: "secret" } });

    expect(localStorage.getItem("termix.login.email")).toBe("next@example.com");
    expect(localStorage.getItem("termix.login.password")).toBeNull();
  });

  it("uses stable field names for browser password managers", () => {
    render(<LoginPage onSuccess={() => {}} onHelp={() => {}} />);

    const email = screen.getByLabelText(/email/i) as HTMLInputElement;
    const password = screen.getByLabelText(/password/i) as HTMLInputElement;

    expect(email.id).toBe("login-email");
    expect(email.name).toBe("username");
    expect(email.getAttribute("autocomplete")).toBe("username");
    expect(password.id).toBe("login-password");
    expect(password.name).toBe("password");
    expect(password.getAttribute("autocomplete")).toBe("current-password");
  });
});
