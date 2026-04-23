# Termix Phase 2 Relay/Watch Foundation Design

Status: draft for review  
Date: 2026-04-23  
Source of truth: `docs/termix-v1-detailed-technical-spec.md`

## Purpose
This document defines the first Phase 2 implementation slice after the completed host/control mainline. It focuses on the shared transport and watch foundation that future Android and Web clients will reuse.

This is a design-only artifact. It does not authorize implementation by itself.

## Repository Context
As of 2026-04-23, the repository already implements:

- PostgreSQL schema and generated query layer for the host/control slice
- `termix-control` auth and host session APIs
- `termixd` bootstrap, local state, and tmux orchestration
- thin `termix` CLI commands for `login`, `start`, `sessions attach`, and `doctor`

This Phase 2 design intentionally excludes the deferred `termix-admin-api` and admin Web UI surfaces.

## Goal
Build the transport and protocol foundation for remote watch without committing to a specific client implementation.

The immediate delivery target is:

- `termixd` outbound relay connection
- `termix-relay` WSS service
- generic viewer watch protocol for future Android and Web clients
- initial snapshot on watch so newly connected viewers immediately see the current screen
- live output fanout to multiple viewers

## Scope
This design includes:

- daemon-to-relay authenticated WSS connection
- viewer-to-relay authenticated WSS connection
- watcher join/leave flow
- current-screen snapshot request and delivery
- live terminal output routing after snapshot
- heartbeats and basic disconnect handling
- protocol reservation for future remote control

This design explicitly does not include:

- Android UI implementation
- Web viewer implementation
- remote input execution
- control lease enforcement
- preview pipeline
- relay-side persistent session state
- relay-side session metadata ownership

## Approaches Considered

### Approach A: Host uplink only
Implement `termixd -> relay` first, validate with logs or test tools, and defer the public viewer protocol.

Pros:

- smallest immediate implementation
- easiest short-term verification

Cons:

- pushes the real protocol design downstream
- likely forces protocol churn once Android/Web viewers appear
- gives no stable contract for `watch + snapshot + live stream`

### Approach B: Watch-first relay foundation
Implement `termixd <-> relay <-> generic viewer` for read-only watch, with initial snapshot and live stream, while reserving protocol space for future control messages.

Pros:

- creates a reusable client-agnostic contract
- solves the TUI idle-screen problem immediately
- keeps current sprint scope stable by excluding writes

Cons:

- slightly more design work than host uplink only
- requires choosing viewer protocol semantics before concrete clients exist

### Approach C: Read and write together
Implement watch, remote control lease, and remote input in the same slice.

Pros:

- gets to remote interaction faster
- avoids a second protocol pass for control

Cons:

- significantly increases sprint risk
- forces lease, input semantics, reconnect, and controller state to land together
- mixes transport stabilization with policy-heavy control behavior

## Recommendation
Use **Approach B**.

This slice should stabilize the transport and read path first, while reserving the protocol and state model for future control. The resulting system is useful on its own and avoids reworking the watch protocol once Android/Web viewers are added.

## Core Decisions

### 1. `termixd` is the sole runtime authority
`termixd` is the only authority for:

- tmux control-mode parsing
- current terminal screen snapshot
- live terminal output stream
- host-side session runtime state

Even if relay later caches a recent snapshot for reconnect smoothing, that cache is not authoritative. The source of truth remains `termixd`.

### 2. `termix-relay` is a light stateful router
`termix-relay` owns:

- daemon/viewer WSS connection registry
- authenticated watch subscriptions
- in-memory watcher fanout for active sessions
- heartbeats, disconnect detection, and backpressure
- snapshot request coordination between viewers and daemons

`termix-relay` must not become the source of truth for session metadata or current screen state.

### 3. Viewer protocol is generic, not Android-specific
The current full V1 spec describes an Android-specific relay client role. This slice changes that first client-side role to a generic **viewer** role so the same protocol can be reused by Android and Web later.

Connection roles for this slice:

- `daemon`
- `viewer`

## Architecture

```text
termixd <-> WSS <-> termix-relay <-> WSS <-> viewer
    |                                |
    |                                +-- watch subscription
    +-- tmux control mode
    +-- snapshot capture
    +-- live output
```

Control plane remains outside the hot data path for screen state, but is still used for identity and authorization decisions.

## Transport and Framing

### WSS model
Use WSS for both daemon and viewer connections.

Each connection sends a role-specific `hello` message after authentication:

- `hello.daemon`
- `hello.viewer`

### Control envelope
All control messages are JSON text frames using the existing base envelope:

```json
{
  "type": "session.watch",
  "request_id": "uuid",
  "payload": {}
}
```

Required fields:

- `type`
- `request_id`
- `payload`

### Binary frames
Keep the existing binary framing direction from the full spec:

- frame type `1`: terminal output
- frame type `3`: snapshot chunk

This preserves a clean separation:

- text frames for control
- binary frames for terminal/snapshot payloads

