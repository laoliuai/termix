import { useSignal } from "@preact/signals";
import { t } from "../i18n/store";

export interface CommandBlockProps {
  command: string;
  className?: string;
}

type CopyState = "idle" | "copied" | "failed";

async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall through to legacy path
  }
  if (typeof document === "undefined") return false;
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "absolute";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  document.body.removeChild(textarea);
  return ok;
}

export function CommandBlock({ command, className }: CommandBlockProps) {
  const state = useSignal<CopyState>("idle");
  const resetTimer = useSignal<number | null>(null);

  const wrapClass = ["command-row", className].filter(Boolean).join(" ");
  const buttonLabel =
    state.value === "copied" ? t("common.copied") :
    state.value === "failed" ? t("common.copyFailed") :
    t("common.copy");
  const buttonClass = ["copy-button", state.value === "copied" ? "is-copied" : "", state.value === "failed" ? "is-failed" : ""].filter(Boolean).join(" ");

  const handleClick = async () => {
    if (resetTimer.value !== null) {
      window.clearTimeout(resetTimer.value);
      resetTimer.value = null;
    }
    const ok = await writeClipboard(command);
    state.value = ok ? "copied" : "failed";
    resetTimer.value = window.setTimeout(() => {
      state.value = "idle";
      resetTimer.value = null;
    }, 1600);
  };

  return (
    <div class={wrapClass}>
      <pre><code>{command}</code></pre>
      <button
        type="button"
        class={buttonClass}
        onClick={handleClick}
        aria-label={t("common.copy")}
        title={buttonLabel}
      >
        {state.value === "copied" ? <CheckIcon /> : <ClipboardIcon />}
        <span class="copy-button-label">{buttonLabel}</span>
      </button>
    </div>
  );
}

function ClipboardIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <rect x="9" y="3" width="10" height="4" rx="1" />
      <path d="M9 5H6a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-3" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <polyline points="5 12 10 17 19 7" />
    </svg>
  );
}
