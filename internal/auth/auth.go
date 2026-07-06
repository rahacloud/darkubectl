// Package auth mints and refreshes Console JWTs for the terminal/exec websocket.
//
// The REST API authenticates with an account Api-key, but the exec websocket
// requires a short-lived Console JWT access token, obtained from an email +
// password + TOTP login (SimpleJWT at /api/v1/token/). `darkubectl login` mints
// the pair once and stores the refresh token; access tokens are then refreshed
// on demand without re-entering 2FA until the refresh token expires.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"resty.dev/v3"
)

const (
	tokenPath   = "/api/v1/token/"         //nolint:gosec // G101: URL path, not a credential
	refreshPath = "/api/v1/token/refresh/" //nolint:gosec // G101: URL path, not a credential

	// otpHeader carries the TOTP code. It is advertised by the API's CORS
	// allowlist (Access-Control-Allow-Headers) as `x-otp`.
	//
	// TODO(protocol): confirm from a console-login capture whether the 2FA code
	// travels in this header (assumed) or as a JSON body field (e.g. "otp").
	otpHeader = "x-otp"

	requestTimeout = 30 * time.Second
)

// Errors returned by the authenticator.
var (
	ErrLoginFailed   = errors.New("login failed")
	ErrRefreshFailed = errors.New("token refresh failed")
)

// Tokens is a Console access/refresh JWT pair.
type Tokens struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

// Client mints tokens against the Hamravesh API.
type Client struct {
	http *resty.Client
}

// New builds an auth client for the given API base URL.
func New(baseURL string) *Client {
	rc := resty.New().
		SetBaseURL(strings.TrimRight(baseURL, "/")).
		SetTimeout(requestTimeout).
		SetHeader("Accept", "application/json")
	return &Client{http: rc}
}

// Close releases the underlying transport.
func (c *Client) Close() error { return c.http.Close() }

// Login mints an access/refresh pair from credentials plus a TOTP code.
func (c *Client) Login(ctx context.Context, email, password, otp string) (*Tokens, error) {
	var out Tokens
	var apiErr apiError
	resp, err := c.http.R().SetContext(ctx).
		SetHeader(otpHeader, otp).
		SetBody(map[string]string{"email": email, "password": password}).
		SetResult(&out).
		SetResultError(&apiErr).
		Post(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("login request: %w", err)
	}
	if resp.IsStatusFailure() {
		return nil, fmt.Errorf("%w: %s", ErrLoginFailed, apiErr.message(resp.Status()))
	}
	return &out, nil
}

// Refresh mints a new access token from a stored refresh token.
func (c *Client) Refresh(ctx context.Context, refresh string) (string, error) {
	var out Tokens
	var apiErr apiError
	resp, err := c.http.R().SetContext(ctx).
		SetBody(map[string]string{"refresh": refresh}).
		SetResult(&out).
		SetResultError(&apiErr).
		Post(refreshPath)
	if err != nil {
		return "", fmt.Errorf("refresh request: %w", err)
	}
	if resp.IsStatusFailure() {
		return "", fmt.Errorf("%w: %s", ErrRefreshFailed, apiErr.message(resp.Status()))
	}
	return out.Access, nil
}

type apiError struct {
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

func (e apiError) message(fallback string) string {
	if e.Detail != "" {
		return e.Detail
	}
	return fallback
}
