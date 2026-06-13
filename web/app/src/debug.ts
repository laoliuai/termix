// Opt-in diagnostics for the terminal viewer. Default OFF: when the
// `termix_debug` localStorage flag is not "1", nothing here changes the
// over-the-wire bytes or the rendered page — communication is identical to a
// build without this module. When ON, the bridge piggybacks an observation
// object onto the (infrequent) `client.resize` control envelope so the daemon
// can log it, and the terminal mounts an on-screen overlay. The terminal byte
// path (snapshot / live-output binary frames) is never touched.

const DEBUG_KEY = "termix_debug";

export function isDebugEnabled(): boolean {
  try {
    return localStorage.getItem(DEBUG_KEY) === "1";
  } catch {
    return false;
  }
}

export function setDebugEnabled(on: boolean): void {
  try {
    if (on) localStorage.setItem(DEBUG_KEY, "1");
    else localStorage.removeItem(DEBUG_KEY);
  } catch {
    /* localStorage unavailable (private mode / SSR) — debug stays off */
  }
}

// applyDebugParam is the in-page DEBUG switch: append `?debug=1` to the URL to
// turn diagnostics on (persisted to localStorage), `?debug=0` to turn them off.
// Absent param leaves the stored flag untouched, so a one-time `?debug=1` visit
// keeps debug on across navigations until explicitly turned off. Called once at
// startup with window.location.search.
export function applyDebugParam(search: string): void {
  let value: string | null = null;
  try {
    value = new URLSearchParams(search).get("debug");
  } catch {
    return;
  }
  if (value === null) return;
  const on = value === "1" || value === "true" || value === "on";
  setDebugEnabled(on);
}

export interface ViewportDebug {
  // Layout viewport (CSS px).
  vw: number;
  vh: number;
  // Visual viewport (CSS px) — shrinks when the mobile soft keyboard opens in
  // resize-mode browsers; null when window.visualViewport is unavailable.
  vvw: number | null;
  vvh: number | null;
  // devicePixelRatio — a HiDPI phone reports 2–3 here.
  dpr: number;
}

// viewportDebug snapshots the browser's current viewport geometry. Used both by
// the wire payload (so server logs can correlate) and by the on-screen overlay.
export function viewportDebug(): ViewportDebug {
  const vv = typeof window !== "undefined" ? window.visualViewport : null;
  return {
    vw: typeof window !== "undefined" ? window.innerWidth : 0,
    vh: typeof window !== "undefined" ? window.innerHeight : 0,
    vvw: vv ? Math.round(vv.width) : null,
    vvh: vv ? Math.round(vv.height) : null,
    dpr: typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1,
  };
}