For the first implementation, most snapshots may fit into a single chunk, but the protocol must support multi-chunk snapshots from the start.

## Message Set for This Slice

### Viewer -> relay
- `hello.viewer`
- `session.watch`
- `session.unwatch`
- `heartbeat`

### Relay -> viewer
- `hello.ok`
- `session.joined`
- `session.left`
- `session.snapshot.ready`
- `error`
- `heartbeat`

### Daemon -> relay
- `hello.daemon`
- `session.online`
- `session.offline`
- `session.snapshot.ready`
- `heartbeat`

Binary frames from daemon to relay:

- terminal output
- snapshot chunk

### Relay -> daemon
- `hello.ok`
- `session.snapshot.request`
- `error`
- `heartbeat`

## Watch Handshake

The watch handshake for a viewer is:

1. viewer connects to relay with bearer auth
2. viewer sends `hello.viewer`
3. viewer sends `session.watch { session_id }`
4. relay validates watch permission for that user and session
5. relay returns `session.joined`
6. relay sends `session.snapshot.request` to the responsible daemon
7. daemon captures the current pane snapshot from tmux
8. daemon sends `session.snapshot.ready`
9. daemon streams snapshot chunk binary frame(s)
10. relay forwards snapshot metadata and snapshot chunks to the viewer
11. daemon continues streaming live output, and relay fans it out to all watchers

Functional note:

- a watch is not considered usable until snapshot delivery succeeds
- if snapshot delivery fails or times out, relay must send an explicit error and terminate that watch subscription instead of leaving the viewer on a blank screen

## Snapshot Model

### Capture source
Snapshot capture comes from tmux on the host:

```bash
tmux capture-pane -p -e -S -200 -t termix_<session_id>:main.0
```

### Semantics
The snapshot represents the current screen state, not terminal history replay.

It exists to solve these cases:

- a viewer joins while the TUI is idle
- a viewer reconnects after a transient network drop
- a second viewer joins an already-running session

### Encoding
Snapshot payloads should preserve ANSI styling and screen content well enough for future Android/Web terminal renderers to reconstruct the current visual state.

The implementation must treat snapshot delivery as first-class behavior, not as an optional optimization.

## Authorization Model

Relay must not infer watch authority from daemon registration alone.

For this slice:

- relay validates bearer identity
- relay delegates session watch authorization to `termix-control`
- relay does not become the owner of session metadata or user/session authorization policy

This keeps ownership aligned with the existing control-plane role.

## Relay Runtime Model

For each active session, relay maintains lightweight in-memory state:

- daemon connection reference
- zero or more watcher connections
- recent liveness timestamps

This state is ephemeral. If relay restarts, clients reconnect and rebuild it.

Relay does not persist:

- full screen history
- full environment
- session metadata authority

## Future Control Reservation

This sprint does not implement write/control, but the protocol must reserve the model now.

Future control rules are fixed as:

- multiple viewers may watch a session
- at most one remote controller may hold control
- control is not preemptive
- a remote client may acquire control only if no controller currently holds it
- the local host terminal remains usable

Reserved future control messages:

- `control.acquire`
- `control.release`
- `control.granted`
- `control.denied`
- `control.revoked`
- `input`

The final authority for control lease should remain outside relay, most likely in `termix-control`.

## Failure Handling

The first implementation should explicitly handle:

- viewer watches an unknown session: relay returns `error` and does not join
- viewer watches a session whose daemon is offline: relay returns `error`
- snapshot request times out: relay returns `error` and removes the watch subscription
- slow viewer: relay may disconnect the viewer rather than blocking daemon forwarding
- daemon disconnect: relay sends `session.left` or equivalent terminal error to active watchers

The system should prefer explicit failure over silent blank watches.

## Testing Strategy

Required verification for this slice should include:

- unit tests for relay envelope parsing and routing
- unit tests for daemon snapshot request handling
- unit tests for watcher fanout semantics
- integration tests for daemon connect -> session online -> watch -> snapshot -> live output
- integration tests for multiple viewers on one session
- integration tests for snapshot timeout and offline daemon failure paths

Manual validation should include:

- watch a running but idle TUI and confirm immediate current-screen rendering
- connect a second viewer to the same session and confirm snapshot + shared live stream
- disconnect and reconnect a viewer while the session remains idle

## Acceptance Criteria for This Slice

This design is considered successfully implemented when:

1. `termixd` can connect to relay over authenticated WSS.
2. relay can track active daemon-backed sessions in memory.
3. a generic viewer can watch a session through relay.
4. a new viewer immediately receives the current screen snapshot even if the session is idle.
5. live output continues after snapshot delivery.
6. multiple viewers can watch the same session concurrently.
7. relay is not the source of truth for session screen state.
8. control messages are reserved but not yet implemented.

## Follow-on Work

After this slice is approved and implemented, the next design/implementation step should cover:

- control lease authority
- remote input semantics
- controller heartbeat and timeout
- Android/Web viewer integration

