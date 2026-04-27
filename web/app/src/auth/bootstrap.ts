import { accessToken, accessTokenExpiresAt, userInfo } from "./store";
import { splashing } from "../app/store";

export type BootstrapResult = "authed" | "unauthed" | "network-error";

/**
 * Cold-start auth probe. Runs once at <App> mount.
 * Posts to /api/v1/auth/refresh which auto-attaches the HttpOnly cookie.
 * - 200 → write access-token signal (and user/device if present), return "authed".
 * - 401 → cookie absent or expired, return "unauthed".
 * - network error → return "network-error" (caller surfaces a snackbar).
 *
 * Always clears the splashing signal in `finally` so the UI never gets stuck.
 */
export async function bootstrap(): Promise<BootstrapResult> {
  splashing.value = true;
  try {
    const res = await fetch("/api/v1/auth/refresh", { method: "POST" });
    if (res.ok) {
      const data = await res.json() as {
        access_token: string;
        expires_in_seconds: number;
        user?: { id: string; email: string; display_name: string; role: "admin" | "user" };
        device?: { id: string; device_type: string; platform: string; label: string };
      };
      accessToken.value = data.access_token;
      accessTokenExpiresAt.value = Date.now() + data.expires_in_seconds * 1000;
      if (data.user && data.device) {
        userInfo.value = { user: data.user, device: data.device };
      }
      return "authed";
    }
    return "unauthed";
  } catch {
    return "network-error";
  } finally {
    splashing.value = false;
  }
}
