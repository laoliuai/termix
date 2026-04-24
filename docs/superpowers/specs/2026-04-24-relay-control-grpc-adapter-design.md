# Termix Relay-Control gRPC Adapter Design

Status: written for review
Date: 2026-04-24
Source of truth: `docs/termix-v1-detailed-technical-spec.md`

## Purpose
This document defines the internal relay-control gRPC adapter slice.

The target result is a contract-first internal API between `termix-relay` and `termix-control` that replaces the current relay-to-control REST lease path for the Android watch/control backend loop, while preserving a safe fallback during rollout.

This is a design-only artifact. It does not authorize implementation by itself.

## Repository Context
As of 2026-04-24, the repository has completed the backend control lease and remote input loop:

- `termix-control` persists control leases and enforces single-controller rules through a reusable `control.LeaseService`.
- `termix-relay` accepts WSS viewer control messages, calls `termix-control` through the generated REST client, and forwards terminal input only from the active controller peer.
- `termixd` receives authorized relay input frames and injects bytes into tmux.
- The V1 technical spec says relay should call control through an internal `RelayControlService` gRPC contract and must not touch PostgreSQL directly.

This slice should close that architectural gap without changing the WSS protocol, tmux behavior, Android-facing message names, or lease semantics.

## Scope
This design includes:

- a new `proto/relay_control.proto` service contract
- generated Go protobuf/gRPC code under `go/gen/proto/relaycontrolv1`
- a `termix-control` gRPC server adapter that reuses existing auth, session, and lease services
- a `termix-relay` gRPC client adapter implementing the existing `relay.SessionAuthorizer` interface
- startup configuration that prefers internal gRPC and keeps REST as a temporary fallback
- error and denial-reason mapping from control service decisions to relay WSS responses
- tests that prove the Android backend watch/control loop still works through the gRPC adapter

This design explicitly excludes:

- Android UI implementation
- Android login/device onboarding changes
- relay WSS protocol changes
- terminal keyboard/IME/mouse protocol expansion
- database schema changes for connection lifecycle audit
- relay connection heartbeat persistence
- removal of the existing REST control lease endpoints

## Decision
Use a **complete proto contract with a partial first implementation**.

The proto should define the full `RelayControlService` listed by the V1 spec:

- `ValidateAccessToken`
- `AuthorizeSessionWatch`
- `AcquireControlLease`
- `RenewControlLease`
- `ReleaseControlLease`
- `MarkConnectionOpened`
- `MarkConnectionClosed`

The first implementation should support only the Android backend control loop:

- `AuthorizeSessionWatch`
- `AcquireControlLease`
- `RenewControlLease`
- `ReleaseControlLease`

`ValidateAccessToken` should be defined but not used as a separate relay preflight call. The implemented RPCs should validate bearer tokens inside each request so relay does not need an extra round trip before every watch/control action.

`MarkConnectionOpened` and `MarkConnectionClosed` should be defined now and left as deferred lifecycle surfaces. The first implementation may return `Unimplemented` or a documented no-op, but it must not pretend connection audit persistence exists.

## Approaches Considered

### Approach A: Big-bang gRPC replacement
Add the internal gRPC service and immediately delete relay's REST authorizer path.

Pros:

- clean final architecture
- no dual-stack configuration
- forces all tests onto the intended internal service boundary

Cons:

- higher rollout risk before Android integration proves the full loop
- no easy fallback if internal gRPC deployment wiring is wrong
- larger diff because REST deletion, config, and integration work happen together

### Approach B: Dual-stack adapter, gRPC preferred
Add the internal gRPC adapter and make `termix-relay` prefer it when `TERMIX_RELAY_CONTROL_GRPC_ADDR` is configured. Keep the current REST authorizer as a temporary fallback.

Pros:

- proves the intended internal boundary while retaining rollback safety
- keeps `relay.SessionAuthorizer` as the stable relay dependency
- minimizes changes to WSS routing and lease enforcement
- allows Android integration to run against gRPC without deleting a working REST path

Cons:

- temporary duplicate transport paths
- tests must ensure REST and gRPC denial mapping stay equivalent while both exist

### Approach C: Proto-only contract
Define and generate `relay_control.proto` but do not wire server/client adapters yet.

Pros:

- smallest immediate change
- gives Android and backend planning a stable contract name

Cons:

- does not prove the adapter shape
- leaves relay on the public REST path
- defers the real integration risk

## Recommendation
Use **Approach B: dual-stack adapter, gRPC preferred**.

This matches the long-term service boundary while keeping the implementation slice narrow and reversible. The important design rule is that both REST and gRPC remain transport adapters over the same control-plane services. Lease policy must stay in `go/internal/control`, and persistence must stay behind `go/internal/persistence`.

## Architecture

