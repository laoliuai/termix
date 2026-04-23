# Termix Phase 2 Control Lease and Remote Input Design

Status: written for review
Date: 2026-04-23
Source of truth: `docs/termix-v1-detailed-technical-spec.md`

## Purpose
This document defines the next Phase 2 slice after the relay/watch foundation. It adds the backend protocol loop for remote control without implementing Android UI.

The target result is:

- a viewer can request a control lease over WSS
- `termix-relay` delegates the lease decision to `termix-control`
- `termix-control` persists and enforces single-controller lease rules
- only the active controller connection can send terminal input frames
- `termixd` receives authorized input frames from relay and injects them into tmux

This is a design-only artifact. It does not authorize implementation by itself.

## Repository Context
As of 2026-04-23, the repository already implements:

- host login and relay-capable host config persistence
- `termix-control` auth, session create/update, and session detail reads
- `termixd` local session orchestration through tmux
- `termix-relay` WSS watch handshake, snapshot request/forwarding, and live-output fanout
- shared relay envelope and binary frame codecs for output and snapshot chunks

The current relay implementation is intentionally light: it tracks daemon peers and watcher peers in memory, while session metadata and authorization remain in `termix-control`.

## Scope
This design includes:

- `control_leases` persistence in PostgreSQL
- reusable control-plane lease service rules
- REST endpoints as the first transport adapter for relay-to-control lease calls
- relay-side controller state and lease-aware input forwarding
- daemon-side input frame handling
- tmux input injection helpers
- Go tests that simulate viewers/controllers and daemons

This design explicitly excludes:

- Android UI
- Web terminal UI
- Android login/device onboarding
- internal relay-control gRPC service
- multi-relay coordination beyond the shared database lease
- full keyboard, IME, mouse, or paste-mode protocol design
- collaborative remote control

## Approach
Use **Postgres-persisted control leases with Control REST endpoints**.

The database remains the durable source of lease truth. `termix-control` owns the authorization and lease rules. `termix-relay` calls `termix-control` over REST for this slice because the existing watch authorization already uses the generated control REST client.

This is not the final internal-service shape. The implementation must keep lease rules in a reusable service layer so a future relay-control gRPC service can call the same methods without rewriting policy.

## Approaches Considered

### Approach A: Relay in-memory lease
Relay would store `session_id -> controller` in memory and allow input only from that connection.

Pros:

- fastest way to route input
- minimal database and control-plane changes

Cons:

- relay restart loses lease state
- multiple relay instances cannot enforce single-controller semantics
- policy would move into relay and later need to be migrated back

### Approach B: Postgres lease with Control REST
Relay calls generated `termix-control` REST endpoints for acquire, renew, and release. `termix-control` persists leases and applies policy.

Pros:

- fits the current codebase and Phase 2 watch authorization path
- keeps policy in the control plane
- gives correct single-controller behavior across relay restarts and instances
- small enough for this backend-only slice

Cons:

- uses public REST infrastructure for an internal relay-to-control operation
- later gRPC support will require another transport adapter

### Approach C: Postgres lease with internal gRPC now
Relay calls a new internal `RelayControlService` with `AcquireControlLease`, `RenewControlLease`, and `ReleaseControlLease`.

Pros:

- matches the long-term V1 service boundary
- clearer internal API separation

Cons:

- adds proto, generated code, gRPC server/client wiring, service auth, and deployment config in the same slice
- expands the task beyond the remote input backend loop

## Recommendation
Use **Approach B** now and preserve Approach C as the future transport.

The lease service must sit below REST handlers. REST handlers parse bearer claims and map HTTP requests/responses, but they must not own lease policy. A future gRPC handler should be able to call the same service methods.

## Architecture

```text
viewer/controller
  -> WSS control.acquire
termix-relay
  -> REST control lease request
termix-control
  -> reusable lease service
  -> PostgreSQL control_leases
termix-relay
  -> WSS control.granted or control.denied

viewer/controller
  -> binary terminal input frame
termix-relay
  -> verify peer holds current local grant
  -> forward input frame to daemon peer
termixd
  -> decode input frame
  -> tmux input injection
```

Responsibilities:

- `termix-control`: owns user/session authorization, lease decisions, lease persistence, and expiry semantics.
- `termix-relay`: owns WSS connections, peer routing, and local enforcement that only the current controller peer can send input.
- `termixd`: owns tmux injection and does not inspect user authorization.
- `tmux`: remains the only terminal substrate.

## Lease Persistence
Add a `control_leases` table:

```sql
create table control_leases (
  session_id uuid primary key references sessions(id) on delete cascade,
  controller_device_id uuid not null references devices(id),
  lease_version bigint not null,
  granted_at timestamptz not null default now(),
  expires_at timestamptz not null
);
```

Rules:

- at most one active lease per session
- expired leases do not block new acquire attempts
- renewing or releasing requires the same controller device
- renew and release require the caller's current `lease_version`
- each successful acquire or renew increments `lease_version`
- relay must treat an expired local grant as invalid even before asking control again

The first TTL should be short enough to recover stale controllers quickly. Use 30 seconds initially, with `renew_after_seconds` set to half the TTL.

## Reusable Lease Service
Add a control-plane lease service below transport handlers. A future gRPC adapter must reuse this service.

Suggested API:

```go
type ControlActor struct {
	UserID   string
	DeviceID string
}

type ControlLeaseService interface {
	Acquire(ctx context.Context, actor ControlActor, sessionID string) (ControlLease, error)
	Renew(ctx context.Context, actor ControlActor, sessionID string, leaseVersion int64) (ControlLease, error)
	Release(ctx context.Context, actor ControlActor, sessionID string, leaseVersion int64) error
	GetActive(ctx context.Context, sessionID string) (ControlLease, bool, error)
}
```

The service owns these checks:

- actor claims are present and valid
- session exists and belongs to the actor's user
- session status is controllable; first implementation allows `running` and `idle`
- actor device belongs to the same user
- active unexpired lease held by another device denies acquire
- expired lease can be replaced
- stale renew/release attempts are denied

Persistence should expose narrow primitives or transaction helpers. It should not duplicate policy decisions that belong to the service.

## Control REST API
Add REST endpoints under the existing bearer-protected control API:

- `POST /api/v1/sessions/{session_id}/control/acquire`
- `POST /api/v1/sessions/{session_id}/control/renew`
- `POST /api/v1/sessions/{session_id}/control/release`

Request bodies:

```json
{}
```

```json
{ "lease_version": 3 }
```

Response body for acquire/renew:

```json
{
  "session_id": "uuid",
  "controller_device_id": "uuid",
  "lease_version": 3,
  "granted_at": "2026-04-23T12:34:00Z",
  "expires_at": "2026-04-23T12:34:30Z",
  "renew_after_seconds": 15
}
```

Response body for release:

```json
{
  "session_id": "uuid",
  "lease_version": 3,
  "released": true
}
```

HTTP mapping:

- `200`: granted, renewed, or released
- `401`: missing or invalid bearer claims
- `403`: user/device is not allowed to control this session
- `404`: session not found for this user
- `409`: lease is held by another active controller, lease version is stale, or session is not controllable

## Relay Protocol
Extend the existing JSON envelope message set:

Viewer/controller to relay:

- `control.acquire`
- `control.renew`
- `control.release`

Relay to viewer/controller:

- `control.granted`
- `control.denied`
- `control.revoked`

`control.acquire`:

```json
{
  "type": "control.acquire",
  "request_id": "uuid",
  "payload": { "session_id": "uuid" }
}
```

`control.granted`:

```json
{
  "type": "control.granted",
  "request_id": "same uuid",
  "payload": {
    "session_id": "uuid",
    "lease_version": 3,
    "expires_at": "2026-04-23T12:34:30Z",
    "renew_after_seconds": 15
  }
}
```

`control.denied`:

```json
{
  "type": "control.denied",
  "request_id": "same uuid",
  "payload": {
    "session_id": "uuid",
    "reason": "already_controlled",
    "message": "control lease is held by another device"
  }
}
```

Stable denial reasons:

- `unauthorized`
- `not_found`
- `session_not_controllable`
- `already_controlled`
- `stale_lease`
- `relay_no_daemon`
- `invalid_request`

`control.revoked` is used when relay tells a controller it no longer holds control. A successful voluntary release sends:

```json
{
  "type": "control.revoked",
  "request_id": "same uuid",
  "payload": {
    "session_id": "uuid",
    "lease_version": 3,
    "reason": "released"
  }
}
```

## Relay Runtime Rules
Relay keeps local controller state per peer after a successful acquire or renew:

```text
peer -> { session_id, lease_version, expires_at }
session_id -> controller_peer
```

Input forwarding requires all of the following:

- sender peer has already joined the session with `session.watch`
- frame type is terminal input
- frame header contains a `session_id`
- sender peer is the current controller for that session
- local grant is not expired according to relay's clock
- frame lease version, if present, matches the current grant
- daemon peer for the session is connected

