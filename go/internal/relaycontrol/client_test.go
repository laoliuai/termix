package relaycontrol

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
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

	grant, err := client.AcquireControl(context.Background(), "access-token", "session-1")
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
	expectedExpiry := time.Date(2026, 4, 24, 8, 0, 30, 0, time.UTC)
	if !grant.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected expiry %s, got %s", expectedExpiry, grant.ExpiresAt)
	}
}

func TestClientMapsDeniedReasons(t *testing.T) {
	client, cleanup := newTestClient(t, &fakeRelayControlService{acquireErrReason: "already_controlled"})
	defer cleanup()

	_, err := client.AcquireControl(context.Background(), "access-token", "session-1")
	var denied relay.ErrControlDenied
	if !errors.As(err, &denied) {
		t.Fatalf("expected ErrControlDenied, got %T %[1]v", err)
	}
	if denied.Reason != "already_controlled" {
		t.Fatalf("expected already_controlled reason, got %q", denied.Reason)
	}
	if denied.Message != "control lease is held" {
		t.Fatalf("expected control lease is held message, got %q", denied.Message)
	}
}

func TestClientLeavesUnknownErrorsAsErrors(t *testing.T) {
	client, cleanup := newTestClient(t, &fakeRelayControlService{internalAcquireFailure: true})
	defer cleanup()

	_, err := client.AcquireControl(context.Background(), "access-token", "session-1")
	if err == nil {
		t.Fatal("expected error")
	}
	var denied relay.ErrControlDenied
	if errors.As(err, &denied) {
		t.Fatalf("expected ordinary error, got denial: %#v", denied)
	}
}

type fakeRelayControlService struct {
	relaycontrolv1.UnimplementedRelayControlServiceServer

	acquireErrReason       string
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
