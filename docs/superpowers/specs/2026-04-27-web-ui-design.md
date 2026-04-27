# Web UI Design

**Status:** Approved 2026-04-27.
**Phase:** Browser-first product slice; replaces the paused Android Compose shell as the primary client.
**Authoritative spec:** `docs/termix-v1-detailed-technical-spec.md` §5.5, §7.8, §10.2, §10.3, §17, §19.
**Predecessor designs:**
- `docs/superpowers/specs/2026-04-25-android-terminal-web-mvp-design.md` — slice-1 `terminal-web` MVP, foundation for the Terminal page.
- `docs/superpowers/specs/2026-04-26-android-app-compose-shell-design.md` — slice-2 Compose shell, paused; this Web UI replaces its product-UI surface.

## §1 Goal and Scope

### Goal
Promote `android/terminal-web/` into `web/app/`: a browser-first SPA with Login + SessionList + Terminal pages, served by `termix-control` over HTTPS on a public domain, installable as a PWA on Android Chrome and iOS Safari. End state of this slice is a single `termix-control` binary deployable to the public internet that delivers the spec §10.2 / §10.3 product loop ("随时随地查看 + 控制 host PC 上的 claude/codex session") to desktop and mobile browsers without any native client.

### In scope
- Three-screen SPA: Login (= landing) / SessionList / Terminal, with route guard and cold-start auto-restore
- Reuse slice-1 `protocol/net/session/ui` modules in place — no core extraction (option A)
- Stack: Preact + `@preact/signals` + `preact-router`, Vite, vite-plugin-pwa, Vitest + happy-dom + `@preact/testing-library`
- Auth: HttpOnly refresh-token cookie + in-memory access-token signal; backend dual-mode by `device_type`
- Backend extensions (4): login/refresh dual-mode, enum widening (`web`/`ios`/`windows`), `POST /v1/auth/logout`, security-headers middleware + login rate limit + relay log scrub
- PWA T1: manifest + service worker pre-caching app shell; **no** API/WSS interception; **no** offline data caching
- 21-key toolbar: 10 digits via `sendText`, 11 special keys via `sendSpecialKey` (slice-1 enum gains `C-j` = `0x0a`)
- Mobile UX: composer textarea + Send (always present), `visualViewport` keyboard avoidance, safe-area insets, 16px input font
- Security hardening: access-token TTL back to 30 min, CSP/HSTS/X-Content-Type-Options headers, login rate limit (5/min per IP+email), relay log query-string scrub, optional `Origin` allowlist middleware
- Single binary deploy: `web/app/dist/` rsynced into `go/internal/controlapi/web_dist/` and `go:embed`-ed; dev override env var `TERMIX_CONTROL_WEB_DIR`
- Visual: cream palette `#F5F2EA` (Anthropic-style) for Login + Sessions; Claude-orange `#C96442` accent (brand glyph, Send/Enter buttons, Release, ANSI frames in xterm); terminal area always dark `#0A0A0A`

### Out of scope (deferred; tracked in PROGRESS.md when slice lands)
- Auto-reconnect with backoff (user re-taps to reconnect)
- WSS sub-protocol auth (still query-string token, mitigated via TTL + log scrub)
- Application-internal session creation (still `termix start <tool>` from host)
- Refresh-token rotation
- Push notifications / Background sync / Periodic sync
- Multi-account support
- Tab/recents UX, multi-active session within a single tab
- Native Android / iOS apps (revisit only if PWA falls short)
- Core extraction into a separate `@termix/terminal-core` package (option B′; deferred until a non-browser consumer materializes)
- Phase 3 hardening: cert pinning, CSP report-uri, SRI, HSTS preload registration
- Admin Web UI / `termix-admin-api` (separate phase)

## §2 Architecture

### Repository layout changes

```text
web/
└── app/                              # promoted from android/terminal-web/
    ├── package.json
    ├── vite.config.ts                # + vite-plugin-pwa, + dev proxy for /api and /ws
    ├── tsconfig.json
    ├── index.html
    ├── public/
    │   ├── manifest.webmanifest
    │   ├── icons/                    # 192/512/maskable PNGs
    │   └── apple-touch-icon.png
    ├── scripts/
    │   └── smoke.sh                  # renamed from android/terminal-web/scripts/smoke.sh
    └── src/
        ├── protocol/                 # slice-1, in place
        ├── net/                      # slice-1, in place
        ├── session/                  # slice-1, in place (control.ts gains C-j row)
        ├── ui/                       # slice-1, in place
        ├── api/
        │   ├── client.ts             # fetchWithAuth + 401 single-flight refresh
        │   └── endpoints.ts          # login, refresh, listSessions, logout
        ├── auth/
        │   ├── store.ts              # accessToken/expiresAt/userInfo signals
        │   ├── refresh.ts            # doRefreshOnce + freshAccessToken helpers
        │   └── bootstrap.ts          # cold-start cookie probe
        ├── routes/
        │   ├── Router.tsx            # preact-router wiring + AuthGuard
        │   └── AuthGuard.tsx
        ├── pages/
        │   ├── login.tsx
        │   ├── sessions.tsx
        │   └── terminal.tsx
        ├── components/
        │   ├── snackbar.tsx          # signal-driven toast
        │   ├── header.tsx            # logo + title + ⋮ menu
        │   ├── toolbar.tsx           # 21 keys, control-state aware
        │   ├── composer.tsx          # textarea + Send
        │   └── splash.tsx            # cold-start loading
        ├── hooks/
        │   ├── useVisibility.ts      # Page Visibility API
        │   └── useViewport.ts        # visualViewport keyboard offset
        ├── theme/
        │   └── styles.css            # cream palette + dark terminal
        └── entry/
            └── main.tsx              # mounts <App>, registers SW

android/terminal-web/                  # deleted; .worktrees/android-app-slice-2/ kept as historical reference
```

### Backend changes (Go)

