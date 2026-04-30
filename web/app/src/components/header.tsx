import { useSignal } from "@preact/signals";
import { userInfo } from "../auth/store";

export interface HeaderProps {
  onLogout?: () => void;
  onRefresh?: () => void;
  onHelp?: () => void;
  refreshing?: boolean;
  refreshDone?: boolean;
}

export function Header({ onLogout, onRefresh, onHelp, refreshing = false, refreshDone = false }: HeaderProps) {
  const menuOpen = useSignal(false);
  const refreshClass = refreshing ? "is-refreshing" : refreshDone ? "is-refreshed" : "";
  return (
    <div class="page-header">
      <div class="brand">
        <img class="brand-mark" src="/icons/termix.svg?v=tmx" alt="" aria-hidden="true" />
        <span>Termix</span>
      </div>
      <div class="title-block">
        {userInfo.value ? (
          <div class="subtitle">{userInfo.value.user.email}</div>
        ) : null}
      </div>
      <div class="actions">
        {onRefresh ? (
          <button
            class={`icon ${refreshClass}`}
            aria-label="refresh"
            aria-busy={refreshing ? "true" : "false"}
            disabled={refreshing}
            onClick={onRefresh}
          >
            {refreshDone ? "✓" : "↻"}
          </button>
        ) : null}
        <button class="icon" aria-label="menu" onClick={() => { menuOpen.value = !menuOpen.value; }}>⋮</button>
        {menuOpen.value && (onLogout || onHelp) ? (
          <div class="menu" role="menu">
            {onHelp ? <button role="menuitem" onClick={() => { menuOpen.value = false; onHelp(); }}>Help</button> : null}
            {onLogout ? <button role="menuitem" onClick={() => { menuOpen.value = false; onLogout(); }}>Logout</button> : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
