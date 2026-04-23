package controlapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	openapi "github.com/termix/termix/go/gen/openapi"
)

type Client struct {
	http *openapi.ClientWithResponses
}

func New(baseURL string, transport http.RoundTripper) (*Client, error) {
	httpClient := &http.Client{Transport: transport}
	c, err := openapi.NewClientWithResponses(baseURL, openapi.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{http: c}, nil
}

func (c *Client) Login(ctx context.Context, req openapi.LoginRequest) (*openapi.LoginResponse, error) {
	resp, err := c.http.PostAuthLoginWithResponse(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		if len(resp.Body) > 0 {
			return nil, fmt.Errorf("login failed with status %d: %s", resp.StatusCode(), string(resp.Body))
		}
		return nil, fmt.Errorf("login failed with status %d", resp.StatusCode())
	}
	return resp.JSON200, nil
}

func (c *Client) CreateHostSession(ctx context.Context, accessToken string, req openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error) {
	resp, err := c.http.PostHostSessionsWithResponse(ctx, req, bearerEditor(accessToken))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, responseError("create host session", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

func (c *Client) UpdateHostSession(ctx context.Context, accessToken string, sessionID string, req openapi.UpdateSessionRequest) (*openapi.Session, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.PatchHostSessionWithResponse(ctx, id, req, bearerEditor(accessToken))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, responseError("update host session", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

func bearerEditor(accessToken string) openapi.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+accessToken)
		return nil
	}
}

func parseUUID(raw string) (openapi_types.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return openapi_types.UUID{}, err
	}
	return openapi_types.UUID(id), nil
}

func responseError(action string, statusCode int, body []byte) error {
	if len(body) > 0 {
		return fmt.Errorf("%s failed with status %d: %s", action, statusCode, string(body))
	}
	return fmt.Errorf("%s failed with status %d", action, statusCode)
}