```text
go/
├── internal/
│   ├── controlapi/
│   │   ├── auth.go                   # Login + Refresh: dual-mode by device_type / cookie
│   │   ├── auth_logout.go            # NEW
│   │   ├── middleware/
│   │   │   ├── security_headers.go   # NEW (CSP, HSTS, X-CTO, X-FO, Referrer-Policy)
│   │   │   ├── rate_limit_login.go   # NEW (per IP+email sliding window, 5/min)
│   │   │   └── origin_allowlist.go   # NEW (optional belt-and-suspenders)
│   │   ├── static.go                 # NEW (go:embed + TERMIX_CONTROL_WEB_DIR override)
│   │   └── web_dist/                 # NEW directory for embedded SPA assets (filled at build)
│   ├── auth/
│   │   └── tokens.go                 # accessTokenTTL: 30d → 30m
│   └── relayapi/
│       └── relay.go                  # log middleware: scrub access_token query param
└── tests/
    ├── auth_login_web_test.go        # NEW
    ├── auth_login_ios_test.go        # NEW (enum contract)
    ├── auth_refresh_cookie_test.go   # NEW
    ├── auth_logout_test.go           # NEW
    ├── login_rate_limit_test.go      # NEW
    ├── relay_log_scrub_test.go       # NEW
    └── static_handler_test.go        # NEW

openapi/control.openapi.yaml          # device_type/platform enum widening, LoginResponse cookie_mode + nullable refresh_token, RefreshRequest nullable refresh_token, /auth/logout endpoint
```

### Module responsibilities

- **`protocol/`, `net/`, `session/`, `ui/`** — slice-1, unchanged except `session/control.ts` editorial addition for `C-j` and `protocol/types.ts` enum addition. The 67 slice-1 + 2 slice-2 unit tests carry over verbatim; one new row added for `C-j`.
- **`api/`** — only place that constructs `fetch` calls to `/api/v1/*`. The `fetchWithAuth` wrapper enforces the single-flight 401-refresh-retry semantic (§4f).
- **`auth/`** — only place that touches `accessToken` signal and the refresh promise. `bootstrap.ts` runs once at `<App>` mount.
- **`routes/`** — only place that calls `route(...)`. Pages emit navigation requests via callbacks; the router is the single navigation authority.
- **`pages/`** — pure UI consumers of signals + `api/` + `auth/`. No direct DOM globals beyond standard refs.
- **`components/`** — leaf UI; `toolbar` and `composer` are dumb (state in/events out).
- **`hooks/`** — non-UI signals/effects that wrap browser APIs (`document.visibilityState`, `window.visualViewport`).

### Key dependencies

| Purpose | Library | Notes |
|---|---|---|
| UI runtime | `preact` + `@preact/signals` | ~3 KB + ~1 KB |
| Routing | `preact-router` | ~2 KB; three routes |
| Terminal | `xterm` + `xterm-addon-fit` | slice-1 already vendored |
| PWA tooling | `vite-plugin-pwa` (Workbox) | manifest + SW generation |
| Build | Vite | slice-1 already vendored |
| Test | Vitest + happy-dom + `@preact/testing-library` + `@testing-library/jest-dom` | extends slice-1 setup |
| Backend | Gin (existing) + `golang.org/x/time/rate` for limiter | one new transitive dep |
| OpenAPI codegen | `oapi-codegen` (existing) | re-run on schema change |

### Concurrency model
- All cross-page state lives in module-level Preact signals (`accessToken`, `accessTokenExpiresAt`, `userInfo`, `snackbar`, `splashing`).
- WSS lifecycle is bound to `<TerminalPage>` via `useEffect`; route change → cleanup → graceful close (`setSession("", "", "", "")`).
- Refresh single-flight uses a module-level `let refreshInflight: Promise<string|null> | null` — JS single-thread guarantees no race.

## §3 Screen Graph and Components

### Routes

```text
/                       LoginPage          (also serves as landing)
/sessions               SessionsPage       (auth-required)
/terminal/:sessionId    TerminalPage       (auth-required)
```

### AuthGuard
On mount, dispatches `auth.bootstrap()`:
1. Show `<Splash />` (PWA splash style, suppressed if response < 200 ms)
2. `POST /api/v1/auth/refresh` (cookie auto-attached)
3. 200 → write `accessToken` signal → if current path is `/`, `route("/sessions")`
4. 401 → cookie absent/expired → stay on `/`
5. Network error → snackbar warn, stay on `/`

`/sessions` and `/terminal/:id` render `<Splash />` until bootstrap finishes; after that, no `accessToken` → `route("/")`.

### Page state and ownership

| Page | Local signals | Reads global |
|---|---|---|
| `LoginPage` | `email`, `password`, `busy`, `error?` | writes `accessToken` + `userInfo` after success |
| `SessionsPage` | `items`, `refreshing`, `firstLoad`, `error?` | reads `userInfo` (display + device.id), dispatches `auth.logout()` |
| `TerminalPage` | `connState`, `controlState`, `composerText`, `sessionMeta?` | reads `accessToken` and `userInfo.device.id` for setSession |

### Global signals

```ts
// auth/store.ts (User and Device come from openapi-generated types)
export const accessToken = signal<string | null>(null);
export const accessTokenExpiresAt = signal<number>(0);
export const userInfo = signal<{user: User; device: Device} | null>(null);

// app/store.ts
export const snackbar = signal<{msg: string; kind: "info"|"warn"|"error"; action?: {label: string; cb: () => void}} | null>(null);
export const splashing = signal<boolean>(true);
```

### Header / Chrome
- `<Header>` is rendered on `/sessions` and `/terminal/:id`: brand left, page title middle, ⋮ menu right (Logout). Height ~52 px.
- `<App>` mounts `<Snackbar />` + `<Splash if={splashing.value} />` at the root, so they overlay any page.
- LoginPage has its own brand block; no `<Header>`.

