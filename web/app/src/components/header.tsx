import { useSignal } from "@preact/signals";
import { userInfo } from "../auth/store";

export interface HeaderProps {
  onLogout?: () => void;
  onRefresh?: () => void;
  refreshing?: boolean;
}

export function Header({ onLogout, onRefresh, refreshing = false }: HeaderProps) {
  const menuOpen = useSignal(false);
  return (
    <div class="page-header">
      <div class="brand">
        <img class="brand-mark" src="/icons/icon-192.png" alt="" aria-hidden="true" />
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
            class={`icon ${refreshing ? "is-refreshing" : ""}`}
            aria-label="refresh"
            aria-busy={refreshing ? "true" : "false"}
            disabled={refreshing}
            onClick={onRefresh}
          >
            ↻
          </button>
        ) : null}
        <button class="icon" aria-label="menu" onClick={() => { menuOpen.value = !menuOpen.value; }}>⋮</button>
        {menuOpen.value && onLogout ? (
          <div class="menu" role="menu">
            <button onClick={() => { menuOpen.value = false; onLogout(); }}>Logout</button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
