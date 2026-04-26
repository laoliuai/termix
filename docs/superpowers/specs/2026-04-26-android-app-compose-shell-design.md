# Android Slice 2: `app/` Compose Shell Design

**Status:** Approved 2026-04-26.
**Phase:** Android UI, slice 2 of 2 (slice 1 is `terminal-web` MVP, completed and smoke-validated 2026-04-26).
**Authoritative spec:** `docs/termix-v1-detailed-technical-spec.md` §5.5, §7.8, §9.2, §10.2, §10.3.
**Predecessor design:** `docs/superpowers/specs/2026-04-25-android-terminal-web-mvp-design.md`.

## §1 Goal and Scope

### Goal
Build `android/app/`: a Kotlin + Jetpack Compose Android app that lets a logged-in user list their running sessions and remote-control a chosen one through the slice 1 `terminal-web` bundle hosted in a `WebView`. End state of slice 2 is a debug-buildable APK that completes the spec §10.2 / §10.3 flows end-to-end against the live Go stack.

### In scope
- **Login screen** — server URL, email, password → `POST /v1/auth/login`.
- **Session list screen** — `GET /v1/sessions?status=running`, pull-to-refresh, refresh on resume, logout.
- **Session detail screen** — header (tool, name, host) + `WebView` hosting `terminal-web` + special-key toolbar + send-text composer + Request/Release Control buttons.
- **JS bridge wiring** — outbound via `WebView.evaluateJavascript("setSession(...)")` etc.; inbound via `@JavascriptInterface TermixBridge` (connection / control / error state).
- **Encrypted credential storage** — refresh token + persisted `server_base_url`.
- **Reactive token refresh** on `onError("auth", …)` from the WebView or REST 401, via a single OkHttp interceptor chokepoint.
- **Backend extensions bundled in this slice:**
  - `device_type=android` + `platform=android` enums on `LoginRequest`
  - `GET /v1/sessions?status=running` (list endpoint, owner-scoped)
  - `POST /v1/auth/refresh`
  Each ships with handler, sqlc query (where needed), and Go tests.
- **Single-active-session navigation**, single Gradle module, **contract-first** Kotlin client generated from `openapi/control.openapi.yaml`.
- **`Backspace` added to the slice 1 `SpecialKey` enum** (one-line extension).

### Out of scope (deferred)
- Tabs / recents switcher.
- Background service or push notifications.
- Multi-account.
- Auto-reconnect with backoff inside Compose (terminal-web exposes connection state; user re-taps the session row to reconnect for V1).
- Cert pinning, biometric unlock, ProGuard hardening (Phase 3).
- `termix sessions stop` / session-management actions from the app.
- Refresh-token rotation server-side (contract supports it; implementation deferred).
- `POST /v1/auth/logout` revoke endpoint.

## §2 Architecture

### Module shape
Single Gradle module `android/app/`, Kotlin DSL, `compileSdk=34`, `minSdk=26`. The `minSdk=26` floor unlocks `EncryptedSharedPreferences` without back-compat code; older devices are out of scope for V1.

### Layers

```text
ui/            Compose screens, navigation, ViewModels
  login/       LoginScreen + LoginViewModel
  sessions/    SessionListScreen + SessionListViewModel
  terminal/    TerminalScreen + TerminalViewModel + WebView host
  common/      shared composables (ErrorBanner, Spinner)

data/          Repository layer (single source of truth per concern)
  AuthRepository, SessionRepository
  TokenStore (EncryptedSharedPreferences), ServerConfigStore (regular SharedPreferences)
  ApiClientProvider (Hilt @Provides for the generated client)
  AuthInterceptor (OkHttp interceptor — adds Bearer; refreshes on 401)

bridge/        WebView ↔ Compose glue
  TermixWebView (custom AndroidView wrapper)
  TermixBridge (@JavascriptInterface, posts events to a SharedFlow)
  SendController (typed wrapper around evaluateJavascript)

di/            Hilt @Modules: NetworkModule, StorageModule, RepositoryModule
```

### Key dependencies

