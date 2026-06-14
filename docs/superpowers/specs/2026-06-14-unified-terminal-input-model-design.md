# Unified Terminal Input Model — Design (v0.5.1)

**Date:** 2026-06-14
**Status:** Draft — awaiting user approval
**Branch:** `fix/v051-input-model`
**Origin:** v0.5.0 manual-testing UX findings (`docs/PROGRESS.md` → "v0.5.0 manual-testing UX findings"), items **input-2a** (expanded) and **layout-1b**.

## Goal

Make typing into a controlled session **direct and uniform** across PC and phone: when control is granted the terminal is focused and the user types straight at the cursor (text, Enter, Backspace, and Chinese/IME), with a slim, collapsible bar for the special keys a phone soft-keyboard cannot produce (Esc, Tab, arrows, Ctrl-combos) — collapsed by default on PC, expanded on phone, and toggleable either way. This replaces the current 3-step Composer flow (type in box → Send → Enter).

## Background — current input chrome

- `web/app/src/pages/terminal.tsx:160-164`: a `#terminal` host, then a `<ComposerDock open={granted}>` wrapping `<Composer>` + `<Toolbar>`.
- `web/app/src/components/composer.tsx`: a `<textarea rows={1}>` + **Send** button; `onSend → window.sendText(text)` writes the text **to the cursor with no CR**.
- `web/app/src/components/toolbar.tsx`: three rows — digits `0-9` (`onDigit → window.sendText`), nav `Esc Tab ↑ ↓ ← →`, edit `⌫ ^C ^D ^J Enter` (`onSpecial → window.sendSpecialKey`).
- `web/app/src/bridge/inbound.ts:~297`: xterm `onData` is **already** forwarded to `active.sendInput()` (same path the Composer uses), gated by `control.canSendInput()`.
- `web/app/src/ui/terminal.ts`: xterm is mounted, then wrapped in a **scaler** `<div>` that applies `transform: scale(N)` (N≤1, Stage-2 downscale-to-fit). The terminal is **never focused** — there is no `term.focus()` anywhere in the codebase.

So the plumbing for direct typing exists; it is unreachable only because (a) the terminal is never focused and (b) the Composer/Toolbar is the only visible input affordance.

## Root causes (confirmed by investigation)

1. **No focus.** Nothing calls `term.focus()`. Without focus, keystrokes/IME never reach xterm's hidden `.xterm-helper-textarea`, so direct typing and click-to-type do not work; the user is forced through the Composer.
2. **Stuck IME char.** The stray "我" that could not be deleted is a focus/IME-state desync: a composition landed in the hidden textarea while focus was not being held, so the follow-up Backspace went nowhere. The Stage-2 `transform: scale()` on an ancestor of the helper textarea can additionally misplace the IME candidate window, but committed text still flows through `compositionend → onData`. **Fixing focus is the primary fix;** the transform's cosmetic effect on the candidate popup is verified on a real device and patched only if it proves unusable (see §4.5).

## Design — unified input model

### 4.1 Auto-focus + tap-to-focus (both platforms)

- Add `focus()` to the `TerminalUI` interface (`web/app/src/ui/terminal.ts`); implement as `term.focus()`.
- In `web/app/src/bridge/inbound.ts`, when control becomes **granted**, call `cfg.ui.focus?.()`. On phones this raises the soft keyboard; on PC it makes the cursor live so the user types immediately.
- Tap/click on the terminal host focuses xterm (xterm provides this natively once the element is interactive; ensure the scaler wrapper does not eat pointer events — it must not set `pointer-events: none` on the path to xterm). No custom click handler is added unless device testing shows native click-to-focus fails through the scaler.
- No PC/phone branching for focus — behavior is identical on both, per the agreed model.

### 4.2 Direct typing (already wired)

- Keep the existing `term.onData → sendInput` path. Text, Enter, Backspace, and IME-committed text all flow through it. Input stays gated by `control.canSendInput()` (read-only viewers cannot type).
- No change to the wire protocol.

### 4.3 Remove the Composer

