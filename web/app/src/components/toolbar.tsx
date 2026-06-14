import type { SpecialKey } from "../protocol/types";

const NAV: SpecialKey[] = ["Escape","Tab","Up","Down","Left","Right"];
const EDIT: SpecialKey[] = ["Backspace","C-c","C-d","C-j","Enter"];

const GLYPHS: Record<SpecialKey, string> = {
  Enter: "Enter",
  Tab: "Tab",
  Escape: "Esc",
  Up: "↑",
  Down: "↓",
  Left: "←",
  Right: "→",
  Backspace: "⌫",
  "C-c": "^C",
  "C-d": "^D",
  "C-j": "^J",
};

export interface ToolbarProps {
  disabled: boolean;
  onSpecial: (k: SpecialKey) => void;
}

function keyClass(k: SpecialKey): string {
  if (k === "Enter") return "key-enter";
  if (k === "C-c" || k === "C-d") return "key-danger";
  return "";
}

// preventDefault on mousedown stops the button from stealing focus from xterm's
// hidden textarea — critical on mobile, where losing textarea focus dismisses
// the soft keyboard. On touch the focus shift rides the *synthesized* mousedown
// (fired after touchend, before click), so this guard covers touch too.
//
// Deliberately NOT touchstart/pointerdown: calling preventDefault there cancels
// the compatibility click as well (per the touch/pointer specs), so onClick —
// which actually sends the key — would never fire on a tap. mousedown-cancel
// does NOT suppress click, so the key still fires via onClick everywhere.
const keepFocus = (e: Event) => e.preventDefault();

export function Toolbar({ disabled, onSpecial }: ToolbarProps) {
  return (
    <div class={`toolbar${disabled ? " is-disabled" : ""}`} aria-disabled={disabled}>
      <div class="row nav">
        {NAV.map(k => (
          <button onMouseDown={keepFocus} onClick={() => onSpecial(k)} disabled={disabled}>{GLYPHS[k]}</button>
        ))}
      </div>
      <div class="row edit">
        {EDIT.map(k => (
          <button class={keyClass(k)} onMouseDown={keepFocus} onClick={() => onSpecial(k)} disabled={disabled}>{GLYPHS[k]}</button>
        ))}
      </div>
    </div>
  );
}