### Visual identity
- Background `#F5F2EA` (cream); header `#FAF7EF`; cards `#FFFFFF` with `0 1px 3px rgba(74,69,56,0.06)` shadow.
- Brand glyph: `>_` monospace on `#1A1A1A` block, `#C96442` orange foreground.
- Accent (Claude orange) `#C96442`: Send button, Enter key, Release button, brand glyph foreground, ANSI box-drawing in xterm.
- Mute color `#8A8270` (warm grey) for secondary text.
- Terminal canvas `#0A0A0A`, terminal foreground `#E0E0E0` (xterm dark theme).

## §4 Data Flow

### 4a. Login (cold start, no cookie)

```text
LoginPage [submit]
  → busy.value = true; error.value = null
  → POST /api/v1/auth/login {
      email, password,
      device_type: "web", platform: "web",
      device_label: navigator.userAgent.slice(0, 80),
      cookie_mode: true
    }
  → 200:
       Set-Cookie: termix_refresh=...; HttpOnly; Secure; SameSite=Strict; Path=/api/v1/auth
       body: { user: User, device: Device, access_token, expires_in_seconds, cookie_mode: true }
       (existing OpenAPI schema; refresh_token field omitted when cookie_mode=true)
       → accessToken.value = access_token
       → accessTokenExpiresAt.value = Date.now() + expires_in_seconds * 1000
       → userInfo.value = { user, device }
       → route("/sessions")
  → 401: error.value = "邮箱或密码错误"; busy.value = false
  → 429: error.value = "尝试过于频繁，请稍候"
  → network: error.value = "无法连接服务器"
```

`device.id` is what slice-1 wsClient calls `deviceId` in the `setSession(...)` argument tuple; we keep the whole `User` and `Device` objects in `userInfo` so the Header can show display name and the Terminal page can pull the device id without a refetch.

### 4b. Cold-start auto-restore

```text
<App> mount
  → useEffect → auth.bootstrap()
       splashing.value = true
       POST /api/v1/auth/refresh   // body absent; cookie auto-attached
       200 → write accessToken/expiresAt/userInfo signals
              splashing.value = false
              if location is "/" → route("/sessions")
       401 → splashing.value = false
              stay on "/"
       network → splashing.value = false
                 snackbar warn "无法连接服务器"
```

Splash is suppressed if response returns < 200 ms (avoids flash). Bootstrap is dispatched exactly once per `<App>` mount.

### 4c. Session list

```text
SessionsPage mount
  → if firstLoad.value → fetch (with 200 ms delayed full-page spinner)
  → else use cached items.value

useVisibility() hook
  → on visibilityState === "visible", debounced 5 s, fetch silently

PullToRefresh gesture
  → refreshing.value = true; fetch; refreshing.value = false

api.listSessions()
  → fetchWithAuth("/api/v1/sessions?status=running")
       (single-flight refresh path lives in fetchWithAuth, §4f)
  → 200: items.value = data.sessions
  → network: snackbar warn "session 列表加载失败 — 下拉刷新"
```

### 4d. Terminal

```text
TerminalPage mount (route /terminal/:id)
  → useEffect:
      const tok = await freshAccessToken()
      if !tok → snackbar warn; route("/")
      else setSession(id,
                      import.meta.env.VITE_RELAY_WS_URL,   // wss://relay.termix.example.com/ws (prod)
                                                            // /ws (dev → proxied to ws://localhost:8090/ws)
                      tok,
                      userInfo.value!.device.id)
  → slice-1 modules take over; outbound dispatcher posts events to local signals

bridge events:
  onConnectionState(state, _) → connState.value = state
  onControlState(state, _) → controlState.value = state
  onError("auth", _) → freshAccessToken() then re-setSession (one retry)
                       if still fails → snackbar warn; route("/")
  onError("watch", msg) → snackbar error msg

toolbar buttons:
  digit "0".."9" → sendText(label)
  special key → sendSpecialKey(key)
  Send (composer) → sendText(composerText.value); composerText.value = ""

unmount (route change / browser back):
  → setSession("", "", "", "")
  → connState/controlState reset
```

### 4e. Logout

```text
SessionsPage [⋮ → Logout]
  → busy state
  → POST /api/v1/auth/logout (cookie auto-attached)
  → unconditionally:
       accessToken.value = null
       accessTokenExpiresAt.value = 0
       userInfo.value = null
       route("/", { replace: true })
  → snackbar info "已退出登录"
```

Server-side `Set-Cookie: termix_refresh=; Max-Age=0` clears the cookie; the row is `revoked_at = now()` in `refresh_tokens`. Even if the call fails, local state is cleared (cookie persists but server has revoked the row, so next refresh yields 401).

### 4f. fetchWithAuth (single-flight 401 refresh)

```ts
let refreshInflight: Promise<string | null> | null = null;

export async function fetchWithAuth(input: RequestInfo, init?: RequestInit): Promise<Response> {
  const doFetch = (token: string | null) => fetch(input, {
    ...init,
    headers: {
      ...(init?.headers ?? {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });

  let res = await doFetch(accessToken.value);
  if (res.status !== 401) return res;

  const refreshed = await (refreshInflight ??= doRefreshOnce());
  refreshInflight = null;
  if (refreshed === null) return res;

  return doFetch(refreshed);
}

export async function doRefreshOnce(): Promise<string | null> {
  if (accessToken.value && Date.now() < accessTokenExpiresAt.value - 5_000) {
    return accessToken.value;
  }
  try {
    const res = await fetch("/api/v1/auth/refresh", { method: "POST" });
    if (!res.ok) return null;
    const data = await res.json();
    accessToken.value = data.access_token;
    accessTokenExpiresAt.value = Date.now() + data.expires_in_seconds * 1000;
    return data.access_token;
  } catch {
    return null;
  }
}
```

