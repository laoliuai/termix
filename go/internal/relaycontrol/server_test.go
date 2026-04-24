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
