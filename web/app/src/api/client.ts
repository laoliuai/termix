import { accessToken } from "../auth/store";
import { doRefreshOnce } from "../auth/refresh";

/**
 * fetch wrapper that attaches the access token from the accessToken signal,
 * and on 401 transparently runs a single-flight refresh + retries once.
 * Any other status passes through unchanged.
 */
export async function fetchWithAuth(input: RequestInfo, init: RequestInit = {}): Promise<Response> {
  const doFetch = (token: string | null): Promise<Response> => fetch(input, {
    ...init,
    headers: {
      ...(init.headers ?? {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });

  const first = await doFetch(accessToken.value);
  if (first.status !== 401) return first;

  // The server rejected our token — treat it as expired so doRefreshOnce
  // bypasses its fast-path cache and actually calls /auth/refresh.
  accessToken.value = null;
  const refreshed = await doRefreshOnce();
  if (refreshed === null) return first;
  return doFetch(refreshed);
}
