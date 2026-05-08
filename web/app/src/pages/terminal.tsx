import { useCallback, useEffect } from "preact/hooks";
import { useSignal } from "@preact/signals";
import { userInfo } from "../auth/store";
import { freshAccessToken } from "../auth/refresh";
import { notify } from "../app/store";
import { useKeyboardOffset } from "../hooks/useViewport";
import { useVisibility } from "../hooks/useVisibility";
import { Toolbar } from "../components/toolbar";
import { Composer } from "../components/composer";
import { ComposerDock } from "../components/composer-dock";
import { ReconnectBanner } from "../components/reconnect-banner";
import { DisconnectModal } from "../components/disconnect-modal";
import type { ConnectionState, SpecialKey } from "../protocol/types";
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

type ConnPhase = ConnectionState["phase"];
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
  const connState = useSignal<ConnectionState>({ phase: "connecting" });
  const controlState = useSignal<ControlState>("none");
  const meta = useSignal<SessionSummary | null>(null);
  // Wall-clock instant (ms) when supervisor entered gave-up; drives a live
  // duration counter in the modal so the user sees seconds tick up.
  const gaveUpAtMs = useSignal<number>(0);
  // Tick once per second while the gave-up modal is open.
  const nowMs = useSignal<number>(Date.now());
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
    const phase: ConnPhase = connState.value.phase;
    if (phase === "disconnected" || phase === "error" || phase === "gave-up") {
      void connectSession(false);
    }
  });

  // Keep nowMs ticking so the gave-up modal shows a live elapsed duration.
  useEffect(() => {
    if (connState.value.phase !== "gave-up") return;
    nowMs.value = Date.now();
    const id = setInterval(() => { nowMs.value = Date.now(); }, 1000);
    return () => clearInterval(id);
  }, [connState.value.phase]);

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
      onConnectionState: (s) => {
        if (s.phase === "gave-up" && connState.value.phase !== "gave-up") {
          gaveUpAtMs.value = Date.now();
        }
        connState.value = s;
      },
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
        <span class={`badge conn-${connState.value.phase}`}>{connState.value.phase}</span>
      </div>
      <div class="control-bar">
        <span class={`ctrl-state ctrl-${controlState.value}`}>● {controlLabel(controlState.value)}</span>
        {controlState.value === "granted" ? (
          <button class="release-btn" onClick={() => window.releaseControl()}>{t("terminal.button.release")}</button>
        ) : (
          <button class="request-btn" onClick={() => window.requestControl()}>{t("terminal.button.request")}</button>
        )}
      </div>
      <ReconnectBanner
        phase={connState.value.phase}
        attempt={connState.value.phase === "reconnecting" ? connState.value.attempt : 0}
      />
      <div id="terminal" class="terminal-host"></div>
      <ComposerDock open={controlState.value === "granted"}>
        <Composer disabled={false} onSend={onCompose} placeholder={t("terminal.placeholder")} />
        <Toolbar disabled={false} onDigit={onDigit} onSpecial={onSpecial} />
      </ComposerDock>
      <DisconnectModal
        open={connState.value.phase === "gave-up"}
        serverUrl={typeof window !== "undefined" ? window.location.host : "termix"}
        attempts={connState.value.phase === "gave-up" ? connState.value.attemptCount : 0}
        durationMs={
          connState.value.phase === "gave-up" && gaveUpAtMs.value > 0
            ? nowMs.value - gaveUpAtMs.value
            : 0
        }
        lastError={
          connState.value.phase === "gave-up" || connState.value.phase === "reconnecting"
            ? connState.value.lastError
            : ""
        }
        attemptHistory={
          connState.value.phase === "gave-up" ? connState.value.attemptHistory : undefined
        }
        onReload={() => window.location.reload()}
        onRetry={() => {
          const w = window as { retryRelay?: () => void };
          w.retryRelay?.();
        }}
      />
    </div>
  );
}
