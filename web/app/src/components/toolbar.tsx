import type { SpecialKey } from "../protocol/types";

const DIGITS = ["0","1","2","3","4","5","6","7","8","9"] as const;
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
  onDigit: (d: string) => void;
  onSpecial: (k: SpecialKey) => void;
}

function keyClass(k: SpecialKey): string {
  if (k === "Enter") return "key-enter";
  if (k === "C-c" || k === "C-d") return "key-danger";
  return "";
}

export function Toolbar({ disabled, onDigit, onSpecial }: ToolbarProps) {
  return (
    <div class={`toolbar${disabled ? " is-disabled" : ""}`} aria-disabled={disabled}>
      <div class="row digits">
        {DIGITS.map(d => (
          <button onClick={() => onDigit(d)} disabled={disabled}>{d}</button>
        ))}
      </div>
      <div class="row nav">
        {NAV.map(k => (
          <button onClick={() => onSpecial(k)} disabled={disabled}>{GLYPHS[k]}</button>
        ))}
      </div>
      <div class="row edit">
        {EDIT.map(k => (
          <button class={keyClass(k)} onClick={() => onSpecial(k)} disabled={disabled}>{GLYPHS[k]}</button>
        ))}
      </div>
    </div>
  );
}
