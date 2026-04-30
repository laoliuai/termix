# Web UI Productization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Web UI into a product surface with a public homepage, `/login`, responsive Sessions workbench, cleaner navigation, and Chinese/English language switching.

**Architecture:** Keep the existing Preact/Vite app and Go static embedding path. Add a small local i18n layer, split the logged-out product homepage from the login form, then redesign shared navigation and Sessions while preserving the existing auth, refresh, relay, and terminal control logic.

**Tech Stack:** Preact, `@preact/signals`, `preact-router`, Vite, Vitest, Testing Library, CSS media queries, existing Go `go:embed` Web asset pipeline.

**Execution Rule:** Implement this plan with subagent-driven development in a project-local git worktree under `.worktrees/web-ui-productization`. Do not implement directly in the main checkout.

---

## File Structure

- Create: `web/app/src/i18n/messages.ts`
  - Owns all user-facing Web UI copy for `zh-CN` and `en`.
  - Exports `type Locale`, `type MessageKey`, `messages`, and `defaultLocale`.
- Create: `web/app/src/i18n/store.ts`
  - Owns selected locale state, browser-language defaulting, `setLocale`, and `t`.
- Create: `web/app/src/i18n/store.test.ts`
  - Covers default language selection, persistence, fallback, and translation lookup.
- Create: `web/app/src/pages/home.tsx`
  - Public homepage for `/`.
  - Shows product positioning, install command, Help/Login/Sessions navigation, and language switcher.
- Create: `web/app/src/pages/home.test.tsx`
  - Covers homepage content and CTA callbacks in both auth states.
- Modify: `web/app/src/routes/Router.tsx`
  - Splits `/` homepage from `/login`.
  - Redirects unauthenticated protected routes to `/login`.
  - Keeps `/help` public and back-aware.
- Modify: `web/app/src/routes/AuthGuard.tsx`
  - Redirects protected unauthenticated routes to `/login`.
- Modify: `web/app/src/entry/main.tsx`
  - Keeps successful cold-start refresh redirect from `/` to `/sessions`.
  - Localizes service-worker and network snackbars.
- Modify: `web/app/src/pages/login.tsx`
  - Keeps the existing login form, moves it behind `/login`, localizes copy, and adds home/help links.
- Modify: `web/app/src/pages/login.test.tsx`
  - Updates tests for localized strings and home/help navigation.
- Modify: `web/app/src/components/header.tsx`
  - Replaces the hidden Help/Logout menu with explicit Help/Refresh actions and an account menu.
  - Adds language selector in the account menu.
- Modify: `web/app/src/components/header.test.tsx`
  - Covers direct Help, Refresh, account menu, language switching, and Logout.
- Modify: `web/app/src/pages/help.tsx`
  - Keeps `/help`, converts copy to i18n, and updates wording to product-facing help center.
- Modify: `web/app/src/pages/help.test.tsx`
  - Covers translated install, platform downloads, workflow, and no `termixd`.
- Modify: `web/app/src/pages/sessions.tsx`
  - Converts simple list into responsive workbench.
  - Adds local search, Running/All filter, sorted rows, desktop metadata, mobile compact cards, and localized empty state.
- Modify: `web/app/src/pages/sessions.test.tsx`
  - Covers workbench metadata, sorting, filter calls, search, mobile-only class markers, empty guidance, refresh, Help, account Logout.
- Modify: `web/app/src/pages/terminal.tsx`
  - Localizes visible control labels, buttons, placeholder, and snackbars.
- Modify: `web/app/src/pages/terminal.test.tsx`
  - Updates visible text expectations for localized labels.
- Modify: `web/app/src/components/composer.tsx`
  - Accepts localized placeholder from caller; no internal English default should leak to product pages.
- Modify: `web/app/src/components/snackbar.test.tsx`
  - Keeps action behavior stable with localized labels.
- Modify: `web/app/src/theme/styles.css`
  - Adds homepage, new login shell, header/account menu, sessions workbench, desktop table, and mobile card styles.
  - Replaces the current beige-heavy logged-out visual direction with a more product-like, technology-forward palette.
- Modify: `docs/PROGRESS.md`
  - Record task status before and after implementation.

## Scope Check

This plan is one cohesive frontend productization slice. It touches several UI surfaces, but they are not independent products: i18n, routing, homepage, navigation, Sessions, and Help must work together for the approved user flow. The plan is split into commit-sized tasks so each task can be tested and reviewed independently.

---

### Task 1: Add Lightweight I18n Foundation

**Files:**
- Create: `web/app/src/i18n/messages.ts`
- Create: `web/app/src/i18n/store.ts`
- Create: `web/app/src/i18n/store.test.ts`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Web UI productization Task 1: add lightweight i18n foundation for Chinese and English UI copy.
```

- [ ] **Step 2: Write failing i18n tests**

Create `web/app/src/i18n/store.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";

describe("i18n store", () => {
  beforeEach(() => {
    vi.resetModules();
    localStorage.clear();
    Object.defineProperty(navigator, "language", {
      value: "en-US",
      configurable: true,
    });
  });

  it("defaults to English for an English browser", async () => {
    const { locale, t } = await import("./store");
    expect(locale.value).toBe("en");
    expect(t("nav.signIn")).toBe("Sign in");
  });

  it("defaults to Chinese for a Chinese browser", async () => {
    Object.defineProperty(navigator, "language", {
      value: "zh-CN",
      configurable: true,
    });
    const { locale, t } = await import("./store");
    expect(locale.value).toBe("zh-CN");
    expect(t("nav.signIn")).toBe("登录");
  });

  it("persists explicit locale selection", async () => {
    const { locale, setLocale, t } = await import("./store");
    setLocale("zh-CN");
    expect(locale.value).toBe("zh-CN");
    expect(localStorage.getItem("termix.locale")).toBe("zh-CN");
    expect(t("nav.help")).toBe("帮助");
  });

  it("falls back to English for an unsupported saved locale", async () => {
    localStorage.setItem("termix.locale", "fr");
    const { locale, t } = await import("./store");
    expect(locale.value).toBe("en");
    expect(t("common.refresh")).toBe("Refresh");
  });
});
```

- [ ] **Step 3: Run tests and verify failure**

Run:

```bash
cd web/app && rtk npm test -- --run src/i18n/store.test.ts
```

Expected: FAIL because `src/i18n/store.ts` does not exist.

- [ ] **Step 4: Add message dictionary**

Create `web/app/src/i18n/messages.ts`:

```ts
export type Locale = "zh-CN" | "en";

export const defaultLocale: Locale = "en";

