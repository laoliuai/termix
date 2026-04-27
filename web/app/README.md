# `web/app`

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

### Quick path: one script

`scripts/smoke.sh` orchestrates everything: Postgres preflight, smoke-user seeding, control + relay + termixd startup, login, host.json patch, `termix start`, and prints the four values you need for `dev.html`. Background services stream logs to `.smoke/logs/`.

```bash
# From repo root, in one shell:
web/app/scripts/smoke.sh

# In another shell (after the script prints the connection blob):
cd web/app && npm run dev
# Paste the four values into dev.html, click Connect.

# When done, Ctrl+C the smoke script. To force-kill any leftover services:
web/app/scripts/smoke.sh --cleanup
```

Override defaults via env vars: `PG_CONTAINER`, `PG_DSN`, `JWT_KEY`, `CONTROL_REST_ADDR`, `CONTROL_GRPC_ADDR`, `RELAY_LISTEN_ADDR`, `RELAY_TO_CONTROL_GRPC`, `SMOKE_EMAIL`, `SMOKE_PASSWORD`, `SMOKE_SESSION_NAME`, `SMOKE_TOOL`.

### Manual path (only if the script doesn't fit your setup)

Prerequisites:

- A Postgres reachable at the DSN below with migrations applied. If using the existing `termix-test-pg` test container (`docker run -e POSTGRES_PASSWORD=test -e POSTGRES_DB=termix_test -p 55432:5432 postgres:16-alpine`), the credentials below already match. If `termix-control` errors on missing tables, run migrations first: `migrate -database "$TERMIX_POSTGRES_DSN" -path db/migrations up`.
- A user row seeded in the `users` table. Since V1 has no self-registration, insert one directly: generate an argon2id hash of your chosen password (`go run` a small helper that calls `auth.HashPassword`, since the package is internal to the module), then `INSERT INTO users (email, display_name, password_hash, role, status) VALUES (...)`.

1. **Shell 1 — `termix-control`** (Postgres + JWT signing + relay-auth gRPC server):

   ```bash
   export TERMIX_POSTGRES_DSN="postgres://postgres:test@127.0.0.1:55432/termix_test?sslmode=disable"
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
   cd go && go run ./cmd/termix login
   ```

   When prompted for the server URL, enter **`http://localhost:8080/api/v1`** (the `/api/v1` suffix is required — `controlapi.NewRouter` mounts all routes under that base).

4. **Shell 3 (continued) — patch `host.json` then start `termixd`**:

   `termix login` writes `relay_ws_url` by reusing the control server's host:port (`go/internal/config/store.go` `DeriveHostConfig`). That's correct for production where control and relay share an origin behind a load balancer, but in local dev they're on separate ports, so `relay_ws_url` ends up pointing at control's port and `termixd` 404s on the WS dial. Patch it before starting the daemon:

   ```bash
   sed -i 's|ws://localhost:8080/ws|ws://localhost:8090/ws|' ~/.config/termix/host.json
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
   cd web/app
   npm run dev
   ```

6. In the browser tab that opens (`dev.html`), paste session_id, relay URL, access token, and device_id. Click **Connect**. Expected:
   - Status panel: `connection: connected`.
   - Snapshot of the existing terminal renders.

7. Click **Request Control**. Expected: `control: granted` within ~1 s.

8. Type a command (e.g. `echo hi`) into the **Send Text** box, click **Enter**. Expected: command echoes in the terminal output stream.

9. Click **Release Control**. Expected: `control: none`. Subsequent **Send Text** clicks produce no output (input gate).