| Purpose | Library | Notes |
|---|---|---|
| UI | Jetpack Compose (BOM) + Material3 | platform standard |
| Navigation | `androidx.navigation:navigation-compose` | type-safe routes |
| DI | Hilt + KSP | one annotation per ViewModel |
| HTTP | Retrofit + Moshi + OkHttp | underneath the generated client |
| OpenAPI codegen | `org.openapitools:openapi-generator-gradle-plugin` | runs from `openapi/control.openapi.yaml` |
| Crypto storage | `androidx.security:security-crypto` | EncryptedSharedPreferences |
| Coroutines | `kotlinx-coroutines-android` | StateFlow / SharedFlow |
| Logging | Timber (debug only) | stripped in release |
| Test | JUnit4, MockWebServer, Compose ui-test, Turbine | unit + Compose tests |

### Concurrency
ViewModels expose `StateFlow<UiState>`; one-shot events (navigate, snackbar) via `SharedFlow`. The WebView bridge is `MainThread`; bridge events get re-emitted on `Dispatchers.Main.immediate`.

### WebView asset shipping
The slice 1 `terminal-web` `dist/` is copied into `android/app/src/main/assets/terminal-web/` by a Gradle task `:app:syncTerminalWebAssets` that depends on `:terminal-web:build` (registered as a `node` exec task that runs `npm run build` if `dist/` is stale). Adds ~4 s to the first build, no-op afterward.

## §3 Screen Graph and Compose Components

### Three top-level screens

1. **LoginScreen** — server URL field (persisted across launches), email, password, single Sign-in button. Inline error line under the form for auth failures.
2. **SessionListScreen** — list of running sessions for the user, pull-to-refresh, ⋮ menu with Logout. Tap a row opens the terminal screen for that session.
3. **TerminalScreen** — header with session label and connection badge, control-state bar with Request/Release Control, the `WebView` filling the middle, a one-line text+Send composer, and a special-key grid (`Esc Tab ↑ ↓ ^C ← → Enter ^D ⌫`).

### Navigation graph

Single Activity, NavHost with three composable destinations. Argument-typed routes via `navigation-compose` `Type` NavArg.

```text
login    ─[sign in OK]─►    sessions    ─[tap row]─►    terminal/{sessionId}
                                  ▲                           │
                                  └─────────[back]────────────┘
sessions ─[⋮ Logout]─►       login   (clears tokens)
```

**Cold-start branch:** at `MainActivity` launch, `AuthRepository.tryRestore()` checks for a stored refresh token; if present and not obviously expired, `NavHost.startDestination = sessions` and the list refreshes in background. Otherwise `login`.

### ViewModel responsibilities

| ViewModel | State | Actions |
|---|---|---|
| `LoginViewModel` | `UiState(serverUrl, email, password, busy, error?)` | `submit()` → `AuthRepository.login()` → emit `Navigate(Sessions)` on success |
| `SessionListViewModel` | `UiState(items, refreshing, error?)` | `refresh()`, `open(id)` → emit `Navigate(Terminal(id))`, `logout()` |
| `TerminalViewModel` | `UiState(connState, controlState, sessionMeta?)` | `requestControl()`, `releaseControl()`, `sendText(s)`, `sendKey(k)`; receives bridge events from a per-VM SharedFlow |

## §4 Data Flow

### 4a. Login (cold start, no stored credentials)

```text
LoginScreen[submit]
  → LoginViewModel.submit(serverUrl, email, password)
  → AuthRepository.login(...)            // suspending
       1. ServerConfigStore.put(serverUrl)
       2. ApiClientProvider.rebind(serverUrl)   // see §4e
       3. authApi.postAuthLogin({email, password,
                                 device_type=android,
                                 platform=android,
                                 device_label=Build.MODEL})
       4. TokenStore.put(access, refresh, expiresAt, deviceId, userId)
       5. return Unit
  → LoginViewModel emits Navigate(Sessions)
```

Errors at step 3 (network / 401) bubble as a sealed `LoginError` and surface as the inline error line. Step 1 + 2 run *before* step 3 so a failed login still leaves the server URL persisted.

### 4b. Session list (resume / pull-to-refresh)

```text
SessionListScreen.onResume() OR pull-to-refresh
  → SessionListViewModel.refresh()
  → SessionRepository.listRunning()      // suspending
       1. sessionsApi.getSessions(status="running")
       2. on 401 → AuthInterceptor refresh path (see §7), then retry once
       3. return List<SessionSummary>
  → ViewModel maps to UiState.items
```

### 4c. Terminal session (open → control → input)

