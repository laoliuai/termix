import { useEffect } from "preact/hooks";

/**
 * Calls onShow whenever the document becomes visible (Page Visibility API).
 * Useful for refreshing data when a tab is switched back to.
 */
export function useVisibility(onShow: () => void): void {
  useEffect(() => {
    const handler = () => {
      if (document.visibilityState === "visible") onShow();
    };
    document.addEventListener("visibilitychange", handler);
    return () => document.removeEventListener("visibilitychange", handler);
  }, [onShow]);
}
