import { useEffect } from "preact/hooks";
import { useSignal } from "@preact/signals";
import { accessToken, userInfo } from "../auth/store";
import { freshAccessToken } from "../auth/refresh";
import { notify } from "../app/store";
import { useKeyboardOffset } from "../hooks/useViewport";
import { Toolbar } from "../components/toolbar";
import { Composer } from "../components/composer";
import type { SpecialKey } from "../protocol/types";
import { getSession, type SessionSummary } from "../api/endpoints";

// Default relay URL: same origin /ws. In dev, Vite's server.proxy proxies
// /ws → ws://localhost:8090. In prod, deploy a reverse proxy so /ws on the
// public domain hits the relay. Override via VITE_RELAY_WS_URL when relay
// is on a different origin.
function defaultRelayUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/ws`;
}

declare global {
  interface Window {
    setSession: (id: string, url: string, tok: string, dev: string) => void;
    sendText: (s: string) => void;
    sendSpecialKey: (k: SpecialKey) => void;
    requestControl: () => void;
    releaseControl: () => void;
    TermixBridge?: {
      onConnectionState: (s: string, d?: string | null) => void;
      onControlState: (s: string, d?: string | null) => void;
      onError: (code: string, msg: string) => void;
    };
  }
}

export interface TerminalPageProps {
  sessionId: string;
  onBack: () => void;
}

type ConnState = "connecting" | "connected" | "disconnected" | "error";
type ControlState = "none" | "requesting" | "granted" | "denied" | "revoked";

function controlLabel(s: ControlState): string {
  switch (s) {
    case "granted": return "You have control";
    case "requesting": return "Requesting…";
    case "denied": return "Control denied";
    case "revoked": return "Control revoked";
    default: return "Read-only";
  }
}

export function TerminalPage({ sessionId, onBack }: TerminalPageProps) {
  const connState = useSignal<ConnState>("connecting");
  const controlState = useSignal<ControlState>("none");
  const meta = useSignal<SessionSummary | null>(null);
  const keyboardOffset = useKeyboardOffset();

  useEffect(() => {
    let cancelled = false;
    getSession(sessionId)
      .then(s => { if (!cancelled) meta.value = s; })
      .catch(() => { /* fall back to id-substring header */ });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  useEffect(() => {
    let retried = false;
    const relayUrl = (import.meta as any).env?.VITE_RELAY_WS_URL ?? defaultRelayUrl();

    window.TermixBridge = {
      onConnectionState: (s) => { connState.value = s as ConnState; },
      onControlState:    (s, detail) => {
        controlState.value = s as ControlState;
        if (s === "denied") notify(`控制请求被拒绝${detail ? "：" + detail : ""}`, "warn");
        else if (s === "revoked") notify(`控制权已被收回${detail ? "：" + detail : ""}`, "warn");
      },
      onError: async (code, msg) => {
        if (code === "auth" && !retried) {
          retried = true;
          notify("会话过期，正在刷新…", "warn");
          const tok = await freshAccessToken();
          if (tok && userInfo.value) {
            window.setSession(sessionId, relayUrl, tok, userInfo.value.device.id);
            return;
          }
          notify("会话已过期，请重新登录", "error");
          onBack();
          return;
        }
        if (code === "watch") notify(msg, "error");
      },
    };

    (async () => {
      const tok = await freshAccessToken();
      if (!tok || !userInfo.value) {
        notify("会话已过期，请重新登录", "warn");
        onBack();
        return;
      }
      window.setSession(sessionId, relayUrl, tok, userInfo.value.device.id);
    })();

    return () => {
      window.setSession("", "", "", "");
      delete window.TermixBridge;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  const onDigit = (d: string) => window.sendText(d);
  const onSpecial = (k: SpecialKey) => window.sendSpecialKey(k);
  const onCompose = (s: string) => window.sendText(s);

  const disabled = controlState.value !== "granted";

  return (
    <div class="terminal-page" style={{ paddingBottom: `${keyboardOffset.value}px` }}>
      <div class="term-header">
        <button class="back" aria-label="back" onClick={onBack}>‹</button>
        <div class="meta">
          <div class="name">
            {meta.value
              ? (meta.value.name
                  ? `${meta.value.tool} · ${meta.value.name}`
                  : meta.value.tool)
              : `session ${sessionId.slice(0, 8)}`}
          </div>
        </div>
        <span class={`badge conn-${connState.value}`}>{connState.value}</span>
      </div>
      <div class="control-bar">
        <span class={`ctrl-state ctrl-${controlState.value}`}>● {controlLabel(controlState.value)}</span>
        {controlState.value === "granted" ? (
          <button class="release-btn" onClick={() => window.releaseControl()}>Release</button>
        ) : (
          <button class="request-btn" onClick={() => window.requestControl()}>Request Control</button>
        )}
      </div>
      <div id="terminal" class="terminal-host"></div>
      <Composer disabled={disabled} onSend={onCompose} placeholder="Type and Send..." />
      <Toolbar disabled={disabled} onDigit={onDigit} onSpecial={onSpecial} />
    </div>
  );
}
