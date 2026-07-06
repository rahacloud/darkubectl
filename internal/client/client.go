// Package client is a thin wrapper over the Hamravesh Darkube REST API.
//
// Authentication is a two-part scheme discovered against the live API:
//
//	Authorization: Api-key <account-token>
//	X-Organization: <tenant-slug>
//
// The account token identifies the user; X-Organization scopes every request to
// one tenant (organization). Requests without a valid X-Organization are rejected
// with 403 permission_denied even though the token itself is valid.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"resty.dev/v3"
)

// DefaultBaseURL is the public Hamravesh API host.
const DefaultBaseURL = "https://api.hamravesh.com"

// requestTimeout bounds every API call.
const requestTimeout = 60 * time.Second

// Client talks to the Darkube API for a single tenant.
type Client struct {
	BaseURL string
	Org     string

	http *resty.Client
}

// New builds a Client. baseURL may be empty to use DefaultBaseURL.
func New(baseURL, token, org string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	rc := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(requestTimeout).
		SetHeader("Accept", "application/json").
		SetHeader("Authorization", "Api-key "+token)
	if org != "" {
		rc.SetHeader("X-Organization", org)
	}

	return &Client{BaseURL: baseURL, Org: org, http: rc}
}

// Close releases the underlying transport (resty v3 clients are closable).
func (c *Client) Close() error { return c.http.Close() }

// APIError is a structured error returned by the API's DRF backend.
type APIError struct {
	StatusCode int    `json:"-"`
	Detail     string `json:"detail"`
	Code       string `json:"code"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("api error %d (%s): %s", e.StatusCode, e.Code, e.Detail)
	}
	return fmt.Sprintf("api error %d: %s", e.StatusCode, e.Detail)
}

// do issues a request and returns the raw response body on 2xx, or an *APIError.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	req := c.http.R().SetContext(ctx)
	if len(query) > 0 {
		req.SetQueryParamsFromValues(query)
	}
	if body != nil {
		req.SetBody(body)
	}

	resp, err := req.Execute(method, path)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	data := resp.Bytes()

	if resp.IsStatusFailure() {
		apiErr := &APIError{StatusCode: resp.StatusCode()}
		if json.Unmarshal(data, apiErr) != nil || apiErr.Detail == "" {
			apiErr.Detail = strings.TrimSpace(string(data))
			if apiErr.Detail == "" {
				apiErr.Detail = resp.Status()
			}
		}
		return nil, apiErr
	}
	return data, nil
}

// getJSON performs a GET and decodes the body into out.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	data, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