export const messages = {
  en: {
    "brand.name": "Termix",
    "nav.help": "Help",
    "nav.signIn": "Sign in",
    "nav.sessions": "Sessions",
    "nav.language": "Language",
    "nav.logout": "Logout",
    "common.refresh": "Refresh",
    "common.open": "Open",
    "common.back": "Back",
    "common.loading": "Loading...",
    "common.updatedNow": "updated just now",
    "common.search": "Search by tool, name, host...",
    "home.hero.title": "Take over AI coding sessions running on your host",
    "home.hero.subtitle": "Termix brings tmux sessions from your host to the browser. It is built for Claude, Codex, and opencode, and also fits long-running scripts, builds, and background jobs.",
    "home.cta.install": "Install Termix",
    "home.cta.help": "View Help",
    "home.cta.sessions": "Open Sessions",
    "home.value.browser.title": "Browser control",
    "home.value.browser.body": "View and control sessions from desktop or mobile.",
    "home.value.tmux.title": "Native tmux sessions",
    "home.value.tmux.body": "Work keeps running on your host even if the browser closes.",
    "home.value.install.title": "One-line install",
    "home.value.install.body": "Set up the macOS or Ubuntu host client quickly.",
    "login.title": "Sign in to Termix",
    "login.email": "Email",
    "login.password": "Password",
    "login.submit": "Sign in",
    "login.signingIn": "Signing in...",
    "login.badCredentials": "Invalid email or password",
    "login.rateLimited": "Too many attempts. Try again later.",
    "login.network": "Cannot connect to server",
    "login.installHint": "Sessions are created from your host with termix start.",
    "sessions.title": "Sessions",
    "sessions.subtitle": "Take over AI and long-running tasks on your hosts.",
    "sessions.running": "Running",
    "sessions.all": "All",
    "sessions.runningCount": "running",
    "sessions.hostsCount": "hosts",
    "sessions.noHost": "unknown host",
    "sessions.activeNow": "active now",
    "sessions.empty.title": "No running sessions",
    "sessions.empty.body": "Install Termix, log in on your host, then start your first session.",
    "sessions.empty.stepInstall": "Install Termix",
    "sessions.empty.stepLogin": "Run termix login on the host",
    "sessions.empty.stepStart": "Start a session",
    "sessions.loadFailed": "Session list failed to load",
    "sessions.loggedOut": "Signed out",
    "help.title": "Install Termix",
    "help.kicker": "Help center",
    "help.download": "Download",
    "help.oneLine": "One-line install",
    "help.startSession": "Start a session",
    "help.supportedTools": "Supported tools",
    "help.troubleshooting": "Troubleshooting",
    "help.backgroundService": "Termix starts its local background service automatically when you create or attach a session.",
    "terminal.control.granted": "You have control",
    "terminal.control.requesting": "Requesting...",
    "terminal.control.denied": "Control denied",
    "terminal.control.revoked": "Control revoked",
    "terminal.control.readOnly": "Read-only",
    "terminal.button.release": "Release",
    "terminal.button.request": "Request Control",
    "terminal.placeholder": "Type and Send...",
    "terminal.authExpired": "Session expired. Please sign in again.",
    "terminal.refreshing": "Session expired. Refreshing...",
    "pwa.updateAvailable": "New version available",
    "pwa.refresh": "Refresh",
    "error.network": "Cannot connect to server",
  },
  "zh-CN": {
    "brand.name": "Termix",
    "nav.help": "帮助",
    "nav.signIn": "登录",
    "nav.sessions": "Sessions",
    "nav.language": "语言",
    "nav.logout": "退出登录",
    "common.refresh": "刷新",
    "common.open": "打开",
    "common.back": "返回",
    "common.loading": "加载中...",
    "common.updatedNow": "刚刚更新",
    "common.search": "按 tool、名称、主机搜索...",
    "home.hero.title": "接管主机上的 AI coding session",
    "home.hero.subtitle": "在浏览器里查看和控制主机 tmux 中的 Claude、Codex、opencode，也适合长时间运行的脚本、构建和后台任务。",
    "home.cta.install": "安装 Termix",
    "home.cta.help": "查看帮助",
    "home.cta.sessions": "进入 Sessions",
    "home.value.browser.title": "浏览器接管",
    "home.value.browser.body": "手机和 PC 都能查看、控制 session。",
    "home.value.tmux.title": "tmux 原生会话",
    "home.value.tmux.body": "工作在主机上持续运行，浏览器关闭也不影响。",
    "home.value.install.title": "一行命令安装",
    "home.value.install.body": "快速接入 macOS 或 Ubuntu 主机客户端。",
    "login.title": "登录 Termix",
    "login.email": "邮箱",
    "login.password": "密码",
    "login.submit": "登录",
    "login.signingIn": "登录中...",
    "login.badCredentials": "邮箱或密码错误",
    "login.rateLimited": "尝试过于频繁，请稍候",
    "login.network": "无法连接服务器",
    "login.installHint": "Session 从主机上的 termix start 创建。",
    "sessions.title": "Sessions",
    "sessions.subtitle": "接管主机上正在运行的 AI 和长任务。",
    "sessions.running": "运行中",
    "sessions.all": "全部",
    "sessions.runningCount": "运行中",
    "sessions.hostsCount": "主机",
    "sessions.noHost": "未知主机",
    "sessions.activeNow": "刚刚活跃",
    "sessions.empty.title": "没有正在运行的 session",
    "sessions.empty.body": "安装 Termix，在主机登录，然后启动第一个 session。",
    "sessions.empty.stepInstall": "安装 Termix",
    "sessions.empty.stepLogin": "在主机运行 termix login",
    "sessions.empty.stepStart": "启动一个 session",
    "sessions.loadFailed": "session 列表加载失败",
    "sessions.loggedOut": "已退出登录",
    "help.title": "安装 Termix",
    "help.kicker": "帮助中心",
    "help.download": "下载",
    "help.oneLine": "一行命令安装",
    "help.startSession": "启动 session",
    "help.supportedTools": "支持的工具",
    "help.troubleshooting": "故障排查",
    "help.backgroundService": "创建或 attach session 时，Termix 会自动启动本地后台服务。",
    "terminal.control.granted": "你拥有控制权",
    "terminal.control.requesting": "正在请求...",
    "terminal.control.denied": "控制请求被拒绝",
    "terminal.control.revoked": "控制权已被收回",
    "terminal.control.readOnly": "只读",
    "terminal.button.release": "释放",
    "terminal.button.request": "请求控制",
    "terminal.placeholder": "输入后发送...",
    "terminal.authExpired": "会话已过期，请重新登录",
    "terminal.refreshing": "会话过期，正在刷新...",
    "pwa.updateAvailable": "新版本可用",
    "pwa.refresh": "刷新",
    "error.network": "无法连接服务器",
  },
} as const;

export type MessageKey = keyof typeof messages.en;
```

- [ ] **Step 5: Add locale store and translation helper**

Create `web/app/src/i18n/store.ts`:

```ts
import { signal } from "@preact/signals";
import { defaultLocale, messages, type Locale, type MessageKey } from "./messages";

const STORAGE_KEY = "termix.locale";

