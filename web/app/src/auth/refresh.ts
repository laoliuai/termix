import { accessToken, accessTokenExpiresAt } from "./store";

let inflight: Promise<string | null> | null = null;

/**
 * Refresh the access token via POST /api/v1/auth/refresh.
 * Concurrent calls coalesce into one network round-trip (single-flight).
 * On success, updates accessToken + accessTokenExpiresAt signals and returns
 * the new access token. Returns null on failure.
 */
export async function doRefreshOnce(): Promise<string | null> {
  if (inflight) return inflight;
  inflight = (async (): Promise<string | null> => {
    // Fast path: the cache may have been refreshed by another caller while
    // we were waiting on the inflight slot.
    if (accessToken.value && Date.now() < accessTokenExpiresAt.value - 5_000) {
      return accessToken.value;
    }
    try {
      const res = await fetch("/api/v1/auth/refresh", { method: "POST" });
      if (!res.ok) return null;
      const data = await res.json() as { access_token: string; expires_in_seconds: number };
      accessToken.value = data.access_token;
      accessTokenExpiresAt.value = Date.now() + data.expires_in_seconds * 1000;
      return data.access_token;
    } catch {
      return null;
    } finally {
      inflight = null;
    }
  })();
  return inflight;
}

/**
 * Returns the cached access token if it's not near expiry, otherwise
 * triggers a refresh. The 60-second cushion gives in-flight WSS / API
 * requests time to use the token before it expires server-side.
 */
export async function freshAccessToken(): Promise<string | null> {
  if (accessToken.value && Date.now() < accessTokenExpiresAt.value - 60_000) {
    return accessToken.value;
  }
  return doRefreshOnce();
}

/** test-only helper */
export function __resetInflight(): void {
  inflight = null;
}
