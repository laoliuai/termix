# Relay-Control gRPC Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace relay's current internal REST authorization path with a relay-control gRPC adapter for the Android backend watch/control loop, while keeping REST as a temporary fallback.

**Architecture:** Define `proto/relay_control.proto` as the internal service contract, then implement transport adapters around the existing `control.LeaseService` and relay `SessionAuthorizer` interface. `termix-control` owns token validation, session authorization, and lease policy; `termix-relay` remains WSS-routing code that only depends on `relay.SessionAuthorizer`.

**Tech Stack:** Go 1.25+, protobuf/gRPC, Gin REST fallback, sqlc-backed persistence, existing relay WSS protocol through `github.com/coder/websocket`.

---

## Approved Design

Use the approved design in `docs/superpowers/specs/2026-04-24-relay-control-grpc-adapter-design.md`.

Key decisions:

- Define all seven spec-listed RPCs in proto.
- Implement only `AuthorizeSessionWatch`, `AcquireControlLease`, `RenewControlLease`, and `ReleaseControlLease` in the first slice.
- Return gRPC `codes.Unimplemented` for deferred lifecycle/introspection RPCs.
- Prefer gRPC in `termix-relay` when `TERMIX_RELAY_CONTROL_GRPC_ADDR` is set.
- Keep REST fallback through `TERMIX_CONTROL_API_URL` until Android end-to-end testing proves the gRPC path.

## File Structure

- Create: `proto/relay_control.proto` — internal relay-control service contract.
- Modify: `Makefile` — include `proto/relay_control.proto` in protobuf generation.
- Generated: `go/gen/proto/relaycontrolv1/relay_control.pb.go`
- Generated: `go/gen/proto/relaycontrolv1/relay_control_grpc.pb.go`
- Modify: `go/internal/auth/tokens.go` — add reusable access-token parsing for non-Gin adapters.
- Create: `go/internal/relaycontrol/errors.go` — gRPC denial reason mapping helpers.
- Create: `go/internal/relaycontrol/server.go` — `termix-control` gRPC server adapter over existing services.
- Create: `go/internal/relaycontrol/client.go` — relay-side gRPC client adapter implementing `relay.SessionAuthorizer`.
- Modify: `go/cmd/termix-control/main.go` — start REST and internal gRPC listeners.
- Modify: `go/cmd/termix-relay/main.go` — prefer gRPC authorizer and keep REST fallback.
- Create: `go/internal/relaycontrol/server_test.go` — server adapter tests.
- Create: `go/internal/relaycontrol/client_test.go` — client adapter tests.
- Modify: `go/tests/relay_integration_test.go` — add a WSS backend-loop test using the gRPC authorizer.
- Modify: `docs/PROGRESS.md` — move plan status forward before completion is reported.

## Task 1: Define and Generate the Relay-Control Proto

**Files:**
- Create: `proto/relay_control.proto`
- Modify: `Makefile`
- Generated: `go/gen/proto/relaycontrolv1/relay_control.pb.go`
- Generated: `go/gen/proto/relaycontrolv1/relay_control_grpc.pb.go`

- [ ] **Step 1: Write the proto contract**

Create `proto/relay_control.proto`:

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

message ValidateAccessTokenRequest {
  string access_token = 1;
}

message ValidateAccessTokenResponse {
  string user_id = 1;
  string device_id = 2;
}

message AuthorizeSessionWatchRequest {
  string access_token = 1;
  string session_id = 2;
}

