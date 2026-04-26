import { decodeFrame, encodeInputFrame } from "@/protocol/frame";
import { decodeEnvelope, encodeEnvelope } from "@/protocol/envelope";
import { openWSClient, type WSClient, type WebSocketFactory } from "@/net/wsClient";
import { startHeartbeat } from "@/net/heartbeat";
import { createControl, encodeSpecialKey, type Control } from "@/session/control";
import { createWatcher } from "@/session/watcher";
import { createOutboundEmitter } from "./outbound";
import type { SpecialKey } from "@/protocol/types";
import type { TerminalUI } from "@/ui/terminal";

interface ActiveSession {
  sessionId: string;
  ws: WSClient;
  control: Control;
  stopHeartbeat: () => void;
  inputSeq: number;
  sendInput: (bytes: Uint8Array) => void;
}

export interface InboundConfig {
  ui: TerminalUI;
  factory?: WebSocketFactory;
}

export function installInboundBridge(cfg: InboundConfig): void {
  const outbound = createOutboundEmitter();
  let active: ActiveSession | null = null;

  const closeActive = () => {
    if (!active) return;
    active.stopHeartbeat();
    active.ws.close();
    active = null;
  };

  const setSession = (sessionId: string, relayUrl: string, accessToken: string, deviceId: string): void => {
    closeActive();
    if (!sessionId || !relayUrl) return; // graceful-close path

    outbound.onConnectionState("connecting");

    const url = new URL(relayUrl);
    url.searchParams.set("access_token", accessToken);
    url.searchParams.set("device_id", deviceId);
    url.searchParams.set("session_id", sessionId);

    const session: ActiveSession = {
      sessionId,
      ws: undefined as unknown as WSClient, // assigned below before any handler can fire
      control: undefined as unknown as Control,
      stopHeartbeat: () => {},
      inputSeq: 0,
      sendInput: (bytes) => {
        if (!session.control.canSendInput()) return;
        session.ws.sendBinary(encodeInputFrame(session.sessionId, session.inputSeq++, bytes));
      },
    };

    session.control = createControl({
      sessionId,
      sendText: (text) => session.ws.sendText(text),
      onState: (state, detail) => outbound.onControlState(state, detail),
    });

    const watcher = createWatcher({ sessionId, write: (b) => cfg.ui.write(b) });

    session.ws = openWSClient(url.toString(), {
      onOpen: () => {
        session.ws.sendText(encodeEnvelope("hello.android", { device_id: deviceId }));
        session.ws.sendText(encodeEnvelope("session.watch", { session_id: sessionId }));
        session.stopHeartbeat = startHeartbeat(
          () => session.ws.sendText(encodeEnvelope("heartbeat", {})),
          20_000,
        );
        outbound.onConnectionState("connected");
      },
      onText: (text) => {
        try {
          const env = decodeEnvelope(text);
          session.control.handleEnvelope(env);
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
        session.control.onConnectionDropped();
        session.stopHeartbeat();
        if (active === session) active = null;
        outbound.onConnectionState("disconnected");
      },
      onError: () => {
        session.control.onConnectionDropped();
        session.stopHeartbeat();
        if (active === session) active = null;
        outbound.onConnectionState("error");
      },
    }, cfg.factory);

    active = session;
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
  };
  const w = window as unknown as WindowGlobals;
  w.setSession = setSession;
  w.sendText = (text) => active?.sendInput(new TextEncoder().encode(text));
  w.sendSpecialKey = (key) => active?.sendInput(encodeSpecialKey(key));
  w.requestControl = () => active?.control.requestControl();
  w.releaseControl = () => active?.control.releaseControl();
}
