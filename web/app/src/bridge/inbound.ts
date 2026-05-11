import { decodeFrame, encodeInputFrame } from "@/protocol/frame";
import { decodeEnvelope, encodeEnvelope } from "@/protocol/envelope";
import { openWSClient, type WSClient, type WebSocketFactory } from "@/net/wsClient";
import { startHeartbeat } from "@/net/heartbeat";
import { createControl, encodeSpecialKey, type Control } from "@/session/control";
import { createWatcher } from "@/session/watcher";
import { createOutboundEmitter } from "./outbound";
import {
  createReconnectSupervisor,
  type ConnectHandle,
  type ReconnectSupervisor,
} from "./reconnect";
import { freshAccessTokenWithStatus } from "@/auth/refresh";
import type { SpecialKey } from "@/protocol/types";
import type { TerminalUI } from "@/ui/terminal";

const GIVE_UP_AFTER_MS = 5 * 60 * 1000;

interface ActiveSession {
  sessionId: string;
  ws: WSClient;
  control: Control;
  stopHeartbeat: () => void;
  inputSeq: number;
  sendInput: (bytes: Uint8Array) => void;
  cols: number;
  rows: number;
}

export interface InboundConfig {
  ui: TerminalUI;
  factory?: WebSocketFactory;
}

export function installInboundBridge(cfg: InboundConfig): void {
  const outbound = createOutboundEmitter();
  let active: ActiveSession | null = null;
  let activeSup: ReconnectSupervisor | null = null;
  // Initial grid mirrors what xterm is actually rendering at install time
  // so the cols/rows we hang on session.watch match the viewport-derived
  // pickGrid result. The previous hardcoded 80×24 left the daemon's tmux
  // pane permanently undersized on desktop and tall mobile.
  let lastGrid: { cols: number; rows: number } = { cols: cfg.ui.cols(), rows: cfg.ui.rows() };

  const closeActive = () => {
    if (activeSup) {
      activeSup.stop();
      activeSup = null;
    }
    if (active) {
      active.stopHeartbeat();
      active.ws.close();
      active = null;
    }
  };

  const setSession = (sessionId: string, relayUrl: string, accessToken: string, deviceId: string): void => {
    closeActive();
    if (!sessionId || !relayUrl) return; // graceful-close path

    // The first dial uses the access token the host bridge handed us.
    // Reconnects re-fetch via freshAccessToken().
    let firstToken: string | null = accessToken;

    const sup: ReconnectSupervisor = createReconnectSupervisor({
      connect: (token) => {
        const url = new URL(relayUrl);
        url.searchParams.set("access_token", token);
        url.searchParams.set("device_id", deviceId);
        url.searchParams.set("session_id", sessionId);

        return new Promise<ConnectHandle>((resolve, reject) => {
          let opened = false;
          // Hold per-attempt resources here so onClose / onError can tear them down.
          let stopHeartbeat: () => void = () => {};
          let inputSeq = 0;
          let ws: WSClient | null = null;
          let control: Control | null = null;
          const watcher = createWatcher({ sessionId, write: (b) => cfg.ui.write(b) });

          ws = openWSClient(url.toString(), {
            onOpen: () => {
              opened = true;
              const wsRef = ws!;
              control = createControl({
                sessionId,
                sendText: (text) => wsRef.sendText(text),
                onState: (state, detail) => outbound.onControlState(state, detail),
              });
              wsRef.sendText(encodeEnvelope("hello.android", { device_id: deviceId }));
              // cols/rows ride on session.watch so the relay forwards them
              // into session.snapshot.req and the daemon resizes the tmux
              // pane before capture-pane runs. The old "client.resize then
              // session.watch" order was rejected by the relay (the
              // isWatching guard on client.resize closed the WS before
              // watch could land), and "watch then client.resize" lost the
              // first snapshot to the previous pane size.
              wsRef.sendText(encodeEnvelope("session.watch", {
                session_id: sessionId,
                cols: lastGrid.cols,
                rows: lastGrid.rows,
              }));
              stopHeartbeat = startHeartbeat(
                () => wsRef.sendText(encodeEnvelope("heartbeat", {})),
                20_000,
              );

              // Bind the active-session pointer so the window globals
              // (sendText / sendInput / requestResize) keep working across
              // reconnects. Each successful open replaces it.
              const session: ActiveSession = {
                sessionId,
                ws: wsRef,
                control,
                stopHeartbeat,
                inputSeq,
                sendInput: (bytes) => {
                  if (!session.control.canSendInput()) return;
                  wsRef.sendBinary(encodeInputFrame(sessionId, session.inputSeq++, bytes));
                },
                cols: lastGrid.cols,
                rows: lastGrid.rows,
              };
              active = session;

              resolve({ disconnect: () => wsRef.close() });
            },
            onText: (text) => {
              try {
                const env = decodeEnvelope(text);
                // The daemon sends `session.snapshot.ready` immediately before
                // the snapshot frame on every (re-)watch. Reset the xterm
                // buffer here so reconnects/revisits don't stack the new
                // snapshot below the previous one.
                if (env.type === "session.snapshot.ready") {
                  cfg.ui.reset();
                }
                control?.handleEnvelope(env);
                if (env.type === "error") {
                  const p = env.payload as { code?: string; message?: string };
                  outbound.onError(p.code ?? "error", p.message ?? "");
                }
              } catch (e) {
                outbound.onError("decode", (e as Error).message);
              }
            },
            onBinary: (data) => {
              try {
                watcher.handleFrame(decodeFrame(new Uint8Array(data)));
              } catch (e) {
                outbound.onError("frame", (e as Error).message);
              }
            },
            onClose: () => {
              stopHeartbeat();
              control?.onConnectionDropped();
              if (active && active.ws === ws) active = null;
              if (opened) {
                sup.signalClose(new Error("ws close"));
              } else {
                reject(new Error("ws close before open"));
              }
            },
            onError: () => {
              stopHeartbeat();
              control?.onConnectionDropped();
              if (active && active.ws === ws) active = null;
              if (opened) {
                sup.signalClose(new Error("ws error"));
              } else {
                reject(new Error("ws error before open"));
              }
            },
          }, cfg.factory);
        });
      },
      refreshToken: async () => {
        if (firstToken) {
          const t = firstToken;
          firstToken = null;
          return t;
        }
        const outcome = await freshAccessTokenWithStatus();
        if (outcome.status === 401) {
          // Refresh token is dead — redirect to login immediately rather than
          // cycling through the backoff loop into gave-up.
          if (typeof window !== "undefined" && window.location) {
            const next = encodeURIComponent(window.location.pathname + window.location.search);
            window.location.href = "/login?next=" + next;
          }
          throw new Error("refresh-401: redirecting to login");
        }
        if (!outcome.accessToken) throw new Error("refresh failed");
        return outcome.accessToken;
      },
      onStateChange: (s) => {
        // Drop state updates from a supervisor that has been replaced or stopped.
        // reconnect.stop() schedules a trailing "reconnecting" microtask after
        // emitting "closed"; without this guard the bridge would surface a
        // spurious reconnect after a graceful close.
        if (activeSup !== sup) return;
        if (s.phase === "closed") {
          // Stop has already run; null out activeSup so the post-close
          // microtask is filtered out by the guard above.
          activeSup = null;
        }
        switch (s.phase) {
          case "connecting":
            outbound.onConnectionState({ phase: "connecting" });
            break;
          case "connected":
            outbound.onConnectionState({ phase: "connected" });
            break;
          case "reconnecting":
            outbound.onConnectionState({
              phase: "reconnecting",
              attempt: s.attempt,
              lastError: s.lastError,
            });
            break;
          case "gave-up": {
            const gaveUpAt = s.gaveUpAt;
            const durationMs = gaveUpAt ? Date.now() - gaveUpAt.getTime() : GIVE_UP_AFTER_MS;
            outbound.onConnectionState({
              phase: "gave-up",
              attemptCount: s.attempt,
              durationMs,
              lastError: s.lastError,
              attemptHistory: s.attemptHistory.slice(-3),
            });
            break;
          }
          case "closed":
            outbound.onConnectionState({ phase: "disconnected" });
            break;
        }
      },
    });

    activeSup = sup;
    // Emit the initial connecting phase up-front. The supervisor's loop
    // skips firing onStateChange for the initial "connecting" state and
    // jumps straight to "connected" on success, so consumers (e.g. the
    // terminal page badge) need this synthetic notification.
    outbound.onConnectionState({ phase: "connecting" });
    sup.start();

    // Expose retry on the window so a disconnect modal (if any) can call it.
    (window as { retryRelay?: () => void }).retryRelay = () => sup.retry();
  };

  const requestResize = (cols: number, rows: number): void => {
    lastGrid = { cols, rows };
    if (!active) return;
    active.cols = cols;
    active.rows = rows;
    active.ws.sendText(encodeEnvelope("client.resize", {
      session_id: active.sessionId,
      cols,
      rows,
    }));
  };

  cfg.ui.onInput((text) => {
    if (!active) return;
    active.sendInput(new TextEncoder().encode(text));
  });

  type WindowGlobals = {
    setSession: typeof setSession;
    sendText: (text: string) => void;
    sendSpecialKey: (key: SpecialKey) => void;
    requestControl: () => void;
    releaseControl: () => void;
    requestResize: (cols: number, rows: number) => void;
  };
  const w = window as unknown as WindowGlobals;
  w.setSession = setSession;
  w.sendText = (text) => active?.sendInput(new TextEncoder().encode(text));
  w.sendSpecialKey = (key) => active?.sendInput(encodeSpecialKey(key));
  w.requestControl = () => active?.control.requestControl();
  w.releaseControl = () => active?.control.releaseControl();
  w.requestResize = requestResize;
}