message AuthorizeSessionWatchResponse {
  string session_id = 1;
  string user_id = 2;
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

message MarkConnectionOpenedRequest {
  string access_token = 1;
  string connection_id = 2;
  string role = 3;
  string session_id = 4;
}

message MarkConnectionOpenedResponse {}

message MarkConnectionClosedRequest {
  string access_token = 1;
  string connection_id = 2;
  string role = 3;
  string session_id = 4;
  string reason = 5;
}

message MarkConnectionClosedResponse {}
```

- [ ] **Step 2: Update protobuf generation**

Modify the protobuf block in `Makefile` so it generates both proto files:

```makefile
	@if [ -f proto/daemon.proto ] && [ -f proto/relay_control.proto ]; then \
		command -v protoc >/dev/null 2>&1 || { echo "protoc binary is required"; exit 1; }; \
		mkdir -p go/gen/proto; \
		protoc --go_out=go --go_opt=module=github.com/termix/termix/go --go-grpc_out=go --go-grpc_opt=module=github.com/termix/termix/go -I proto proto/daemon.proto proto/relay_control.proto; \
	else \
		echo "Skipping proto generation: proto/daemon.proto or proto/relay_control.proto not found"; \
	fi
```

- [ ] **Step 3: Generate code**

Run:

```bash
make generate
```

Expected: command exits 0 and creates `go/gen/proto/relaycontrolv1/relay_control.pb.go` plus `go/gen/proto/relaycontrolv1/relay_control_grpc.pb.go`.

- [ ] **Step 4: Compile generated package**

Run:

```bash
cd go && go test ./gen/proto/relaycontrolv1
```

Expected: PASS, or `? github.com/termix/termix/go/gen/proto/relaycontrolv1 [no test files]`.

- [ ] **Step 5: Commit**

```bash
git add Makefile proto/relay_control.proto go/gen/proto/relaycontrolv1
git commit -m "Define the relay-control gRPC contract" -m $'Relay and control need an internal service contract before replacing the current REST authorizer path. The proto defines the full spec-listed surface while leaving implementation scope to later adapter tasks.\n\nConstraint: Protobuf contracts live under /proto and generated Go code is committed\nConfidence: high\nScope-risk: narrow\nDirective: Extend relay_control.proto additively; do not repurpose field numbers\nTested: make generate; cd go && go test ./gen/proto/relaycontrolv1\nNot-tested: Server/client behavior is covered by follow-on adapter tasks'
```

## Task 2: Add Reusable Token Parsing and gRPC Error Mapping

**Files:**
- Modify: `go/internal/auth/tokens.go`
- Create: `go/internal/relaycontrol/errors.go`
- Test: `go/internal/relaycontrol/server_test.go`

- [ ] **Step 1: Write failing tests for token parsing and error mapping**

Create `go/internal/relaycontrol/server_test.go` with these initial tests:

```go
package relaycontrol

import (
	"errors"
	"testing"
	"time"

	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/control"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParseAccessTokenForRelayControl(t *testing.T) {
	token, err := auth.IssueAccessToken("signing-key", "user-1", "device-1", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	claims, err := auth.ParseAccessToken("signing-key", token)
	if err != nil {
		t.Fatalf("ParseAccessToken returned error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("expected user-1, got %q", claims.UserID)
	}
	if claims.DeviceID != "device-1" {
		t.Fatalf("expected device-1, got %q", claims.DeviceID)
	}
}

func TestGRPCErrorUsesStableReasonMessage(t *testing.T) {
	err := grpcError(control.ErrAlreadyControlled)
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T", err)
	}
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %s", st.Code())
	}
	if st.Message() != "already_controlled" {
		t.Fatalf("expected already_controlled message, got %q", st.Message())
	}
}

func TestGRPCErrorMapsUnknownErrorsToInternal(t *testing.T) {
	err := grpcError(errors.New("database unavailable"))
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T", err)
	}
	if st.Code() != codes.Internal {
		t.Fatalf("expected Internal, got %s", st.Code())
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
cd go && go test ./internal/relaycontrol -run 'TestParseAccessTokenForRelayControl|TestGRPCError' -v
```

Expected: FAIL because `auth.ParseAccessToken` and `grpcError` do not exist yet.

- [ ] **Step 3: Add reusable access-token parsing**

Append this function to `go/internal/auth/tokens.go`:

```go
func ParseAccessToken(signingKey, tokenString string) (AccessClaims, error) {
	if signingKey == "" {
		return AccessClaims{}, errors.New("signing key is required")
	}
	if tokenString == "" {
		return AccessClaims{}, errors.New("access token is required")
	}

	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(signingKey), nil
	})
	if err != nil || token == nil || !token.Valid {
		if err != nil {
			return AccessClaims{}, err
		}
		return AccessClaims{}, errors.New("invalid access token")
	}
	if claims.UserID == "" || claims.DeviceID == "" {
		return AccessClaims{}, errors.New("missing bearer claims")
	}
	return *claims, nil
}
```

- [ ] **Step 4: Reuse parser from Gin middleware**

Replace the parse block inside `go/internal/auth/middleware.go` with:

```go
		claims, err := ParseAccessToken(signingKey, tokenString)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("device_id", claims.DeviceID)
		c.Next()
```

Then remove the unused `github.com/golang-jwt/jwt/v5` import from `go/internal/auth/middleware.go`.

- [ ] **Step 5: Add relay-control error helpers**

Create `go/internal/relaycontrol/errors.go`:

```go
package relaycontrol

