# Unified Terminal Input Model — Implementation Plan (v0.5.1)

> **For agentic workers:** TDD per task (red → green → refactor). Steps use checkbox syntax. Spec: `docs/superpowers/specs/2026-06-14-unified-terminal-input-model-design.md`.

**Goal:** Direct typing into the terminal (auto-focus on control-grant, both platforms) + a slim, collapsible special-keys toolbar (PC default collapsed, phone default expanded, user-toggle persisted). Delete the text Composer. Condense the header in landscape. SPA-only, no daemon change.

**Branch:** `fix/v051-input-model` (worktree `.worktrees/v051-input`).

**Test/build:** `cd web/app && npm test -- --run`, `npm run typecheck`, `npm run build`.

---

## Task 1: `focus()` on TerminalUI

**Files:** `web/app/src/ui/terminal.ts`, `web/app/src/ui/terminal.test.ts`

- [ ] **Step 1 — failing test.** In `terminal.test.ts`: (a) add `focus: ReturnType<typeof vi.fn>` to the mock instance type and `focus: vi.fn()` to the mock factory instance. (b) Add:
```ts
it("exposes focus() that calls the underlying term.focus()", () => {
  const container = document.createElement("div");
  setContainerSize(container, 1280, 800);
  const ui = mountTerminal(container);
  (ui as unknown as { focus: () => void }).focus();
  expect(terminalMock.instances[0].focus).toHaveBeenCalledTimes(1);
  ui.dispose();
});
```
- [ ] **Step 2 — run, expect fail** (`focus` not on TerminalUI / not a function).
- [ ] **Step 3 — implement.** In `terminal.ts`: add `focus(): void;` to the `TerminalUI` interface; add `focus() { term.focus(); },` to the returned object.
- [ ] **Step 4 — run, expect pass.** Also confirm no other suite broke.
- [ ] **Step 5 — commit:** `feat(spa): add TerminalUI.focus()`.

## Task 2: auto-focus the terminal on control-grant

**Files:** `web/app/src/bridge/inbound.ts`, `web/app/src/bridge/inbound.test.ts`

- [ ] **Step 1 — failing test.** In `inbound.test.ts`: extend `StubUI` with `focusCalls: number` and `focus() { ui.focusCalls += 1; }` (init `focusCalls: 0`). Add:
```ts
it("focuses the terminal when control becomes granted", async () => {
  const { factory } = mockFactory();
  installInboundBridge({ ui, factory });
  w.setSession!("sess-1", "wss://r/", "tok", "dev-1");
  const ws = await flushUntilWS();
  ws.triggerOpen(); await flush();
  expect(ui.focusCalls).toBe(0);
  w.requestControl!();
  ws.triggerText(JSON.stringify({ type: "control.granted", request_id: null,
    payload: { session_id: "sess-1", lease_version: 1, expires_at: "2099-01-01T00:00:00Z", controller_device_id: "dev-1" } }));
  expect(ui.focusCalls).toBe(1);
});
```
- [ ] **Step 2 — run, expect fail** (focusCalls stays 0).
- [ ] **Step 3 — implement.** In `inbound.ts`, the `createControl({ onState })` callback (currently `onState: (state, detail) => outbound.onControlState(state, detail)`):
```ts
onState: (state, detail) => {
  if (state === "granted") cfg.ui.focus();
  outbound.onControlState(state, detail);
},
```
- [ ] **Step 4 — run, expect pass.**
- [ ] **Step 5 — commit:** `feat(spa): auto-focus terminal on control grant`.

## Task 3: streamline Toolbar (drop digits) + focus-preserving buttons

**Files:** `web/app/src/components/toolbar.tsx`, `web/app/src/components/toolbar.test.tsx`

- [ ] **Step 1 — update tests (red).** In `toolbar.test.tsx`:
  - Delete `"renders 10 digit buttons"` and `"digit click calls onDigit"`.
  - Drop `onDigit={...}` from every `render(<Toolbar .../>)` call (prop removed).
  - In `"disabled state applies class and disables buttons"`, replace `screen.getByText("3")` with `screen.getByText("Esc")`.
  - Add:
```ts
it("special-key button preventDefaults mousedown to keep terminal focus", () => {
  const onSpecial = vi.fn();
  render(<Toolbar disabled={false} onSpecial={onSpecial} />);
  const ev = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
  screen.getByText("Esc").dispatchEvent(ev);
  expect(ev.defaultPrevented).toBe(true);
});
```
- [ ] **Step 2 — run, expect fail** (onDigit type error / preventDefault not wired).
- [ ] **Step 3 — implement** `toolbar.tsx`:
  - Remove `DIGITS` const and the `<div class="row digits">…</div>` block.
  - Remove `onDigit` from `ToolbarProps` and the destructure.
  - On each special-key `<button>`, add `onMouseDown={e => e.preventDefault()}` (keep existing `onClick={() => onSpecial(k)}`).