```text
TerminalScreen onCreate(sessionId)
  → TerminalViewModel observes a per-VM bridgeEvents: SharedFlow<BridgeEvent>
  → loads file:///android_asset/terminal-web/index.html into WebView
  → WebViewClient.onPageFinished:
       webView.evaluateJavascript("setSession('<id>', '<relayUrl>',
                                              '<accessToken>', '<deviceId>')",
                                   _)
  → terminal-web (slice 1) opens WSS, sends hello.android + session.watch
  → TermixBridge.@JavascriptInterface methods receive state changes
       onConnectionState("connected") → bridgeEvents.emit(...)
       onControlState("granted")      → bridgeEvents.emit(...)
       onError("auth", msg)           → bridgeEvents.emit(...)  // triggers refresh path

Toolbar tap (Esc/^C/etc):
  → TerminalViewModel.sendKey(SpecialKey.Esc)
  → SendController.sendSpecialKey(SpecialKey.Esc)
  → webView.evaluateJavascript("sendSpecialKey('Escape')", _)

Send text:
  → TerminalViewModel.sendText(s)
  → SendController.sendText(s)
  → webView.evaluateJavascript("sendText(${js(s)})", _)

Request control / Release:
  → window.requestControl() / window.releaseControl() via evaluateJavascript
```

### 4d. Cold-start state hydration

```text
MainActivity launch
  → AuthRepository.tryRestore() returns Restored | NeedLogin
       1. read serverUrl from ServerConfigStore — if missing → NeedLogin
       2. read TokenStore — if no refresh token → NeedLogin
       3. rebind ApiClientProvider with serverUrl
       4. return Restored(deviceId, userId)
  → if Restored: NavHost.startDestination = sessions
  → if NeedLogin: startDestination = login
```

### 4e. How `ApiClientProvider.rebind(serverUrl)` works
The generated Retrofit client wraps a base URL that's only known at login time, so we cannot bind a single immutable `Retrofit` instance at app startup. The `ApiClientProvider` Hilt singleton holds a `@Volatile var retrofit: Retrofit?` and a `rebind(url)` method that **rebuilds** the `Retrofit` (and its API services) with the new base URL. This is safe because no concurrent REST calls are in flight at the two moments rebind happens (login submit and cold-start restore). Subsequent screens (`SessionRepository`, etc.) read the current `apiProvider.sessionsApi` lazily on each call, so they always see the latest binding.

## §5 WebView ↔ Compose Bridge Wiring

The slice 1 design (its §3) is the contract. Slice 2 implements the host side.

### 5a. The WebView itself (`bridge/TermixWebView.kt`)

```kotlin
@Composable
fun TermixWebView(
    sessionContext: SessionContext,
    onBridgeEvent: (BridgeEvent) -> Unit,
    sendController: SendController,
) {
    val context = LocalContext.current
    AndroidView(factory = {
        WebView(context).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccessFromFileURLs = false
            settings.mediaPlaybackRequiresUserGesture = false
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(v: WebView?, r: WebResourceRequest?) = true
                override fun onPageFinished(v: WebView?, url: String?) {
                    v?.evaluateJavascript(
                        "setSession(${js(sessionContext.sessionId)}," +
                                  "${js(sessionContext.relayUrl)}," +
                                  "${js(sessionContext.accessToken)}," +
                                  "${js(sessionContext.deviceId)})", null)
                    sendController.attach(v!!)
                }
                override fun onRenderProcessGone(view: WebView?, detail: RenderProcessGoneDetail?) =
                    onBridgeEvent(BridgeEvent.RendererCrashed).let { true }
            }
            addJavascriptInterface(TermixBridge(onBridgeEvent), "TermixBridge")
            loadUrl("file:///android_asset/terminal-web/index.html")
        }
    })
}
```

### 5b. Inbound bridge (Compose → JS) — `SendController`

```kotlin
class SendController {
    private var webView: WebView? = null
    fun attach(v: WebView) { webView = v }
    fun sendText(s: String)        = post { "sendText(${js(s)})" }
    fun sendSpecialKey(k: SpecialKey) = post { "sendSpecialKey(${js(k.wireName)})" }
    fun requestControl()           = post { "requestControl()" }
    fun releaseControl()           = post { "releaseControl()" }
    fun setSession(ctx: SessionContext) = post { /* same as onPageFinished body */ }
    private fun post(build: () -> String) {
        val v = webView ?: return
        v.post { v.evaluateJavascript(build(), null) }
    }
}
```