```text
Android viewer/controller
  -> WSS session.watch / control.acquire / control.renew / control.release
termix-relay
  -> relay.SessionAuthorizer
  -> relay-control gRPC client adapter
termix-control
  -> relay-control gRPC server adapter
  -> token validation and actor extraction
  -> session authorization / control.LeaseService
  -> PostgreSQL through sqlc-backed persistence
termix-relay
  -> WSS session.joined / control.granted / control.denied / control.revoked
```

The relay package should not know whether authorization is backed by REST or gRPC. It should continue to call:

- `AuthorizeWatch`
- `AcquireControl`
- `RenewControl`
- `ReleaseControl`

The adapter decides how those calls are transported.

## Protobuf Contract

Create `proto/relay_control.proto` with package and Go package names similar to:

```proto
syntax = "proto3";

package termix.relaycontrol.v1;

option go_package = "github.com/termix/termix/go/gen/proto/relaycontrolv1";

service RelayControlService {
  rpc ValidateAccessToken(ValidateAccessTokenRequest) returns (ValidateAccessTokenResponse);
  rpc AuthorizeSessionWatch(AuthorizeSessionWatchRequest) returns (AuthorizeSessionWatchResponse);
  rpc AcquireControlLease(AcquireControlLeaseRequest) returns (ControlLeaseResponse);
  rpc RenewControlLease(RenewControlLeaseRequest) returns (ControlLeaseResponse);
  rpc ReleaseControlLease(ReleaseControlLeaseRequest) returns (ReleaseControlLeaseResponse);
  rpc MarkConnectionOpened(MarkConnectionOpenedRequest) returns (MarkConnectionOpenedResponse);
  rpc MarkConnectionClosed(MarkConnectionClosedRequest) returns (MarkConnectionClosedResponse);
}
```

For the first implementation, every implemented request should carry `access_token` directly:

```proto
message AuthorizeSessionWatchRequest {
  string access_token = 1;
  string session_id = 2;
}

message AcquireControlLeaseRequest {
  string access_token = 1;
  string session_id = 2;
}

message RenewControlLeaseRequest {
  string access_token = 1;
  string session_id = 2;
  int64 lease_version = 3;
}

message ReleaseControlLeaseRequest {
  string access_token = 1;
  string session_id = 2;
  int64 lease_version = 3;
}
```

Lease responses should preserve the data already used by relay:

```proto
message ControlLeaseResponse {
  string session_id = 1;
  string controller_device_id = 2;
  int64 lease_version = 3;
  string granted_at = 4;
  string expires_at = 5;
  int32 renew_after_seconds = 6;
}

message ReleaseControlLeaseResponse {
  string session_id = 1;
  int64 lease_version = 2;
  bool released = 3;
}
```

Use RFC3339 strings for timestamps in this contract unless implementation identifies a strong reason to add `google.protobuf.Timestamp`. Strings keep generation simple and match the existing REST response shape.

## Deferred RPCs

`ValidateAccessToken` is deferred as a standalone relay preflight. The first Android loop should not call it separately because `AuthorizeSessionWatch` and lease RPCs already validate the bearer token and derive the actor.

`MarkConnectionOpened` and `MarkConnectionClosed` are deferred lifecycle hooks. They should be present in proto so future relay connection audit, online counts, and heartbeat work have a stable location. The first implementation should either:

- return gRPC `Unimplemented`, or
- return success with an explicit code comment that no persistence or audit side effect is performed yet.

The implementation plan should choose one behavior and test it. Returning `Unimplemented` is stricter and avoids misleading operational assumptions.

## Error Semantics

The relay needs stable denial reasons, not transport-specific messages. gRPC errors should map to the existing WSS denial reasons:

| Reason | gRPC Code | Relay behavior |
| --- | --- | --- |
| `unauthorized` | `Unauthenticated` | send `control.denied` or fail watch |
| `not_found` | `NotFound` | send `control.denied` |
| `already_controlled` | `FailedPrecondition` or `AlreadyExists` | send `control.denied` |
| `stale_lease` | `FailedPrecondition` | send `control.denied` and clear local controller state on renew/release |
| `session_not_controllable` | `FailedPrecondition` | send `control.denied` and clear local controller state |
| `invalid_request` | `InvalidArgument` | send `control.denied` |
| `internal` | `Internal` | treat as server error, not a denial |

The adapter should carry the stable reason in gRPC metadata or a typed error detail. Metadata is sufficient for the first implementation and avoids adding custom protobuf error-detail messages before they are needed.

The relay-side gRPC client should convert recognized denial reasons into `relay.ErrControlDenied`, matching the current REST adapter behavior. Unknown gRPC errors should remain ordinary errors so relay closes or reports the connection rather than silently denying.

## Control Service Adapter

Add a Go package such as `go/internal/relaycontrol` for the internal gRPC adapter. It should expose server construction without moving policy into transport code.

The server adapter should:

- parse and validate `access_token`
- derive the same actor fields used by REST handlers: `user_id` and `device_id`
- authorize watch by reusing the same session lookup semantics as `GetSessionForViewer`
- call `control.LeaseService` for acquire, renew, and release
- map domain errors to gRPC status code plus stable reason
- format lease timestamps consistently with REST responses