function isLocale(value: string | null): value is Locale {
  return value === "en" || value === "zh-CN";
}

function browserLocale(): Locale {
  const lang = navigator.language.toLowerCase();
  return lang.startsWith("zh") ? "zh-CN" : defaultLocale;
}

function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (isLocale(saved)) return saved;
  } catch {
    return browserLocale();
  }
  return browserLocale();
}

export const locale = signal<Locale>(initialLocale());

export function setLocale(next: Locale): void {
  locale.value = next;
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // Private browsing storage failures should not block language switching.
  }
}

export function t(key: MessageKey): string {
  return messages[locale.value][key] ?? messages[defaultLocale][key];
}
```

- [ ] **Step 6: Run i18n tests**

Run:

```bash
cd web/app && rtk npm test -- --run src/i18n/store.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
rtk git add docs/PROGRESS.md web/app/src/i18n/messages.ts web/app/src/i18n/store.ts web/app/src/i18n/store.test.ts
rtk git commit -m "Add Web UI i18n foundation"
```

---

### Task 2: Split Homepage and Login Routes

**Files:**
- Create: `web/app/src/pages/home.tsx`
- Create: `web/app/src/pages/home.test.tsx`
- Modify: `web/app/src/routes/Router.tsx`
- Modify: `web/app/src/routes/AuthGuard.tsx`
- Modify: `web/app/src/pages/login.tsx`
- Modify: `web/app/src/pages/login.test.tsx`
- Modify: `web/app/src/theme/styles.css`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Web UI productization Task 2: split public homepage from `/login` and update auth route behavior.
```

- [ ] **Step 2: Write failing homepage tests**

Create `web/app/src/pages/home.test.tsx`:

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/preact";

import { HomePage } from "./home";
import { accessToken, clearAuth } from "../auth/store";
import { setLocale } from "../i18n/store";

describe("HomePage", () => {
  beforeEach(() => {
    cleanup();
    clearAuth();
    localStorage.clear();
    setLocale("en");
  });

  it("renders product positioning and install command for logged-out users", () => {
    render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={() => {}} />);

    expect(screen.getByRole("heading", { name: /take over ai coding sessions/i })).toBeTruthy();
    expect(screen.getByText(/long-running scripts/i)).toBeTruthy();
    expect(screen.getByText(/curl -fsSL https:\/\/raw\.githubusercontent\.com\/termix\/termix\/main\/install\.sh \| sh/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();
  });

  it("shows Sessions CTA when already authenticated", () => {
    accessToken.value = "tk";
    render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={() => {}} />);

    expect(screen.getByRole("button", { name: "Open Sessions" })).toBeTruthy();
  });

  it("routes header actions through callbacks", () => {
    const onLogin = vi.fn();
    const onHelp = vi.fn();
    render(<HomePage onLogin={onLogin} onHelp={onHelp} onSessions={() => {}} />);

    fireEvent.click(screen.getByRole("button", { name: "Help" }));
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(onHelp).toHaveBeenCalledOnce();
    expect(onLogin).toHaveBeenCalledOnce();
  });

  it("renders Chinese copy after language switch", () => {
    setLocale("zh-CN");
    render(<HomePage onLogin={() => {}} onHelp={() => {}} onSessions={() => {}} />);

    expect(screen.getByRole("heading", { name: "接管主机上的 AI coding session" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "登录" })).toBeTruthy();
  });
});
```

- [ ] **Step 3: Run homepage test and verify failure**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/home.test.tsx
```

Expected: FAIL because `HomePage` does not exist.

- [ ] **Step 4: Create homepage component**

Create `web/app/src/pages/home.tsx`:

```tsx
import { accessToken } from "../auth/store";
import { locale, setLocale, t } from "../i18n/store";

const installCommand = "curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh";

export interface HomePageProps {
  onLogin: () => void;
  onHelp: () => void;
  onSessions: () => void;
}

export function HomePage({ onLogin, onHelp, onSessions }: HomePageProps) {
  const authed = accessToken.value !== null;
  return (
    <main class="home-page">
      <header class="home-nav">
        <div class="brand">
          <img class="brand-mark" src="/icons/termix.svg?v=tmx" alt="" aria-hidden="true" />
          <span>Termix</span>
        </div>
        <div class="home-nav-actions">
          <button type="button" class="nav-link" onClick={onHelp}>{t("nav.help")}</button>
          <button
            type="button"
            class="nav-link"
            onClick={() => setLocale(locale.value === "en" ? "zh-CN" : "en")}
          >
            {locale.value === "en" ? "中文" : "EN"}
          </button>
          <button type="button" class="nav-primary" onClick={authed ? onSessions : onLogin}>
            {authed ? t("home.cta.sessions") : t("nav.signIn")}
          </button>
        </div>
      </header>

      <section class="home-hero">
        <div class="home-hero-copy">
          <h1>{t("home.hero.title")}</h1>
          <p>{t("home.hero.subtitle")}</p>
          <div class="home-hero-actions">
            <button type="button" class="btn-primary home-cta" onClick={authed ? onSessions : onHelp}>
              {authed ? t("home.cta.sessions") : t("home.cta.install")}
            </button>
            <button type="button" class="btn-secondary home-cta" onClick={onHelp}>{t("home.cta.help")}</button>
          </div>
          <pre class="home-command"><code>{installCommand}</code></pre>
        </div>
        <div class="home-visual" aria-hidden="true">
          <div class="home-terminal-window">
            <div class="terminal-dots"><span></span><span></span><span></span></div>
            <pre>{`$ termix start codex --name main
connected to relay
session live: codex / main

> review the failing test`}</pre>
          </div>
        </div>
      </section>

      <section class="home-values" aria-label="Termix values">
        <article>
          <h2>{t("home.value.browser.title")}</h2>
          <p>{t("home.value.browser.body")}</p>
        </article>
        <article>
          <h2>{t("home.value.tmux.title")}</h2>
          <p>{t("home.value.tmux.body")}</p>
        </article>
        <article>
          <h2>{t("home.value.install.title")}</h2>
          <p>{t("home.value.install.body")}</p>
        </article>
      </section>

      <section class="home-install">
        <h2>{t("home.cta.install")}</h2>
        <pre class="command-block"><code>{installCommand}</code></pre>
        <ol>
          <li><code>termix login</code></li>
          <li><code>termix start codex --name main</code></li>
          <li>{t("home.cta.sessions")}</li>
        </ol>
      </section>
    </main>
  );
}
```

- [ ] **Step 5: Add routes for `/` and `/login`**

Modify `web/app/src/routes/Router.tsx`:

```tsx
import Router, { route } from "preact-router";
import { HomePage } from "../pages/home";
import { LoginPage } from "../pages/login";
import { SessionsPage } from "../pages/sessions";
import { TerminalPage } from "../pages/terminal";
import { HelpPage } from "../pages/help";
import { AuthGuard } from "./AuthGuard";
import { logout } from "../api/endpoints";
import { accessToken, clearAuth } from "../auth/store";

const HomeRoute = (_props: { path?: string }) => (
  <HomePage
    onLogin={() => route("/login")}
    onHelp={() => route("/help")}
    onSessions={() => route("/sessions")}
  />
);

const LoginRoute = (_props: { path?: string }) => (
  <LoginPage
    onSuccess={() => route("/sessions", true)}
    onHelp={() => route("/help")}
    onHome={() => route("/")}
  />
);

const SessionsRoute = (_props: Record<string, unknown>) => (
  <AuthGuard>
    <SessionsPage
      onOpen={(id) => route(`/terminal/${id}`)}
      onHelp={() => route("/help")}
      onLogout={async () => {
        await logout();
        clearAuth();
        route("/", true);
      }}
    />
  </AuthGuard>
);

const TerminalRoute = (props: { path?: string; sessionId?: string }) => (
  <AuthGuard>
    <TerminalPage
      sessionId={props.sessionId ?? ""}
      onBack={() => route("/sessions")}
    />
  </AuthGuard>
);

const HelpRoute = (_props: { path?: string }) => (
  <HelpPage onBack={() => route(accessToken.value ? "/sessions" : "/", true)} />
);

export function AppRouter() {
  return (
    <Router>
      <HomeRoute path="/" />
      <LoginRoute path="/login" />
      <HelpRoute path="/help" />
      <SessionsRoute path="/sessions" />
      <TerminalRoute path="/terminal/:sessionId" />
    </Router>
  );
}
```

- [ ] **Step 6: Update auth guard redirect**

Modify `web/app/src/routes/AuthGuard.tsx` so unauthenticated protected routes go to `/login`:

```tsx
import type { ComponentChildren } from "preact";
import { useEffect } from "preact/hooks";
import { useComputed } from "@preact/signals";
import { route } from "preact-router";
import { accessToken } from "../auth/store";
import { splashing } from "../app/store";

export function AuthGuard({ children }: { children: ComponentChildren }) {
  const guardState = useComputed(() => ({
    splashing: splashing.value,
    hasToken: accessToken.value !== null,
  }));

  useEffect(() => {
    if (!guardState.value.splashing && !guardState.value.hasToken) {
      route("/login", true);
    }
  }, [guardState.value.splashing, guardState.value.hasToken]);

  if (guardState.value.splashing) return null;
  if (!guardState.value.hasToken) return null;
  return <>{children}</>;
}
```

- [ ] **Step 7: Update login props and localized copy**

Modify `web/app/src/pages/login.tsx`:

```tsx
import { useSignal } from "@preact/signals";
import { login } from "../api/endpoints";
import { accessToken, accessTokenExpiresAt, userInfo } from "../auth/store";
import { t } from "../i18n/store";

const LAST_EMAIL_KEY = "termix.login.email";

function storedEmail(): string {
  try {
    return localStorage.getItem(LAST_EMAIL_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveEmail(value: string): void {
  try {
    if (value) localStorage.setItem(LAST_EMAIL_KEY, value);
    else localStorage.removeItem(LAST_EMAIL_KEY);
  } catch {
    // Storage can be disabled in private browsing; login should still work.
  }
}

export interface LoginPageProps {
  onSuccess: () => void;
  onHelp: () => void;
  onHome: () => void;
}

export function LoginPage({ onSuccess, onHelp, onHome }: LoginPageProps) {
  const email = useSignal(storedEmail());
  const password = useSignal("");
  const busy = useSignal(false);
  const error = useSignal<string | null>(null);

  const submit = async (e: Event) => {
    e.preventDefault();
    if (busy.value) return;
    busy.value = true;
    error.value = null;
    try {
      const res = await login({
        email: email.value,
        password: password.value,
        device_label: navigator.userAgent.slice(0, 80),
      });
      if (!res.ok) {
        error.value =
          res.status === 401 ? t("login.badCredentials") :
          res.status === 429 ? t("login.rateLimited") :
          res.status === 0   ? t("login.network") :
          (res.message || t("login.network"));
        busy.value = false;
        return;
      }
      accessToken.value = res.data.access_token;
      accessTokenExpiresAt.value = Date.now() + res.data.expires_in_seconds * 1000;
      userInfo.value = { user: res.data.user, device: res.data.device };
      onSuccess();
    } catch {
      error.value = t("login.network");
      busy.value = false;
    }
  };

  return (
    <div class="login-screen">
      <div class="login-shell">
        <button type="button" class="login-brand" onClick={onHome} aria-label="Termix home">
          <img class="brand-mark" src="/icons/termix.svg?v=tmx" alt="" aria-hidden="true" />
          <span>Termix</span>
        </button>
        <form class="login-page" onSubmit={submit}>
          <div class="brand-block">
            <h1>{t("login.title")}</h1>
            <p class="tagline">{t("login.installHint")}</p>
          </div>
          <label>
            <span class="input-label">{t("login.email")}</span>
            <input id="login-email" name="username" class="input-field" type="email" autocomplete="username" required
                   value={email.value}
                   onInput={e => {
                     email.value = (e.currentTarget as HTMLInputElement).value;
                     saveEmail(email.value);
                   }} />
          </label>
          <label>
            <span class="input-label">{t("login.password")}</span>
            <input id="login-password" name="password" class="input-field" type="password" autocomplete="current-password" required
                   value={password.value}
                   onInput={e => { password.value = (e.currentTarget as HTMLInputElement).value; }} />
          </label>
          {error.value ? <div class="form-error">{error.value}</div> : null}
          <button type="submit" class="btn-primary" disabled={busy.value}>
            {busy.value ? t("login.signingIn") : t("login.submit")}
          </button>
          <button class="link-button login-help-link" type="button" onClick={onHelp}>{t("home.cta.install")}</button>
        </form>
      </div>
    </div>
  );
}
```

- [ ] **Step 8: Update login tests**

In `web/app/src/pages/login.test.tsx`, update render calls to pass `onHome={() => {}}`, and change text expectations to accept localized strings:

```tsx
render(<LoginPage onSuccess={onSuccess} onHelp={() => {}} onHome={() => {}} />);
```

Add this test:

```tsx
it("calls home and help callbacks", () => {
  const onHome = vi.fn();
  const onHelp = vi.fn();
  render(<LoginPage onSuccess={() => {}} onHelp={onHelp} onHome={onHome} />);

  fireEvent.click(screen.getByRole("button", { name: "Termix home" }));
  fireEvent.click(screen.getByRole("button", { name: "Install Termix" }));

  expect(onHome).toHaveBeenCalledOnce();
  expect(onHelp).toHaveBeenCalledOnce();
});
```

- [ ] **Step 9: Add homepage/login styles**

Append to `web/app/src/theme/styles.css`:

```css
.home-page {
  min-height: 100%;
  background: #f6f8fb;
  color: #0f172a;
}
.home-nav {
  max-width: 1180px;
  margin: 0 auto;
  padding: 18px 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.home-nav-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.nav-link,
.nav-primary {
  border: none;
  background: transparent;
  color: #0f172a;
  font-size: 14px;
}
.nav-primary {
  background: #0f172a;
  color: #fff;
  border-radius: 999px;
  padding: 9px 14px;
  font-weight: 700;
}
.home-hero {
  max-width: 1180px;
  margin: 0 auto;
  padding: 34px 20px 28px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(320px, 460px);
  gap: 32px;
  align-items: center;
}
.home-hero-copy h1 {
  margin: 0 0 14px;
  font-size: clamp(36px, 5vw, 64px);
  line-height: 1.02;
  letter-spacing: 0;
}
.home-hero-copy p {
  max-width: 680px;
  color: #475569;
  font-size: 18px;
  line-height: 1.6;
}
.home-hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin: 24px 0 16px;
}
.home-cta {
  min-width: 138px;
  padding: 0 18px;
}
.btn-secondary {
  height: 48px;
  border: 1px solid #cbd5e1;
  border-radius: 10px;
  background: #fff;
  color: #0f172a;
  font-size: 16px;
  font-weight: 700;
}
.home-command {
  overflow-x: auto;
  background: #0b1020;
  color: #d1fae5;
  padding: 14px;
  border-radius: 10px;
  font-size: 13px;
}
.home-visual {
  min-height: 360px;
  border-radius: 22px;
  padding: 18px;
  background:
    radial-gradient(circle at 26% 18%, rgba(45, 212, 191, .86), transparent 28%),
    radial-gradient(circle at 78% 76%, rgba(59, 130, 246, .70), transparent 34%),
    linear-gradient(135deg, #101a33 0%, #142844 52%, #06101d 100%);
  border: 1px solid rgba(15, 23, 42, .14);
}
.home-terminal-window {
  background: rgba(2, 6, 23, .78);
  border: 1px solid rgba(255, 255, 255, .12);
  border-radius: 14px;
  padding: 14px;
  color: #b7ffe9;
  font-family: ui-monospace, monospace;
  min-height: 220px;
}
.terminal-dots {
  display: flex;
  gap: 6px;
  margin-bottom: 14px;
}
.terminal-dots span {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: rgba(255, 255, 255, .35);
}
.home-values,
.home-install {
  max-width: 1180px;
  margin: 0 auto;
  padding: 20px;
}
.home-values {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 14px;
}
.home-values article,
.home-install {
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 14px;
  padding: 18px;
}
.login-shell {
  width: 100%;
  max-width: 420px;
  margin: 0 auto;
  padding: 32px 20px;
}
.login-brand {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: none;
  background: transparent;
  color: var(--fg);
  font-weight: 800;
  margin-bottom: 18px;
}
.login-help-link {
  margin: 4px auto 0;
}
@media (max-width: 760px) {
  .home-nav {
    align-items: flex-start;
  }
  .home-nav-actions {
    gap: 6px;
  }
  .home-hero,
  .home-values {
    grid-template-columns: 1fr;
  }
  .home-hero {
    padding-top: 14px;
  }
  .home-visual {
    min-height: 260px;
  }
}
```

- [ ] **Step 10: Run focused tests**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/home.test.tsx src/pages/login.test.tsx
```

Expected: PASS.

- [ ] **Step 11: Commit**

Run:

```bash
rtk git add docs/PROGRESS.md web/app/src/pages/home.tsx web/app/src/pages/home.test.tsx web/app/src/routes/Router.tsx web/app/src/routes/AuthGuard.tsx web/app/src/pages/login.tsx web/app/src/pages/login.test.tsx web/app/src/theme/styles.css
rtk git commit -m "Add Web product homepage and login route"
```

---

### Task 3: Redesign Authenticated Header Navigation

**Files:**
- Modify: `web/app/src/components/header.tsx`
- Modify: `web/app/src/components/header.test.tsx`
- Modify: `web/app/src/theme/styles.css`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Web UI productization Task 3: redesign authenticated header navigation with direct Help, Refresh, account menu, and language switching.
```

- [ ] **Step 2: Update failing header tests**

Replace the Help menu test in `web/app/src/components/header.test.tsx` with:

```tsx
it("exposes Help directly instead of hiding it in the account menu", () => {
  const onHelp = vi.fn();
  render(<Header onHelp={onHelp} />);

  fireEvent.click(screen.getByRole("button", { name: "Help" }));

  expect(onHelp).toHaveBeenCalledOnce();
});
```

Add:

```tsx
it("opens account menu with language and logout actions", () => {
  const onLogout = vi.fn();
  render(<Header onLogout={onLogout} />);

  fireEvent.click(screen.getByRole("button", { name: "account menu" }));
  expect(screen.getByRole("menuitem", { name: "Language: English" })).toBeTruthy();

  fireEvent.click(screen.getByRole("menuitem", { name: "Language: English" }));
  expect(screen.getByRole("menuitem", { name: "语言：中文" })).toBeTruthy();

  fireEvent.click(screen.getByRole("menuitem", { name: "退出登录" }));
  expect(onLogout).toHaveBeenCalledOnce();
});

it("keeps refresh as a direct action when provided", () => {
  const onRefresh = vi.fn();
  render(<Header onRefresh={onRefresh} />);

  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

  expect(onRefresh).toHaveBeenCalledOnce();
});
```

- [ ] **Step 3: Run header tests and verify failure**

Run:

```bash
cd web/app && rtk npm test -- --run src/components/header.test.tsx
```

Expected: FAIL because current Help is hidden behind `menu` and account menu language does not exist.

- [ ] **Step 4: Replace header implementation**

Modify `web/app/src/components/header.tsx`:

```tsx
import { useSignal } from "@preact/signals";
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
  const refreshClass = refreshing ? "is-refreshing" : refreshDone ? "is-refreshed" : "";
  const languageLabel = locale.value === "en" ? "Language: English" : "语言：中文";

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
        {onHelp ? (
          <button class="icon text-icon" aria-label={t("nav.help")} onClick={onHelp}>?</button>
        ) : null}
        {onRefresh ? (
          <button
            class={`icon ${refreshClass}`}
            aria-label={t("common.refresh")}
            aria-busy={refreshing ? "true" : "false"}
            disabled={refreshing}
            onClick={onRefresh}
          >
            {refreshDone ? "✓" : "↻"}
          </button>
        ) : null}
        {(onLogout || userInfo.value) ? (
          <button class="account-button" aria-label="account menu" onClick={() => { menuOpen.value = !menuOpen.value; }}>
            <span>{userInfo.value?.user.email ?? "Account"}</span>
            <span aria-hidden="true">⌄</span>
          </button>
        ) : null}
        {menuOpen.value ? (
          <div class="menu account-menu" role="menu">
            {userInfo.value ? <div class="menu-label">{userInfo.value.user.email}</div> : null}
            <button role="menuitem" onClick={() => setLocale(locale.value === "en" ? "zh-CN" : "en")}>{languageLabel}</button>
            {onLogout ? <button role="menuitem" onClick={() => { menuOpen.value = false; onLogout(); }}>{t("nav.logout")}</button> : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
```

