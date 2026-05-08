const BACKOFF_SCHEDULE_MS = [1000, 2000, 5000, 10000, 30000];
const GIVE_UP_AFTER_MS = 5 * 60 * 1000;

export interface SupervisorState {
  phase: "connecting" | "connected" | "reconnecting" | "gave-up" | "closed";
  attempt: number;
  lastConnectedAt: Date | null;
  lastError: string;
  attemptHistory: Array<{ at: Date; error: string }>;
  /** Wall-clock instant when the supervisor transitioned to gave-up. Null otherwise. */
  gaveUpAt: Date | null;
}

export interface ConnectHandle {
  disconnect: () => void;
  onCloseTrigger?: () => void; // for tests; real connect attaches close handlers itself
}

export interface ReconnectOptions {
  connect: (token: string) => Promise<ConnectHandle>;
  refreshToken: () => Promise<string>;
  onStateChange: (state: SupervisorState) => void;
  now?: () => Date;
  rng?: () => number;
}

export interface ReconnectSupervisor {
  start: () => void;
  stop: () => void;
  retry: () => void;
  signalClose: (err: unknown) => void;
  state: () => SupervisorState;
}

function backoffMs(attempt: number, rng: () => number): number {
  const base =
    attempt < BACKOFF_SCHEDULE_MS.length
      ? BACKOFF_SCHEDULE_MS[attempt]
      : BACKOFF_SCHEDULE_MS[BACKOFF_SCHEDULE_MS.length - 1];
  const factor = 0.8 + 0.4 * rng();
  return Math.round(base * factor);
}

export function createReconnectSupervisor(opts: ReconnectOptions): ReconnectSupervisor {
  const now = opts.now ?? (() => new Date());
  const rng = opts.rng ?? Math.random;
  let state: SupervisorState = {
    phase: "connecting",
    attempt: 0,
    lastConnectedAt: null,
    lastError: "",
    attemptHistory: [],
    gaveUpAt: null,
  };
  let stopped = false;
  let pendingTimer: ReturnType<typeof setTimeout> | null = null;
  let giveUpAt: Date | null = null;
  let currentHandle: ConnectHandle | null = null;
  let closeSignal: { resolve: (err: unknown) => void } | null = null;

  const setState = (mut: (s: SupervisorState) => void) => {
    state = { ...state };
    state.attemptHistory = [...state.attemptHistory];
    mut(state);
    opts.onStateChange(state);
  };

  const waitClose = () =>
    new Promise<unknown>((resolve) => {
      closeSignal = { resolve };
    });

  const attemptOnce = async () => {
    try {
      const token = await opts.refreshToken();
      const handle = await opts.connect(token);
      currentHandle = handle;
      setState((s) => {
        s.phase = "connected";
        s.lastConnectedAt = now();
        s.lastError = "";
      });
      giveUpAt = null;
      // Register the close-wait promise BEFORE invoking onCloseTrigger
      // so a synchronous signalClose() inside the trigger finds closeSignal set.
      const closePromise = waitClose();
      // Allow tests to fire a close right after connect.
      if (handle.onCloseTrigger) handle.onCloseTrigger();
      const closeErr = await closePromise;
      currentHandle = null;
      const errMsg = closeErr instanceof Error ? closeErr.message : String(closeErr ?? "closed");
      setState((s) => {
        s.phase = "reconnecting";
        s.attempt += 1;
        s.lastError = errMsg;
        s.attemptHistory.push({ at: now(), error: errMsg });
        if (s.attemptHistory.length > 5) s.attemptHistory.shift();
      });
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : String(err);
      setState((s) => {
        s.phase = "reconnecting";
        s.attempt += 1;
        s.lastError = errMsg;
        s.attemptHistory.push({ at: now(), error: errMsg });
        if (s.attemptHistory.length > 5) s.attemptHistory.shift();
      });
    }
  };

  const loop = async () => {
    while (!stopped) {
      // Give-up uses the real (fake-timer-controlled) wall clock so that
      // vi.advanceTimersByTimeAsync drives the deadline. opts.now is reserved
      // for deterministic timestamps stamped into the state object.
      if (giveUpAt && Date.now() >= giveUpAt.getTime()) {
        setState((s) => {
          s.phase = "gave-up";
          s.gaveUpAt = now();
        });
        return;
      }
      if (state.phase !== "connecting" && state.attempt > 0) {
        if (giveUpAt === null) giveUpAt = new Date(Date.now() + GIVE_UP_AFTER_MS);
        // Clamp backoff so we wake exactly at the deadline rather than
        // straddling it (a 30s backoff scheduled at t=298s would otherwise
        // fire the next attempt at t=328s, well past the give-up cutoff).
        const baseDelay = backoffMs(state.attempt - 1, rng);
        const remaining = Math.max(0, giveUpAt.getTime() - Date.now());
        const delay = Math.min(baseDelay, remaining);
        await new Promise<void>((resolve) => {
          pendingTimer = setTimeout(resolve, delay);
        });
        if (stopped) return;
        // Re-check the deadline before issuing the next attempt: the sleep
        // may have woken us at exactly the give-up moment, in which case we
        // must transition to gave-up rather than fire one more connect call.
        if (giveUpAt && Date.now() >= giveUpAt.getTime()) {
          setState((s) => {
            s.phase = "gave-up";
            s.gaveUpAt = now();
          });
          return;
        }
      }
      await attemptOnce();
      if (stopped) return;
    }
  };

  return {
    start: () => {
      stopped = false;
      void loop();
    },
    stop: () => {
      stopped = true;
      if (pendingTimer) clearTimeout(pendingTimer);
      pendingTimer = null;
      if (currentHandle) currentHandle.disconnect();
      if (closeSignal) closeSignal.resolve(new Error("supervisor stopped"));
      setState((s) => {
        s.phase = "closed";
      });
    },
    retry: () => {
      giveUpAt = null;
      setState((s) => {
        s.phase = "reconnecting";
        s.attempt = 0;
      });
      void loop();
    },
    signalClose: (err) => {
      if (closeSignal) {
        closeSignal.resolve(err);
        closeSignal = null;
      }
    },
    state: () => state,
  };
}
