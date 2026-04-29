# Web UI Remaining Issues Review - 2026-04-28

Scope: `web-ui` worktree, branch `web-ui`, reviewed at commit `c263fc9`
(`Web UI post-smoke fixes: PC layout, terminal page wiring, phone fit`).

This note records the current known problems found during the post-smoke code
review. It is intended as the handoff list before merging the Web UI branch.

## Verification Snapshot

Passing:

- `npm test -- --run` in `web/app`: 130 tests passed.
- `npm run build` in `web/app`: production Vite build passed.
- `go test ./...` in `go`: passed.
- `git diff --check main..HEAD`: passed.

Failing:

- `npm run typecheck` in `web/app`: failed.
- `go vet ./...` in `go`: failed.

## High

### 1. Web UI TypeScript typecheck fails

Command:

```bash
cd web/app && npm run typecheck
```

Observed failures:

- `web/app/src/pages/terminal.tsx` and
  `web/app/src/pages/terminal.test.tsx` both augment `Window`, but the
  declarations disagree on optionality and function signatures.
- `web/app/src/routes/Router.tsx` passes `path` to route components whose props
  do not declare it.
- `web/app/src/api/client.test.ts` and `web/app/src/api/endpoints.test.ts`
  read `fetchSpy.mock.calls[0][1]` from a mock inferred as a zero-arg function,
  so TypeScript sees no tuple element at index 1.
- `web/app/src/hooks/useViewport.test.tsx` imports unused `vi`.
- `web/app/src/pages/terminal.tsx` imports unused `accessToken`.

Impact: test and build pass, but any CI or release gate that runs
`tsc --noEmit` will fail.

Suggested fix:

- Move shared browser globals into a single `.d.ts` or shared test helper.
- Add a small route-prop wrapper type for `preact-router`, or cast route
  components at the routing boundary.
- Type fetch mocks with arguments, or read mock calls through `unknown` /
  explicit helper functions.
- Remove unused imports.

### 2. Embedded SPA bundle is not reproducible from a clean checkout

Current tracked files under `go/internal/controlapi/web_dist`:

```text
.gitignore
index.html
```

`go/internal/controlapi/web_dist/.gitignore` ignores all generated assets except
`index.html`. The tracked `index.html` references hashed files such as
`/assets/index-CMr6iHCw.js`, but those assets are ignored and are not tracked.

After a fresh `npm run build`, the produced hashes are different
(`index-CiCGBCVv.js`, `index-gEpcdA3Z.css` during this review). That means:

- A clean checkout can embed an `index.html` that points at missing assets.
- `make build-go` can produce a `termix-control` binary from stale local ignored
  assets if `make build-web` was not run first.

Suggested fix:

- Make `build-go` depend on `build-web`, or make `build` the only release build
  target.
- Either track the full `web_dist` release artifact, or do not track hashed
  `index.html` as if it were stable source.
- Add a release check that `go/internal/controlapi/web_dist/index.html`
  references files that exist in the embedded directory.

## Medium

### 3. `go vet ./...` fails in `auth_refresh_test.go`

Command:

```bash
cd go && go vet ./...
```

Failure:

```text
tests/auth_refresh_test.go:113:8: using res before checking for errors
```

Location:

- `go/tests/auth_refresh_test.go`: `http.Post` error is ignored, then
  `defer res.Body.Close()` runs unconditionally.

Impact: `go test` passes, but a normal static-analysis gate fails. If the test
server call ever errors, this path can panic with a nil response.

Suggested fix: check the `http.Post` error before deferring `res.Body.Close()`.

### 4. Logout clears the browser cookie even when DB revoke fails

Location:

- `go/internal/controlapi/auth_logout.go`

`PostAuthLogout` intentionally ignores `RevokeRefreshToken` errors and still
returns 204 when a cookie was present. If the database write fails, the browser
cookie is cleared but the server-side refresh token can remain active.

Impact: the user sees a successful logout while the refresh token may still be
valid if it was copied or replayed elsewhere.

Suggested fix: always clear the cookie, but log and return 500/503 when token
revocation fails.

### 5. Reaper treats every `tmux has-session` failure as "session is dead"

Locations:

- `go/internal/tmux/runner.go`: `HasSession(ctx, name) bool`
- `go/internal/session/manager.go`: `Manager.Reap`

`HasSession` returns only `bool`, so command execution errors, missing `tmux`,
or transient failures are indistinguishable from "session not found". `Reap`
then PATCHes the session to `exited` and deletes local state.

Impact: transient host/tmux failures can incorrectly mark a live session exited.

Suggested fix: change `HasSession` to return `(bool, error)`. Reap only when the
command clearly reports a missing session; log and retry later on generic
execution errors.

## Low

### 6. Progress ledger has stale and duplicated items

Locations:

- `docs/PROGRESS.md`

Known cleanup:

- The Web UI smoke-fix note references `make web`, but the Makefile target is
  `make build-web`.
- The deferred Android Compose shell entry appears twice.
- The `Next Up` section still says to brainstorm, plan, and implement the Web
  UI even though those steps are already complete on `web-ui`.

Impact: project status is confusing during handoff.

Suggested fix: update `Next Up` to the current merge-readiness work, remove the
duplicate deferred entry, and correct the Makefile target name.

