# CLI / daemon version handshake

## Background

`install.sh` (commit `d43e692`) added `stop_running_daemon` to kill any running
`termix __daemon` before overwriting the binary, fixing the case where a
freshly-logged-in CLI silently routed IPC to a stale daemon. That fix only
covers the official one-line installer path. Manual upgrade paths bypass it:

- `go install github.com/termix/termix/go/cmd/termix@latest`
- `scp` / `rsync` of a freshly built binary into `~/.local/bin`
- IDE rebuilds during dev
- Smoke and CI workflows that overwrite the binary while a previous daemon is
  still running

In every case the symptom is the same: new CLI talks to an old daemon over the
existing daemon socket and uses code paths that no longer match the binary the
user thinks they are running. We need a defense in `ensureDaemon` itself, not
in any single upgrade tool.

## Goal

`termix` and the daemon process serving its socket must be the same physical
build. Whenever they are not, the CLI must terminate the running daemon and
spawn a new one from its own binary before continuing.

The defense lives in the CLI's `ensureDaemon` path because that path is reached
on every user-facing command (`start`, `sessions attach`, `doctor`, ...) before
any meaningful daemon RPC.

## Non-goals

- Detecting upgrades while a long-running daemon RPC is in flight. The check
  fires on the next CLI invocation, which is the cheapest correct moment.
- Cross-daemon protocol versioning. We are defending against same-binary,
  different-build skew, not negotiating compatibility between major versions.
- Avoiding the cost of a session disconnect on mismatch. Killing the daemon
  closes any in-flight `sessions attach`. The user just changed their binary;
  the brief disconnect is the expected price and matches what `install.sh`
  already does.
- Touching `install.sh`. It is correct as is — this slice covers the cases it
  cannot reach.

## Identity tuple

CLI and daemon both compute and compare a `(version, revision, modified)`
triple. The data sources:

| Field      | Source                                              | Example                                |
| ---------- | --------------------------------------------------- | -------------------------------------- |
| `version`  | `var version = "dev"` in `cmd/termix/main.go`,      | `v1.2.3` (release) or `dev` (local)    |
|            | overridable via `-ldflags "-X main.version=..."`    |                                        |
| `revision` | `debug.ReadBuildInfo().Settings["vcs.revision"]`,   | `d43e692857d2`                         |
|            | truncated to first 12 hex chars                     |                                        |
| `modified` | `debug.ReadBuildInfo().Settings["vcs.modified"]`,   | `true` (dirty tree) or `false` (clean) |
|            | parsed as boolean                                   |                                        |

`Matches` rule:

- All three fields equal **and** both sides have `modified == false` → same
  build, reuse the daemon.
- Any field differs **or** either side has `modified == true` → mismatch,
  rebuild.

The `modified == true → always mismatch` clause matters because two different
dirty rebuilds at the same commit produce identical `(version, revision)` but
different code; treating dirty as always-mismatch is the dev-loop behaviour we
want.

## Protocol changes

`proto/daemon.proto` extends the existing `HealthResponse`:

```protobuf
message HealthResponse {
  string status   = 1;  // existing
  string version  = 2;  // main.version
  string revision = 3;  // short vcs.revision (<= 12 chars)
  bool   modified = 4;  // vcs.modified
}
```

Backward compatibility: an old daemon (pre-this-slice) returns a
`HealthResponse` with the new fields zero-valued. The CLI treats an empty
`version` AND empty `revision` AND `modified == false` from a *successful*
Health response as a sentinel "old schema" and forces a mismatch — which is
exactly the behaviour we want when upgrading from any pre-handshake daemon.

`HealthRequest` is unchanged. The compare runs on the response only; the CLI
already has its own identity locally.

## New package: `internal/buildinfo`

```go
package buildinfo

type Identity struct {
    Version  string
    Revision string  // short, <= 12 hex chars
    Modified bool
}

// Current returns the identity of the currently running binary. Caller passes
// the linker-injected version string from package main so the import graph
// stays clean.
func Current(version string) Identity

// Matches reports whether two identities are byte-for-byte equal and both
// clean (modified == false on both sides).
func (a Identity) Matches(b Identity) bool

// String returns a short human-readable form for log lines:
//   "v1.2.3@d43e692857d2"           (clean release)
//   "v1.2.3@d43e692857d2-dirty"     (modified)
//   "dev@-"                         (dev with no VCS)
func (a Identity) String() string
```

Both `cmd/termix` and `internal/hostdaemon` import this package. There is no
duplicated identity logic anywhere else.

## Terminating the running daemon: `Shutdown` RPC

`DaemonService` gains:

```protobuf
rpc Shutdown(ShutdownRequest) returns (ShutdownResponse);

message ShutdownRequest  {}
message ShutdownResponse {}
```

The handler hooks into the existing cancellation path inside
`hostdaemon.Run` (already audited and bounded by recent code-review
follow-ups). It acks immediately, then triggers the same teardown SIGTERM /
context-cancel would. The CLI does not wait on the RPC for the actual shutdown
— it polls for the socket file disappearing.

