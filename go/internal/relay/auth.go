package relay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/controlapi"
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

type ControlSessionClient interface {
	GetSessionForViewer(ctx context.Context, accessToken string, sessionID string) (*openapi.Session, error)
	AcquireControlLease(ctx context.Context, accessToken string, sessionID string) (*openapi.ControlLeaseResponse, error)
	RenewControlLease(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) (*openapi.ControlLeaseResponse, error)
	ReleaseControlLease(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) (*openapi.ReleaseControlLeaseResponse, error)
}

type ControlAuthorizer struct {
	client ControlSessionClient
}

func NewControlAuthorizer(client ControlSessionClient) *ControlAuthorizer {
	return &ControlAuthorizer{client: client}
}

func (a *ControlAuthorizer) AuthorizeWatch(ctx context.Context, accessToken string, sessionID string) error {
	_, err := a.client.GetSessionForViewer(ctx, accessToken, sessionID)
	return err
}

func (a *ControlAuthorizer) AcquireControl(ctx context.Context, accessToken string, sessionID string) (ControlGrant, error) {
	lease, err := a.client.AcquireControlLease(ctx, accessToken, sessionID)
	if err != nil {
		return ControlGrant{}, mapControlAPIError(err)
	}
	return mapControlGrant(lease), nil
}

func (a *ControlAuthorizer) RenewControl(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) (ControlGrant, error) {
	lease, err := a.client.RenewControlLease(ctx, accessToken, sessionID, leaseVersion)
	if err != nil {
		return ControlGrant{}, mapControlAPIError(err)
	}
	return mapControlGrant(lease), nil
}

func (a *ControlAuthorizer) ReleaseControl(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) error {
	_, err := a.client.ReleaseControlLease(ctx, accessToken, sessionID, leaseVersion)
	if err != nil {
		return mapControlAPIError(err)
	}
	return nil
}

func mapControlGrant(grant *openapi.ControlLeaseResponse) ControlGrant {
	if grant == nil {
		return ControlGrant{}
	}
	return ControlGrant{
		SessionID:         grant.SessionId.String(),
		LeaseVersion:      grant.LeaseVersion,
		ExpiresAt:         grant.ExpiresAt,
		RenewAfterSeconds: grant.RenewAfterSeconds,
	}
}

func mapControlAPIError(err error) error {
	var apiErr *controlapi.APIError
	if !errors.As(err, &apiErr) {
		return err
	}

	reason := apiErr.Reason()
	if reason == "conflict" {
		reason = "already_controlled"
	}
	if reason == "" {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			reason = "unauthorized"
		case http.StatusNotFound:
			reason = "not_found"
		case http.StatusConflict:
			reason = "already_controlled"
		case http.StatusBadRequest:
			reason = "invalid_request"
		default:
			reason = "invalid_request"
		}
	}

	return ErrControlDenied{
		Reason:  reason,
		Message: deniedMessage(reason),
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
	case "invalid_request":
		return "invalid control request"
	default:
		return reason
	}
}

func bearerToken(authHeader string) string {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return authHeader
	}
	return token
}
