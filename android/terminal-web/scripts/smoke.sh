#!/usr/bin/env bash
#
# End-to-end orchestrator for the terminal-web slice 1 smoke test.
#
# Boots the full Go stack against a local Postgres test container, seeds a
# smoke user, logs in, starts a tmux-backed session, and prints the four
# values you need to paste into dev.html. Background services stream logs
# to .smoke/logs/ and are torn down on Ctrl+C.
#
# Usage:
#   android/terminal-web/scripts/smoke.sh [--cleanup]
#
#   --cleanup      Kill any running termix-* and termixd processes and exit.
#
# Env overrides (all optional):
#   PG_CONTAINER, PG_DSN, JWT_KEY,
#   CONTROL_REST_ADDR, CONTROL_GRPC_ADDR,
#   RELAY_LISTEN_ADDR, RELAY_TO_CONTROL_GRPC,
#   SMOKE_EMAIL, SMOKE_PASSWORD, SMOKE_SESSION_NAME, SMOKE_TOOL.

set -euo pipefail

# --- Paths --------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
SMOKE_DIR="${REPO_ROOT}/.smoke"
LOG_DIR="${SMOKE_DIR}/logs"
mkdir -p "${LOG_DIR}"

# --- Configuration ------------------------------------------------------------
PG_CONTAINER="${PG_CONTAINER:-termix-test-pg}"
PG_DSN="${PG_DSN:-postgres://postgres:test@127.0.0.1:55432/termix_test?sslmode=disable}"

JWT_KEY="${JWT_KEY:-dev-smoke-secret}"
CONTROL_REST_ADDR="${CONTROL_REST_ADDR:-:8080}"
CONTROL_GRPC_ADDR="${CONTROL_GRPC_ADDR:-:9090}"
RELAY_LISTEN_ADDR="${RELAY_LISTEN_ADDR:-:8090}"
RELAY_TO_CONTROL_GRPC="${RELAY_TO_CONTROL_GRPC:-127.0.0.1:9090}"

SMOKE_EMAIL="${SMOKE_EMAIL:-smoke@test.local}"
SMOKE_PASSWORD="${SMOKE_PASSWORD:-smoke-pass}"
SMOKE_SESSION_NAME="${SMOKE_SESSION_NAME:-smoke}"
SMOKE_TOOL="${SMOKE_TOOL:-claude}"

PIDS=()

