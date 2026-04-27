import { useSignal } from "@preact/signals";
import { userInfo } from "../auth/store";

export interface HeaderProps {
  title: string;
  onLogout?: () => void;
  onRefresh?: () => void;
}

export function Header({ title, onLogout, onRefresh }: HeaderProps) {
  const menuOpen = useSignal(false);
  return (
    <div class="page-header">
      <div class="brand">
        <span class="brand-glyph">{">_"}</span>
        <span>Termix</span>
      </div>
      <div class="title-block">
        <div class="title">{title}</div>
        {userInfo.value ? (
          <div class="subtitle">{userInfo.value.user.email}</div>
        ) : null}
      </div>
      <div class="actions">
        {onRefresh ? (
          <button class="icon" aria-label="refresh" onClick={onRefresh}>↻</button>
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