The adapter must not:

- query PostgreSQL directly except through existing persistence interfaces
- duplicate lease policy already in `control.LeaseService`
- inspect or proxy terminal bytes
- introduce Python or Android dependencies

## Relay Client Adapter

Add a relay-control gRPC client adapter that implements `relay.SessionAuthorizer`.

Responsibilities:

- dial the configured internal gRPC address
- call `AuthorizeSessionWatch` for `AuthorizeWatch`
- call `AcquireControlLease`, `RenewControlLease`, and `ReleaseControlLease` for control lease operations
- convert `ControlLeaseResponse` into `relay.ControlGrant`
- map known gRPC denial reasons to `relay.ErrControlDenied`
- preserve timeout/cancellation through the caller's context

The relay package itself should remain transport-agnostic. Existing WSS flow tests should need little or no change beyond swapping the fake authorizer/client shape in integration tests.

## Startup and Configuration

`termix-control` should start both:

- public REST API
- internal relay-control gRPC server

Initial environment variables:

- `TERMIX_CONTROL_REST_ADDR`, default `:8080` if the current default remains useful
- `TERMIX_CONTROL_RELAY_GRPC_ADDR`, required when running the internal service in production

`termix-relay` should prefer:

- `TERMIX_RELAY_CONTROL_GRPC_ADDR` for the new internal gRPC adapter
- `TERMIX_CONTROL_API_URL` for the existing REST fallback

If both are unset, relay should fail startup with a clear error. If both are set, gRPC should win and REST should be logged or documented as fallback-only.

The implementation should avoid TLS decisions inside this slice unless deployment requires it immediately. Local and single-host development can start with insecure internal gRPC, while production transport security can be captured as a later deployment hardening task.

## Testing Strategy

Contract and generation:

- update `Makefile generate` so protobuf generation includes `proto/relay_control.proto`
- verify generated code is deterministic
- include generated `go/gen/proto/relaycontrolv1` files in the commit

Control adapter tests:

- valid watch authorization succeeds for a session owned by the token user
- invalid token maps to `unauthorized`
- unknown session maps to `not_found`
- acquire returns the same lease fields as REST
- already-held lease maps to `already_controlled`
- stale renew/release maps to `stale_lease`
- deferred lifecycle RPC behavior is explicit and tested

Relay adapter tests:

- gRPC `ControlLeaseResponse` maps to `relay.ControlGrant`
- gRPC denial metadata maps to `relay.ErrControlDenied`
- unknown gRPC errors are not converted into denials

Integration tests:

- relay watch uses the gRPC authorizer and still joins a daemon-backed session
- viewer acquire over WSS returns `control.granted`
- active controller input still forwards to daemon
- stale or non-controller input is still rejected
- REST fallback still works until the fallback is intentionally removed

Standard verification:

```bash
make generate
cd go && go test ./...
cd go && go vet ./...
```

## Rollout Plan

1. Add and generate the proto contract.
2. Add control-side gRPC adapter tests and implementation for the four Android-loop RPCs.
3. Add relay-side gRPC client adapter tests and implementation.
4. Wire `termix-control` startup to serve internal gRPC.
5. Wire `termix-relay` startup to prefer gRPC and fall back to REST.
6. Run full Go verification.
7. Keep REST fallback until Android end-to-end testing proves the gRPC path.

## Acceptance Criteria

- `proto/relay_control.proto` defines all seven spec-listed RPCs.
- Relay-control gRPC code is generated and committed.
- `termix-control` serves the four Android-loop RPCs through gRPC.
- `termix-relay` can use gRPC for watch authorization and control lease operations without changing WSS behavior.
- Deferred lifecycle/introspection RPC behavior is documented in code and covered by tests.
- REST fallback remains available and does not affect the default gRPC path when the gRPC address is configured.
- `docs/PROGRESS.md` is updated before reporting completion.

## Risks

- Dual-stack REST/gRPC behavior may drift if tests do not compare denial reason mapping.
- Insecure internal gRPC is acceptable for local development but not enough for hardened deployment.
- Returning no-op success for lifecycle RPCs could mislead future operators; `Unimplemented` is safer unless a concrete caller requires success.
- Timestamp strings are simple but less strongly typed than protobuf timestamps. If multiple non-Go clients consume this service directly, `google.protobuf.Timestamp` may become preferable.

## Future Work

- Remove REST fallback after Android end-to-end tests validate the gRPC path.
- Implement standalone `ValidateAccessToken` if relay later needs long-lived connection validation independent of a session.
- Implement `MarkConnectionOpened` and `MarkConnectionClosed` with durable connection audit or online presence tracking.
- Add internal gRPC transport security and service-to-service identity for production deployment.
- Add metrics for gRPC authorization latency, denial reasons, and lease churn.
