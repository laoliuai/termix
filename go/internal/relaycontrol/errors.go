package relaycontrol

import (
	"errors"

	"github.com/termix/termix/go/internal/control"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func grpcError(err error) error {
	reason, code := reasonAndCode(err)
	return status.Error(code, reason)
}

func invalidRequestError() error {
	return status.Error(codes.InvalidArgument, "invalid_request")
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