All `evaluateJavascript` calls go through `WebView.post {}` so they're guaranteed to hit the WebView's UI thread.

### 5c. Outbound bridge (JS → Compose) — `TermixBridge`

```kotlin
class TermixBridge(private val emit: (BridgeEvent) -> Unit) {
    @JavascriptInterface
    fun onConnectionState(state: String, detail: String?) =
        emit(BridgeEvent.Connection(ConnState.from(state), detail))

    @JavascriptInterface
    fun onControlState(state: String, detail: String?) =
        emit(BridgeEvent.Control(ControlState.from(state), detail))

    @JavascriptInterface
    fun onError(code: String, message: String) =
        emit(BridgeEvent.Error(code, message))
}
```

`@JavascriptInterface` callbacks fire on a binder thread. `TermixBridge` itself is dumb — re-dispatching to `Dispatchers.Main.immediate` is the responsibility of the `emit` lambda passed in by the caller. In practice, `TerminalScreen` constructs that lambda as a `MainScope().launch(Dispatchers.Main.immediate) { bridgeEvents.emit(it) }`, so the `SharedFlow` collector in `TerminalViewModel` is already on the main thread by the time it observes the event.

### 5d. Lifecycle

The WebView lives inside the Composable's `AndroidView`. `DisposableEffect(Unit)` calls `webView.destroy()` on dispose. No singleton, no leak. When the user navigates back to the list, the WebView is destroyed; on next open, a fresh one loads — terminal-web reloads in ~100 ms (assets are local) and re-fetches the snapshot.

### 5e. The `js()` helper

```kotlin
private fun js(s: String): String =
    "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"")
            .replace("\n", "\\n").replace("\r", "\\r") + "\""
```

Unit-tested. Every JS call goes through it; nothing builds raw JS strings.

### 5f. Slice 1 `SpecialKey` extension

`Backspace` is added to the enum in `android/terminal-web/src/protocol/types.ts` and to the encoding table in the slice 1 design §4c:

| `SpecialKey` | Bytes |
|---|---|
| `Backspace` | `0x7f` (DEL — what most ttys expect from the Backspace key) |

The `encodeSpecialKey` function in `android/terminal-web/src/session/control.ts` gets one new case. The `dev.html` toolbar gets one new button. The `control.test.ts` special-key encoding-table test gets one new row.

## §6 Backend Extensions (contract-first)

Three additive changes to `openapi/control.openapi.yaml`. Each ships with handler + tests in the same slice; nothing slice 2 relies on is fake.

### 6a. `LoginRequest` enum widening

```yaml
device_type: { type: string, enum: [host, android] }
platform:    { type: string, enum: [macos, ubuntu, android] }
```

The `devices` table column is already a free string per migration. Both Go server and the existing Go control client get regenerated. Existing CLI callers continue to send `host` — no behavior change.

### 6b. `GET /v1/sessions` (list)

```yaml
/sessions:
  get:
    operationId: listSessions
    security: [ { bearerAuth: [] } ]
    parameters:
      - in: query
        name: status
        schema: { type: string, enum: [running, idle, exited, all] }
    responses:
      "200":
        description: sessions for the authenticated user
        content:
          application/json:
            schema:
              type: object
              required: [sessions]
              properties:
                sessions:
                  type: array
                  items: { $ref: '#/components/schemas/Session' }
```

Returns sessions where `user_id = subject(JWT)`, ordered by `last_activity_at DESC`. `status=all` (or omitted) returns everything; otherwise filtered. Backed by a new `ListUserSessions` sqlc query. Cross-user isolation is enforced by the `WHERE user_id = $1` clause — the same pattern the existing single-session GET uses.

### 6c. `POST /v1/auth/refresh`

```yaml
/auth/refresh:
  post:
    operationId: postAuthRefresh
    requestBody:
      required: true
      content:
        application/json:
          schema:
            type: object
            required: [refresh_token]
            properties:
              refresh_token: { type: string }
    responses:
      "200":
        description: refreshed
        content:
          application/json:
            schema:
              type: object
              required: [access_token, expires_in_seconds]
              properties:
                access_token:        { type: string }
                expires_in_seconds:  { type: integer }
                refresh_token:       { type: string, nullable: true }   # set if rotated
      "401":
        description: refresh token invalid or revoked
```