- [ ] **Step 4 — run, expect pass** (special-key click tests still pass via onClick; preventDefault test passes).
- [ ] **Step 5 — commit:** `feat(spa): streamline toolbar to special keys, focus-preserving`.

## Task 4: `initialToolbarExpanded()` helper (platform default + stored pref)

**Files:** `web/app/src/pages/terminal.tsx` (export helper), `web/app/src/pages/terminal.test.tsx`

- [ ] **Step 1 — failing test.** In `terminal.test.tsx`, add a describe importing `initialToolbarExpanded` from `./terminal`:
```ts
describe("initialToolbarExpanded", () => {
  const setMM = (matches: boolean) => {
    Object.defineProperty(window, "matchMedia", { configurable: true, writable: true,
      value: (q: string) => ({ matches, media: q, addEventListener() {}, removeEventListener() {} }) });
  };
  beforeEach(() => localStorage.removeItem("termix_toolbar_expanded"));
  it("desktop (hover+fine pointer) defaults collapsed", () => { setMM(true); expect(initialToolbarExpanded()).toBe(false); });
  it("touch (no hover/fine) defaults expanded", () => { setMM(false); expect(initialToolbarExpanded()).toBe(true); });
  it("stored '1' overrides desktop default", () => { setMM(true); localStorage.setItem("termix_toolbar_expanded", "1"); expect(initialToolbarExpanded()).toBe(true); });
  it("stored '0' overrides touch default", () => { setMM(false); localStorage.setItem("termix_toolbar_expanded", "0"); expect(initialToolbarExpanded()).toBe(false); });
});
```
- [ ] **Step 2 — run, expect fail** (helper not exported).
- [ ] **Step 3 — implement** in `terminal.tsx` (module scope):
```ts
const TOOLBAR_PREF_KEY = "termix_toolbar_expanded";
export function initialToolbarExpanded(): boolean {
  try {
    const stored = localStorage.getItem(TOOLBAR_PREF_KEY);
    if (stored === "1") return true;
    if (stored === "0") return false;
  } catch { /* ignore */ }
  // No stored pref → platform default: desktop (hover + fine pointer) collapsed,
  // touch/phone expanded (soft keyboard can't send the special keys).
  const desktop = typeof window !== "undefined" && typeof window.matchMedia === "function"
    && window.matchMedia("(hover: hover) and (pointer: fine)").matches;
  return !desktop;
}
```
- [ ] **Step 4 — run, expect pass.**
- [ ] **Step 5 — commit:** `feat(spa): platform-aware initial toolbar collapse`.

## Task 5: terminal page — remove Composer, add collapsible toolbar + toggle

**Files:** `web/app/src/pages/terminal.tsx`, `web/app/src/pages/terminal.test.tsx`, `web/app/src/i18n/messages.ts`, delete `web/app/src/components/composer.tsx` + `composer.test.tsx`

- [ ] **Step 1 — i18n.** In `messages.ts`, EN block: remove `"terminal.placeholder": "Type and Send...",`; add `"terminal.toolbar.show": "Show keys",` and `"terminal.toolbar.hide": "Hide keys",`. ZH block: remove `"terminal.placeholder": "输入后发送...",`; add `"terminal.toolbar.show": "显示按键",` and `"terminal.toolbar.hide": "收起按键",`.
- [ ] **Step 2 — update page tests (red).** In `terminal.test.tsx`:
  - Add `localStorage.removeItem("termix_toolbar_expanded");` to the top-level `beforeEach`.
  - `"read-only state: composer and toolbar are not in the DOM"` → rename to `"read-only: toolbar not in the DOM"`; assertions: `expect(container.querySelector(".composer")).toBeNull();` (stays null forever) and `expect(container.querySelector(".toolbar")).toBeNull();`.
  - `"granted state: composer and toolbar appear in the DOM"` → rename `"granted (expanded by default in test env): toolbar appears"`; drop the `.composer` assertion; keep `expect(container.querySelector(".toolbar")).toBeTruthy()` after granting.
  - Delete `"toolbar digit click sends sendText…"` and `"composer Send sends text and clears the input"`.
  - Keep `"toolbar special-key click sends sendSpecialKey"` (uses `fireEvent.click("^J")`).
  - Add:
```ts
it("granted: toggle collapses the toolbar and persists the choice", async () => {
  const { container } = render(<TerminalPage sessionId="s1" onBack={() => {}} />);
  await waitFor(() => expect(setSessionSpy).toHaveBeenCalled());
  window.TermixBridge?.onControlState?.("granted");
  await waitFor(() => expect(container.querySelector(".toolbar")).toBeTruthy());
  const toggle = screen.getByRole("button", { name: /hide keys/i });
  fireEvent.click(toggle);
  await waitFor(() => expect(container.querySelector(".toolbar")).toBeNull());
  expect(localStorage.getItem("termix_toolbar_expanded")).toBe("0");
  // toggle remains available to re-expand
  expect(screen.getByRole("button", { name: /show keys/i })).toBeTruthy();
});
```
- [ ] **Step 3 — run, expect fail.**
- [ ] **Step 4 — implement** `terminal.tsx`:
  - Imports: drop `Composer`; keep `Toolbar`, `ComposerDock`. (Keep `useSignal` already imported.)
  - Add signal: `const toolbarExpanded = useSignal<boolean>(initialToolbarExpanded());`
  - Add toggle handler:
```ts
const toggleToolbar = () => {
  const next = !toolbarExpanded.value;
  toolbarExpanded.value = next;
  try { localStorage.setItem(TOOLBAR_PREF_KEY, next ? "1" : "0"); } catch { /* ignore */ }
};
```
  - Remove `const onCompose = …` and `const onDigit = …`. Keep `const onSpecial = (k) => window.sendSpecialKey(k);`.
  - In `.control-bar`, wrap the right-side controls and add the toggle (only when granted), focus-preserving:
```tsx
<div class="ctrl-actions">
  {controlState.value === "granted" && (
    <button
      class="toolbar-toggle"
      aria-label={toolbarExpanded.value ? t("terminal.toolbar.hide") : t("terminal.toolbar.show")}
      onMouseDown={e => e.preventDefault()}
      onClick={toggleToolbar}
    >⌨</button>
  )}
  {controlState.value === "granted"
    ? <button class="release-btn" onClick={() => window.releaseControl()}>{t("terminal.button.release")}</button>
    : <button class="request-btn" onClick={() => window.requestControl()}>{t("terminal.button.request")}</button>}
</div>
```
  - Replace the dock block:
```tsx
<ComposerDock open={controlState.value === "granted" && toolbarExpanded.value}>
  <Toolbar disabled={false} onSpecial={onSpecial} />
</ComposerDock>
```
- [ ] **Step 5 — delete** `web/app/src/components/composer.tsx` and `web/app/src/components/composer.test.tsx` (`git rm`).
- [ ] **Step 6 — run** full `npm test -- --run` + `npm run typecheck`; expect green (fix any stragglers referencing Composer/placeholder/onDigit).
- [ ] **Step 7 — commit:** `feat(spa): collapsible toolbar, remove text composer`.

## Task 6: CSS — remove composer, add toggle + collapsed + landscape

**Files:** `web/app/src/theme/styles.css`

- [ ] **Step 1 — edit.**
  - Delete `.composer { … }`, `.composer textarea { … }`, `.send-btn { … }`, `.send-btn[disabled]` (composer removed).
  - Delete `.toolbar .row.digits { … }` and `.toolbar .row.digits button { … }` (digits removed).
  - Add toggle + actions styles:
```css
.ctrl-actions { display: flex; align-items: center; gap: 8px; }
.toolbar-toggle {
  background: transparent; color: var(--accent);
  border: 1px solid #4a3530; border-radius: 4px;
  padding: 3px 8px; font-size: 13px; line-height: 1;
}
```
  - Update the dock comment to "Toolbar slides in only when control is granted and expanded."
  - Add landscape condense (§4.7), after the `.control-bar` rules:
```css
@media (orientation: landscape) and (max-height: 500px) {
  .term-header { padding: 4px 12px; }
  .term-header .name { font-size: 12px; }
  .term-header .back { font-size: 18px; }
  .control-bar { padding: 3px 12px; font-size: 10px; }
  .release-btn, .request-btn, .toolbar-toggle { padding: 2px 8px; }
}
```
- [ ] **Step 2 — verify** `npm run build` succeeds and `npm test -- --run` still green (CSS isn't unit-tested; this guards the bundle).
- [ ] **Step 3 — commit:** `style(spa): drop composer css, add toolbar toggle + landscape condense`.

## Task 7: final verification + PROGRESS

- [ ] `npm run typecheck` clean; `npm test -- --run` all green; `npm run build` OK.
- [ ] Adversarial review of the full diff (focus timing, collapse persistence, no dead refs to Composer/placeholder/onDigit, mobile focus-preservation).
- [ ] Update `docs/PROGRESS.md`: mark input-2a (expanded → unified input model) + layout-1b done in this slice; note input-2b dropped (composer removed), layout-1a deferred, display-3 still pending evidence.
- [ ] Commit PROGRESS; hand off to finishing-a-development-branch (merge to local main per prior pattern) → release v0.5.1.

## Manual / device verification (user, post-deploy)

- PC: grant control → cursor live, type directly; toolbar collapsed by default, toggle expands.
- Phone: grant → soft keyboard rises, type directly (incl. Chinese IME), char deletable; toolbar expanded by default, taps don't dismiss keyboard; landscape header is shorter.
- Confirm collapse choice persists across reloads.
