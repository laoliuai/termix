import { useCallback, useEffect } from "preact/hooks";
import { useSignal } from "@preact/signals";
import { userInfo } from "../auth/store";
import { freshAccessToken } from "../auth/refresh";
import { notify } from "../app/store";
import { useKeyboardOffset } from "../hooks/useViewport";
import { useVisibility } from "../hooks/useVisibility";
import { Toolbar } from "../components/toolbar";
import { Composer } from "../components/composer";
import type { SpecialKey } from "../protocol/types";
import { getSession, type SessionSummary } from "../api/endpoints";
import { t } from "../i18n/store";

// Default relay URL: same origin /ws. In dev, Vite's server.proxy proxies
// /ws → ws://localhost:8090. In prod, deploy a reverse proxy so /ws on the
// public domain hits the relay. Override via VITE_RELAY_WS_URL when relay
// is on a different origin.
function defaultRelayUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/ws`;
}

export interface TerminalPageProps {
  sessionId: string;
  onBack: () => void;
}

type ConnState = "connecting" | "connected" | "disconnected" | "error";
type ControlState = "none" | "requesting" | "granted" | "denied" | "revoked";

function controlLabel(s: ControlState): string {
  switch (s) {
    case "granted": return t("terminal.control.granted");
    case "requesting": return t("terminal.control.requesting");
    case "denied": return t("terminal.control.denied");
    case "revoked": return t("terminal.control.revoked");
    default: return t("terminal.control.readOnly");
  }
}

export function TerminalPage({ sessionId, onBack }: TerminalPageProps) {
  const connState = useSignal<ConnState>("connecting");
  const controlState = useSignal<ControlState>("none");
  const meta = useSignal<SessionSummary | null>(null);
  const keyboardOffset = useKeyboardOffset();
  const relayUrl = (import.meta as any).env?.VITE_RELAY_WS_URL ?? defaultRelayUrl();

  const connectSession = useCallback(async (redirectOnFailure: boolean): Promise<boolean> => {
    const tok = await freshAccessToken();
    if (!tok || !userInfo.value) {
      notify(t("terminal.authExpired"), "warn");
      if (redirectOnFailure) onBack();
      return false;
    }
    window.setSession(sessionId, relayUrl, tok, userInfo.value.device.id);
    return true;
  }, [onBack, relayUrl, sessionId]);

  useVisibility(() => {
    if (connState.value === "disconnected" || connState.value === "error") {
      void connectSession(false);
    }
  });

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

    window.TermixBridge = {
      onConnectionState: (s) => { connState.value = s as ConnState; },
      onControlState:    (s, detail) => {
        controlState.value = s as ControlState;
        if (s === "denied") notify(detail ? `${t("terminal.control.denied")}: ${detail}` : t("terminal.control.denied"), "warn");
        else if (s === "revoked") notify(detail ? `${t("terminal.control.revoked")}: ${detail}` : t("terminal.control.revoked"), "warn");
      },
      onError: async (code, msg) => {
        if (code === "auth" && !retried) {
          retried = true;
          notify(t("terminal.refreshing"), "warn");
          if (await connectSession(false)) {
            return;
          }
          notify(t("terminal.authExpired"), "error");
          onBack();
          return;
        }
        if (code === "watch") notify(msg, "error");
      },
    };

    void connectSession(true);

    return () => {
      window.setSession("", "", "", "");
      delete window.TermixBridge;
    };
  }, [connectSession]);

  const onDigit = (d: string) => window.sendText(d);
  const onSpecial = (k: SpecialKey) => window.sendSpecialKey(k);
  const onCompose = (s: string) => window.sendText(s);

  const disabled = controlState.value !== "granted";

  return (
    <div class="terminal-page" style={{ paddingBottom: `${keyboardOffset.value}px` }}>
      <div class="term-header">
        <button class="back" aria-label={t("common.back")} onClick={onBack}>‹</button>
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
          <button class="release-btn" onClick={() => window.releaseControl()}>{t("terminal.button.release")}</button>
        ) : (
          <button class="request-btn" onClick={() => window.requestControl()}>{t("terminal.button.request")}</button>
        )}
      </div>
      <div id="terminal" class="terminal-host"></div>
      <Composer disabled={disabled} onSend={onCompose} placeholder={t("terminal.placeholder")} />
      <Toolbar disabled={disabled} onDigit={onDigit} onSpecial={onSpecial} />
    </div>
  );
}
