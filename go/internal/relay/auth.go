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
