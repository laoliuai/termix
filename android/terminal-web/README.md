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

1. From the repo root, start the Go stack against the running Postgres test container:

   ```bash
   export TERMIX_TEST_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:55432/termix?sslmode=disable"
   cd go
   go run ./cmd/termix-control &
   go run ./cmd/termix-relay &
   go run ./cmd/termixd &
   ```

2. Log in and start a session via the CLI:

   ```bash
   ./bin/termix login
   ./bin/termix start claude --name "smoke"
   ./bin/termix sessions list
   ```

   Note the `session_id`, the relay URL, and your access token (from `~/.config/termix/credentials.json` or the daemon log).

3. In another shell, run the dev harness:

   ```bash
   cd android/terminal-web
   npm run dev
   ```

4. In the browser tab that opens (`dev.html`), paste session_id, relay URL, access token, and device_id. Click **Connect**. Expected:
   - Status panel: `connection: connected`.
   - Snapshot of the existing terminal renders.

5. Click **Request Control**. Expected: `control: granted` within ~1 s.

6. Type a command (e.g. `echo hi`) into the **Send Text** box, click **Enter**. Expected: command echoes in the terminal output stream.

7. Click **Release Control**. Expected: `control: none`. Subsequent **Send Text** clicks produce no output (input gate).