Two invariants (slice-2 §7b parity):
- At most one refresh in flight per process
- Requests blocked on `refreshInflight` see the freshly-written signal value, never double-refresh

## §5 Auth Lifecycle

### 5a. Where state lives

| Datum | Location | Persistence | XSS exposure |
|---|---|---|---|
| `refresh_token` | HttpOnly cookie + server-side `refresh_tokens` row (hash) | 30 days | None (JS cannot read) |
| `access_token` | Preact signal `accessToken` (memory only) | tab close → gone | Same-origin JS reads it; 30-min lifetime caps damage |
| `User` / `Device` (id, email, label, …) | Preact signal `userInfo` (memory only) | tab close → gone | Metadata only |
| Server origin | Same-origin `window.location.origin` | N/A | N/A |

Compared to slice-2 Compose (refresh in EncryptedSharedPreferences): the Web flow is stricter — the SPA never holds the refresh token in addressable JS memory.

### 5b. WSS access-token lifecycle

WSS does not go through `fetchWithAuth`, so a parallel helper:

```ts
export async function freshAccessToken(): Promise<string | null> {
  if (accessToken.value && Date.now() < accessTokenExpiresAt.value - 60_000) {
    return accessToken.value;
  }
  return await (refreshInflight ??= doRefreshOnce());
}
```

Used at: `<TerminalPage>` mount, and after `onError("auth", ...)` from the bridge. One retry on auth error; second failure → `route("/")`.

### 5c. Cookie hygiene

- Never read `document.cookie` (HttpOnly anyway)
- Never write `document.cookie`
- All `/api/v1/auth/*` requests go to same-origin (default `credentials: "same-origin"` already includes cookie); explicit `credentials: "include"` only required if origin separates in future
- `Path=/api/v1/auth` keeps the cookie out of every other request

### 5d. Logout endpoint contract

```yaml
/auth/logout:
  post:
    operationId: postAuthLogout
    security: []
    responses:
      "204":
        description: logged out (cookie cleared, refresh-token row revoked if found)
        headers:
          Set-Cookie: { schema: { type: string } }
      "400":
        description: no refresh credential present
```

Server: read `r.Cookie("termix_refresh")` → hash → `UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1` (idempotent — no error if row missing) → write `Set-Cookie` with `Max-Age=0` and matching attributes.