- [ ] **Step 5: Add header styles**

Modify the existing header section in `web/app/src/theme/styles.css`:

```css
.page-header .actions {
  display: flex;
  gap: 6px;
  position: relative;
  align-items: center;
}
.page-header .icon.text-icon {
  font-weight: 800;
}
.account-button {
  max-width: min(220px, 38vw);
  height: 34px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--border);
  background: var(--card);
  color: var(--fg);
  border-radius: 999px;
  padding: 0 10px;
  font-size: 12px;
}
.account-button span:first-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.account-menu {
  min-width: 190px;
}
.menu-label {
  padding: 10px 14px 6px;
  color: var(--muted);
  font-size: 12px;
  border-bottom: 1px solid var(--border);
  overflow: hidden;
  text-overflow: ellipsis;
}
@media (max-width: 520px) {
  .account-button {
    width: 34px;
    padding: 0;
    justify-content: center;
  }
  .account-button span:first-child {
    display: none;
  }
}
```

- [ ] **Step 6: Run header tests**

Run:

```bash
cd web/app && rtk npm test -- --run src/components/header.test.tsx
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
rtk git add docs/PROGRESS.md web/app/src/components/header.tsx web/app/src/components/header.test.tsx web/app/src/theme/styles.css
rtk git commit -m "Redesign Web app header navigation"
```

---

### Task 4: Convert Help Page to Bilingual Help Center

**Files:**
- Modify: `web/app/src/pages/help.tsx`
- Modify: `web/app/src/pages/help.test.tsx`
- Modify: `web/app/src/theme/styles.css`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Web UI productization Task 4: convert Help page into bilingual install and first-run help center.
```

- [ ] **Step 2: Update Help tests for i18n**

In `web/app/src/pages/help.test.tsx`, import `setLocale`:

```tsx
import { setLocale } from "../i18n/store";
```

Add to `beforeEach`:

```tsx
setLocale("en");
```

Add a Chinese render test:

```tsx
it("renders Chinese help copy", () => {
  setLocale("zh-CN");
  render(<HelpPage onBack={() => {}} />);

  expect(screen.getByRole("heading", { name: "安装 Termix" })).toBeTruthy();
  expect(screen.getByText("一行命令安装")).toBeTruthy();
  expect(screen.getByText("支持的工具")).toBeTruthy();
});
```

- [ ] **Step 3: Run Help tests and verify failure**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/help.test.tsx
```

Expected: FAIL because Help copy is still hard-coded in English.

- [ ] **Step 4: Localize Help component**

Modify `web/app/src/pages/help.tsx`:

```tsx
import { t } from "../i18n/store";

export interface HelpPageProps {
  onBack: () => void;
}

const installCommand = "curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh";

export function HelpPage({ onBack }: HelpPageProps) {
  return (
    <main class="help-page">
      <header class="help-header">
        <button class="icon help-back" type="button" aria-label={t("common.back")} onClick={onBack}>{"<"}</button>
        <div>
          <p class="help-kicker">{t("help.kicker")}</p>
          <h1>{t("help.title")}</h1>
        </div>
      </header>

      <section class="help-section">
        <h2>{t("help.download")}</h2>
        <div class="download-grid">
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Darwin_arm64.tar.gz">
            <span class="download-title">macOS Apple Silicon</span>
            <span class="download-sub">termix_Darwin_arm64.tar.gz</span>
          </a>
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Darwin_x86_64.tar.gz">
            <span class="download-title">macOS Intel</span>
            <span class="download-sub">termix_Darwin_x86_64.tar.gz</span>
          </a>
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Linux_x86_64.tar.gz">
            <span class="download-title">Ubuntu x86_64</span>
            <span class="download-sub">termix_Linux_x86_64.tar.gz</span>
          </a>
          <a class="download-option" href="https://github.com/termix/termix/releases/latest/download/termix_Linux_arm64.tar.gz">
            <span class="download-title">Ubuntu arm64</span>
            <span class="download-sub">termix_Linux_arm64.tar.gz</span>
          </a>
        </div>
      </section>

      <section class="help-section">
        <h2>{t("help.oneLine")}</h2>
        <pre class="command-block"><code>{installCommand}</code></pre>
      </section>

      <section class="help-section">
        <h2>{t("help.startSession")}</h2>
        <ol class="help-steps">
          <li><code>termix login</code></li>
          <li><code>termix start codex --name main</code></li>
          <li>{t("home.cta.sessions")}</li>
        </ol>
      </section>

      <section class="help-section">
        <h2>{t("help.supportedTools")}</h2>
        <div class="tool-row">
          <span>claude</span>
          <span>codex</span>
          <span>opencode</span>
        </div>
        <p class="help-copy">{t("help.backgroundService")}</p>
      </section>

      <section class="help-section">
        <h2>{t("help.troubleshooting")}</h2>
        <ul class="help-list">
          <li><code>termix doctor</code></li>
          <li><code>~/.local/bin</code> on <code>PATH</code></li>
          <li><code>tmux</code> installed</li>
        </ul>
      </section>
    </main>
  );
}
```

- [ ] **Step 5: Add Chinese back-button coverage**

Add this test to `web/app/src/pages/help.test.tsx`:

```tsx
it("calls onBack from the localized Chinese back button", () => {
  const onBack = vi.fn();
  setLocale("zh-CN");
  render(<HelpPage onBack={onBack} />);
  fireEvent.click(screen.getByRole("button", { name: "返回" }));
  expect(onBack).toHaveBeenCalledOnce();
});
```

- [ ] **Step 6: Run Help tests**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/help.test.tsx
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
rtk git add docs/PROGRESS.md web/app/src/pages/help.tsx web/app/src/pages/help.test.tsx web/app/src/theme/styles.css
rtk git commit -m "Localize Web help page"
```

---

### Task 5: Build Responsive Sessions Workbench

**Files:**
- Modify: `web/app/src/pages/sessions.tsx`
- Modify: `web/app/src/pages/sessions.test.tsx`
- Modify: `web/app/src/theme/styles.css`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Web UI productization Task 5: build responsive Sessions workbench with desktop metadata and mobile quick-resume cards.
```

- [ ] **Step 2: Add failing Sessions workbench tests**

In `web/app/src/pages/sessions.test.tsx`, update the empty-state expectation from no `Sessions` heading to expecting a content title:

```tsx
expect(screen.getByRole("heading", { name: "Sessions" })).toBeTruthy();
```

