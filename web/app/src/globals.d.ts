import type { SpecialKey, TermixBridge } from "./protocol/types";

declare global {
  interface Window {
    setSession: (sessionId: string, relayUrl: string, accessToken: string, deviceId: string) => void;
    sendText: (text: string) => void;
    sendSpecialKey: (key: SpecialKey) => void;
    requestControl: () => void;
    releaseControl: () => void;
    TermixBridge?: Partial<TermixBridge>;
  }
}

export {};