### 5e. Token leakage hygiene
- `access_token` never enters localStorage / sessionStorage / IndexedDB / `document.title` / URL hash / log statements
- WSS query-string `access_token=...` is unavoidable (browser WS API can't set headers); mitigated by 30-min TTL + relay-side log scrub (§6j)
- Fetch error detail strings strip any field named `access_token` or matching the JWT prefix pattern before snackbar display
- DevTools network panel still shows the access token to the page owner (acceptable; it's their session)

## §6 Backend Extensions

Four endpoints (1 new + 2 modified + 1 unchanged-but-extended) plus three middlewares plus one TTL change plus one relay-side log scrub. **All additive; existing host CLI and Compose clients keep working.**

### 6a. OpenAPI diff

```yaml
LoginRequest:
  properties:
    device_type:
      type: string
-     enum: [host, android]
+     enum: [host, android, ios, web]
    platform:
      type: string
-     enum: [macos, ubuntu, android]
+     enum: [macos, ubuntu, windows, android, ios, web]
    cookie_mode:
+     type: boolean
+     default: false
+     description: when true, refresh_token is returned as HttpOnly cookie instead of body

LoginResponse:
- required: [user, device, access_token, refresh_token, expires_in_seconds]
+ required: [user, device, access_token, expires_in_seconds]
  properties:
    refresh_token:
      type: string
+     nullable: true
+     description: omitted when cookie_mode=true
+   cookie_mode:
+     type: boolean
+     description: echoed from request

RefreshRequest:
  properties:
    refresh_token:
      type: string
+     nullable: true
+     description: omitted when caller relies on HttpOnly cookie

paths:
+ /auth/logout:
+   post:
+     operationId: postAuthLogout
+     security: []
+     responses:
+       "204":
+         description: logged out
+       "400":
+         description: no refresh credential present
```

### 6b. Login handler dual-mode (Gin sketch)

```go
func (s *Server) Login(c *gin.Context) {
    var req openapi.LoginRequest
    if err := c.ShouldBindJSON(&req); err != nil { c.JSON(400, errBody("bad request")); return }

    user, err := s.users.Authenticate(c.Request.Context(), req.Email, req.Password)
    if err != nil { c.JSON(401, errBody("invalid credentials")); return }

    device, _ := s.devices.Upsert(c.Request.Context(), user.ID, req.DeviceType, req.Platform, req.DeviceLabel)
    accessTok, _ := s.tokens.MintAccess(user.ID, device.ID)
    refreshTok, _ := s.tokens.MintRefresh(c.Request.Context(), user.ID, device.ID)

    resp := openapi.LoginResponse{
        User: user, Device: device,
        AccessToken: accessTok.Value,
        ExpiresInSeconds: int(accessTok.TTL.Seconds()),
        CookieMode: req.CookieMode,
    }
    if req.CookieMode {
        http.SetCookie(c.Writer, &http.Cookie{
            Name: "termix_refresh", Value: refreshTok.Value,
            Path: "/api/v1/auth", MaxAge: int(refreshTok.TTL.Seconds()),
            HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
        })
    } else {
        resp.RefreshToken = &refreshTok.Value
    }
    c.JSON(200, resp)
}
```

### 6c. Refresh handler dual-mode (Gin sketch)

```go
func (s *Server) Refresh(c *gin.Context) {
    var rawToken string
    if ck, err := c.Request.Cookie("termix_refresh"); err == nil {
        rawToken = ck.Value
    } else {
        var req openapi.RefreshRequest
        _ = c.ShouldBindJSON(&req)
        if req.RefreshToken != nil { rawToken = *req.RefreshToken }
    }
    if rawToken == "" { c.JSON(401, errBody("missing refresh credential")); return }

    row, err := s.tokens.LookupRefresh(c.Request.Context(), rawToken)
    if err != nil { c.JSON(401, errBody("invalid refresh")); return }

    accessTok, _ := s.tokens.MintAccess(row.UserID, row.DeviceID)
    c.JSON(200, openapi.RefreshResponse{
        AccessToken: accessTok.Value,
        ExpiresInSeconds: int(accessTok.TTL.Seconds()),
        RefreshToken: nil,  // V1 does not rotate
    })
}
```

### 6d. Logout handler (Gin sketch)

```go
func (s *Server) Logout(c *gin.Context) {
    var rawToken string
    if ck, err := c.Request.Cookie("termix_refresh"); err == nil { rawToken = ck.Value }

    if rawToken != "" {
        _ = s.tokens.RevokeRefresh(c.Request.Context(), rawToken) // idempotent
    }
    http.SetCookie(c.Writer, &http.Cookie{
        Name: "termix_refresh", Value: "",
        Path: "/api/v1/auth", MaxAge: -1,
        HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
    })
    if rawToken == "" { c.Status(400); return }
    c.Status(204)
}
```

### 6e. Static handler

```go
//go:embed all:web_dist
var embeddedWebFS embed.FS

func StaticHandler() gin.HandlerFunc {
    var fsys fs.FS
    if devDir := os.Getenv("TERMIX_CONTROL_WEB_DIR"); devDir != "" {
        fsys = os.DirFS(devDir)
    } else {
        sub, _ := fs.Sub(embeddedWebFS, "web_dist")
        fsys = sub
    }
    fileServer := http.FileServerFS(fsys)
    return func(c *gin.Context) {
        path := strings.TrimPrefix(c.Request.URL.Path, "/")
        if path == "" { path = "index.html" }
        if !strings.HasPrefix(c.Request.URL.Path, "/assets") {
            if _, err := fs.Stat(fsys, path); err != nil {
                c.Request.URL.Path = "/"  // SPA fallback for client-side routes
            }
        }
        fileServer.ServeHTTP(c.Writer, c.Request)
    }
}
```

Mounted at the root group by the existing Gin router; API routes (`/api/v1/...`) take precedence because they are registered before the catch-all `NoRoute` fallback.

### 6f. Security headers middleware (Gin)

```go
func SecurityHeaders(relayOrigin string) gin.HandlerFunc {
    return func(c *gin.Context) {
        h := c.Writer.Header()
        h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
        h.Set("Content-Security-Policy",
            "default-src 'self'; "+
            "connect-src 'self' "+relayOrigin+"; "+
            "style-src 'self' 'unsafe-inline'; "+
            "img-src 'self' data:; "+
            "manifest-src 'self'; "+
            "worker-src 'self';")
        h.Set("X-Content-Type-Options", "nosniff")
        h.Set("X-Frame-Options", "DENY")
        h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
        h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
        c.Next()
    }
}
```

`relayOrigin` from env `TERMIX_CONTROL_RELAY_ORIGIN` (e.g. `wss://relay.termix.example.com`).

### 6g. Login rate limit (Gin)

`golang.org/x/time/rate` token bucket per `(remote_ip, email)` with refill 5/min, burst 5. Map keyed off the tuple, swept with TTL eviction. ~60 LOC. Wired at route registration time:

```go
auth := api.Group("/auth")
auth.POST("/login", LoginRateLimit(5, time.Minute), s.Login)
auth.POST("/refresh", s.Refresh)   // not limited
auth.POST("/logout", s.Logout)     // not limited
```

Refresh and logout are not limited; cookie-bearing clients are already authenticated. The limiter takes the email from the parsed body, so it must run inline in the handler for that arm of the bucket; the IP-only arm runs as middleware.

### 6h. Optional Origin allowlist (Gin)

```go
func OriginCheck(allowed string) gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method != "GET" && c.Request.Header.Get("Origin") != "" && c.Request.Header.Get("Origin") != allowed {
            c.AbortWithStatusJSON(403, gin.H{"error": "forbidden origin"})
            return
        }
        c.Next()
    }
}
```

`allowed` from env `TERMIX_ALLOWED_ORIGIN` (e.g. `https://termix.example.com`).

CLI and Compose clients send no `Origin`; only browser-initiated cross-origin requests get blocked. Belt-and-suspenders to CSP + SameSite cookies.

### 6i. Access token TTL

`internal/auth/tokens.go` `accessTokenTTL` reverts from 30 days (slice-1 commit `86e56d0` for dev convenience) to **30 minutes**. Configurable via env `TERMIX_ACCESS_TOKEN_TTL` for ops emergencies.

### 6j. Relay log scrub

`go/cmd/termix-relay/` log middleware adds a step before writing access-log line: scrub `access_token=<value>` query params (URL-encoded or raw) to `access_token=<redacted>`. Implementation: regex on `r.URL.RawQuery`. Tested in isolation.

### 6k. Test matrix (Go)

| File | Coverage |
|---|---|
| `auth_login_web_test.go` (new) | `device_type=web, cookie_mode=true` → response body lacks `refresh_token`; Set-Cookie has HttpOnly Secure SameSite=Strict Path=/api/v1/auth |
| `auth_login_ios_test.go` (new) | enum contract: `device_type=ios, platform=ios` → 200 |
| `auth_refresh_cookie_test.go` (new) | cookie present → 200 with no body refresh; cookie + body both → cookie wins; expired cookie → 401 |
| `auth_logout_test.go` (new) | cookie present → row revoked + cookie cleared (204); no cookie → still cookie cleared (400); double-logout idempotent |
| `login_rate_limit_test.go` (new) | 6th login from same IP+email within 1 min → 429 |
| `relay_log_scrub_test.go` (new) | query `access_token=abc&x=y` → log `access_token=<redacted>&x=y`; URL-encoded forms; multi-occurrence |
| `static_handler_test.go` (new) | embed mode returns index.html for `/`; dev override mode reads disk; SPA fallback for non-/assets unknown path |
| `auth_login_test.go` (existing) | `device_type=host` continues returning body refresh_token (regression) |
| `auth_refresh_test.go` (existing) | body refresh path unchanged (regression) |
| `sessions_list_test.go` (existing) | unchanged |

## §7 PWA Configuration

### 7a. `vite.config.ts` (sketch)

```ts
import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    preact(),
    VitePWA({
      registerType: "prompt",
      includeAssets: ["icons/*.png", "apple-touch-icon.png"],
      manifest: {
        name: "Termix",
        short_name: "Termix",
        description: "Remote terminal control for tmux sessions",
        theme_color: "#1A1A1A",
        background_color: "#F5F2EA",
        display: "standalone",
        orientation: "any",
        start_url: "/",
        scope: "/",
        icons: [
          { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png" },
          { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png" },
          { src: "/icons/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
      workbox: {
        navigateFallback: "/index.html",
        navigateFallbackDenylist: [/^\/api\//, /^\/ws\//],
        runtimeCaching: [
          {
            urlPattern: ({ url }) => url.pathname.startsWith("/assets/"),
            handler: "StaleWhileRevalidate",
            options: { cacheName: "termix-assets" },
          },
        ],
      },
      devOptions: { enabled: false },
    }),
  ],
  server: {
    proxy: {
      "/api/v1": "http://localhost:8080",
      "/ws":     { target: "ws://localhost:8090", ws: true },
    },
  },
  test: { environment: "happy-dom" },
});
```

### 7b. SW update flow

`registerType: "prompt"` → newly installed SW does NOT auto-activate. `entry/main.tsx`:

```ts
import { registerSW } from "virtual:pwa-register";
const updateSW = registerSW({
  onNeedRefresh() {
    snackbar.value = {
      msg: "新版本可用",
      kind: "info",
      action: { label: "刷新", cb: () => updateSW(true) },
    };
  },
});
```

User-initiated activation prevents mid-session disruption.

### 7c. iOS PWA notes
- iOS 16.4+ has full SW support; below that, manifest-only (still installable, no offline shell)
- iOS PWA storage sandbox is independent of Safari's: first launch from home-screen icon requires a re-login. Onboarding text in Login screen mentions "首次从主屏图标打开需要重新登录"
- `apple-mobile-web-app-status-bar-style="default"` (white background, black text, matching cream theme)
- `apple-touch-icon.png` at 180×180 PNG

### 7d. Asset budget
- Initial SPA bundle target: < 80 KB gzipped (Preact + signals + router + xterm + app code)
- xterm.js dominates (~45 KB gzipped); preact stack adds ~6 KB
- All static, hashed, fingerprinted by Vite; service worker precaches everything

## §8 Mobile UX Details

### 8a. Toolbar component contract

```tsx
const TOOLBAR = {
  digits: ["0","1","2","3","4","5","6","7","8","9"],
  nav:    ["Esc", "Tab", "Up", "Down", "Left", "Right"] as const,
  edit:   ["Backspace", "C-c", "C-d", "C-j", "Enter"] as const,
};

export function Toolbar(props: {
  onDigit: (d: string) => void;
  onSpecial: (k: SpecialKey) => void;
}) {
  const disabled = controlState.value !== "granted";
  return (
    <div class={`toolbar ${disabled ? "is-disabled" : ""}`} aria-disabled={disabled}>
      <div class="row digits">{TOOLBAR.digits.map(d =>
        <button onClick={() => props.onDigit(d)} disabled={disabled}>{d}</button>
      )}</div>
      <div class="row nav">{TOOLBAR.nav.map(k =>
        <button onClick={() => props.onSpecial(k)} disabled={disabled}>{glyph(k)}</button>
      )}</div>
      <div class="row edit">{TOOLBAR.edit.map(k =>
        <button class={k === "Enter" ? "key-enter" : k === "C-c" || k === "C-d" ? "key-danger" : ""}
                onClick={() => props.onSpecial(k)} disabled={disabled}>{glyph(k)}</button>
      )}</div>
    </div>
  );
}
```

`glyph()` maps `"Backspace"` → `"⌫"`, `"Up"` → `"↑"`, `"C-j"` → `"^J"`, etc.

### 8b. Layout CSS (relevant rules)

```css
.toolbar {
  display: flex; flex-direction: column; gap: 4px;
  padding-bottom: env(safe-area-inset-bottom);
  background: #0d0d10; border-top: 1px solid #2a2a2a; padding: 4px 6px 8px;
}
.toolbar .row { display: grid; gap: 4px; }
.toolbar .row.digits { grid-template-columns: repeat(10, 1fr); }
.toolbar .row.nav    { grid-template-columns: repeat(6, 1fr); }
.toolbar .row.edit   { grid-template-columns: repeat(5, 1fr); }
.toolbar button { min-height: 36px; font-size: 14px; }
.toolbar .row.nav button,
.toolbar .row.edit button { min-height: 56px; font-size: 16px; }
.toolbar .key-enter { background: #C96442; color: #fff; }
.toolbar .key-danger { background: #3a2a2a; color: #ff8a8a; }
.toolbar.is-disabled { opacity: 0.4; pointer-events: none; }

.composer textarea {
  font-size: 16px;        /* >= 16px to disable iOS focus zoom */
  min-height: 48px; padding: 12px; width: 100%;
}

@media (min-width: 768px) {
  .toolbar .row.nav    { grid-template-columns: repeat(6, auto); }
  .toolbar .row.edit   { grid-template-columns: repeat(5, auto); }
}
```

### 8c. visualViewport hook

```ts
export function useKeyboardOffset() {
  const offset = useSignal(0);
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;
    const update = () => { offset.value = window.innerHeight - vv.height - vv.offsetTop; };
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    update();
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);
  return offset;
}
```

`<TerminalPage>` reads `offset` and applies `height: calc(100dvh - var(--toolbar-h) - var(--composer-h) - ${offset}px)` to the xterm container; on resize, xterm-addon-fit re-fits.

### 8d. Theme

```css
:root {
  color-scheme: light dark;
  --bg: #F5F2EA;
  --bg-header: #FAF7EF;
  --card: #FFFFFF;
  --fg: #1A1A1A;
  --muted: #8A8270;
  --accent: #C96442;
  --border: #E8E2D0;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #1a1a1a; --bg-header: #232323; --card: #2a2a2a;
    --fg: #f0f0f0; --muted: #888; --border: #333;
  }
}
.terminal-host { background: #0A0A0A; color: #E0E0E0; }
```

xterm theme via constructor option:

```ts
{ theme: { background: "#0A0A0A", foreground: "#E0E0E0", cursor: "#E0E0E0",
           green: "#7eebac", yellow: "#ebbe7e", magenta: "#c879ff", blue: "#6cf" } }
```

### 8e. Touch / IME / selection
- xterm-addon-fit handles dimensions
- xterm v5+ default selection on long-press: kept
- Composer textarea natively supports IME, paste, autocorrect
- Direct xterm typing on desktop: focus → keypress → `onData` → `sendText`; matches slice-1 contract

## §9 Error Handling

| Source | Trigger | Presentation | Recovery |
|---|---|---|---|
| `POST /v1/auth/login` network | DNS / connect fail | Login form red text "无法连接服务器" | User retries Sign in |
| `POST /v1/auth/login` 401 | bad creds | Login form red text "邮箱或密码错误" | User edits + retries |
| `POST /v1/auth/login` 429 | rate limit | Login form red text "尝试过于频繁，请稍候" | Backoff |
| `POST /v1/auth/refresh` network (cold start) | Boot offline | snackbar warn "无法连接服务器"; stay on `/` | User retries Login |
| `POST /v1/auth/refresh` 401 | cookie expired/revoked | Silent route to `/` with banner "会话已过期，请重新登录" | User re-logs in |
| `GET /v1/sessions` network | Bad backend | snackbar warn "session 列表加载失败 — 下拉刷新" | Pull-to-refresh |
| `GET /v1/sessions` 401 | access expired | Silent single-flight refresh + retry (§4f) | Automatic |
| Bridge `onConnectionState("disconnected")` | WSS close | Yellow banner "连接已断开 · 点此重连" | User taps |
| Bridge `onConnectionState("error")` | WSS error | Red banner with code | User goes back to list |
| Bridge `onError("auth", _)` | WSS auth | snackbar warn "会话过期，正在刷新…" → §5b refresh + re-setSession; 2nd failure → route("/") | Automatic + fallback |
| Bridge `onError("watch", msg)` | Session not visible | snackbar error msg | User re-taps row |
| xterm render error (window.onerror) | Renderer fault | Terminal area "终端崩溃 · 点此刷新" | User taps; component remounts |
| SW detects new version | Build deployed, user has PWA | snackbar info "新版本可用 [刷新]" (manual activation) | User taps |
| `POST /v1/auth/logout` failure | Network | Local clear + route("/") + snackbar warn "服务端清理失败，本地已退出" | User re-logs in |

## §10 Test Strategy

### 10a. Slice-1 inheritance
The 67 slice-1 + 2 slice-2 unit tests carry over from `android/terminal-web/src/**` to `web/app/src/**` unchanged. Adding `C-j` brings the count to 70.

### 10b. New frontend tests (Vitest + happy-dom + @preact/testing-library)

| File | Coverage |
|---|---|
| `auth/store.test.ts` | accessToken/userInfo signal write/clear; expiresAt math |
| `auth/refresh.test.ts` | 10 concurrent calls coalesce to 1 refresh; failure returns null; freshAccessToken's 60 s threshold |
| `api/client.test.ts` | 401 → refresh + retry once; refresh-failure surfaces original 401; non-401 untouched |
| `api/endpoints.test.ts` | URL/method/body shape per endpoint contract |
| `pages/login.test.tsx` | submit → busy; 401 / 429 / network text; success → navigate |
| `pages/sessions.test.tsx` | first-load spinner ≥200 ms; empty state; list render; pull-to-refresh; logout |
| `pages/terminal.test.tsx` | mount calls setSession; toolbar digit → sendText; special → sendSpecialKey; composer Send → sendText + clears; control-state disables toolbar |
| `components/snackbar.test.tsx` | signal write → render; 3 s auto-clear; error kind sticky |
| `components/toolbar.test.tsx` | 21-key mapping; disabled state |
| `hooks/useViewport.test.ts` | visualViewport mock → offset compute |

### 10c. Slice-1 protocol extension test
- `protocol/types.test.ts`: `SpecialKey` enum includes `C-j`
- `session/control.test.ts`: encoding-table row `C-j` → `[0x0a]`

### 10d. Backend Go tests
See §6k matrix.

### 10e. Manual smoke checklist (recorded in slice-completion commit)

1. `make smoke` boots Postgres + control + relay + termixd + smoke session
2. `cd web/app && npm run dev` → Vite + proxy at `http://localhost:5173`
3. **Desktop browser** (Chrome current): Login (bad creds → error; correct → navigate) → Sessions (smoke session visible) → tap → Terminal → Request Control → digit `1` (claude approval) → `↑` (history) → `Esc / Tab / ⌫ / ^C / ^J / Enter` each used → composer multi-line prompt + Send → close tab → reopen (cookie auto-restores via `/auth/refresh`) → Logout
4. **Mobile browser** (Android Chrome + iOS Safari, at least one each): same flow; verify PWA install to home screen; relaunch from icon (iOS PWA cookie sandbox requires re-login); virtual-keyboard visible → terminal area not hidden behind keyboard
5. **Error paths**: wrong password (401 text); kill Postgres post-login then pull-refresh (snackbar warn); wait > 30 min then act (silent refresh)

### 10f. CI
- Existing Go test job + 7 new tests
- New `web/app` job: `npm install && npm test && npm run build`
- No automated browser E2E this slice (manual smoke covers; Playwright is Phase 3)

## §11 Build / Dev / Deploy

### 11a. Dev workflow

```bash
# Shell 1
web/app/scripts/smoke.sh    # Postgres + control + relay + termixd + seed
# Shell 2
cd web/app && npm run dev    # Vite + API/WSS proxy at http://localhost:5173
```

`scripts/smoke.sh` is the renamed-and-relocated `android/terminal-web/scripts/smoke.sh`; functionality unchanged (Postgres preflight, smoke seed, REST login, host.json patch, `termix start`).

### 11b. Prod build

```bash
make build-web      # cd web/app && npm install && npm run build
                    #   && rsync -a --delete web/app/dist/ go/internal/controlapi/web_dist/
make build-go       # cd go && go build -tags embed -o bin/termix-control ./cmd/termix-control
                    # cd go && go build -o bin/termix-relay ./cmd/termix-relay
                    # cd go && go build -o bin/termixd ./cmd/termixd
                    # cd go && go build -o bin/termix ./cmd/termix
make build          # build-web + build-go
```

`-tags embed` activates `//go:embed` in `static.go`. Without the tag, `embeddedWebFS` is empty and the binary requires `TERMIX_CONTROL_WEB_DIR` env to start.

### 11c. Deploy topology

```text
[Internet] → Cloudflare/Caddy/nginx (TLS termination)
              ├─ termix.example.com         → termix-control :8080 (SPA + REST)
              └─ relay.termix.example.com   → termix-relay :8090 (WSS)
```

CSP `connect-src 'self' wss://relay.termix.example.com`. Both subdomains carry their own certs.

Environment variables required at deploy:

| Name | Where | Example |
|---|---|---|
| `TERMIX_CONTROL_RELAY_ORIGIN` | termix-control runtime | `wss://relay.termix.example.com` (used in CSP) |
| `TERMIX_ALLOWED_ORIGIN` | termix-control runtime | `https://termix.example.com` (Origin allowlist) |
| `TERMIX_ACCESS_TOKEN_TTL` | termix-control runtime, optional | `30m` (default) |
| `VITE_RELAY_WS_URL` | `npm run build` time | `wss://relay.termix.example.com/ws` (baked into SPA bundle) |

### 11d. New Makefile targets

```make
.PHONY: build-web build-go build smoke web-dev web-test

build-web:
	cd web/app && npm install && npm run build
	rsync -a --delete web/app/dist/ go/internal/controlapi/web_dist/

build-go:
	cd go && go build -tags embed -o bin/termix-control ./cmd/termix-control
	cd go && go build -o bin/termix-relay ./cmd/termix-relay
	cd go && go build -o bin/termixd ./cmd/termixd
	cd go && go build -o bin/termix ./cmd/termix

build: build-web build-go

web-dev:
	cd web/app && npm run dev

web-test:
	cd web/app && npm test

smoke:
	./web/app/scripts/smoke.sh
```

## §12 Known Risks and Deferred

### 12a. Known risks
1. **Access token in WSS query string** — browsers cannot set custom WS headers. Mitigated by 30-min TTL + relay log scrub. Phase 3 may move to `Sec-WebSocket-Protocol` sub-protocol; would touch slice-1 wsClient + relay handshake.
2. **iOS PWA cookie sandbox** — first launch from home-screen icon requires re-login. Platform behavior; not solvable client-side. Mentioned in Login onboarding copy.
3. **Service Worker stale-bundle vs new API** — `registerType: "prompt"` defers activation, but extreme staleness can let an old SPA hit a new API. Backend keeps OpenAPI changes additive; release notes flag breaking shifts.
4. **xterm.js iOS Safari IME edge cases** — known upstream quirks with certain IME compositions. Composer textarea path is unaffected; mobile users are guided to the composer.
5. **`device_label` is untrusted input** — limited to 80 chars at the SPA but not sanitized server-side. Admin UI rendering must HTML-escape (admin UI not in this slice; spec note flagged).
6. **Multi-tab uncoordinated control** — last-write-wins via relay state machine. Acceptable for V1; if "block other tabs" UX is desired later, that's a V2 task.

### 12b. Deferred (to PROGRESS.md upon merge)
- Auto-reconnect with exponential backoff
- WSS authentication via sub-protocol header
- In-app session creation
- Refresh-token rotation
- Push notifications / Background Sync / Periodic Sync
- Multi-account
- Tab/recents UX in Terminal
- Native Android / iOS apps (revisit only if PWA falls short)
- Core extraction into `@termix/terminal-core` workspace package (option B′)
- Phase 3 hardening: cert pinning, CSP report-uri, SRI, HSTS preload registration
- Admin Web UI / `termix-admin-api`

## §13 Slice Completion Criteria

Slice is complete when:

1. `cd web/app && npm install && npm test && npm run build` is green; ≥ 70 unit tests pass.
2. OpenAPI diff is in `openapi/control.openapi.yaml`, `oapi-codegen` regenerated, `cd go && go test ./...` green including 7 new tests.
3. `make build` produces a single `termix-control` binary that, on launch, serves the SPA at `/` and `/api/v1/*` continues to work.
4. Public deployment on a real domain + cert (`termix.example.com` + `relay.termix.example.com`) completes the §10e manual smoke checklist end-to-end, including a mobile browser run (Android Chrome + iOS Safari) with PWA install verified.
5. Lighthouse PWA audit passes "Installable" + "Service Worker" categories (other categories not gating).
6. `docs/PROGRESS.md` updated: slice listed as completed, §12b items entered as Pending.
