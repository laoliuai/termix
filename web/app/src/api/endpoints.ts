import { fetchWithAuth } from "./client";
import type { User, Device } from "../auth/store";

// ---- login ----

export interface LoginInput {
  email: string;
  password: string;
  device_label: string; // e.g. navigator.userAgent.slice(0,80)
}

export interface LoginOk {
  user: User;
  device: Device;
  access_token: string;
  expires_in_seconds: number;
}

export type LoginResult =
  | { ok: true; data: LoginOk }
  | { ok: false; status: number; message: string };

export async function login(input: LoginInput): Promise<LoginResult> {
  let res: Response;
  try {
    res = await fetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: input.email,
        password: input.password,
        device_type: "web",
        platform: "web",
        device_label: input.device_label,
        cookie_mode: true,
      }),
    });
  } catch {
    return { ok: false, status: 0, message: "" };  // network error
  }
  if (res.ok) return { ok: true, data: await res.json() as LoginOk };

  let message = "";
  try {
    const body = await res.json() as { error?: string };
    if (body.error) message = body.error;
  } catch {
    // non-JSON body: leave message empty
  }
  return { ok: false, status: res.status, message };
}

// ---- list sessions ----

export interface SessionSummary {
  id: string;
  user_id: string;
  device_id: string;
  tool: string;
  name: string;
  status: "running" | "idle" | "exited";
  host_label?: string;
  last_activity_at?: string;
  created_at?: string;
  control?: SessionControlState;
}

export interface SessionControlState {
  holder: "self" | "other";
  holder_label?: string;
}

export async function listSessions(status: "running" | "all" = "running"): Promise<SessionSummary[]> {
  const res = await fetchWithAuth(`/api/v1/sessions?status=${status}`);
  if (!res.ok) throw new Error(`list sessions failed: ${res.status}`);
  const data = await res.json() as { sessions: SessionSummary[] };
  return data.sessions;
}

// ---- get one session ----

export async function getSession(id: string): Promise<SessionSummary | null> {
  const res = await fetchWithAuth(`/api/v1/sessions/${encodeURIComponent(id)}`);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`get session failed: ${res.status}`);
  return await res.json() as SessionSummary;
}

// ---- logout ----

export async function logout(): Promise<void> {
  await fetch("/api/v1/auth/logout", { method: "POST" }).catch(() => {
    // intentional swallow: local state is cleared regardless
  });
}
