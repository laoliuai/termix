import type { DecodedFrame } from "@/protocol/types";

export interface Watcher {
  handleFrame(frame: DecodedFrame): void;
  setCurrentGeneration(gen: number): void;
  setSnapshotPending(pending: boolean): void;
}

export interface WatcherConfig {
  sessionId: string;
  write: (bytes: Uint8Array) => void;
}

// Generation fence: inbound calls setCurrentGeneration(N) + setSnapshotPending(true)
// when it receives snapshot.ready(gen=N). Until the final snapshot chunk
// (type 3, is_last) arrives, live output frames (type 1) are dropped — they
// belong to the previous snapshot/generation and would layer onto the new one.
export function createWatcher(cfg: WatcherConfig): Watcher {
  let snapshotPending = false;
  // currentGeneration is recorded for parity with inbound; arrival-order +
  // pending flag are the operative fence (frame headers do not carry a gen).
  let currentGeneration = 0;

  return {
    setCurrentGeneration(gen) {
      currentGeneration = gen;
      void currentGeneration; // recorded to mirror inbound's contract; no per-frame gen exists yet
    },
    setSnapshotPending(pending) {
      snapshotPending = pending;
    },
    handleFrame(frame) {
      if (frame.header.session_id !== cfg.sessionId) return;
      if (frame.type === 3) {
        cfg.write(frame.payload);
        if ((frame.header as { is_last?: boolean }).is_last) snapshotPending = false;
        return;
      }
      if (frame.type === 1) {
        if (snapshotPending) return; // fence: drop pre-snapshot live output
        cfg.write(frame.payload);
      }
      // type 2 is input; never received from server.
    },
  };
}