import (
	"errors"

	"github.com/termix/termix/go/internal/control"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const reasonMetadataKey = "termix-denial-reason"

func grpcError(err error) error {
	reason, code := reasonAndCode(err)
	return status.Error(code, reason)
}

func reasonAndCode(err error) (string, codes.Code) {
	switch {
	case errors.Is(err, control.ErrUnauthorized):
		return "unauthorized", codes.Unauthenticated
	case errors.Is(err, control.ErrNotFound):
		return "not_found", codes.NotFound
	case errors.Is(err, control.ErrSessionNotControllable):
		return "session_not_controllable", codes.FailedPrecondition
	case errors.Is(err, control.ErrAlreadyControlled):
		return "already_controlled", codes.FailedPrecondition
	case errors.Is(err, control.ErrStaleLease):
		return "stale_lease", codes.FailedPrecondition
	default:
		return "internal", codes.Internal
	}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
```

- [ ] **Step 6: Run focused tests**

Run:

```bash
cd go && go test ./internal/auth ./internal/relaycontrol -run 'TestParseAccessTokenForRelayControl|TestGRPCError' -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go/internal/auth go/internal/relaycontrol
git commit -m "Share access-token parsing with internal adapters" -m $'Relay-control gRPC needs the same bearer claims that the REST middleware already derives. The auth package now exposes reusable parsing and the relay-control package centralizes stable denial reason mapping.\n\nConstraint: gRPC adapters must not duplicate REST auth parsing logic\nRejected: Parse JWTs directly in relaycontrol server | would fork bearer validation behavior\nConfidence: high\nScope-risk: narrow\nDirective: Keep denial reason strings stable because relay maps them to WSS control.denied payloads\nTested: cd go && go test ./internal/auth ./internal/relaycontrol -run '\\''TestParseAccessTokenForRelayControl|TestGRPCError'\\'' -v\nNot-tested: Full gRPC server/client flow is covered by later tasks'
```

## Task 3: Implement the Control-Side gRPC Server Adapter

**Files:**
- Modify: `go/internal/relaycontrol/server_test.go`
- Create: `go/internal/relaycontrol/server.go`

- [ ] **Step 1: Add server adapter tests**

Append to `go/internal/relaycontrol/server_test.go`:

```go
func TestServerAuthorizeWatchAndLeaseFlow(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServer(repo, "signing-key", ServerConfig{
		LeaseTTL: 30 * time.Second,
		Now: func() time.Time {
			return time.Date(2026, 4, 24, 8, 0, 0, 0, time.UTC)
		},
	})

	token, err := auth.IssueAccessToken("signing-key", repo.userID, repo.controllerDeviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	watch, err := svc.AuthorizeSessionWatch(t.Context(), &relaycontrolv1.AuthorizeSessionWatchRequest{
		AccessToken: token,
		SessionId:   repo.sessionID,
	})
	if err != nil {
		t.Fatalf("AuthorizeSessionWatch returned error: %v", err)
	}
	if watch.SessionId != repo.sessionID {
		t.Fatalf("expected session id %s, got %s", repo.sessionID, watch.SessionId)
	}

	acquired, err := svc.AcquireControlLease(t.Context(), &relaycontrolv1.AcquireControlLeaseRequest{
		AccessToken: token,
		SessionId:   repo.sessionID,
	})
	if err != nil {
		t.Fatalf("AcquireControlLease returned error: %v", err)
	}
	if acquired.LeaseVersion != 1 {
		t.Fatalf("expected acquire lease version 1, got %d", acquired.LeaseVersion)
	}
	if acquired.RenewAfterSeconds != 15 {
		t.Fatalf("expected renew_after_seconds 15, got %d", acquired.RenewAfterSeconds)
	}

	renewed, err := svc.RenewControlLease(t.Context(), &relaycontrolv1.RenewControlLeaseRequest{
		AccessToken:  token,
		SessionId:    repo.sessionID,
		LeaseVersion: acquired.LeaseVersion,
	})
	if err != nil {
		t.Fatalf("RenewControlLease returned error: %v", err)
	}
	if renewed.LeaseVersion != 2 {
		t.Fatalf("expected renew lease version 2, got %d", renewed.LeaseVersion)
	}

	released, err := svc.ReleaseControlLease(t.Context(), &relaycontrolv1.ReleaseControlLeaseRequest{
		AccessToken:  token,
		SessionId:    repo.sessionID,
		LeaseVersion: renewed.LeaseVersion,
	})
	if err != nil {
		t.Fatalf("ReleaseControlLease returned error: %v", err)
	}
	if !released.Released {
		t.Fatal("expected release response to indicate released")
	}
}

func TestServerDenialsAndDeferredMethods(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServer(repo, "signing-key", ServerConfig{})

	_, err := svc.AuthorizeSessionWatch(t.Context(), &relaycontrolv1.AuthorizeSessionWatchRequest{
		AccessToken: "not-a-token",
		SessionId:   repo.sessionID,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for invalid token, got %s", status.Code(err))
	}

	token, err := auth.IssueAccessToken("signing-key", repo.userID, repo.controllerDeviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}
	_, err = svc.RenewControlLease(t.Context(), &relaycontrolv1.RenewControlLeaseRequest{
		AccessToken:  token,
		SessionId:    repo.sessionID,
		LeaseVersion: 99,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition for stale lease, got %s", status.Code(err))
	}

	_, err = svc.ValidateAccessToken(t.Context(), &relaycontrolv1.ValidateAccessTokenRequest{AccessToken: token})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected ValidateAccessToken to be Unimplemented, got %s", status.Code(err))
	}
	_, err = svc.MarkConnectionOpened(t.Context(), &relaycontrolv1.MarkConnectionOpenedRequest{AccessToken: token})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected MarkConnectionOpened to be Unimplemented, got %s", status.Code(err))
	}
	_, err = svc.MarkConnectionClosed(t.Context(), &relaycontrolv1.MarkConnectionClosedRequest{AccessToken: token})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected MarkConnectionClosed to be Unimplemented, got %s", status.Code(err))
	}
}
```

Also add these imports to the file:

```go
import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/control"
	"github.com/termix/termix/go/internal/persistence"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)
```

Add the fake repository in the same file:

```go
type fakeRepo struct {
	userID             string
	hostDeviceID       string
	controllerDeviceID string
	sessionID          string
	lease              persistence.ControlLease
	hasLease           bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		userID:             "11111111-1111-1111-1111-111111111111",
		hostDeviceID:       "22222222-2222-2222-2222-222222222222",
		controllerDeviceID: "33333333-3333-3333-3333-333333333333",
		sessionID:          "44444444-4444-4444-4444-444444444444",
	}
}

func (f *fakeRepo) GetSessionForUser(_ context.Context, sessionID, userID string) (persistence.Session, error) {
	if sessionID != f.sessionID || userID != f.userID {
		return persistence.Session{}, pgx.ErrNoRows
	}
	return persistence.Session{
		ID:              f.sessionID,
		UserID:          f.userID,
		HostDeviceID:    f.hostDeviceID,
		Tool:            "claude",
		LaunchCommand:   "claude",
		Cwd:             "/tmp/project",
		CwdLabel:        "project",
		TmuxSessionName: "termix_" + f.sessionID,
		Status:          "running",
	}, nil
}

func (f *fakeRepo) GetDeviceForUser(_ context.Context, deviceID, userID string) (persistence.Device, error) {
	if deviceID != f.controllerDeviceID || userID != f.userID {
		return persistence.Device{}, pgx.ErrNoRows
	}
	return persistence.Device{ID: deviceID, UserID: userID, DeviceType: "android", Platform: "android", Label: "phone"}, nil
}

func (f *fakeRepo) GetActiveControlLease(_ context.Context, sessionID string, now time.Time) (persistence.ControlLease, bool, error) {
	if sessionID != f.sessionID || !f.hasLease || !f.lease.ExpiresAt.After(now) {
		return persistence.ControlLease{}, false, nil
	}
	return f.lease, true, nil
}

func (f *fakeRepo) UpsertControlLease(_ context.Context, params persistence.UpsertControlLeaseParams) (persistence.ControlLease, error) {
	version := int64(1)
	if f.hasLease {
		version = f.lease.LeaseVersion + 1
	}
	f.lease = persistence.ControlLease{
		SessionID:          params.SessionID,
		ControllerDeviceID: params.ControllerDeviceID,
		LeaseVersion:       version,
		GrantedAt:          params.Now,
		ExpiresAt:          params.ExpiresAt,
	}
	f.hasLease = true
	return f.lease, nil
}

func (f *fakeRepo) RenewControlLease(_ context.Context, params persistence.RenewControlLeaseParams) (persistence.ControlLease, error) {
	if !f.hasLease || f.lease.LeaseVersion != params.LeaseVersion || f.lease.ControllerDeviceID != params.ControllerDeviceID || !f.lease.ExpiresAt.After(params.Now) {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}
	f.lease.LeaseVersion++
	f.lease.ExpiresAt = params.ExpiresAt
	return f.lease, nil
}

