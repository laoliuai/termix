import type { ConnectionState, ControlState, TermixBridge } from "@/protocol/types";

export function createOutboundEmitter(): TermixBridge {
  const log = (method: string, ...args: unknown[]) => {
    console.info(`[TermixBridge] ${method} ${args.join(" ")}`);
  };

  const bridge = (): Partial<TermixBridge> | undefined => {
    if (typeof window === "undefined") return undefined;
    return (window as { TermixBridge?: Partial<TermixBridge> }).TermixBridge;
  };

  const formatConn = (s: ConnectionState): string => {
    switch (s.phase) {
      case "reconnecting": return `reconnecting attempt=${s.attempt} lastError=${s.lastError}`;
      case "gave-up":      return `gave-up attempts=${s.attemptCount} durationMs=${s.durationMs} lastError=${s.lastError}`;
      default:             return s.phase;
    }
  };

  return {
    onConnectionState(state: ConnectionState) {
      const fn = bridge()?.onConnectionState;
      if (fn) fn(state);
      else log("onConnectionState", formatConn(state));
    },
    onControlState(state: ControlState, detail?: string) {
      const fn = bridge()?.onControlState;
      if (fn) fn(state, detail);
      else log("onControlState", state, detail);
    },
    onError(code: string, message: string) {
      const fn = bridge()?.onError;
      if (fn) fn(code, message);
      else log("onError", code, message);
    },
  };
}
