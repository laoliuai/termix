import { mountTerminal } from "@/ui/terminal";
import { installInboundBridge } from "@/bridge/inbound";
import type { ConnectionState, ControlState, SpecialKey, TermixBridge } from "@/protocol/types";

type WindowGlobals = {
  setSession?: (s: string, r: string, t: string, d: string) => void;
  sendText?: (t: string) => void;
  sendSpecialKey?: (k: SpecialKey) => void;
  requestControl?: () => void;
  releaseControl?: () => void;
  TermixBridge?: TermixBridge;
};
const w = window as unknown as WindowGlobals & Window;

const log = (text: string) => {
  const div = document.createElement("div");
  div.className = "line";
  div.textContent = `[${new Date().toLocaleTimeString()}] ${text}`;
  const el = document.getElementById("log")!;
  el.prepend(div);
};

// Restore form values from localStorage.
const fields = ["sessionId", "relayUrl", "accessToken", "deviceId"] as const;
for (const f of fields) {
  const v = localStorage.getItem(`terminal-web.${f}`);
  if (v) (document.getElementById(f) as HTMLInputElement).value = v;
}
for (const f of fields) {
  (document.getElementById(f) as HTMLInputElement).addEventListener("input", (e) => {
    localStorage.setItem(`terminal-web.${f}`, (e.target as HTMLInputElement).value);
  });
}

// Install bridge before mounting buttons so window.* globals exist.
const ui = mountTerminal(document.getElementById("terminal")!);
installInboundBridge({ ui });

// Install dev TermixBridge to surface state changes in the status panel.
w.TermixBridge = {
  onConnectionState(state: ConnectionState, detail?: string) {
    document.getElementById("conn")!.textContent = `connection: ${state}${detail ? ` (${detail})` : ""}`;
    log(`connection: ${state}${detail ? ` (${detail})` : ""}`);
  },
  onControlState(state: ControlState, detail?: string) {
    document.getElementById("ctrl")!.textContent = `control: ${state}${detail ? ` (${detail})` : ""}`;
    log(`control: ${state}${detail ? ` (${detail})` : ""}`);
  },
  onError(code: string, message: string) {
    log(`error: ${code} — ${message}`);
  },
};

const val = (id: string) => (document.getElementById(id) as HTMLInputElement).value;

document.getElementById("btnConnect")!.addEventListener("click", () => {
  w.setSession!(val("sessionId"), val("relayUrl"), val("accessToken"), val("deviceId"));
});
document.getElementById("btnDisconnect")!.addEventListener("click", () => {
  w.setSession!("", "", "", "");
});
document.getElementById("btnRequest")!.addEventListener("click", () => w.requestControl!());
document.getElementById("btnRelease")!.addEventListener("click", () => w.releaseControl!());
document.getElementById("btnSendText")!.addEventListener("click", () => {
  w.sendText!(val("text"));
  (document.getElementById("text") as HTMLInputElement).value = "";
});
for (const btn of Array.from(document.querySelectorAll<HTMLButtonElement>("button[data-key]"))) {
  btn.addEventListener("click", () => {
    const key = btn.dataset.key as SpecialKey;
    w.sendSpecialKey!(key);
  });
}