func (f *fakeRepo) ReleaseControlLease(_ context.Context, params persistence.ReleaseControlLeaseParams) (persistence.ControlLease, error) {
	if !f.hasLease || f.lease.LeaseVersion != params.LeaseVersion || f.lease.ControllerDeviceID != params.ControllerDeviceID {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}
	lease := f.lease
	f.hasLease = false
	return lease, nil
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
cd go && go test ./internal/relaycontrol -run 'TestServerAuthorizeWatchAndLeaseFlow|TestServerDenialsAndDeferredMethods' -v
```

Expected: FAIL because `NewServer`, `ServerConfig`, and server methods are not implemented.

- [ ] **Step 3: Implement server adapter**

Create `go/internal/relaycontrol/server.go`:

```go
package relaycontrol

import (
	"context"
	"time"

	"github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/control"
	"github.com/termix/termix/go/internal/persistence"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultLeaseTTL = 30 * time.Second

type ServerConfig struct {
	LeaseTTL time.Duration
	Now      func() time.Time
}

type Server struct {
	relaycontrolv1.UnimplementedRelayControlServiceServer
	signingKey   string
	leaseTTL     time.Duration
	leaseService *control.LeaseService
	repo         control.LeaseRepository
}

func NewServer(repo control.LeaseRepository, signingKey string, cfg ServerConfig) *Server {
	ttl := cfg.LeaseTTL
	if ttl <= 0 {
		ttl = defaultLeaseTTL
	}
	return &Server{
		signingKey: signingKey,
		leaseTTL:   ttl,
		leaseService: control.NewLeaseService(repo, control.LeaseServiceConfig{
			TTL: ttl,
			Now: cfg.Now,
		}),
		repo: repo,
	}
}

func (s *Server) AuthorizeSessionWatch(ctx context.Context, req *relaycontrolv1.AuthorizeSessionWatchRequest) (*relaycontrolv1.AuthorizeSessionWatchResponse, error) {
	actor, err := s.actor(req.GetAccessToken())
	if err != nil {
		return nil, grpcError(control.ErrUnauthorized)
	}
	session, err := s.repo.GetSessionForUser(ctx, req.GetSessionId(), actor.UserID)
	if err != nil {
		return nil, grpcError(control.ErrNotFound)
	}
	return &relaycontrolv1.AuthorizeSessionWatchResponse{
		SessionId: session.ID,
		UserId:    session.UserID,
	}, nil
}

func (s *Server) AcquireControlLease(ctx context.Context, req *relaycontrolv1.AcquireControlLeaseRequest) (*relaycontrolv1.ControlLeaseResponse, error) {
	actor, err := s.actor(req.GetAccessToken())
	if err != nil {
		return nil, grpcError(control.ErrUnauthorized)
	}
	lease, err := s.leaseService.Acquire(ctx, actor, req.GetSessionId())
	if err != nil {
		return nil, grpcError(err)
	}
	return s.leaseResponse(lease), nil
}

func (s *Server) RenewControlLease(ctx context.Context, req *relaycontrolv1.RenewControlLeaseRequest) (*relaycontrolv1.ControlLeaseResponse, error) {
	actor, err := s.actor(req.GetAccessToken())
	if err != nil {
		return nil, grpcError(control.ErrUnauthorized)
	}
	lease, err := s.leaseService.Renew(ctx, actor, req.GetSessionId(), req.GetLeaseVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return s.leaseResponse(lease), nil
}

func (s *Server) ReleaseControlLease(ctx context.Context, req *relaycontrolv1.ReleaseControlLeaseRequest) (*relaycontrolv1.ReleaseControlLeaseResponse, error) {
	actor, err := s.actor(req.GetAccessToken())
	if err != nil {
		return nil, grpcError(control.ErrUnauthorized)
	}
	lease, err := s.leaseService.Release(ctx, actor, req.GetSessionId(), req.GetLeaseVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &relaycontrolv1.ReleaseControlLeaseResponse{
		SessionId:    lease.SessionID,
		LeaseVersion: lease.LeaseVersion,
		Released:     true,
	}, nil
}

func (s *Server) ValidateAccessToken(context.Context, *relaycontrolv1.ValidateAccessTokenRequest) (*relaycontrolv1.ValidateAccessTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "validate access token is deferred")
}

func (s *Server) MarkConnectionOpened(context.Context, *relaycontrolv1.MarkConnectionOpenedRequest) (*relaycontrolv1.MarkConnectionOpenedResponse, error) {
	return nil, status.Error(codes.Unimplemented, "connection lifecycle tracking is deferred")
}

func (s *Server) MarkConnectionClosed(context.Context, *relaycontrolv1.MarkConnectionClosedRequest) (*relaycontrolv1.MarkConnectionClosedResponse, error) {
	return nil, status.Error(codes.Unimplemented, "connection lifecycle tracking is deferred")
}

func (s *Server) actor(accessToken string) (control.ControlActor, error) {
	claims, err := auth.ParseAccessToken(s.signingKey, accessToken)
	if err != nil {
		return control.ControlActor{}, err
	}
	return control.ControlActor{
		UserID:   claims.UserID,
		DeviceID: claims.DeviceID,
	}, nil
}

func (s *Server) leaseResponse(lease persistence.ControlLease) *relaycontrolv1.ControlLeaseResponse {
	return &relaycontrolv1.ControlLeaseResponse{
		SessionId:          lease.SessionID,
		ControllerDeviceId: lease.ControllerDeviceID,
		LeaseVersion:       lease.LeaseVersion,
		GrantedAt:          lease.GrantedAt.UTC().Format(time.RFC3339),
		ExpiresAt:          lease.ExpiresAt.UTC().Format(time.RFC3339),
		RenewAfterSeconds:  int32(control.RenewAfterSeconds(s.leaseTTL)),
	}
}
```

- [ ] **Step 4: Run focused server tests**

Run:

```bash
cd go && go test ./internal/relaycontrol -run 'TestServerAuthorizeWatchAndLeaseFlow|TestServerDenialsAndDeferredMethods' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/internal/relaycontrol go/internal/auth
git commit -m "Serve relay-control lease decisions over gRPC" -m $'The control plane now exposes the Android backend watch/control subset through the internal relay-control gRPC service. The adapter validates bearer tokens and delegates lease policy to the existing control service.\n\nConstraint: termix-control remains the source of truth for sessions, devices, and leases\nRejected: Move lease checks into the gRPC handler | would duplicate policy outside go/internal/control\nConfidence: high\nScope-risk: moderate\nDirective: Deferred RelayControlService RPCs intentionally return Unimplemented until lifecycle tracking has real persistence semantics\nTested: cd go && go test ./internal/relaycontrol -run '\\''TestServerAuthorizeWatchAndLeaseFlow|TestServerDenialsAndDeferredMethods'\\'' -v\nNot-tested: Networked relay client calls are covered by the next task'
```

## Task 4: Implement the Relay-Side gRPC Client Adapter

**Files:**
- Modify: `go/internal/relaycontrol/client_test.go`
- Create: `go/internal/relaycontrol/client.go`

- [ ] **Step 1: Write failing client adapter tests**

Create `go/internal/relaycontrol/client_test.go`:

```go
package relaycontrol

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/relay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestClientAcquireMapsGrant(t *testing.T) {
	client, cleanup := newTestClient(t, &fakeRelayControlService{})
	defer cleanup()

	grant, err := client.AcquireControl(t.Context(), "access-token", "session-1")
	if err != nil {
		t.Fatalf("AcquireControl returned error: %v", err)
	}
	if grant.SessionID != "session-1" {
		t.Fatalf("expected session-1, got %q", grant.SessionID)
	}
	if grant.LeaseVersion != 7 {
		t.Fatalf("expected lease version 7, got %d", grant.LeaseVersion)
	}
	if grant.RenewAfterSeconds != 15 {
		t.Fatalf("expected renew_after_seconds 15, got %d", grant.RenewAfterSeconds)
	}
}

func TestClientMapsDeniedReasons(t *testing.T) {
	client, cleanup := newTestClient(t, &fakeRelayControlService{acquireErrReason: "already_controlled"})
	defer cleanup()

	_, err := client.AcquireControl(t.Context(), "access-token", "session-1")
	var denied relay.ErrControlDenied
	if !errors.As(err, &denied) {
		t.Fatalf("expected ErrControlDenied, got %T %[1]v", err)
	}
	if denied.Reason != "already_controlled" {
		t.Fatalf("expected already_controlled reason, got %q", denied.Reason)
	}
}

func TestClientLeavesUnknownErrorsAsErrors(t *testing.T) {
	client, cleanup := newTestClient(t, &fakeRelayControlService{internalAcquireFailure: true})
	defer cleanup()

	_, err := client.AcquireControl(t.Context(), "access-token", "session-1")
	if err == nil {
		t.Fatal("expected error")
	}
	var denied relay.ErrControlDenied
	if errors.As(err, &denied) {
		t.Fatalf("expected ordinary error, got denial: %#v", denied)
	}
}
```

Add required imports if your editor does not do it automatically:

```go
import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/relay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)
```

Add test server helpers:

```go
type fakeRelayControlService struct {
	relaycontrolv1.UnimplementedRelayControlServiceServer
	acquireErrReason      string
	internalAcquireFailure bool
}

func (f *fakeRelayControlService) AuthorizeSessionWatch(context.Context, *relaycontrolv1.AuthorizeSessionWatchRequest) (*relaycontrolv1.AuthorizeSessionWatchResponse, error) {
	return &relaycontrolv1.AuthorizeSessionWatchResponse{SessionId: "session-1", UserId: "user-1"}, nil
}

func (f *fakeRelayControlService) AcquireControlLease(ctx context.Context, req *relaycontrolv1.AcquireControlLeaseRequest) (*relaycontrolv1.ControlLeaseResponse, error) {
	if f.internalAcquireFailure {
		return nil, status.Error(codes.Internal, "database unavailable")
	}
	if f.acquireErrReason != "" {
		_ = grpc.SetHeader(ctx, metadata.Pairs(reasonMetadataKey, f.acquireErrReason))
		return nil, status.Error(codes.FailedPrecondition, f.acquireErrReason)
	}
	return &relaycontrolv1.ControlLeaseResponse{
		SessionId:          req.GetSessionId(),
		ControllerDeviceId: "device-1",
		LeaseVersion:       7,
		GrantedAt:          time.Date(2026, 4, 24, 8, 0, 0, 0, time.UTC).Format(time.RFC3339),
		ExpiresAt:          time.Date(2026, 4, 24, 8, 0, 30, 0, time.UTC).Format(time.RFC3339),
		RenewAfterSeconds:  15,
	}, nil
}

func (f *fakeRelayControlService) RenewControlLease(_ context.Context, req *relaycontrolv1.RenewControlLeaseRequest) (*relaycontrolv1.ControlLeaseResponse, error) {
	return &relaycontrolv1.ControlLeaseResponse{
		SessionId:          req.GetSessionId(),
		ControllerDeviceId: "device-1",
		LeaseVersion:       req.GetLeaseVersion() + 1,
		GrantedAt:          time.Date(2026, 4, 24, 8, 0, 0, 0, time.UTC).Format(time.RFC3339),
		ExpiresAt:          time.Date(2026, 4, 24, 8, 0, 30, 0, time.UTC).Format(time.RFC3339),
		RenewAfterSeconds:  15,
	}, nil
}

func (f *fakeRelayControlService) ReleaseControlLease(context.Context, *relaycontrolv1.ReleaseControlLeaseRequest) (*relaycontrolv1.ReleaseControlLeaseResponse, error) {
	return &relaycontrolv1.ReleaseControlLeaseResponse{SessionId: "session-1", LeaseVersion: 7, Released: true}, nil
}

func newTestClient(t *testing.T, impl relaycontrolv1.RelayControlServiceServer) (*Client, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	relaycontrolv1.RegisterRelayControlServiceServer(server, impl)
	go func() {
		_ = server.Serve(listener)
	}()

	conn, err := grpc.Dial(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		_ = listener.Close()
		t.Fatalf("dial: %v", err)
	}
	return NewClient(conn), func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
cd go && go test ./internal/relaycontrol -run 'TestClient' -v
```

Expected: FAIL because `Client` and `NewClient` do not exist.

- [ ] **Step 3: Implement client adapter**

Create `go/internal/relaycontrol/client.go`:

```go
package relaycontrol

import (
	"context"
	"time"

	"github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/relay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Client struct {
	client relaycontrolv1.RelayControlServiceClient
}

func NewClient(conn grpc.ClientConnInterface) *Client {
	return &Client{client: relaycontrolv1.NewRelayControlServiceClient(conn)}
}

func (c *Client) AuthorizeWatch(ctx context.Context, accessToken string, sessionID string) error {
	var header metadata.MD
	_, err := c.client.AuthorizeSessionWatch(ctx, &relaycontrolv1.AuthorizeSessionWatchRequest{
		AccessToken: accessToken,
		SessionId:   sessionID,
	}, grpc.Header(&header))
	return mapClientError(err, header)
}

func (c *Client) AcquireControl(ctx context.Context, accessToken string, sessionID string) (relay.ControlGrant, error) {
	var header metadata.MD
	resp, err := c.client.AcquireControlLease(ctx, &relaycontrolv1.AcquireControlLeaseRequest{
		AccessToken: accessToken,
		SessionId:   sessionID,
	}, grpc.Header(&header))
	if err != nil {
		return relay.ControlGrant{}, mapClientError(err, header)
	}
	return grantFromProto(resp)
}

func (c *Client) RenewControl(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) (relay.ControlGrant, error) {
	var header metadata.MD
	resp, err := c.client.RenewControlLease(ctx, &relaycontrolv1.RenewControlLeaseRequest{
		AccessToken:  accessToken,
		SessionId:    sessionID,
		LeaseVersion: leaseVersion,
	}, grpc.Header(&header))
	if err != nil {
		return relay.ControlGrant{}, mapClientError(err, header)
	}
	return grantFromProto(resp)
}

func (c *Client) ReleaseControl(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) error {
	var header metadata.MD
	_, err := c.client.ReleaseControlLease(ctx, &relaycontrolv1.ReleaseControlLeaseRequest{
		AccessToken:  accessToken,
		SessionId:    sessionID,
		LeaseVersion: leaseVersion,
	}, grpc.Header(&header))
	return mapClientError(err, header)
}

func grantFromProto(resp *relaycontrolv1.ControlLeaseResponse) (relay.ControlGrant, error) {
	expiresAt, err := time.Parse(time.RFC3339, resp.GetExpiresAt())
	if err != nil {
		return relay.ControlGrant{}, err
	}
	return relay.ControlGrant{
		SessionID:         resp.GetSessionId(),
		LeaseVersion:      resp.GetLeaseVersion(),
		ExpiresAt:         expiresAt,
		RenewAfterSeconds: int(resp.GetRenewAfterSeconds()),
	}, nil
}

func mapClientError(err error, header metadata.MD) error {
	if err == nil {
		return nil
	}
	reason := first(header.Get(reasonMetadataKey))
	if reason == "" {
		if st, ok := status.FromError(err); ok {
			reason = st.Message()
		}
	}
	if isDeniedReason(reason) {
		return relay.ErrControlDenied{
			Reason:  reason,
			Message: deniedMessage(reason),
		}
	}
	return err
}

func isDeniedReason(reason string) bool {
	switch reason {
	case "unauthorized", "not_found", "already_controlled", "invalid_request", "stale_lease", "session_not_controllable":
		return true
	default:
		return false
	}
}

func deniedMessage(reason string) string {
	switch reason {
	case "unauthorized":
		return "unauthorized"
	case "not_found":
		return "session not found"
	case "already_controlled":
		return "control lease is held"
	case "stale_lease":
		return "stale control lease"
	case "session_not_controllable":
		return "session is not controllable"
	case "invalid_request":
		return "invalid control request"
	default:
		return reason
	}
}

```

- [ ] **Step 4: Run focused client tests**

Run:

```bash
cd go && go test ./internal/relaycontrol -run 'TestClient' -v
```

Expected: PASS.

- [ ] **Step 5: Run relaycontrol package tests**

Run:

```bash
cd go && go test ./internal/relaycontrol -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go/internal/relaycontrol
git commit -m "Adapt relay authorization to relay-control gRPC" -m $'The relay can now satisfy its SessionAuthorizer dependency through the internal relay-control gRPC service. The adapter maps proto lease responses to relay grants and preserves stable denial reasons for WSS responses.\n\nConstraint: relay package must remain transport-agnostic\nRejected: Call generated gRPC clients directly from relay server handlers | would couple WSS routing to control transport details\nConfidence: high\nScope-risk: moderate\nDirective: Keep REST and gRPC denial mappings equivalent while both transports exist\nTested: cd go && go test ./internal/relaycontrol -v\nNot-tested: Binary input forwarding through WSS is covered by integration tests'
```

## Task 5: Wire Service Startup with gRPC Preferred and REST Fallback

**Files:**
- Modify: `go/cmd/termix-control/main.go`
- Modify: `go/cmd/termix-relay/main.go`

- [ ] **Step 1: Run current command package tests**

Run:

```bash
cd go && go test ./cmd/termix-control ./cmd/termix-relay
```

Expected: PASS or `[no test files]`.

- [ ] **Step 2: Wire `termix-control` to serve REST and gRPC**

Replace `go/cmd/termix-control/main.go` with:

```go
package main

import (
	"context"
	"log"
	"net"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/persistence"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"google.golang.org/grpc"
)

func main() {
	dsn := os.Getenv("TERMIX_POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("TERMIX_POSTGRES_DSN is required")
	}

	signingKey := os.Getenv("TERMIX_JWT_SIGNING_KEY")
	if signingKey == "" {
		log.Fatal("TERMIX_JWT_SIGNING_KEY is required")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	store := persistence.New(pool)
	if addr := os.Getenv("TERMIX_CONTROL_RELAY_GRPC_ADDR"); addr != "" {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
		grpcServer := grpc.NewServer()
		relaycontrolv1.RegisterRelayControlServiceServer(grpcServer, relaycontrol.NewServer(store, signingKey, relaycontrol.ServerConfig{}))
		go func() {
			log.Printf("relay-control gRPC listening on %s", addr)
			if err := grpcServer.Serve(listener); err != nil {
				log.Fatal(err)
			}
		}()
		defer grpcServer.GracefulStop()
	}

	restAddr := os.Getenv("TERMIX_CONTROL_REST_ADDR")
	if restAddr == "" {
		restAddr = ":8080"
	}
	router := controlapi.NewRouter(store, signingKey)
	if err := router.Run(restAddr); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 3: Wire `termix-relay` to prefer gRPC**

Replace `go/cmd/termix-relay/main.go` with:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/termix/termix/go/internal/controlapi"
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
	if grpcAddr := os.Getenv("TERMIX_RELAY_CONTROL_GRPC_ADDR"); grpcAddr != "" {
		conn, err := grpc.DialContext(context.Background(), grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, func() {}, err
		}
		return relaycontrol.NewClient(conn), func() { _ = conn.Close() }, nil
	}

	controlURL := os.Getenv("TERMIX_CONTROL_API_URL")
	if controlURL == "" {
		return nil, func() {}, errors.New("TERMIX_RELAY_CONTROL_GRPC_ADDR or TERMIX_CONTROL_API_URL is required")
	}
	controlClient, err := controlapi.New(controlURL, http.DefaultTransport)
	if err != nil {
		return nil, func() {}, err
	}
	return relay.NewControlAuthorizer(controlClient), func() {}, nil
}
```

Add the missing import:

```go
	"errors"
```

- [ ] **Step 4: Run gofmt**

Run:

```bash
cd go && gofmt -w ./cmd/termix-control ./cmd/termix-relay ./internal/relaycontrol ./internal/auth
```

Expected: no output.

- [ ] **Step 5: Run command package tests**

Run:

```bash
cd go && go test ./cmd/termix-control ./cmd/termix-relay ./internal/relaycontrol -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go/cmd/termix-control go/cmd/termix-relay go/internal/relaycontrol go/internal/auth
git commit -m "Prefer relay-control gRPC for relay authorization" -m $'The control service can now expose the internal relay-control gRPC listener, and relay selects that adapter when configured while retaining the existing REST fallback.\n\nConstraint: Android integration needs a reversible backend rollout path\nRejected: Require both REST and gRPC configuration | creates ambiguous source selection and unnecessary deployment coupling\nConfidence: medium\nScope-risk: moderate\nDirective: When TERMIX_RELAY_CONTROL_GRPC_ADDR is set, relay authorization must use gRPC and not public REST\nTested: cd go && go test ./cmd/termix-control ./cmd/termix-relay ./internal/relaycontrol -v\nNot-tested: End-to-end WSS routing with gRPC authorizer is covered by the next task'
```

## Task 6: Prove the WSS Backend Loop Through gRPC

**Files:**
- Modify: `go/tests/relay_integration_test.go`

- [ ] **Step 1: Add gRPC-backed relay integration test**

Append to `go/tests/relay_integration_test.go`:

```go
func TestRelayControlInputBackendLoopWithGRPCAuthorizer(t *testing.T) {
	grpcAuthorizer := newFakeGRPCAuthorizer(t)
	defer grpcAuthorizer.cleanup()

	server := relay.NewServer(grpcAuthorizer.client)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type inputCall struct {
		sessionID string
		payload   []byte
	}
	inputCalls := make(chan inputCall, 1)

	daemonClient := relayclient.New("ws"+httpServer.URL[len("http"):]+"/ws", "daemon-token", "device-1")
	daemonClient.SetInputHandler(func(_ context.Context, sessionID string, payload []byte) error {
		inputCalls <- inputCall{
			sessionID: sessionID,
			payload:   append([]byte(nil), payload...),
		}
		return nil
	})
	if err := daemonClient.Connect(ctx); err != nil {
		t.Fatalf("connect daemon relay client: %v", err)
	}
	if err := daemonClient.AnnounceSession(ctx, session.LocalSession{SessionID: "session-1"}); err != nil {
		t.Fatalf("announce session: %v", err)
	}

	viewer := watchViewer(t, ctx, httpServer.URL, "viewer-token")
	defer viewer.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "grpc-acquire-loop",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	granted := readEnvelope(t, ctx, viewer, relayproto.TypeControlGranted)
	if granted.RequestID != "grpc-acquire-loop" {
		t.Fatalf("expected acquire request id grpc-acquire-loop, got %q", granted.RequestID)
	}

	writeBinaryFrame(t, ctx, viewer, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(1),
		},
		Payload: []byte("id\n"),
	})

	select {
	case got := <-inputCalls:
		if got.sessionID != "session-1" {
			t.Fatalf("expected session_id session-1, got %q", got.sessionID)
		}
		if string(got.payload) != "id\n" {
			t.Fatalf("expected payload %q, got %q", "id\n", got.payload)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for daemon input handler: %v", ctx.Err())
	}
}
```

Add helper types in the same file:

```go
type fakeGRPCAuthorizer struct {
	client  relay.SessionAuthorizer
	cleanup func()
}

func newFakeGRPCAuthorizer(t *testing.T) fakeGRPCAuthorizer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	relaycontrolv1.RegisterRelayControlServiceServer(server, &fakeRelayControlServiceForIntegration{})
	go func() {
		_ = server.Serve(listener)
	}()
	conn, err := grpc.Dial(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		_ = listener.Close()
		t.Fatalf("dial: %v", err)
	}
	return fakeGRPCAuthorizer{
		client: relaycontrol.NewClient(conn),
		cleanup: func() {
			_ = conn.Close()
			server.Stop()
			_ = listener.Close()
		},
	}
}

type fakeRelayControlServiceForIntegration struct {
	relaycontrolv1.UnimplementedRelayControlServiceServer
}

func (f *fakeRelayControlServiceForIntegration) AuthorizeSessionWatch(context.Context, *relaycontrolv1.AuthorizeSessionWatchRequest) (*relaycontrolv1.AuthorizeSessionWatchResponse, error) {
	return &relaycontrolv1.AuthorizeSessionWatchResponse{SessionId: "session-1", UserId: "user-1"}, nil
}

func (f *fakeRelayControlServiceForIntegration) AcquireControlLease(_ context.Context, req *relaycontrolv1.AcquireControlLeaseRequest) (*relaycontrolv1.ControlLeaseResponse, error) {
	return &relaycontrolv1.ControlLeaseResponse{
		SessionId:          req.GetSessionId(),
		ControllerDeviceId: "device-1",
		LeaseVersion:       1,
		GrantedAt:          time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:          time.Now().UTC().Add(30 * time.Second).Format(time.RFC3339),
		RenewAfterSeconds:  15,
	}, nil
}
```

Add imports:

```go
	"net"

	"github.com/termix/termix/go/gen/proto/relaycontrolv1"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
```

- [ ] **Step 2: Run test and verify it fails before adapter wiring**

Run:

```bash
cd go && go test ./tests -run TestRelayControlInputBackendLoopWithGRPCAuthorizer -v
```

Expected before prior tasks are complete: FAIL because relay-control generated code or client adapter is absent. Expected after prior tasks: PASS.

- [ ] **Step 3: Run relay integration tests**

Run:

```bash
cd go && go test ./tests -run 'TestRelayControl.*|TestRelayWatchHandshakeRequestsSnapshotAndFansOutFrames' -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add go/tests/relay_integration_test.go
git commit -m "Prove relay WSS control through gRPC authorizer" -m $'The relay integration suite now exercises the backend control-input loop with a relay-control gRPC authorizer, proving the new internal transport preserves WSS behavior.\n\nConstraint: Android-facing relay protocol must not change in this adapter slice\nRejected: Only test the gRPC client package | would not prove relay still forwards authorized input frames\nConfidence: high\nScope-risk: narrow\nDirective: Keep this test as the guardrail before removing the REST fallback\nTested: cd go && go test ./tests -run '\\''TestRelayControl.*|TestRelayWatchHandshakeRequestsSnapshotAndFansOutFrames'\\'' -v\nNot-tested: Real Android client interaction remains deferred'
```

## Task 7: Final Verification and Ledger Update

**Files:**
- Modify: `docs/PROGRESS.md`

- [ ] **Step 1: Run full generation**

Run:

```bash
make generate
```

Expected: PASS and no unexpected generated diff beyond relay-control protobuf files.

- [ ] **Step 2: Run all Go tests**

Run:

```bash
cd go && go test ./...
```

Expected: PASS. Tests that require `TERMIX_TEST_DATABASE_URL` may skip when the variable is absent.

- [ ] **Step 3: Run Go vet**

Run:

```bash
cd go && go vet ./...
```

Expected: PASS.

- [ ] **Step 4: Update progress ledger**

Modify `docs/PROGRESS.md`:

```markdown
## Completed
- [x] Implement the internal relay-control gRPC adapter for the Android backend watch/control loop.

## Pending
- [ ] Deferred: remove relay REST fallback after Android end-to-end testing validates the gRPC path.
- [ ] Deferred: implement relay-control connection lifecycle RPCs when audit or online presence is scheduled.
```

Keep existing completed and deferred items. Do not delete unrelated pending work.

- [ ] **Step 5: Check working tree and whitespace**

Run:

```bash
git diff --check
git status --short
```

Expected: `git diff --check` has no output. `git status --short` shows only files intentionally changed by this plan.

- [ ] **Step 6: Commit final verification ledger**

```bash
git add docs/PROGRESS.md
git commit -m "Record relay-control gRPC adapter completion" -m $'The project ledger now records the relay-control gRPC adapter as complete and keeps the remaining REST fallback and lifecycle RPC work visible as deferred follow-ups.\n\nConstraint: docs/PROGRESS.md is the repository status source of truth\nConfidence: high\nScope-risk: narrow\nTested: make generate; cd go && go test ./...; cd go && go vet ./...; git diff --check\nNot-tested: Android device end-to-end validation remains deferred'
```

## Self-Review

Spec coverage:

- Full proto surface: Task 1.
- Android-loop RPC implementation: Tasks 3 and 4.
- Deferred RPC behavior: Task 3.
- gRPC preferred with REST fallback: Task 5.
- Relay WSS behavior unchanged: Task 6.
- Verification and progress ledger: Task 7.

Type consistency:

- Generated package name is `relaycontrolv1`.
- Server constructor is `relaycontrol.NewServer(repo, signingKey, relaycontrol.ServerConfig{})`.
- Client constructor is `relaycontrol.NewClient(conn)`.
- Relay still depends on `relay.SessionAuthorizer`.

Known implementation watchpoints:

- Fake repositories use `pgx.ErrNoRows` because `persistence.IsNotFound` already recognizes it.
- Client error mapping reads gRPC header metadata first and falls back to `status.Message()`, so fake gRPC services and real server errors both preserve the same denial reason strings.
- If command packages have no tests, `[no test files]` is acceptable.
