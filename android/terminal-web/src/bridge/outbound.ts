import type { ConnectionState, ControlState, TermixBridge } from "@/protocol/types";

export function createOutboundEmitter(): TermixBridge {
  const log = (method: string, ...args: unknown[]) => {
    console.info(`[TermixBridge] ${method} ${args.join(" ")}`);
  };

  const bridge = (): Partial<TermixBridge> | undefined => {
    if (typeof window === "undefined") return undefined;
    return (window as { TermixBridge?: Partial<TermixBridge> }).TermixBridge;
  };

  return {
    onConnectionState(state: ConnectionState, detail?: string) {
      const fn = bridge()?.onConnectionState;
      if (fn) fn(state, detail);
      else log("onConnectionState", state, detail);
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
