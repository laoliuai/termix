# Remove Relay REST Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Validate the Phase 2 relay-control gRPC path end-to-end with real components (real Postgres + real `relaycontrol.Server` + real `relaycontrol.Client` + real `relay.Server` + WSS daemon and viewer), then remove `termix-relay`'s REST authorizer fallback so gRPC is the only relay→control path.

**Architecture:** Add one new Go integration test under `go/tests/relay_grpc_e2e_test.go` that wires a real `relaycontrol.Server` over a loopback gRPC listener, dials it with the real `relaycontrol.Client`, registers that client as the relay's `SessionAuthorizer`, then drives `session.watch`, `control.acquire`, `control.renew`, terminal-input forwarding, and `control.release` over WSS. Once the test passes (and existing tests still pass), drop `cmd/termix-relay/main.go`'s `TERMIX_CONTROL_API_URL` REST branch and the now-unused `relay.ControlAuthorizer` / `relay.ControlSessionClient` types in `go/internal/relay/auth.go`. Leave the REST control-lease HTTP handlers and `controlapi.Client` lease/viewer methods in place — record their removal as a deferred follow-up so this slice stays focused.

**Tech Stack:** Go 1.x, pgx + sqlc, grpc-go, coder/websocket, real Postgres test DB via `persistence.NewTestStore`.

---

## File Structure

- Create: `go/tests/relay_grpc_e2e_test.go` — single end-to-end integration test wiring real Postgres + real gRPC + real relay + WSS viewer.
- Modify: `go/cmd/termix-relay/main.go` — drop the REST fallback path in `buildAuthorizer`.
- Modify: `go/internal/relay/auth.go` — delete `ControlAuthorizer`, `ControlSessionClient`, `mapControlGrant`, `mapControlAPIError`, `isDeniedReason`, `deniedMessage`, and the `controlapi`/`openapi` imports they depended on. Keep `ControlGrant`, `ErrControlDenied`, and `SessionAuthorizer` (used by the gRPC client adapter and relay server).
- Modify: `docs/PROGRESS.md` — move the deferred REST fallback line into Completed; add a deferred follow-up for cleaning the dead REST control-lease HTTP surface.

No file is renamed or split. The REST control-lease HTTP handlers in `go/internal/controlapi/server.go` and the corresponding `controlapi.Client` lease/viewer methods stay untouched in this slice.

---

### Task 1: Add real-components end-to-end gRPC integration test

**Files:**
- Create: `go/tests/relay_grpc_e2e_test.go`

**Why first:** the design doc says REST fallback may be removed only after the gRPC path is proven end-to-end. The existing `TestRelayControlInputBackendLoopWithGRPCAuthorizer` only uses a fake gRPC server, so it does not exercise the real `relaycontrol.Server` + `LeaseService` + `persistence.Store` chain. We add a test that does, then keep it green through the cleanup.

- [ ] **Step 1: Write the new integration test**

Create `go/tests/relay_grpc_e2e_test.go` with the following content. The test uses the existing `seedLeaseSession` helper from `go/tests/control_integration_test.go` (same package), `auth.IssueAccessToken`, `persistence.NewTestStore`, `relaycontrol.NewServer`, `relaycontrol.NewClient`, `relay.NewServer`, `relayclient.New`, the `writeEnvelope`/`readEnvelope`/`writeBinaryFrame` helpers from `go/tests/relay_integration_test.go`, and `coder/websocket` for the viewer.

