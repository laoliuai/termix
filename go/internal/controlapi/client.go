package controlapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	openapi "github.com/termix/termix/go/gen/openapi"
)

type Client struct {
	http *openapi.ClientWithResponses
}

type APIError struct {
	Action     string
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string {
	if len(e.Body) > 0 {
		return fmt.Sprintf("%s failed with status %d: %s", e.Action, e.StatusCode, string(e.Body))
	}
	return fmt.Sprintf("%s failed with status %d", e.Action, e.StatusCode)
}

func (e *APIError) Reason() string {
	if len(e.Body) == 0 {
		return ""
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(e.Body, &payload); err != nil {
		return ""
	}
	return payload.Reason
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

func (c *Client) GetSessionForViewer(ctx context.Context, accessToken string, sessionID string) (*openapi.Session, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.GetSessionWithResponse(ctx, id, bearerEditor(accessToken))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, responseError("get session", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

func (c *Client) AcquireControlLease(ctx context.Context, accessToken string, sessionID string) (*openapi.ControlLeaseResponse, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.PostSessionControlAcquireWithResponse(ctx, id, bearerEditor(accessToken))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, responseError("acquire control lease", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

func (c *Client) RenewControlLease(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) (*openapi.ControlLeaseResponse, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.PostSessionControlRenewWithResponse(ctx, id, openapi.ControlLeaseRequest{
		LeaseVersion: leaseVersion,
	}, bearerEditor(accessToken))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, responseError("renew control lease", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200, nil
}

func (c *Client) ReleaseControlLease(ctx context.Context, accessToken string, sessionID string, leaseVersion int64) (*openapi.ReleaseControlLeaseResponse, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.PostSessionControlReleaseWithResponse(ctx, id, openapi.ControlLeaseRequest{
		LeaseVersion: leaseVersion,
	}, bearerEditor(accessToken))
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, responseError("release control lease", resp.StatusCode(), resp.Body)
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
	return &APIError{
		Action:     action,
		StatusCode: statusCode,
		Body:       append([]byte(nil), body...),
	}
}
