import { mountTerminal } from "@/ui/terminal";
import { installInboundBridge } from "@/bridge/inbound";

const container = document.getElementById("terminal");
if (!container) {
  throw new Error("missing #terminal container");
}
const ui = mountTerminal(container);
installInboundBridge({ ui });