Server hashes the supplied refresh token, looks up the row, checks `revoked_at IS NULL AND expires_at > now()`, mints a new access JWT for the same `user_id`. **Refresh-token rotation is opt-in** — V1 returns `refresh_token: null` (caller keeps the existing one). When rotation lands, populate the field and revoke the old row in one transaction. Existing `refresh_tokens` table already has the columns we need.

### 6d. Test coverage (Go)

- `tests/auth_login_android_test.go` — login as `device_type=android, platform=android` succeeds; existing host login still passes.
- `tests/sessions_list_test.go` — owner sees their sessions, status filter applies, foreign user cannot see them.
- `tests/auth_refresh_test.go` — happy path, expired refresh → 401, revoked refresh → 401.

No migration needed; uses existing tables.

## §7 Token Storage and Refresh Lifecycle

### 7a. Where things live

- **`TokenStore`** — `EncryptedSharedPreferences` named `termix-tokens`. Stores `access_token`, `refresh_token`, `access_token_expires_at` (epoch ms), `device_id`, `user_id`. Wiped on logout.
- **`ServerConfigStore`** — plain `SharedPreferences` named `termix-config`. Stores `server_base_url` only. Survives logout.
- Refresh tokens never enter logs, never appear in `Timber.d`, and are masked in any error UI ("…" + last 4).

### 7b. The `AuthInterceptor` (the only place refresh runs)

```kotlin
class AuthInterceptor(
    private val tokenStore: TokenStore,
    private val authApi: AuthApi,
) : Interceptor {
    private val mutex = Mutex()      // collapses concurrent 401s into one refresh

    override fun intercept(chain: Interceptor.Chain): Response {
        val req = chain.request().newBuilder()
            .header("Authorization", "Bearer ${tokenStore.accessTokenBlocking()}")
            .build()
        val res = chain.proceed(req)
        if (res.code != 401) return res

        // Try to refresh; if refresh fails, surface the original 401 to the caller.
        val refreshed = runBlocking { refreshOnce() } ?: return res

        res.close()
        return chain.proceed(req.newBuilder()
            .header("Authorization", "Bearer $refreshed")
            .build())
    }

    private suspend fun refreshOnce(): String? = mutex.withLock {
        val current = tokenStore.snapshot()
        // Has another request already refreshed while we were waiting?
        if (current.accessTokenExpiresAt > System.currentTimeMillis() + 5_000) {
            return current.accessToken
        }
        val resp = try { authApi.postAuthRefresh(RefreshRequest(current.refreshToken)) }
                   catch (_: Throwable) { return null }
        tokenStore.update(
            accessToken = resp.accessToken,
            expiresAt   = System.currentTimeMillis() + resp.expiresInSeconds * 1000L,
            refreshToken = resp.refreshToken ?: current.refreshToken,
        )
        return resp.accessToken
    }
}
```

Two invariants enforced by the `mutex`: (a) at most one refresh in flight per process, (b) requests blocked on the mutex re-read the freshly rotated token instead of double-refreshing.

### 7c. WSS auth uses the same path

terminal-web doesn't go through OkHttp, so the interceptor doesn't help directly. Instead:

- When `TerminalViewModel` builds the `SessionContext`, it calls `tokenStore.accessTokenFresh()`, a suspending helper that *also* takes the mutex above and refreshes if `expiresAt - now < 60_000`. So the access token handed to `setSession(...)` is guaranteed-fresh-or-just-refreshed.
- If the WSS still closes with `onError("auth", ...)`, `TerminalViewModel` calls `accessTokenFresh()` again (forces a refresh) and re-calls `setSession(...)`. One retry, then surfaces the error.

### 7d. Logout flow

```text
SessionListScreen [⋮ → Logout]
  → SessionListViewModel.logout()
       1. tokenStore.clear()
       2. emit Navigate(Login)         // does not preserve nav back-stack
  // No server call: the refresh token will sit on the server until expiry.
  // A future POST /v1/auth/logout revoke endpoint can be added without
  // changing the Compose flow.
```

### 7e. Refresh-token leakage hygiene

OkHttp logs are at `Level.NONE` in release. Debug builds use `Level.HEADERS` (not `Level.BODY`). The refresh-token POST body bypasses logging via a `redactHeader("Authorization")` and a custom body redactor.

## §8 Error Handling

One error vocabulary across all three screens, surfaced as the inline strip in the mockups (red text). No dialogs, no toasts unless noted.

