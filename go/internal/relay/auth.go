package relay

import (
	"context"
	"strings"

	openapi "github.com/termix/termix/go/gen/openapi"
)

type SessionAuthorizer interface {
	AuthorizeWatch(ctx context.Context, accessToken string, sessionID string) error
}

type ControlSessionClient interface {
	GetSessionForViewer(ctx context.Context, accessToken string, sessionID string) (*openapi.Session, error)
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

func bearerToken(authHeader string) string {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return authHeader
	}
	return token
}
