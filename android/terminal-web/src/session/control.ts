import { encodeEnvelope, isControlDenied, isControlGranted, isControlRevoked } from "@/protocol/envelope";
import type { ControlState, Envelope, SpecialKey } from "@/protocol/types";

const RENEW_LEAD_MS = 30_000;
const RENEW_FLOOR_MS = 5_000;

export interface Control {
  readonly state: ControlState;
  requestControl(): void;
  releaseControl(): void;
  handleEnvelope(env: Envelope): void;
  onConnectionDropped(): void;
  canSendInput(): boolean;
}

export interface ControlConfig {
  sessionId: string;
  sendText: (text: string) => void;
  onState: (state: ControlState, detail?: string) => void;
}

export function createControl(cfg: ControlConfig): Control {
  let state: ControlState = "none";
  let leaseVersion: number | null = null;
  let renewHandle: ReturnType<typeof setTimeout> | null = null;

  const setState = (next: ControlState, detail?: string) => {
    if (state === next) return;
    state = next;
    cfg.onState(next, detail);
  };

  const clearRenew = () => {
    if (renewHandle !== null) {
      clearTimeout(renewHandle);
      renewHandle = null;
    }
  };

  const scheduleRenew = (expiresAtIso: string) => {
    clearRenew();
    const expiresAtMs = Date.parse(expiresAtIso);
    const delay = Math.max(RENEW_FLOOR_MS, expiresAtMs - Date.now() - RENEW_LEAD_MS);
    renewHandle = setTimeout(() => {
      if (state !== "granted" || leaseVersion === null) return;
      cfg.sendText(encodeEnvelope("control.renew", { session_id: cfg.sessionId, lease_version: leaseVersion }));
    }, delay);
  };

  return {
    get state() { return state; },

    requestControl() {
      cfg.sendText(encodeEnvelope("control.acquire", { session_id: cfg.sessionId }));
      setState("requesting");
    },

    releaseControl() {
      if (state !== "granted" || leaseVersion === null) return;
      cfg.sendText(encodeEnvelope("control.release", { session_id: cfg.sessionId, lease_version: leaseVersion }));
      clearRenew();
      leaseVersion = null;
      setState("none");
    },

    handleEnvelope(env: Envelope) {
      if (isControlGranted(env)) {
        leaseVersion = env.payload.lease_version;
        setState("granted");
        scheduleRenew(env.payload.expires_at);
      } else if (isControlDenied(env)) {
        clearRenew();
        leaseVersion = null;
        setState("denied", env.payload.reason);
      } else if (isControlRevoked(env)) {
        clearRenew();
        leaseVersion = null;
        setState("revoked", env.payload.reason);
      }
    },

    onConnectionDropped() {
      clearRenew();
      leaseVersion = null;
      setState("none");
    },

    canSendInput() { return state === "granted"; },
  };
}

const SPECIAL_KEY_BYTES: Record<SpecialKey, number[]> = {
  Enter:     [0x0d],
  Tab:       [0x09],
  Escape:    [0x1b],
  Up:        [0x1b, 0x5b, 0x41],
  Down:      [0x1b, 0x5b, 0x42],
  Right:     [0x1b, 0x5b, 0x43],
  Left:      [0x1b, 0x5b, 0x44],
  "C-c":     [0x03],
  "C-d":     [0x04],
  Backspace: [0x7f],
};

export function encodeSpecialKey(key: SpecialKey): Uint8Array {
  return new Uint8Array(SPECIAL_KEY_BYTES[key]);
}