Import `setLocale` and set English in `beforeEach` so text expectations stay stable:

```tsx
import { setLocale } from "../i18n/store";

beforeEach(() => {
  cleanup();
  clearAuth();
  setLocale("en");
  accessToken.value = "tk";
  userInfo.value = {
    user: { id: "u", email: "a@b", display_name: "A", role: "user" },
    device: { id: "d", device_type: "web", platform: "web", label: "ua" },
  };
  snackbar.value = null;
  mockList.mockReset();
  mockLogout.mockReset();
});
```

Add tests:

```tsx
it("renders desktop metadata and mobile compact markers", async () => {
  mockList.mockResolvedValueOnce([
    {
      id: "s1",
      user_id: "u",
      device_id: "d",
      tool: "codex",
      name: "main",
      status: "running",
      host_label: "MacBook Pro",
      last_activity_at: new Date().toISOString(),
      created_at: new Date(Date.now() - 60_000).toISOString(),
    },
  ]);
  const { container } = render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

  await waitFor(() => expect(screen.getByText(/codex · main/)).toBeTruthy());
  expect(screen.getByText("MacBook Pro")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Open codex main" })).toBeTruthy();
  expect(container.querySelector(".sessions-desktop-table")).toBeTruthy();
  expect(container.querySelector(".sessions-mobile-list")).toBeTruthy();
});

it("sorts most recently active sessions first", async () => {
  mockList.mockResolvedValueOnce([
    { id: "old", user_id: "u", device_id: "d", tool: "claude", name: "old", status: "running", last_activity_at: "2026-04-29T00:00:00Z" },
    { id: "new", user_id: "u", device_id: "d", tool: "codex", name: "new", status: "running", last_activity_at: "2026-04-30T00:00:00Z" },
  ]);
  render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

  await waitFor(() => screen.getByText(/codex · new/));
  const rows = screen.getAllByTestId("session-row");
  expect(rows[0].textContent).toContain("codex · new");
});

it("filters visible sessions by local search", async () => {
  mockList.mockResolvedValueOnce([
    { id: "s1", user_id: "u", device_id: "d", tool: "claude", name: "ui", status: "running", host_label: "Ubuntu" },
    { id: "s2", user_id: "u", device_id: "d", tool: "codex", name: "main", status: "running", host_label: "MacBook" },
  ]);
  render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

  await waitFor(() => screen.getByText(/claude · ui/));
  fireEvent.input(screen.getByPlaceholderText("Search by tool, name, host..."), { target: { value: "mac" } });

  expect(screen.queryByText(/claude · ui/)).toBeNull();
  expect(screen.getByText(/codex · main/)).toBeTruthy();
});

it("loads all sessions when All filter is selected", async () => {
  mockList.mockResolvedValueOnce([]);
  mockList.mockResolvedValueOnce([
    { id: "s1", user_id: "u", device_id: "d", tool: "codex", name: "done", status: "exited" },
  ]);
  render(<SessionsPage onOpen={() => {}} onLogout={() => {}} onHelp={() => {}} />);

  await waitFor(() => screen.getByRole("button", { name: "All" }));
  fireEvent.click(screen.getByRole("button", { name: "All" }));

  await waitFor(() => expect(mockList).toHaveBeenLastCalledWith("all"));
  await waitFor(() => expect(screen.getByText(/codex · done/)).toBeTruthy());
});
```