Relay must not forward terminal input from watchers that have not acquired control.

On `control.release`, relay calls control, clears local controller state on success, sends `control.revoked` with `reason="released"`, and rejects later input from that peer.

On disconnect, relay should clear local controller state. It must not synchronously release the persisted lease in the first implementation; the short TTL is the recovery mechanism for stale controller connections.

## Binary Input Frames
Add binary frame type `2`: terminal input.

Header:

```json
{
  "session_id": "uuid",
  "seq": 555,
  "encoding": "raw",
  "lease_version": 3
}
```

Payload:

- raw bytes to inject into the tmux pane

The initial implementation should accept `encoding="raw"` only. Unknown encodings are rejected or dropped with an error envelope to the sender.

## Daemon Input Handling
`termixd` receives terminal input frames only from relay. It should decode the frame, load the local session by `session_id`, and call an input injection callback with the local tmux session name.

Suggested daemon-side boundary:

```go
type InputFunc func(ctx context.Context, tmuxSessionName string, payload []byte) error
```

The relay client should expose:

```go
SetInputHandler(func(context.Context, string, []byte) error)
```

where the `string` is the Termix session ID. The session manager can translate Termix session ID to tmux session name before calling `InputFunc`.

## tmux Input Injection
Add a tmux helper that turns raw input bytes into conservative `tmux send-keys` invocations.

Initial behavior:

- printable UTF-8 text uses `tmux send-keys -l -- <text>`
- `\r` and `\n` map to `Enter`
- `\t` maps to `Tab`
- `0x03` maps to `C-c`
- `0x1b` maps to `Escape`

The first implementation does not need a full keyboard protocol. It should keep the byte-to-tmux mapping isolated so future special-key, IME, mouse, and paste-mode support can extend it without changing relay routing.

## Error Handling
Control-plane service errors should be typed so REST and future gRPC adapters can map them without string matching.

Relay behavior:

- invalid acquire/renew/release payload: send `control.denied` with `invalid_request`
- control API denial: send `control.denied` with the mapped reason
- input without active lease: send `error` or `control.denied`; do not forward
- input for offline daemon: send `error`; do not forward
- expired local grant: clear local controller state and deny input

Daemon behavior:

- unknown session ID: drop input and optionally log
- tmux injection failure: report an error envelope back to relay if the client-side protocol supports it; otherwise log and continue

## Testing Strategy
Use test-first implementation for this slice.

Persistence and service tests:

- acquire creates a lease
- second device is denied while lease is active
- same device can renew
- stale renew is denied
- stale release is denied
- expired lease can be replaced
- non-owner cannot acquire
- non-running/non-idle session cannot be controlled

Control API tests:

- REST endpoints pass bearer claims into the service
- acquire response includes `lease_version`, `expires_at`, and `renew_after_seconds`
- conflict and authorization errors map to stable HTTP statuses

Relay integration tests:

- viewer watches, acquires control, and receives `control.granted`
- second viewer receives `control.denied`
- input from non-controller is not forwarded
- input from controller is forwarded to the daemon peer
- release clears local controller state and later input is rejected

Daemon relay client tests:

- terminal input frame calls the registered input handler
- unknown input encoding is ignored or rejected
- input handler errors do not crash the read loop

tmux tests:

- printable text maps to literal `send-keys`
- Enter, Tab, C-c, and Escape map to expected symbolic args
- mixed raw input is split into safe tmux command calls

End-to-end backend smoke:

- simulated daemon announces a session
- simulated viewer watches and acquires control
- simulated viewer sends a terminal input binary frame
- daemon-side test hook observes the input payload for the right session

## Open Questions Deferred
These are intentionally not solved in this slice:

- exact Android keyboard event encoding
- bracketed paste behavior
- mouse reporting
- controller handoff UX
- explicit control revocation before TTL expiry
- dedicated relay-control gRPC deployment topology

## Success Criteria
The slice is complete when:

1. `termix-control` persists and enforces a single active remote controller per session.
2. Lease rules live in a reusable service layer beneath REST handlers.
3. `termix-relay` grants, denies, renews, and releases control through control API calls.
4. `termix-relay` forwards terminal input only from the active controller connection.
5. `termixd` receives forwarded input and injects it into tmux through a tested helper boundary.
6. Go tests prove the backend loop without Android UI.
7. `docs/PROGRESS.md` records the completed design and planned implementation handoff.
