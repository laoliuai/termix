package controlapi

import (
	"context"
	"fmt"
	"net/http"

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
