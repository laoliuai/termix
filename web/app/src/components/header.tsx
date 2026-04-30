import { useSignal } from "@preact/signals";
import { useEffect, useRef } from "preact/hooks";
import { userInfo } from "../auth/store";
import { locale, setLocale, t } from "../i18n/store";

export interface HeaderProps {
  onLogout?: () => void;
  onRefresh?: () => void;
  onHelp?: () => void;
  refreshing?: boolean;
  refreshDone?: boolean;
}

export function Header({ onLogout, onRefresh, onHelp, refreshing = false, refreshDone = false }: HeaderProps) {
  const menuOpen = useSignal(false);
  const actionsRef = useRef<HTMLDivElement>(null);
  const refreshClass = refreshing ? "is-refreshing" : refreshDone ? "is-refreshed" : "";
  const email = userInfo.value?.user.email;
  const hasAccountMenu = Boolean(onLogout || userInfo.value);
  const languageLabel = locale.value === "en" ? "Language: English" : "语言：中文";

  useEffect(() => {
    if (!menuOpen.value) return;

    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        menuOpen.value = false;
      }
    }

    function closeOnOutsideClick(event: MouseEvent) {
      if (!actionsRef.current?.contains(event.target as Node)) {
        menuOpen.value = false;
      }
    }

    document.addEventListener("keydown", closeOnEscape);
    document.addEventListener("mousedown", closeOnOutsideClick);
    return () => {
      document.removeEventListener("keydown", closeOnEscape);
      document.removeEventListener("mousedown", closeOnOutsideClick);
    };
  }, [menuOpen.value]);

  function toggleLanguage() {
    setLocale(locale.value === "en" ? "zh-CN" : "en");
  }

  return (
    <div class="page-header">
      <div class="brand">
        <img class="brand-mark" src="/icons/termix.svg?v=tmx" alt="" aria-hidden="true" />
        <span>Termix</span>
      </div>
      <div class="title-block">
        {email ? (
          <div class="subtitle">{email}</div>
        ) : null}
      </div>
      <div class="actions" ref={actionsRef}>
        {onHelp ? (
          <button type="button" class="icon text-icon" aria-label={t("nav.help")} onClick={onHelp}>
            ?
          </button>
        ) : null}
        {onRefresh ? (
          <button
            type="button"
            class={`icon ${refreshClass}`}
            aria-label={t("common.refresh")}
            aria-busy={refreshing ? "true" : "false"}
            disabled={refreshing}
            onClick={onRefresh}
          >
            {refreshDone ? "✓" : "↻"}
          </button>
        ) : null}
        {hasAccountMenu ? (
          <button
            type="button"
            class="account-button"
            aria-label="account menu"
            aria-haspopup="menu"
            aria-expanded={menuOpen.value ? "true" : "false"}
            onClick={() => { menuOpen.value = !menuOpen.value; }}
          >
            <span>{email ?? "Account"}</span>
            <span aria-hidden="true">⌄</span>
          </button>
        ) : null}
        {menuOpen.value && hasAccountMenu ? (
          <div class="menu account-menu" role="menu">
            {email ? <div class="menu-label" role="presentation">{email}</div> : null}
            <button type="button" role="menuitem" onClick={toggleLanguage}>{languageLabel}</button>
            {onLogout ? (
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  menuOpen.value = false;
                  onLogout();
                }}
              >
                {t("nav.logout")}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