# --- Helpers ------------------------------------------------------------------
say() { printf '\033[1;36m→\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; }

cleanup() {
  local code=$?
  echo
  say "Tearing down background processes..."
  for pid in "${PIDS[@]:-}"; do
    [[ -n "${pid}" ]] && kill "${pid}" 2>/dev/null || true
  done
  # Best-effort: kill any orphaned go-run children
  pkill -P $$ 2>/dev/null || true
  wait 2>/dev/null || true
  exit "${code}"
}
trap cleanup EXIT INT TERM

port_of() {
  # Strip leading ":" from an addr like ":8080" → "8080"
  echo "${1#:}"
}

is_listening() {
  local port="$1"
  ss -tnl "sport = :${port}" 2>/dev/null | grep -q LISTEN
}

wait_listening() {
  local addr="$1" name="$2" timeout="${3:-30}"
  local port
  port="$(port_of "${addr}")"
  say "Waiting for ${name} on :${port} (timeout ${timeout}s)..."
  for _ in $(seq 1 "${timeout}"); do
    if is_listening "${port}"; then
      ok "${name} is listening."
      return 0
    fi
    sleep 1
  done
  err "${name} did not start listening on :${port} within ${timeout}s. Recent log:"
  tail -n 30 "${LOG_DIR}/${name}.log" >&2 || true
  return 1
}

kill_termix_processes() {
  pkill -f 'go-build.*/termix-control' 2>/dev/null || true
  pkill -f 'go-build.*/termix-relay'   2>/dev/null || true
  pkill -f 'go-build.*/termixd'        2>/dev/null || true
  pkill -f 'cmd/termix-control'        2>/dev/null || true
  pkill -f 'cmd/termix-relay'          2>/dev/null || true
  pkill -f 'cmd/termixd\b'             2>/dev/null || true
  sleep 1
}

# --- Subcommand: --cleanup ----------------------------------------------------
if [[ "${1:-}" == "--cleanup" ]]; then
  trap - EXIT INT TERM
  say "Killing any running termix services..."
  kill_termix_processes
  ok "Done. (tmux sessions left intact; use 'tmux kill-server' if needed.)"
  exit 0
fi

# --- Step 1: Postgres container -----------------------------------------------
say "Checking Postgres container '${PG_CONTAINER}'..."
if ! docker ps --filter "name=${PG_CONTAINER}" --format '{{.Names}}' | grep -q "^${PG_CONTAINER}$"; then
  err "Postgres container '${PG_CONTAINER}' is not running."
  echo "  Start it with:"
  echo "    docker run -d --name ${PG_CONTAINER} -e POSTGRES_PASSWORD=test \\"
  echo "      -e POSTGRES_DB=termix_test -p 55432:5432 postgres:16-alpine"
  exit 1
fi
ok "Postgres container is running."

# --- Step 2: migrations applied ----------------------------------------------
TABLE_COUNT=$(docker exec "${PG_CONTAINER}" psql -U postgres -d termix_test -t -A -c \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" | tr -d ' ')
if [[ "${TABLE_COUNT}" -lt 5 ]]; then
  err "termix_test DB has only ${TABLE_COUNT} tables; migrations are not applied."
  echo "  Run from repo root:"
  echo "    migrate -database \"${PG_DSN}\" -path db/migrations up"
  exit 1
fi
ok "Postgres has migrations (${TABLE_COUNT} tables)."

# --- Step 3: ports free -------------------------------------------------------
for addr in "${CONTROL_REST_ADDR}" "${CONTROL_GRPC_ADDR}" "${RELAY_LISTEN_ADDR}"; do
  port="$(port_of "${addr}")"
  if is_listening "${port}"; then
    err "Port :${port} is already in use. Run with --cleanup first, or kill the process."
    exit 1
  fi
done
ok "Required ports are free."

# --- Step 4: seed smoke user (idempotent) -------------------------------------
say "Seeding smoke user '${SMOKE_EMAIL}'..."
SEED_DIR="${REPO_ROOT}/go/_smoke_seed_helper"
mkdir -p "${SEED_DIR}"
cat > "${SEED_DIR}/main.go" <<'GO'
package main

import (
	"fmt"
	"os"

	"github.com/termix/termix/go/internal/auth"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: smoke-seed <password>")
		os.Exit(2)
	}
	h, err := auth.HashPassword(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(h)
}
GO
HASH=$(cd "${REPO_ROOT}/go" && go run ./_smoke_seed_helper "${SMOKE_PASSWORD}")
rm -rf "${SEED_DIR}"

docker exec -i "${PG_CONTAINER}" psql -U postgres -d termix_test >/dev/null <<SQL
INSERT INTO users (email, display_name, password_hash, role, status)
VALUES ('${SMOKE_EMAIL}', 'Smoke User', '${HASH}', 'user', 'active')
ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash, status = 'active';
SQL
ok "Smoke user is ready."

# --- Step 5: termix-control ---------------------------------------------------
say "Starting termix-control..."
(
  cd "${REPO_ROOT}/go"
  TERMIX_POSTGRES_DSN="${PG_DSN}" \
  TERMIX_JWT_SIGNING_KEY="${JWT_KEY}" \
  TERMIX_CONTROL_RELAY_GRPC_ADDR="${CONTROL_GRPC_ADDR}" \
  TERMIX_CONTROL_REST_ADDR="${CONTROL_REST_ADDR}" \
  exec go run ./cmd/termix-control
) > "${LOG_DIR}/control.log" 2>&1 &
PIDS+=("$!")
wait_listening "${CONTROL_REST_ADDR}" "control" 60

# --- Step 6: termix-relay -----------------------------------------------------
say "Starting termix-relay..."
(
  cd "${REPO_ROOT}/go"
  TERMIX_RELAY_CONTROL_GRPC_ADDR="${RELAY_TO_CONTROL_GRPC}" \
  TERMIX_RELAY_LISTEN_ADDR="${RELAY_LISTEN_ADDR}" \
  exec go run ./cmd/termix-relay
) > "${LOG_DIR}/relay.log" 2>&1 &
PIDS+=("$!")
wait_listening "${RELAY_LISTEN_ADDR}" "relay" 30

# --- Step 7: login (writes ~/.config/termix/{host.json,credentials.json}) -----
say "Logging in as '${SMOKE_EMAIL}'..."
(
  cd "${REPO_ROOT}/go"
  printf 'http://localhost%s/api/v1\n%s\n%s\n' \
    "${CONTROL_REST_ADDR}" "${SMOKE_EMAIL}" "${SMOKE_PASSWORD}" \
    | go run ./cmd/termix login
) > "${LOG_DIR}/login.log" 2>&1
ok "Logged in. Credentials at ~/.config/termix/credentials.json."

# --- Step 8: patch host.json relay URL ---------------------------------------
HOST_CFG="${HOME}/.config/termix/host.json"
sed -i "s|\"relay_ws_url\": \"ws://localhost${CONTROL_REST_ADDR}/ws\"|\"relay_ws_url\": \"ws://localhost${RELAY_LISTEN_ADDR}/ws\"|" "${HOST_CFG}"
ok "host.json relay_ws_url patched to ws://localhost${RELAY_LISTEN_ADDR}/ws."

# --- Step 9: termixd ----------------------------------------------------------
say "Starting termixd..."
(
  cd "${REPO_ROOT}/go"
  exec go run ./cmd/termixd
) > "${LOG_DIR}/termixd.log" 2>&1 &
PIDS+=("$!")
# termixd binds a UDS socket (no TCP port). Path comes from
# config.DefaultHostPaths: $XDG_RUNTIME_DIR/termix/daemon.sock if set,
# otherwise $HOME/.termix/run/daemon.sock.
if [[ -n "${XDG_RUNTIME_DIR:-}" ]]; then
  SOCK="${XDG_RUNTIME_DIR}/termix/daemon.sock"
else
  SOCK="${HOME}/.termix/run/daemon.sock"
fi
say "Waiting for termixd socket at ${SOCK}..."
for _ in $(seq 1 30); do
  if [[ -S "${SOCK}" ]]; then
    ok "termixd socket is up."
    break
  fi
  sleep 1
done
if [[ ! -S "${SOCK}" ]]; then
  err "termixd socket not found at ${SOCK} after 30s. Recent log:"
  tail -n 30 "${LOG_DIR}/termixd.log" >&2
  exit 1
fi

# --- Step 10: termix start ----------------------------------------------------
say "Starting tmux-backed session '${SMOKE_SESSION_NAME}' running '${SMOKE_TOOL}'..."
# 'termix start' wants to attach a TTY by default. We don't want it to take
# over this terminal — redirect stdin to /dev/null so it just creates the
# session and exits with an attach-failure (the session is still running).
(
  cd "${REPO_ROOT}/go"
  go run ./cmd/termix start "${SMOKE_TOOL}" --name "${SMOKE_SESSION_NAME}" </dev/null
) > "${LOG_DIR}/start.log" 2>&1 || true
ok "Session create command finished (attach is expected to fail without a TTY)."

# --- Step 11: discover session_id from DB ------------------------------------
DEVICE_ID=$(jq -r .device_id ~/.config/termix/credentials.json)
SESSION_ID=$(docker exec "${PG_CONTAINER}" psql -U postgres -d termix_test -t -A -c \
  "SELECT id FROM sessions WHERE name='${SMOKE_SESSION_NAME}' AND host_device_id='${DEVICE_ID}' AND status='running' ORDER BY created_at DESC LIMIT 1;" \
  | tr -d ' ')
if [[ -z "${SESSION_ID}" ]]; then
  err "No running session named '${SMOKE_SESSION_NAME}' for device ${DEVICE_ID}. Recent start.log:"
  tail -n 30 "${LOG_DIR}/start.log" >&2
  exit 1
fi
RELAY_URL=$(jq -r .relay_ws_url ~/.config/termix/host.json)
ACCESS_TOKEN=$(jq -r .access_token ~/.config/termix/credentials.json)

# --- Step 12: print the connection blob & wait --------------------------------
cat <<EOF

\033[1;32m===============================================================
✓ Smoke stack is up. Paste these four values into dev.html:
===============================================================\033[0m
  Session ID:   ${SESSION_ID}
  Relay URL:    ${RELAY_URL}
  Access Token: ${ACCESS_TOKEN}
  Device ID:    ${DEVICE_ID}

In another shell:
  cd android/terminal-web && npm run dev

Logs:
  ${LOG_DIR}/control.log
  ${LOG_DIR}/relay.log
  ${LOG_DIR}/termixd.log
  ${LOG_DIR}/login.log
  ${LOG_DIR}/start.log

Press Ctrl+C here to tear down the entire stack.
EOF

# Echo with -e so the color codes render.
echo -e ""

# Tail logs so the user can see what's happening live.
tail -F \
  "${LOG_DIR}/control.log" \
  "${LOG_DIR}/relay.log" \
  "${LOG_DIR}/termixd.log"
