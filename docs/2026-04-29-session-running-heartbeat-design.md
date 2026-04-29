# Session Running Heartbeat Design

Date: 2026-04-29

## Decision

`GET /sessions?status=running` means the session is currently accessible:

- the control-plane row is in `running`
- the host daemon has recently confirmed the tmux session still exists
- the heartbeat is fresh enough that the host is considered reachable

A stale heartbeat must not be returned as `running`. It may be presented as
`disconnected` until the host daemon returns and confirms tmux state again.

## Approach

Add a session-level heartbeat from `termixd` to `termix-control`.

The daemon already walks local session state and checks `tmux has-session` in
the reaper. Extend that loop so live tmux sessions send
`POST /host/sessions/{session_id}/heartbeat`. Dead tmux sessions continue to be
patched to `exited`.

The control plane stores `sessions.last_seen_at`, refreshed only by session
heartbeat. Session listing computes an effective status:

- active status + fresh `last_seen_at` keeps the stored status
- active status + stale `last_seen_at` is exposed as `disconnected`
- `status=running` only returns effective `running`

This avoids marking a network partition or crashed daemon as `exited`; only the
host-side tmux check can prove exit.

## Freshness

The first implementation uses a 90 second freshness window. The daemon reaper
runs every 30 seconds today, so 90 seconds tolerates one missed sweep while
still removing unreachable sessions from the running list quickly.

## Out Of Scope

- Replacing the reaper with synchronous tmux checks from `GET /sessions`
- Relay presence/audit tables
- Dynamic recovery/re-announcement of all existing sessions after daemon restart