This avoids platform-specific tricks (`SO_PEERCRED`, `pkill` with cmdline
matching, Windows job objects) and keeps the contract inside the existing gRPC
service.

## CLI flow (revised `ensureDaemon`)

```
1. Try existing socket: dialDaemon → Health
   1a. dial fails → fall through to launch (existing behaviour)
   1b. Health fails → fall through to launch (existing behaviour)
   1c. Health succeeds:
       local := buildinfo.Current(version)
       remote := identity from response
       if local.Matches(remote): return nil
       log to stderr: "daemon version mismatch (<remote> → <local>), restarting…"
       Shutdown RPC (1s timeout); ignore error
       wait up to 2s for socket file to disappear; if it doesn't, remove it
       fall through to launch
2. launchDaemon(...) — existing
3. Poll Health up to 20× / 100ms — existing, but on success also verify
   identity matches and treat mismatch as fatal ("daemon spawned with wrong
   identity"). Same binary spawning a different identity should be impossible
   in practice; this catches accidental skew (someone overrode the launcher
   path during testing).
```

## Error handling

| Situation                                    | Behaviour                                        |
| -------------------------------------------- | ------------------------------------------------ |
| Health succeeds, identity matches            | Reuse, no log, no respawn (hot path).            |
| Health succeeds, identity differs            | Log "daemon version mismatch", Shutdown, respawn. |
| Health succeeds, response has zero fields    | Treat as mismatch (old daemon, pre-handshake).   |
| Shutdown RPC errors                          | Log, then remove socket file ourselves and proceed. |
| `debug.ReadBuildInfo()` unavailable on CLI   | Identity falls back to `(version, "", false)`. Match still works for two binaries linked the same way. Will force a respawn against any daemon that *does* have VCS info — acceptable, it errs toward respawn. |
| `debug.ReadBuildInfo()` unavailable on daemon| Same; daemon reports `("", "", false)` → mismatch with any CLI that has identity → respawn. |
| Newly spawned daemon's identity also differs | Fail with `daemon spawned with mismatched identity`. The launcher and CLI live in the same binary; this would mean the OS is executing some other binary at our `os.Args[0]`. Rare; surface clearly. |

## Active sessions

Killing the daemon ends any `sessions attach` over the daemon socket. This is
acceptable and consistent with `install.sh stop_running_daemon`. Documented
behaviour: the `restarting…` stderr line tells the user what happened, and
`termix start` re-establishes the session via the new daemon.

We do **not** wait for sessions to drain. The whole point of the handshake is
that running with a stale daemon is unsafe; deferring shutdown defeats it.

## Testing

`internal/buildinfo/buildinfo_test.go`:

- Truth table for `Matches`:
  - identical clean → true
  - identical but one side `modified=true` → false
  - identical but both sides `modified=true` → false (modified-always-mismatch)
  - any single field differs → false
  - both sides empty zero-value → false (treat zero as unknown, force respawn)
- `Current("v1.2.3")` smoke: returns non-empty `Version`; if running under
  `go test` with VCS info present, returns non-empty `Revision`.

`cmd/termix/main_test.go` (extending `fakeDaemonClient`):

- `healthResponse` carries identity; tests assert respawn / no-respawn:
  - identical identity → no Shutdown call, no relaunch
  - differing identity → Shutdown called once, then launchDaemon called,
    second Health checked
  - response with zero identity (old daemon) → treated as mismatch
  - both sides clean but daemon `modified=true` → mismatch
  - Shutdown RPC errors → socket file removed, launch still happens
  - Newly spawned daemon also mismatches → returns error containing
    "mismatched identity"

`internal/hostdaemon` Shutdown handler test:

- Calling `Shutdown` causes `Run` to return within a bounded time (reuses the
  existing shutdown bound from prior code review).

No real-binary end-to-end test. The unit coverage above plus the existing
manual smoke (`make smoke`) is sufficient.

## Files touched

- `proto/daemon.proto` — `HealthResponse` fields, `Shutdown` RPC + messages.
- `go/gen/proto/daemonv1/*` — regenerated.
- `go/internal/buildinfo/buildinfo.go` (new) + test file.
- `go/cmd/termix/main.go` — `ensureDaemon` revised; passes `version` into
  `buildinfo.Current`.
- `go/cmd/termix/main_test.go` — extended fakes and new test cases.
- `go/internal/hostdaemon/*` — `Health` handler populates identity from
  `buildinfo.Current`; new `Shutdown` handler.
- `go/internal/hostdaemon/*_test.go` — `Shutdown` test.
- `docs/PROGRESS.md` — task entry.

No DB migration. No web/admin/relay changes. No `install.sh` changes.

## Verification commands

```
cd go && go build ./...
cd go && go vet ./...
cd go && go test ./internal/buildinfo ./internal/hostdaemon ./cmd/termix -count=1
cd go && go test ./... -count=1
```

(Full DB integration suite continues to be skipped without
`TERMIX_TEST_DATABASE_URL`; this slice does not exercise it.)