- Delete `web/app/src/components/composer.tsx` and its test; remove `<Composer>` from `terminal.tsx` and the `onCompose`/`window.sendText`-for-compose wiring that only served it.
- `input-2b` (textarea auto-grow) is **dropped** — there is no Composer to grow.
- Multi-line input is handled by direct typing + `^J` (newline-in-Claude). A future optional multi-line composer is explicitly out of scope.

### 4.4 Streamlined, focus-preserving Toolbar

- Reduce the toolbar to the keys a soft keyboard cannot send, plus two fallbacks:
  `Esc  Tab  ↑  ↓  ←  →  ^C  ^J  ^D  ⌫  Enter`.
  **Drop the `0-9` digit row** (type digits directly). `⌫`/`Enter` are kept as fallbacks because some mobile soft keyboards fire neither a reliable `keydown` nor `onData` for them.
- **Focus preservation (critical for mobile):** toolbar buttons must NOT steal focus from xterm, or the soft keyboard collapses on every tap. Implement each button to send on `onMouseDown`/`onTouchStart` with `preventDefault()` (so the default focus-shift never happens), instead of `onClick`. After sending, focus remains on the terminal.
- The `ComposerDock` wrapper is kept as the granted-state slide-in container but now wraps only the `Toolbar` (rename deferred to avoid churn). Its visibility is further governed by the collapse state in §4.5.

### 4.5 Collapsible toolbar panel (platform default + user toggle)

- The toolbar panel is **collapsible**, independent of the control-granted gate:
  - **Not granted** → panel and toggle hidden (unchanged).
  - **Granted + expanded** → full toolbar visible.
  - **Granted + collapsed** → toolbar hidden; only a small toggle affordance remains so the terminal reclaims the vertical space and the user can re-expand.
- **Platform default** (first visit, no stored preference), via `window.matchMedia('(hover: hover) and (pointer: fine)')`:
  - desktop (match) → **default collapsed** (physical keyboard sends every special key natively, so the on-screen bar is usually clutter);
  - phone/touch (no match) → **default expanded** (soft keyboard cannot send the special keys).
- **User toggle**: a button — visible whenever control is granted, in both states — flips collapsed/expanded. The choice is **persisted to `localStorage` (`termix_toolbar_expanded` = "1" | "0")** and overrides the platform default on subsequent loads.
- Resolution order on mount: stored preference if present, else platform default. A helper `initialToolbarExpanded()` encapsulates this (unit-testable by mocking `localStorage` + `matchMedia`).
- The toggle is focus-preserving like the other toolbar buttons (§4.4), so expanding/collapsing on mobile does not dismiss the soft keyboard.
- i18n: add `terminal.toolbar.show` / `terminal.toolbar.hide` (EN + ZH) for the toggle's accessible label.

### 4.6 Scaler / IME handling

- Keep Stage-2's `transform: scale()` downscale-to-fit (do not revert to font-scaling — that would undo Stage 2).
- Primary fix is §4.1 focus management.
- **Device verification (user, post-deploy):** with control granted on PC and Android, confirm: (a) clicking/tapping the terminal types directly; (b) a Chinese IME composition commits to the cursor and is deletable; (c) the candidate window is positioned acceptably.
- **Conditional patch (only if device testing fails):** if the IME candidate window is unusably misplaced under the scaler, apply a targeted inverse-scale to `.xterm-helper-textarea` (counter-transform) so its screen rect matches reality. This is held back unless needed, to avoid destabilizing the verified Stage-2 fit.

### 4.7 layout-1b — condense header in landscape / short viewports

- Removing the Composer already returns significant vertical space, so this shrinks to a CSS-only tidy.
- Add to `web/app/src/theme/styles.css`: `@media (orientation: landscape) and (max-height: 500px)` that reduces `.term-header` / `.control-bar` padding and font-size so the two bands cost less height. Keep both bars in document flow (no absolute repositioning) to avoid layout surprises. Connection badge stays visible.

## Components & files affected

