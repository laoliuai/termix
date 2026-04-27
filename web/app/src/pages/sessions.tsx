import { useEffect } from "preact/hooks";
import { useSignal } from "@preact/signals";
import { listSessions, logout, type SessionSummary } from "../api/endpoints";
import { clearAuth } from "../auth/store";
import { notify } from "../app/store";
import { useVisibility } from "../hooks/useVisibility";
import { Header } from "../components/header";

export interface SessionsPageProps {
  onOpen: (sessionId: string) => void;
  onLogout: () => void;
}

export function SessionsPage({ onOpen, onLogout }: SessionsPageProps) {
  const items = useSignal<SessionSummary[] | null>(null);
  const refreshing = useSignal(false);
  const showSpinner = useSignal(false);
  const lastFetched = useSignal(0);

  const fetch = async (silent = false): Promise<void> => {
    if (!silent) refreshing.value = true;
    try {
      const list = await listSessions("running");
      items.value = list;
      lastFetched.value = Date.now();
    } catch {
      if (!silent) notify("session 列表加载失败 — 下拉刷新", "warn");
    } finally {
      refreshing.value = false;
      showSpinner.value = false;
    }
  };

  useEffect(() => {
    const t = setTimeout(() => {
      if (items.value === null) showSpinner.value = true;
    }, 200);
    fetch();
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useVisibility(() => {
    if (Date.now() - lastFetched.value > 5000) fetch(true);
  });

  const doLogout = async () => {
    await logout();
    clearAuth();
    onLogout();
    notify("已退出登录", "info");
  };

  return (
    <div class="sessions-page">
      <Header
        title="Sessions"
        onLogout={doLogout}
        onRefresh={() => fetch(false)}
      />
      {items.value === null && showSpinner.value ? (
        <div class="page-spinner">Loading…</div>
      ) : null}
      {items.value && items.value.length === 0 ? (
        <div class="empty-state">
          <div class="empty-glyph">$_</div>
          <p class="empty-title">没有正在运行的 session</p>
          <p class="empty-sub">在你的主机上跑 <code>termix start &lt;tool&gt;</code> 来开一个</p>
        </div>
      ) : null}
      {items.value && items.value.length > 0 ? (
        <div class="session-list">
          {items.value.map(s => (
            <button class="card session-row" key={s.id} onClick={() => onOpen(s.id)}>
              <div class="session-icon">{s.tool.slice(0, 2)}</div>
              <div class="session-meta">
                <div class="session-title">{s.tool} · {s.name}</div>
                <div class="session-sub">
                  <span class="session-status"></span>
                  {s.status} {s.host_label ? `on ${s.host_label}` : ""}
                </div>
              </div>
              <span class="badge">{s.status === "running" ? "live" : s.status}</span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
