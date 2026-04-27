import type { ComponentChildren } from "preact";
import { useEffect } from "preact/hooks";
import { useComputed } from "@preact/signals";
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
  const guardState = useComputed(() => ({
    splashing: splashing.value,
    hasToken: accessToken.value !== null,
  }));

  useEffect(() => {
    if (!guardState.value.splashing && !guardState.value.hasToken) {
      route("/", true);
    }
  }, [guardState.value.splashing, guardState.value.hasToken]);

  if (guardState.value.splashing) return null;
  if (!guardState.value.hasToken) return null;
  return <>{children}</>;
}
