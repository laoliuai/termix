import { signal } from "@preact/signals";

// User and Device shapes mirror the openapi-generated types.
// Keep the surface here minimal (id is the most-needed field across the app).
export interface User {
  id: string;
  email: string;
  display_name: string;
  role: "admin" | "user";
}

export interface Device {
  id: string;
  device_type: string;
  platform: string;
  label: string;
}

export const accessToken = signal<string | null>(null);
export const accessTokenExpiresAt = signal<number>(0);  // epoch ms
export const userInfo = signal<{ user: User; device: Device } | null>(null);

export function clearAuth(): void {
  accessToken.value = null;
  accessTokenExpiresAt.value = 0;
  userInfo.value = null;
}
