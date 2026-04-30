import { render } from "preact";
import { route } from "preact-router";

import "../theme/styles.css";

import { AppRouter } from "../routes/Router";
import { Snackbar } from "../components/snackbar";
import { Splash } from "../components/splash";
import { splashing, snackbar } from "../app/store";
import { bootstrap } from "../auth/bootstrap";
import { t } from "../i18n/store";

// === Slice-1 bridge + xterm wiring ===
// installInboundBridge(cfg: InboundConfig) requires { ui: TerminalUI }.
// TerminalUI is the object returned by mountTerminal(container: HTMLElement).
// The #terminal container is rendered by TerminalPage, so it only exists in
// the DOM after navigation to /terminal/:sessionId.
//
// We use a MutationObserver to detect when #terminal is inserted and then
// mount xterm + install the bridge exactly once per visit to that route.
// On removal (navigating away) the xterm instance is disposed.
import { mountTerminal } from "../ui/terminal";
import { installInboundBridge } from "../bridge/inbound";
import type { TerminalUI } from "../ui/terminal";

function watchTerminalContainer(): void {
  let currentUI: TerminalUI | null = null;

  const tryMount = () => {
    const container = document.getElementById("terminal");
    if (container && !currentUI) {
      currentUI = mountTerminal(container);
      installInboundBridge({ ui: currentUI });
    }
    if (!container && currentUI) {
      currentUI.dispose();
      currentUI = null;
    }
  };

  const observer = new MutationObserver(() => tryMount());
  observer.observe(document.body, { childList: true, subtree: true });
  // Also try immediately in case the element is already present.
  tryMount();
}

// === PWA service worker registration ===
// vite-plugin-pwa generates this virtual module at build time.
// In dev (devOptions.enabled=false), this is a no-op.
import { registerSW } from "virtual:pwa-register";
const updateSW = registerSW({
  onNeedRefresh() {
    snackbar.value = {
      msg: t("pwa.updateAvailable"),
      kind: "info",
      action: { label: t("pwa.refresh"), cb: () => updateSW(true) },
    };
  },
});

// === Cold-start auth probe ===
bootstrap().then((res) => {
  if (res === "authed" && location.pathname === "/") {
    route("/sessions", true);
  } else if (res === "network-error") {
    snackbar.value = { msg: t("error.network"), kind: "warn" };
  }
});

// === Mount the SPA ===
function App() {
  return (
    <>
      {splashing.value ? <Splash /> : null}
      <AppRouter />
      <Snackbar />
    </>
  );
}

const root = document.getElementById("app");
if (!root) throw new Error("missing #app element");
render(<App />, root);

// Start watching for the terminal container after the SPA is mounted.
watchTerminalContainer();