```go
package tests

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/persistence"
	"github.com/termix/termix/go/internal/relay"
	"github.com/termix/termix/go/internal/relayclient"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"github.com/termix/termix/go/internal/relayproto"
	"github.com/termix/termix/go/internal/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestRelayWatchAndControlEndToEndOverGRPCWithRealControlServer(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run end-to-end gRPC integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, cleanupStore := persistence.NewTestStore(t)
	defer cleanupStore()

	seed := seedLeaseSession(t, ctx, store)

	const signingKey = "signing-key"
	accessToken, err := auth.IssueAccessToken(signingKey, seed.userID, seed.controllerDeviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	relaycontrolv1.RegisterRelayControlServiceServer(
		grpcServer,
		relaycontrol.NewServer(store, signingKey, relaycontrol.ServerConfig{}),
	)
	go func() { _ = grpcServer.Serve(grpcListener) }()
	defer grpcServer.Stop()

	grpcConn, err := grpc.Dial(grpcListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer grpcConn.Close()
	authorizer := relaycontrol.NewClient(grpcConn)

	relayServer := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(relayServer.Handler())
	defer httpServer.Close()

	inputCalls := make(chan []byte, 1)
	daemonClient := relayclient.New("ws"+httpServer.URL[len("http"):]+"/ws", "daemon-token", "device-1")
	daemonClient.SetInputHandler(func(_ context.Context, _ string, payload []byte) error {
		inputCalls <- append([]byte(nil), payload...)
		return nil
	})
	if err := daemonClient.Connect(ctx); err != nil {
		t.Fatalf("daemon connect: %v", err)
	}
	if err := daemonClient.AnnounceSession(ctx, session.LocalSession{SessionID: seed.sessionID}); err != nil {
		t.Fatalf("announce session: %v", err)
	}

	viewer, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + accessToken}},
	})
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewer.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:    relayproto.TypeHelloViewer,
		Payload: map[string]any{},
	})
	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:    relayproto.TypeSessionWatch,
		Payload: map[string]any{"session_id": seed.sessionID},
	})
	readEnvelope(t, ctx, viewer, relayproto.TypeSessionJoined)

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-e2e",
		Payload:   map[string]any{"session_id": seed.sessionID},
	})
	granted := readEnvelope(t, ctx, viewer, relayproto.TypeControlGranted)
	if v, _ := granted.Payload["lease_version"].(float64); int64(v) != 1 {
		t.Fatalf("expected acquire lease_version 1, got %#v", granted.Payload["lease_version"])
	}

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlRenew,
		RequestID: "renew-e2e",
		Payload: map[string]any{
			"session_id":    seed.sessionID,
			"lease_version": int64(1),
		},
	})
	renewed := readEnvelope(t, ctx, viewer, relayproto.TypeControlGranted)
	if v, _ := renewed.Payload["lease_version"].(float64); int64(v) != 2 {
		t.Fatalf("expected renew lease_version 2, got %#v", renewed.Payload["lease_version"])
	}

	writeBinaryFrame(t, ctx, viewer, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    seed.sessionID,
			"encoding":      "raw",
			"lease_version": int64(2),
		},
		Payload: []byte("e2e\n"),
	})
	select {
	case got := <-inputCalls:
		if string(got) != "e2e\n" {
			t.Fatalf("expected daemon payload %q, got %q", "e2e\n", got)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for daemon input handler: %v", ctx.Err())
	}

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlRelease,
		RequestID: "release-e2e",
		Payload: map[string]any{
			"session_id":    seed.sessionID,
			"lease_version": int64(2),
		},
	})
	readEnvelope(t, ctx, viewer, relayproto.TypeControlRevoked)
}
```

- [ ] **Step 2: Run the new test against a live Postgres test database**

Run:

```bash
cd go && TERMIX_TEST_DATABASE_URL="$TERMIX_TEST_DATABASE_URL" go test ./tests -run TestRelayWatchAndControlEndToEndOverGRPCWithRealControlServer -v
```

Expected: PASS. If `TERMIX_TEST_DATABASE_URL` is not set, the test skips with `set TERMIX_TEST_DATABASE_URL to run end-to-end gRPC integration test`. The expectation is that the maintainer runs the test against a real DB before approving the cleanup steps in Task 2; if no DB is available, surface that to the user before continuing — do not silently proceed.

- [ ] **Step 3: Run the full Go test suite to confirm no regression**

