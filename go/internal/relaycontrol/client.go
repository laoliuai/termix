package relaycontrol

import (
	"context"
	"errors"
	"time"

	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/relay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const reasonMetadataKey = "termix-denial-reason"

type Client struct {
	client relaycontrolv1.RelayControlServiceClient
}

var _ relay.SessionAuthorizer = (*Client)(nil)

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
	if resp == nil {
		return relay.ControlGrant{}, errors.New("empty control lease response")
	}
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
	if !isDeniedReason(reason) {
		return err
	}
	return relay.ErrControlDenied{
		Reason:  reason,
		Message: deniedMessage(reason),
	}
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

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