- `web/app/src/ui/terminal.ts` — add `focus()` to interface + impl; verify scaler does not block pointer/focus.
- `web/app/src/bridge/inbound.ts` — call `ui.focus()` on control-granted.
- `web/app/src/components/toolbar.tsx` — drop digits row; focus-preserving `onMouseDown`+`preventDefault`; key set per §4.4.
- `web/app/src/pages/terminal.tsx` — remove `<Composer>`; add toolbar collapse state + a focus-preserving toggle button (rendered whenever control is granted); show `ComposerDock` only when **granted AND expanded**; seed state from `initialToolbarExpanded()`; persist toggles to `localStorage`; drop `onCompose`/`onDigit` if unused.
- `web/app/src/components/composer.tsx` + `composer.test.tsx` — **deleted**.
- `web/app/src/i18n/messages.ts` — add `terminal.toolbar.show` / `terminal.toolbar.hide` (EN + ZH); remove the now-dead `terminal.placeholder` key (Composer-only).
- `web/app/src/theme/styles.css` — collapsed-state + toggle-button styles (§4.5); landscape media query (§4.7); remove now-dead `.composer*` rules.
- Tests: `terminal.test.ts`, `inbound.test.ts`, `toolbar.test.tsx`, `pages/terminal.test.tsx`, `composer-dock.test.tsx` updated to the new model; new tests for `initialToolbarExpanded()` + toggle/persistence.

## Testing strategy

- **Unit (vitest), TDD:**
  - `terminal.test.ts`: `mountTerminal` exposes `focus()` and it calls the underlying `term.focus()`.
  - `inbound.test.ts`: on `control.granted`, `ui.focus()` is called once (and existing control-state assertions still hold).
  - `toolbar.test.tsx`: digit row absent; key set = Esc/Tab/arrows/^C/^J/^D/⌫/Enter; a button press fires the handler **without** moving focus (assert `preventDefault` called on `mousedown`).
  - `initialToolbarExpanded()`: desktop (`matchMedia` hover+fine-pointer) → collapsed (`false`); touch → expanded (`true`); a stored `localStorage` preference overrides the platform default in both directions.
  - `pages/terminal.test.tsx`: no Composer rendered; when granted, the toggle button is present and the Toolbar's visibility follows the collapse state; clicking the toggle flips visibility and writes `localStorage`; when collapsed, the toggle shows but the key buttons do not.
- **Not unit-testable → device-manual (user, post-deploy):** real IME composition/deletion, soft-keyboard persistence across toolbar taps, candidate-window placement under the scaler, landscape header height.

## Out of scope / deferred (tracked in PROGRESS)

- **input-2b** — dropped (Composer removed).
- **layout-1a** (phone font small) — deferred; rely on native pinch-zoom (downscale-to-fit is the Stage-2 design).
- **display-3** (doubled statusline after IME+Esc) — needs runtime evidence (`?debug=1` raw bytes / frame order); investigate after this slice, may share the focus/IME root cause.
- General modifier-key mechanism (hold-Ctrl/Alt then next key, full `Ctrl+Shift+...` matrix) — future enhancement; this slice ships a fixed key set.

## Risks & mitigations

- **Mobile soft keyboard collapses on toolbar tap** → focus-preserving `mousedown`+`preventDefault` (§4.4); covered by a unit test for `preventDefault`, confirmed on device.
- **IME candidate window misplaced by scaler** → focus-first fix usually suffices; conditional counter-transform held in reserve (§4.5); user device-verifies.
- **Removing Composer loses multi-line compose** → accepted per user decision; `^J` covers newlines; future optional composer noted.
- **Auto-focus raising the keyboard is intrusive on phone** → only fires on the explicit control-granted transition (user already tapped "Request"), not on mere page load.
- **Collapsed toolbar is undiscoverable** → when granted+collapsed a small toggle affordance is always shown (never fully hidden), and the user's manual choice is remembered, so PC users who want it stay expanded after one tap.

## Rollout

Ship as **v0.5.1** following the established flow (bump `web/app/package.json` + lockfile, tag, GH Actions release, `deploy/deploy.sh`). SPA-only behaviour reaches users on next browser refresh (autoUpdate SW); **no daemon change** in this slice. Post-deploy device soak per ship-and-iterate.
