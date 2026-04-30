import { useEffect } from "preact/hooks";
import { useComputed, useSignal } from "@preact/signals";
import { listSessions, logout, type SessionSummary } from "../api/endpoints";
import { clearAuth } from "../auth/store";
import { notify } from "../app/store";
import { useVisibility } from "../hooks/useVisibility";
import { Header } from "../components/header";
import { t } from "../i18n/store";

export interface SessionsPageProps {
  onOpen: (sessionId: string) => void;
  onLogout: () => void;
  onHelp: () => void;
}

type SessionFilter = "running" | "all";

function displayName(s: SessionSummary): string {
  return s.name ? `${s.tool} · ${s.name}` : s.tool;
}

function timestamp(s: SessionSummary): number {
  const raw = s.last_activity_at ?? s.created_at ?? "";
  const parsed = Date.parse(raw);
  return Number.isFinite(parsed) ? parsed : 0;
}

function recentLabel(s: SessionSummary): string {
  const ts = timestamp(s);
  if (ts === 0) return t("sessions.activeNow");
  const ageMs = Date.now() - ts;
  if (ageMs < 60_000) return t("sessions.activeNow");
  const mins = Math.max(1, Math.round(ageMs / 60_000));
  return `${mins}m ago`;
}

function sortedSessions(items: SessionSummary[]): SessionSummary[] {
  return [...items].sort((a, b) => {
    if (a.status !== b.status) return a.status === "running" ? -1 : 1;
    return timestamp(b) - timestamp(a);
  });
}

function matchesSearch(s: SessionSummary, query: string): boolean {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return true;
  return [s.tool, s.name, s.host_label, s.status].filter(Boolean).join(" ").toLowerCase().includes(normalized);
}

export function SessionsPage({ onOpen, onLogout, onHelp }: SessionsPageProps) {
  const items = useSignal<SessionSummary[] | null>(null);
  const filter = useSignal<SessionFilter>("running");
  const search = useSignal("");
  const refreshing = useSignal(false);
  const refreshDone = useSignal(false);
  const refreshDoneTimer = useSignal<number | null>(null);
  const showSpinner = useSignal(false);
  const lastFetched = useSignal(0);

  const visibleItems = useComputed(() => sortedSessions(items.value ?? []).filter(s => matchesSearch(s, search.value)));
  const hostCount = useComputed(() => new Set((items.value ?? []).map(s => s.host_label).filter(Boolean)).size);
  const runningCount = useComputed(() => (items.value ?? []).filter(s => s.status === "running").length);

  const clearRefreshDoneTimer = (): void => {
    if (refreshDoneTimer.value !== null) window.clearTimeout(refreshDoneTimer.value);
    refreshDoneTimer.value = null;
  };

  const markRefreshDone = (): void => {
    clearRefreshDoneTimer();
    refreshDone.value = true;
    refreshDoneTimer.value = window.setTimeout(() => {
      refreshDone.value = false;
      refreshDoneTimer.value = null;
    }, 900);
  };

  const fetch = async (silent = false, showSuccess = false, nextFilter = filter.value): Promise<void> => {
    let loaded = false;
    if (!silent) {
      clearRefreshDoneTimer();
      refreshDone.value = false;
      refreshing.value = true;
    }
    try {
      const list = await listSessions(nextFilter);
      items.value = list;
      lastFetched.value = Date.now();
      loaded = true;
    } catch {
      if (!silent) notify(t("sessions.loadFailed"), "warn");
    } finally {
      if (!silent) {
        refreshing.value = false;
        if (loaded && showSuccess) markRefreshDone();
      }
      showSpinner.value = false;
    }
  };

  const setFilter = (next: SessionFilter): void => {
    if (filter.value === next) return;
    filter.value = next;
    void fetch(false, false, next);
  };

  useEffect(() => {
    const timer = setTimeout(() => {
      if (items.value === null) showSpinner.value = true;
    }, 200);
    fetch();
    return () => {
      clearTimeout(timer);
      clearRefreshDoneTimer();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useVisibility(() => {
    if (Date.now() - lastFetched.value > 5000) fetch(true);
  });

  const doLogout = async () => {
    await logout();
    clearAuth();
    onLogout();
    notify(t("sessions.loggedOut"), "info");
  };

  return (
    <div class="sessions-page">
      <Header
        onLogout={doLogout}
        onRefresh={() => fetch(false, true)}
        onHelp={onHelp}
        refreshing={refreshing.value}
        refreshDone={refreshDone.value}
      />
      <main class="sessions-workbench">
        <section class="sessions-hero">
          <div>
            <h1>{t("sessions.title")}</h1>
            <p>{t("sessions.subtitle")}</p>
          </div>
          <div class="session-stats" aria-label={t("sessions.statsAria")}>
            <div><strong>{runningCount.value}</strong><span>{t("sessions.runningCount")}</span></div>
            <div><strong>{hostCount.value}</strong><span>{t("sessions.hostsCount")}</span></div>
          </div>
        </section>

        <section class="sessions-tools">
          <input
            class="session-search"
            type="search"
            placeholder={t("common.search")}
            value={search.value}
            onInput={e => { search.value = (e.currentTarget as HTMLInputElement).value; }}
          />
          <div class="segmented">
            <button type="button" class={filter.value === "running" ? "active" : ""} onClick={() => setFilter("running")}>{t("sessions.running")}</button>
            <button type="button" class={filter.value === "all" ? "active" : ""} onClick={() => setFilter("all")}>{t("sessions.all")}</button>
          </div>
        </section>

        {items.value === null && showSpinner.value ? (
          <div class="page-spinner">{t("common.loading")}</div>
        ) : null}

        {items.value && items.value.length === 0 ? (
          <div class="empty-state sessions-empty">
            <div class="empty-glyph">TMX</div>
            <p class="empty-title">{t("sessions.empty.title")}</p>
            <p class="empty-sub">{t("sessions.empty.body")}</p>
            <ol class="empty-steps">
              <li>{t("sessions.empty.stepInstall")}</li>
              <li><code>termix login</code></li>
              <li><code>termix start codex --name main</code></li>
            </ol>
            <button type="button" class="btn-secondary" onClick={onHelp}>{t("nav.help")}</button>
          </div>
        ) : null}

        {items.value && items.value.length > 0 ? (
          <>
            <div class="sessions-desktop-table">
              {visibleItems.value.map(s => (
                <button class="session-row-rich" data-testid="session-row" key={s.id} onClick={() => onOpen(s.id)} aria-label={`Open ${s.tool} ${s.name}`}>
                  <strong><span class="session-status"></span>{displayName(s)}</strong>
                  <span>{s.host_label || t("sessions.noHost")}</span>
                  <span>{recentLabel(s)}</span>
                  <span class="badge">{s.status === "running" ? "live" : s.status}</span>
                  <span class="session-command">termix start {s.tool}</span>
                  <span class="open-cell">{t("common.open")}</span>
                </button>
              ))}
            </div>
            <div class="sessions-mobile-list">
              {visibleItems.value.map(s => (
                <button class="session-mobile-card" key={s.id} onClick={() => onOpen(s.id)} aria-label={`Open ${s.tool} ${s.name}`}>
                  <span>
                    <strong><span class="session-status"></span>{displayName(s)}</strong>
                    <small>{s.host_label || t("sessions.noHost")} · {recentLabel(s)}</small>
                  </span>
                  <span class="badge">{s.status === "running" ? "live" : s.status}</span>
                </button>
              ))}
            </div>
          </>
        ) : null}
      </main>
    </div>
  );
}
