import type { ComponentChildren } from "preact";
import { useEffect } from "preact/hooks";
import { route } from "preact-router";
import { accessToken } from "../auth/store";
import { splashing } from "../app/store";

/**
 * Auth-required wrapper. While the cold-start bootstrap is in flight
 * (splashing.value === true), render nothing and let <Splash /> cover
 * the screen. After bootstrap resolves, if there's no access token,
 * redirect to "/".
 */
export function AuthGuard({ children }: { children: ComponentChildren }) {
  useEffect(() => {
    if (!splashing.value && accessToken.value === null) {
      route("/", true);
    }
  }, []);

  if (splashing.value) return null;
  if (accessToken.value === null) return null;
  return <>{children}</>;
}
