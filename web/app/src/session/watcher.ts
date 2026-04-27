import type { DecodedFrame } from "@/protocol/types";

export interface Watcher {
  handleFrame(frame: DecodedFrame): void;
}

export interface WatcherConfig {
  sessionId: string;
  write: (bytes: Uint8Array) => void;
}

export function createWatcher(cfg: WatcherConfig): Watcher {
  return {
    handleFrame(frame) {
      if (frame.header.session_id !== cfg.sessionId) return;
      if (frame.type === 1 || frame.type === 3) {
        cfg.write(frame.payload);
      }
      // type 2 is input; never received from server.
    },
  };
}