| Source | Compose presentation | Recovery |
|---|---|---|
| `AuthRepository.login` failure (network) | Inline "Can't reach server" under the form | User retries Sign in button |
| `AuthRepository.login` failure (401) | Inline "Invalid email or password" | User edits + retries |
| `SessionRepository.listRunning` failure | Snackbar "Failed to load sessions — pull to refresh" | Pull-to-refresh |
| Cold-start `tryRestore` returns NeedLogin (refresh expired) | Silently lands on Login (no error UI) | User logs in again |
| Bridge `onError("auth", …)` | Snackbar "Session expired, reconnecting…" then auto-retry once via §7c. If retry fails: snackbar "Sign in again" + Navigate(Login). | Automatic |
| Bridge `onError("watch", …)` | Snackbar with the relay's message | Tap session row again |
| `WebView` crash (`onRenderProcessGone`) | Full-screen "Terminal crashed — tap to reload" | Tap reloads the WebView |

`onRenderProcessGone` is the easy-to-forget one. Without it, a renderer crash leaves the AndroidView blank and the user sees a stuck UI. Returning `true` keeps the host process alive.

## §9 Test Strategy

Three layers; the first two run in `./gradlew test`, the third is a manual smoke checklist matching slice 1's pattern.

### 9a. Unit tests (JVM, no Android framework)

