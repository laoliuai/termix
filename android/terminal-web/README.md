# `android/terminal-web`

Static-asset bundle that the Termix Android Compose shell loads inside a WebView. It owns the WSS protocol, terminal rendering (xterm.js), and the JS bridge contract used by the native shell.

See `docs/superpowers/specs/2026-04-25-android-terminal-web-mvp-design.md` for the full design.

## Commands

```bash
npm install        # one-time
npm run dev        # opens http://localhost:5173/dev.html with hot reload
npm test           # vitest run (unit tests)
npm run build      # produces dist/ for the Compose shell to copy in
npm run typecheck  # tsc --noEmit
```

## Manual smoke checklist (spec §6c)

This needs three concurrent shells for the Go stack plus a fourth for this dev server. All commands assume repo root unless noted.

Prerequisites: a Postgres reachable at the DSN below, with migrations applied. If `termix-control` errors on missing tables, run migrations first: `migrate -database "$TERMIX_POSTGRES_DSN" -path db/migrations up`.

1. **Shell 1 — `termix-control`** (Postgres + JWT signing + relay-auth gRPC server):

   ```bash
   export TERMIX_POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:55432/termix?sslmode=disable"
   export TERMIX_JWT_SIGNING_KEY="dev-smoke-secret"
   export TERMIX_CONTROL_RELAY_GRPC_ADDR=":9090"
   cd go && go run ./cmd/termix-control
   ```

   Listens on `:8080` (REST) and `:9090` (gRPC for relay).

2. **Shell 2 — `termix-relay`** (WSS gateway, authorizes via control's gRPC):

   ```bash
   export TERMIX_RELAY_CONTROL_GRPC_ADDR="127.0.0.1:9090"
   cd go && go run ./cmd/termix-relay
   ```

   Listens on `:8090`.

3. **Shell 3 — log in once** (writes `~/.config/termix/host.json` + credentials, which `termixd` reads on startup):

   ```bash
   cd go && go run ./cmd/termix login        # point at http://localhost:8080
   ```

4. **Shell 3 (continued) — `termixd`** (host daemon; needs `host.json` from step 3):

   ```bash
   cd go && go run ./cmd/termixd
   ```

5. **Shell 4 — start a session and run the dev server**:

   ```bash
   cd go && go run ./cmd/termix start claude --name "smoke"
   go run ./cmd/termix sessions list
   ```

   Note the `session_id`, relay URL (`ws://localhost:8090/ws`), access token (from `~/.config/termix/credentials.json` or daemon log), and your device_id.

   Then start the dev server:

   ```bash
   cd android/terminal-web
   npm run dev
   ```

6. In the browser tab that opens (`dev.html`), paste session_id, relay URL, access token, and device_id. Click **Connect**. Expected:
   - Status panel: `connection: connected`.
   - Snapshot of the existing terminal renders.

7. Click **Request Control**. Expected: `control: granted` within ~1 s.

8. Type a command (e.g. `echo hi`) into the **Send Text** box, click **Enter**. Expected: command echoes in the terminal output stream.

9. Click **Release Control**. Expected: `control: none`. Subsequent **Send Text** clicks produce no output (input gate).