- [ ] **Step 3: Run Sessions tests and verify failure**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/sessions.test.tsx
```

Expected: FAIL because the workbench layout, filters, and search do not exist.

- [ ] **Step 4: Replace Sessions component with workbench**

Modify `web/app/src/pages/sessions.tsx`:

```tsx
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
          <div class="session-stats" aria-label="session stats">
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
```

- [ ] **Step 5: Add sessions workbench styles**

Append to `web/app/src/theme/styles.css`:

```css
.sessions-page {
  display: flex;
  flex-direction: column;
  min-height: 100%;
  background: #f6f8fb;
}
.sessions-workbench {
  width: 100%;
  max-width: 1120px;
  margin: 0 auto;
  padding: 20px 16px calc(32px + env(safe-area-inset-bottom));
}
.sessions-hero {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 18px;
  margin-bottom: 16px;
}
.sessions-hero h1 {
  margin: 0 0 4px;
  font-size: 30px;
  letter-spacing: 0;
}
.sessions-hero p {
  margin: 0;
  color: #64748b;
}
.session-stats {
  display: flex;
  gap: 10px;
}
.session-stats div {
  min-width: 90px;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px 12px;
}
.session-stats strong {
  display: block;
  font-size: 20px;
}
.session-stats span {
  color: #64748b;
  font-size: 12px;
}
.sessions-tools {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 14px;
}
.session-search {
  flex: 1;
  height: 40px;
  border: 1px solid #d8dde6;
  background: #fff;
  border-radius: 10px;
  padding: 0 12px;
}
.segmented {
  display: inline-flex;
  gap: 4px;
  background: #e2e8f0;
  padding: 4px;
  border-radius: 999px;
}
.segmented button {
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: #334155;
  padding: 7px 12px;
  font-weight: 700;
}
.segmented button.active {
  background: #0f172a;
  color: #fff;
}
.sessions-desktop-table {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.session-row-rich {
  display: grid;
  grid-template-columns: minmax(180px, 1.3fr) minmax(120px, .8fr) minmax(96px, .65fr) minmax(64px, .45fr) minmax(140px, .75fr) auto;
  align-items: center;
  gap: 12px;
  width: 100%;
  border: 1px solid #e2e8f0;
  background: #fff;
  color: #0f172a;
  border-radius: 13px;
  padding: 14px;
  text-align: left;
}
.session-row-rich:hover,
.session-mobile-card:hover {
  border-color: #1b6ef3;
}
.session-command {
  color: #64748b;
  font-family: ui-monospace, monospace;
  font-size: 12px;
}
.open-cell {
  background: #1b6ef3;
  color: #fff;
  border-radius: 8px;
  padding: 7px 12px;
  font-weight: 800;
}
.sessions-mobile-list {
  display: none;
}
.session-mobile-card {
  width: 100%;
  border: 1px solid #e2e8f0;
  background: #fff;
  border-radius: 13px;
  padding: 14px;
  text-align: left;
  color: #0f172a;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.session-mobile-card small {
  display: block;
  color: #64748b;
  margin-top: 4px;
}
.empty-steps {
  display: inline-block;
  text-align: left;
  margin: 14px auto;
  color: #64748b;
}
@media (max-width: 720px) {
  .sessions-hero {
    display: block;
  }
  .session-stats {
    margin-top: 12px;
  }
  .sessions-tools {
    display: block;
  }
  .session-search {
    width: 100%;
    margin-bottom: 10px;
  }
  .sessions-desktop-table {
    display: none;
  }
  .sessions-mobile-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
}
```

- [ ] **Step 6: Run Sessions tests**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/sessions.test.tsx
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
rtk git add docs/PROGRESS.md web/app/src/pages/sessions.tsx web/app/src/pages/sessions.test.tsx web/app/src/theme/styles.css
rtk git commit -m "Build responsive Sessions workbench"
```

---

### Task 6: Localize Terminal and Global UI Messages

**Files:**
- Modify: `web/app/src/entry/main.tsx`
- Modify: `web/app/src/pages/terminal.tsx`
- Modify: `web/app/src/pages/terminal.test.tsx`
- Modify: `web/app/src/components/composer.tsx`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark task in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Web UI productization Task 6: localize terminal controls, composer placeholder, and global snackbars.
```

- [ ] **Step 2: Update terminal tests for localized labels**

In `web/app/src/pages/terminal.test.tsx`, update visible button expectations to keep English default labels and add a Chinese case:

```tsx
import { setLocale } from "../i18n/store";
```

Add:

```tsx
it("renders Chinese terminal control labels", async () => {
  setLocale("zh-CN");
  render(<TerminalPage sessionId="s1" onBack={() => {}} />);

  await waitFor(() => expect(screen.getByText("只读")).toBeTruthy());
  expect(screen.getByRole("button", { name: "请求控制" })).toBeTruthy();
});
```

- [ ] **Step 3: Run terminal tests and verify failure**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/terminal.test.tsx
```

Expected: FAIL because terminal labels are hard-coded English.

- [ ] **Step 4: Localize terminal page**

Modify relevant parts of `web/app/src/pages/terminal.tsx`:

```tsx
import { t } from "../i18n/store";

function controlLabel(s: ControlState): string {
  switch (s) {
    case "granted": return t("terminal.control.granted");
    case "requesting": return t("terminal.control.requesting");
    case "denied": return t("terminal.control.denied");
    case "revoked": return t("terminal.control.revoked");
    default: return t("terminal.control.readOnly");
  }
}
```

Update visible strings:

```tsx
notify(t("terminal.authExpired"), "warn");
notify(t("terminal.refreshing"), "warn");
notify(t("terminal.authExpired"), "error");
```

Update buttons and composer:

```tsx
<button class="release-btn" onClick={() => window.releaseControl()}>{t("terminal.button.release")}</button>
<button class="request-btn" onClick={() => window.requestControl()}>{t("terminal.button.request")}</button>
<Composer disabled={disabled} onSend={onCompose} placeholder={t("terminal.placeholder")} />
```

- [ ] **Step 5: Localize global snackbars**

Modify `web/app/src/entry/main.tsx`:

```tsx
import { t } from "../i18n/store";
```

Replace service-worker update strings:

```tsx
snackbar.value = {
  msg: t("pwa.updateAvailable"),
  kind: "info",
  action: { label: t("pwa.refresh"), cb: () => updateSW(true) },
};
```

Replace bootstrap network error:

```tsx
snackbar.value = { msg: t("error.network"), kind: "warn" };
```

- [ ] **Step 6: Ensure Composer fallback is not product-visible**

Modify `web/app/src/components/composer.tsx` to keep the fallback but make Terminal pass localized copy:

```tsx
placeholder={placeholder ?? ""}
```

- [ ] **Step 7: Run focused tests**

Run:

```bash
cd web/app && rtk npm test -- --run src/pages/terminal.test.tsx src/components/snackbar.test.tsx
```

Expected: PASS.

- [ ] **Step 8: Commit**

Run:

```bash
rtk git add docs/PROGRESS.md web/app/src/entry/main.tsx web/app/src/pages/terminal.tsx web/app/src/pages/terminal.test.tsx web/app/src/components/composer.tsx
rtk git commit -m "Localize terminal and global Web messages"
```

---

### Task 7: Full Web Verification and Embedded Asset Rebuild

**Files:**
- Modify: `go/internal/controlapi/web_dist/**`
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Mark final implementation verification in progress**

Edit `docs/PROGRESS.md`:

```markdown
## In Progress
- [ ] Web UI productization Task 7: run full Web verification and rebuild embedded assets.
```

- [ ] **Step 2: Run Web typecheck**

Run:

```bash
cd web/app && rtk npm run typecheck
```

Expected: PASS with TypeScript reporting no errors.

- [ ] **Step 3: Run Web tests**

Run:

```bash
cd web/app && rtk npm test -- --run
```

Expected: PASS.

- [ ] **Step 4: Run Web build**

Run:

```bash
cd web/app && rtk npm run build
```

Expected: PASS and `web/app/dist/` is regenerated.

- [ ] **Step 5: Rebuild embedded assets**

Run:

```bash
rtk make build-web
```

Expected: PASS and `go/internal/controlapi/web_dist/` updates to the new built SPA.

- [ ] **Step 6: Check embedded assets**

Run:

```bash
rtk make check-web-dist
```

Expected: PASS.

- [ ] **Step 7: Diff hygiene**

Run:

```bash
rtk git diff --check
```

Expected: no output.

- [ ] **Step 8: Mark implementation complete in progress ledger**

Edit `docs/PROGRESS.md`:

```markdown
## Completed
- [x] Implement Web UI productization: public homepage, `/login`, responsive Sessions workbench, cleaned authenticated navigation, bilingual Help, and lightweight Chinese/English language switching. Verification: `cd web/app && rtk npm run typecheck`; `cd web/app && rtk npm test -- --run`; `cd web/app && rtk npm run build`; `rtk make build-web`; `rtk make check-web-dist`; `rtk git diff --check`.
```

Remove the matching `Web UI productization Task 7` item from `## In Progress`.

- [ ] **Step 9: Commit**

Run:

```bash
rtk git add docs/PROGRESS.md go/internal/controlapi/web_dist
rtk git commit -m "Rebuild embedded Web assets for product UI"
```

---

## Plan Self-Review

Spec coverage:

- Product homepage: Task 2.
- `/login` split and auth redirects: Task 2.
- Sessions desktop workbench and mobile quick-resume list: Task 5.
- Cleaner Help/Refresh/Logout navigation: Task 3.
- Help page remains and becomes bilingual: Task 4.
- Lightweight Chinese/English switching: Task 1 plus Tasks 2-6.
- Terminal labels and common snackbars: Task 6.
- Embedded asset rebuild: Task 7.
- `docs/PROGRESS.md` updates: every task includes progress steps.

Type consistency:

- Locale types are defined in `messages.ts` and consumed in `store.ts`.
- `t()` consumes `MessageKey` and returns `string`.
- Router callbacks match component prop names: `HomePage.onLogin/onHelp/onSessions`, `LoginPage.onSuccess/onHelp/onHome`, existing `SessionsPage` and `TerminalPage` callbacks.
- Existing `SessionSummary` fields are used without requiring backend API changes.

No backend or protocol changes are required.