- `AuthRepositoryTest` — login success writes TokenStore; login failure leaves stored URL but no token; refresh-once mutex collapses concurrent calls (use `Turbine` + `UnconfinedTestDispatcher`).
- `AuthInterceptorTest` — 401 triggers exactly one refresh under concurrent requests; refresh failure → original 401 returned (no infinite loop). Use `MockWebServer`.
- `SessionRepositoryTest` — list happy path; status filter passed through; 401 retry uses the new token.
- `TokenStoreTest` — `EncryptedSharedPreferences` round-trip; `accessTokenFresh()` triggers refresh when expiry < 60 s.
- `JsEscapeTest` — golden tests over a fixed set of sample strings (empty, plain ASCII, with `"`, with `\`, with `\n`, with `\r\n`, with mixed) asserting `js(s)` produces the expected wire output. The slice 1 `dev.html` cycle covers the JS-side round-trip; the JVM unit test only verifies escape correctness.
- `SpecialKeyMapperTest` — every `SpecialKey` → wireName matches the slice 1 enum literal (including `Backspace`).
- ViewModel tests for each screen: state transitions, navigation events, error mapping. Turbine to assert StateFlow/SharedFlow.

### 9b. Compose UI tests (`androidTest`)

- `LoginScreenTest` — types into fields, taps Sign in, verifies VM action.
- `SessionListScreenTest` — empty state, populated list, pull-to-refresh, logout menu.
- `TerminalScreenTest` with a fake `SendController` — toolbar buttons fire the right `sendSpecialKey` payload; Send composes and clears the input.
- WebView is replaced by a `Box { Text("[fake terminal]") }` in tests; the real WebView path is covered by the manual smoke.

### 9c. Backend Go tests (in `go/tests/`)

- `auth_login_android_test.go`
- `sessions_list_test.go`
- `auth_refresh_test.go`

### 9d. Manual smoke checklist (live stack, recorded in slice-completion commit)

1. Start `android/terminal-web/scripts/smoke.sh`.
2. Build the debug APK on a Pixel-class emulator (or a phone on the same LAN).
3. Sign in with `smoke@test.local` / `smoke-pass`, server `http://10.0.2.2:8080/api/v1` (emulator) or LAN IP.
4. Session list shows the smoke session.
5. Tap → terminal renders snapshot.
6. Request Control → `granted` badge.
7. Send `echo hello-from-android`; observe output.
8. Special-key toolbar: `↑` recalls history; `^C` interrupts; `Enter` submits; `Backspace` deletes.
9. Background app for 30 s, foreground — connection re-establishes (user re-taps row).
10. Logout → returns to Login; auto-restore does not fire on next launch.

### 9e. CI

Slice 2 adds `cd android && ./gradlew testDebugUnitTest lintDebug` to the existing CI matrix. Compose UI tests are gated to a separate workflow because they need an emulator.

## §10 Build and Run

### 10a. Layout

```text
android/
├── app/
│   ├── build.gradle.kts
│   ├── proguard-rules.pro
│   ├── src/
│   │   ├── main/
│   │   │   ├── AndroidManifest.xml
│   │   │   ├── kotlin/com/termix/app/...
│   │   │   ├── res/...
│   │   │   └── assets/terminal-web/    ← synced from android/terminal-web/dist/
│   │   ├── test/                       ← unit tests
│   │   └── androidTest/                ← Compose UI tests
├── build.gradle.kts
├── settings.gradle.kts
├── gradle.properties
├── gradlew, gradlew.bat
└── gradle/wrapper/...
```

### 10b. Gradle tasks

```bash
cd android
./gradlew assembleDebug                # builds APK; depends on syncTerminalWebAssets
./gradlew :app:syncTerminalWebAssets   # runs `npm run build` in terminal-web/, copies dist/
./gradlew testDebugUnitTest            # JVM unit tests
./gradlew connectedDebugAndroidTest    # Compose UI tests, needs emulator/device
./gradlew :app:openApiGenerate         # regenerates the Kotlin client
```

### 10c. Generator config (sketch)

```kotlin
openApiGenerate {
    generatorName.set("kotlin")
    inputSpec.set("$rootDir/../openapi/control.openapi.yaml")
    outputDir.set("$buildDir/generated/openapi")
    apiPackage.set("com.termix.api")
    modelPackage.set("com.termix.api.model")
    configOptions.set(mapOf(
        "library"              to "jvm-retrofit2",
        "useCoroutines"        to "true",
        "serializationLibrary" to "moshi",
        "moshiCodeGen"         to "true",
        "dateLibrary"          to "kotlinx-datetime",
    ))
}
tasks.named("preBuild") { dependsOn("openApiGenerate") }
```

### 10d. Local-network HTTPS

Debug builds set `usesCleartextTraffic="true"` in a debug-only `network_security_config.xml` so we can hit `http://10.0.2.2:8080` (emulator) and `http://<host>:8080` (LAN). Release builds inherit Android's HTTPS-only default.

### 10e. Versioning

`versionName="0.2.0-debug"` until first beta, then bumped per release.

## §11 Deferred and Known Risks

### 11a. Deferred to follow-ups (tracked in PROGRESS.md when slice 2 lands)

- Recents switcher / tab UX.
- Auto-reconnect with backoff in Compose (today: user re-taps the row).
- Background service / push notification on session events.
- Cert pinning, biometric unlock, ProGuard hardening — Phase 3.
- Refresh-token rotation on the server side (the contract supports it; the implementation will land when refresh-token reuse detection is added).
- `POST /v1/auth/logout` revoke endpoint.
- A landing-page action to start a new session from the app (user still does this from their host terminal).

### 11b. Known risks

- **WebView version skew.** Android system WebView varies by OEM. We pin to "WebView 90+ behavior" because that's the floor where modern `evaluateJavascript` callbacks behave consistently. minSdk=26 doesn't guarantee that, but Play Services updates WebView on most devices.
- **`@JavascriptInterface` thread.** Calls fire on a binder thread. Forgetting `Main.immediate` would race with Compose recomposition. Mitigation: the bridge always re-emits via `MainScope().launch(Dispatchers.Main.immediate) { emit(...) }`. Tested.
- **Token in JS string.** `setSession(...)` passes the access token as a JavaScript string argument. It lives in the WebView process's heap for the session lifetime. Acceptable for V1 (the device is trusted); revisit if we add a per-request signature scheme.
- **Asset sync staleness.** If `android/terminal-web/dist/` is older than the app build, `syncTerminalWebAssets` rebuilds; if `npm` isn't on PATH the task fails fast with a clear message. CI installs Node before running gradle.
- **Cold start with stale token.** If `tryRestore` succeeds but the relay closes with 401 immediately, the user sees an empty list briefly, then gets bounced to Login. Acceptable.
- **WebView memory.** Each terminal screen entry allocates ~30–50 MB. Single-active-session caps this at one. If we ever add tabs, this becomes the dominant constraint.

## §12 Slice Completion Criteria

Slice 2 is done when:

1. `cd android && ./gradlew assembleDebug testDebugUnitTest lintDebug` passes from a clean checkout.
2. Backend extensions §6 are in `openapi/control.openapi.yaml`, regenerated Go server compiles, all `go/tests/...` pass.
3. The §9d manual smoke checklist runs end-to-end against the live Go stack and is recorded in the slice-completion commit message.
4. `docs/PROGRESS.md` lists the slice as completed and the deferred items as Pending.