Run:

```bash
cd go && go test ./...
```

Expected: PASS for every package (integration tests covered by Postgres skip cleanly when DB env var is missing, but if `TERMIX_TEST_DATABASE_URL` is set they should all pass).

- [ ] **Step 4: Commit**

```bash
git add go/tests/relay_grpc_e2e_test.go
git commit -m "$(cat <<'EOF'
Prove relay watch/control end-to-end through real gRPC adapter

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Drop relay-side REST authorizer fallback

**Files:**
- Modify: `go/cmd/termix-relay/main.go` (whole file rewrite — small)
- Modify: `go/internal/relay/auth.go` (delete REST authorizer types, keep `ControlGrant`, `ErrControlDenied`, `SessionAuthorizer`, `bearerToken` if used — see step 2)

- [ ] **Step 1: Verify which `relay/auth.go` symbols still have callers outside the file**

Run:

```bash
cd go && grep -rn "relay\.\(NewControlAuthorizer\|ControlAuthorizer\|ControlSessionClient\|bearerToken\)" ./cmd ./internal ./tests
```

Expected: no matches in `./cmd` or `./internal/*` (the relay package itself or non-test files outside `relay/auth.go`). If `bearerToken` is unused outside `auth.go`, remove it too. If `bearerToken` is referenced by another file in `internal/relay/`, keep it (this preserves the WSS handshake's Authorization-header parsing).

Also verify the `controlapi`/`openapi` imports are only used by the deletion targets:

```bash
cd go && grep -n "controlapi\|openapi" ./internal/relay/auth.go
```

Expected: every match is inside the blocks slated for deletion (the `ControlSessionClient` interface, `mapControlAPIError`, the `mapControlGrant` body).

- [ ] **Step 2: Rewrite `go/internal/relay/auth.go` to drop the REST authorizer**

Replace the current file content with:

```go
package relay

import (
	"context"
	"strings"
	"time"
)

type ControlGrant struct {
	SessionID         string
	LeaseVersion      int64
	ExpiresAt         time.Time
	RenewAfterSeconds int
}

type ErrControlDenied struct {
	Reason  string
	Message string
}

func (e ErrControlDenied) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Reason
}

type SessionAuthorizer interface {
	AuthorizeWatch(ctx context.Context, accessToken string, sessionID string) error
	AcquireControl(ctx context.Context, accessToken string, sessionID string) (ControlGrant, error)
	RenewControl(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) (ControlGrant, error)
	ReleaseControl(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) error
}

func bearerToken(authHeader string) string {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return authHeader
	}
	return token
}
```

If Step 1 showed `bearerToken` has no callers outside this file, delete the function and the `strings` import. Otherwise keep it as written. If you find any tests in `go/internal/relay/` that still import `controlapi` or exercise `ControlAuthorizer`/`ControlSessionClient`, delete those test files in the same step (they cover code that no longer exists).

- [ ] **Step 3: Rewrite `go/cmd/termix-relay/main.go` to require gRPC**

Replace the current file content with:

```go
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/termix/termix/go/internal/relay"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	authorizer, cleanup, err := buildAuthorizer()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	addr := os.Getenv("TERMIX_RELAY_LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	server := relay.NewServer(authorizer)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}

func buildAuthorizer() (relay.SessionAuthorizer, func(), error) {
	grpcAddr := os.Getenv("TERMIX_RELAY_CONTROL_GRPC_ADDR")
	if grpcAddr == "" {
		return nil, func() {}, errors.New("TERMIX_RELAY_CONTROL_GRPC_ADDR is required")
	}

	conn, err := grpc.DialContext(context.Background(), grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, func() {}, err
	}
	return relaycontrol.NewClient(conn), func() { _ = conn.Close() }, nil
}
```

The `TERMIX_CONTROL_API_URL` environment variable is no longer read by `termix-relay`. (It is still read by `termix` and `termixd` and stays valid for them.)

- [ ] **Step 4: Build the relay command to confirm the deletions compiled**

Run:

```bash
cd go && go build ./cmd/termix-relay
```

Expected: no errors.

- [ ] **Step 5: Run the full Go test suite**

Run:

```bash
cd go && go test ./...
```

Expected: every package passes. If a previously existing test imported `relay.ControlAuthorizer` or `relay.NewControlAuthorizer` and was missed in Step 2, fix it now (delete the obsolete file or migrate the test to the gRPC client adapter). Re-run until green.

- [ ] **Step 6: Run go vet**

Run:

```bash
cd go && go vet ./...
```

Expected: no warnings.

- [ ] **Step 7: Commit**

```bash
git add go/cmd/termix-relay/main.go go/internal/relay/auth.go
# add any deleted relay tests if applicable
git commit -m "$(cat <<'EOF'
Remove relay REST authorizer fallback in favor of gRPC

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Update PROGRESS.md

**Files:**
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Update the Current Milestone status**

Edit `docs/PROGRESS.md`. Change the `Status:` paragraph under "Current Milestone" so it reads:

```text
Status: the host/control slice, Phase 2 relay/watch foundation, backend control lease/input loop, internal relay-control gRPC adapter, and end-to-end gRPC validation are complete. `termix-relay` now requires `TERMIX_RELAY_CONTROL_GRPC_ADDR` and no longer accepts the REST fallback. Android UI remains deferred.
```

- [ ] **Step 2: Move the REST fallback removal into Completed**

In the `## Completed` list, append:

```markdown
- [x] Validate the Phase 2 relay-control gRPC path end-to-end with a real Postgres + gRPC + relay + WSS integration test.
- [x] Remove the relay REST authorizer fallback so `termix-relay` requires the internal gRPC adapter.
```

In the `## Pending` list, delete the line:

```markdown
- [ ] Deferred: remove relay REST fallback after Android end-to-end testing validates the gRPC path.
```

Add a new deferred line under `## Pending` to record what is still dead-but-present:

```markdown
- [ ] Deferred: remove the now-unused REST control-lease HTTP handlers (`POST /sessions/{id}/control/{acquire,renew,release}`, `GET /sessions/{id}`) and the matching `controlapi.Client` lease/viewer methods after Android end-to-end testing confirms no REST consumer is needed.
```

- [ ] **Step 3: Update Next Up**

Replace the current `## Next Up` section with:

```markdown
## Next Up
1. Decide whether to add Android control UI next.
2. Deferred: remove the REST control-lease HTTP surface once Android testing confirms no REST consumer.
3. Deferred: revisit `termix-admin-api` and admin Web UI when ready.
```

- [ ] **Step 4: Commit**

```bash
git add docs/PROGRESS.md
git commit -m "$(cat <<'EOF'
Record removal of relay REST authorizer fallback

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review

**Spec coverage:**
- Validate gRPC path end-to-end with real components → Task 1.
- Remove relay REST fallback → Task 2.
- Update PROGRESS.md before reporting completion → Task 3.
- "REST control-lease HTTP surface still alive" tradeoff → tracked as deferred line in Task 3.

**Placeholder scan:** every code step shows the file content or the exact rewrite. No "TBD"/"appropriate"/"similar to". The one branch in Task 2 Step 2 (whether to keep `bearerToken` and the `strings` import) is resolved by the grep in Step 1; both branches are spelled out.

**Type consistency:** `relay.SessionAuthorizer`, `relay.ControlGrant`, `relay.ErrControlDenied`, `relaycontrol.NewClient`, `relaycontrol.NewServer`, `relayproto.TypeControlGranted`, etc. are used identically across tasks and match the current source.

**Risk:** if there is no live Postgres test DB available, Task 1 Step 2 cannot truly prove end-to-end; in that case the executor must stop and ask the user before continuing into Task 2 deletions. Documented in Task 1 Step 2.
