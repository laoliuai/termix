# Web UI Productization Design

Date: 2026-04-30
Status: Approved for implementation planning

## Problem

The current Web UI works as an app surface, but it still feels like an internal tool. The root page is a login form, the product value is not visible before authentication, the Sessions page is a simple list, and the authenticated header hides Help and Logout behind a crowded menu. The UI also mixes English and Chinese strings without a language model.

Termix needs to feel like a product entry point: a new user should understand what Termix does, install the host client, log in, and then quickly resume or control running sessions from desktop or mobile.

## Goals

- Make `/` a real product homepage for unauthenticated users.
- Keep the product positioning focused on AI coding sessions, while mentioning long-running bash or background tasks as a secondary use case.
- Move login to a clearer product flow at `/login`.
- Keep authenticated users landing on `/sessions`.
- Redesign `/sessions` as a responsive session workbench:
  - desktop: richer management view
  - mobile: compact quick-resume list
- Clean up authenticated navigation so Help, Refresh, Logout, and language switching have predictable homes.
- Keep `/help`, but evolve it into a small bilingual help center for install and first-run instructions.
- Add lightweight Chinese/English language switching without overbuilding the frontend stack.

## Non-Goals

- Do not change the tmux-backed architecture.
- Do not put Python or browser code on the terminal byte path.
- Do not replace the current relay/watch/control flow.
- Do not build a full marketing site with separate pricing, blog, docs, or account settings pages.
- Do not promise arbitrary custom command sessions as a completed product feature in this slice. The homepage may mention long-running scripts as a Termix-shaped use case, but the current CLI only supports `claude`, `codex`, and `opencode`.
- Do not internationalize backend API responses in this slice.

## Product Positioning

Primary positioning:

```text
Take over AI coding sessions running on your host.
```

Chinese homepage headline:

```text
接管主机上的 AI coding session
```

Supporting copy should mention the broader terminal use case without making it the main category:

```text
Termix brings tmux sessions from your host to the browser. It is built for Claude,
Codex, and opencode, and also fits long-running scripts, builds, and background jobs.
```

Chinese supporting copy:

```text
在浏览器里查看和控制主机 tmux 中的 Claude、Codex、opencode，
也适合长时间运行的脚本、构建和后台任务。
```

This avoids positioning Termix as a generic SSH or web-shell replacement while still making the durable-session value clear.

## Routes

Target route model:

```text
/                  Product homepage for unauthenticated users
/login             Login form
/sessions          Authenticated session workbench
/terminal/:id      Authenticated terminal control surface
/help              Help and install page
```

Cold-start auth behavior:

- If refresh succeeds and the current path is `/`, redirect to `/sessions`.
- If refresh fails on `/`, show the product homepage.
- If an unauthenticated user opens `/sessions` or `/terminal/:id`, redirect to `/login`.
- `/help` stays public. When opened from the authenticated app, its back action returns to `/sessions`; otherwise it returns to `/`.

## Homepage Design

The homepage is a product entry and install entry, not a login page.

Header:

- Left: `TMX Termix`.
- Right: `Help`, language switcher, `Sign in` / `登录`.
- If authenticated, replace `Sign in` with `Sessions` / `进入 Sessions`.

Hero:

- Use a technology-forward bitmap or product visual, not a purely decorative gradient.
- The visual should imply remote terminal sessions, tmux, and host-to-browser control.
- Avoid hiding the product behind vague abstract imagery.

Hero content:

- H1: the core offer, with Termix branding in the header.
- Supporting copy: AI coding sessions first, long-running scripts second.
- Primary CTA: install Termix.
- Secondary CTA: view Help.
- If authenticated, primary CTA becomes `Open Sessions`.

Homepage body:

- Three value blocks:
  - Browser control: view and control sessions from desktop or mobile.
  - Native tmux sessions: work keeps running on the host.
  - One-line install: macOS and Ubuntu host client setup.
- Install section:
  ```bash
  curl -fsSL https://raw.githubusercontent.com/termix/termix/main/install.sh | sh
  termix login
  termix start codex --name main
  ```
- Link to `/help` for platform downloads and troubleshooting.

## Login Page

Move the login form to `/login`.

The login page should be compact and task-focused:

- Brand header with Termix mark.
- Email and password fields.
- Inline error state.
- Link back to homepage.
- Link to Help or install instructions.

After successful login, route to `/sessions`.

## Sessions Workbench

The Sessions page becomes the main authenticated app surface.

### Desktop Layout

Desktop is a session workbench optimized for scanning and management.

Header:

- Left: `TMX Termix`.
- Current page title can sit below or in the content area as `Sessions`.
- Right:
  - Help icon or `?`
  - Refresh icon with busy/success state
  - Account menu

Account menu:

- Signed-in email.
- Language selector.
- Logout.

Content:

- Page title: `Sessions`.
- Small summary: running count, host count if available, last updated time.
- Search box if cheap to implement locally.
- Segmented filter:
  - `Running`
  - `All`
- Session rows:
  - session name and tool
  - host label
  - recent activity
  - status or control state
  - launch command or concise command hint when available
  - primary `Open` action

Sort order:

- Running sessions first.
- Within running sessions, most recently active first.

### Mobile Layout

Mobile is a quick-resume list, not a compressed desktop table.

Each session card should show only the information needed to choose and open:

- `tool · name`
- host label
- recent activity
- live/control badge
- large `Open` action

Hide secondary information such as full command, created time, and detailed host metadata on mobile. Those details can appear after opening the terminal or in later detail surfaces.

### Empty State

The empty state should teach the next action instead of just reporting that no sessions exist.

It should include:

1. Install Termix.
2. Run `termix login` on the host.
3. Start a session:
   ```bash
   termix start codex --name main
   ```
4. Link to Help.

## Help Page

Keep `/help`.

In this slice, Help remains one page but becomes bilingual and product-facing:

- Install command.
- Direct download links for macOS and Ubuntu artifacts.
- First-run steps.
- Supported tools:
  - `claude`
  - `codex`
  - `opencode`
- Troubleshooting:
  - `termix doctor`
  - `~/.local/bin` on `PATH`
  - `tmux` installed
- Short explanation that Termix starts its local background service automatically.

Help should avoid using `termixd` as normal product vocabulary.

## Navigation Behavior

Unauthenticated homepage:

- `Help`
- language switcher
- `Sign in`

Authenticated app header:

- Help is visible, not hidden behind a menu.
- Refresh is visible only where useful, initially on Sessions.
- Logout is inside the account menu.
- Language switching is inside the account menu, with optional direct access on homepage.

This reduces right-side crowding and matches common product navigation patterns.

## Internationalization

Use lightweight built-in i18n.

Add a frontend module:

```text
web/app/src/i18n/
  messages.ts
  store.ts
```

Responsibilities:

- Support `zh-CN` and `en`.
- Store the selected language in `localStorage`.
- Default to browser language when no saved language exists.
- Expose a simple translation helper such as `t(key)`.
- Keep routes stable across languages.

Initial translation scope:

- Homepage.
- Login page.
- Sessions page.
- Help page.
- Terminal control bar and composer placeholder.
- Snackbar messages.
- Empty states.
- Common auth and network errors.

Backend errors:

- Do not internationalize backend responses yet.
- Map common frontend-visible status codes and known messages to local strings.
- Preserve raw backend error text only as a fallback.

This is intentionally smaller than adding i18next or a full localization pipeline. The Web UI is small enough that a local dictionary is easier to maintain.

## Data and API Needs

The first implementation can use the existing sessions list API.

Useful current fields:

- `id`
- `tool`
- `name`
- `status`
- `host_label`
- `last_activity_at`
- `created_at`

Potential follow-ups:

- Add host online summary if the product needs host counts.
- Add explicit control-state summary if the list should show whether a session is currently controlled elsewhere.
- Add custom command support if long-running arbitrary bash commands become a formal product promise.

The desktop workbench should degrade gracefully if optional fields are missing.

## Error Handling

Homepage:

- If auth refresh fails, show the homepage without a scary error.
- If the network is unreachable, keep the homepage visible and show a small warning.

Login:

- 401: invalid email or password.
- 429: too many attempts.
- network failure: server unreachable.

Sessions:

- Initial load: delayed spinner to avoid flicker.
- Refresh: busy state and short success feedback.
- Load failure: localized snackbar.
- Empty list: install/start guidance.

Terminal:

- Keep the existing reconnect/auth-refresh behavior.
- Localize visible control labels and errors.

## Testing

Unit and component tests:

- Homepage renders unauthenticated product content and install command.
- Homepage routes to `/login`, `/help`, and `/sessions` depending on auth state.
- Login route still authenticates and redirects to `/sessions`.
- Sessions desktop content renders richer metadata.
- Sessions mobile layout hides secondary desktop-only details through CSS/responsive classes.
- Header exposes Help and Refresh directly where expected.
- Account menu exposes language and logout.
- Language selection persists across reload.
- Core pages render in both `zh-CN` and `en`.
- Help page renders translated install/start/troubleshooting content.

Build verification:

```bash
cd web/app && npm run typecheck
cd web/app && npm test -- --run
cd web/app && npm run build
make build-web
make check-web-dist
```

Manual smoke:

- Desktop Chrome:
  - `/` shows homepage when unauthenticated.
  - `Sign in` opens `/login`.
  - Login redirects to `/sessions`.
  - Sessions workbench opens a terminal.
  - Help is directly reachable.
  - Logout returns to homepage or login flow.
- Mobile browser:
  - Homepage hero and install section do not overlap.
  - Sessions page uses compact cards.
  - Header controls do not crowd the viewport.
  - Terminal page remains usable with virtual keyboard.

## Rollout

1. Add i18n infrastructure and convert existing hard-coded strings.
2. Add the product homepage and `/login` route.
3. Update auth redirects and guards.
4. Redesign the authenticated header.
5. Redesign the Sessions page with desktop and mobile responsive layouts.
6. Update Help page content and translations.
7. Update tests.
8. Rebuild embedded Web assets.
9. Update `docs/PROGRESS.md` with implementation status.

## Decisions

- The primary product message is AI coding session control.
- Long-running bash/script tasks are mentioned as a secondary use case, not as a full arbitrary-command promise in this slice.
- Desktop Sessions is a workbench.
- Mobile Sessions is a quick-resume list.
- Help remains at `/help`.
- Logout moves into the account menu.
- Help and Refresh become explicit header actions where relevant.
- Language switching uses a lightweight local dictionary, not a full external i18n framework.
