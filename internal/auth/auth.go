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

	// Refresh retry budget. api.hamravesh.com returns intermittent 500s and bare
	// EOFs from the refresh endpoint, often enough to make the whole CLI look
	// broken: every tenant-scoped command mints an access token first, so one
	// flaky refresh fails the command outright. Observed repeatedly on
	// 2026-08-27, where the identical command succeeded seconds later each time.
	//
	// Four attempts over ~3.5s of backoff. Only transient failures are retried —
	// a rejected refresh token fails immediately, because no amount of waiting
	// turns an expired login into a valid one.
	defaultRefreshAttempts = 4
	defaultRefreshBackoff  = 500 * time.Millisecond

	// serverErrorFloor is the status at and above which a response is treated as
	// the server's fault, and therefore worth retrying.
	serverErrorFloor = 500
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

	// Retry policy for Refresh. Exported-in-package so tests can shrink the
	// backoff; callers get the defaults from New.
	refreshAttempts int
	refreshBackoff  time.Duration
	sleep           func(time.Duration)
}

// New builds an auth client for the given API base URL.
func New(baseURL string) *Client {
	rc := resty.New().
		SetBaseURL(strings.TrimRight(baseURL, "/")).
		SetTimeout(requestTimeout).
		SetHeader("Accept", "application/json")
	return &Client{
		http:            rc,
		refreshAttempts: defaultRefreshAttempts,
		refreshBackoff:  defaultRefreshBackoff,
		sleep:           time.Sleep,
	}
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

// Refresh mints a new access token from a stored refresh token, retrying
// transient upstream failures.
//
// A transport error or a 5xx is the server's problem and is retried with linear
// backoff; anything else (notably a 401 for an expired or revoked refresh token)
// is returned immediately. See defaultRefreshAttempts for why this exists.
func (c *Client) Refresh(ctx context.Context, refresh string) (string, error) {
	attempts := max(c.refreshAttempts, 1)

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			// Linear backoff: 0.5s, 1s, 1.5s. Bounded and short, because a human
			// is usually waiting on the command that triggered this.
			if err := c.wait(ctx, time.Duration(attempt)*c.refreshBackoff); err != nil {
				return "", err
			}
		}
		access, err, retryable := c.refreshOnce(ctx, refresh)
		if err == nil {
			return access, nil
		}
		lastErr = err
		if !retryable {
			return "", err
		}
	}
	return "", fmt.Errorf("%w after %d attempts", lastErr, attempts)
}

// wait sleeps for d, or returns early if the context is cancelled first.
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		c.sleep(d)
		return ctx.Err()
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// refreshOnce performs a single refresh, reporting whether a failure is worth
// retrying.
//
//nolint:revive // the trailing retryable flag reads better here than a sentinel error
func (c *Client) refreshOnce(ctx context.Context, refresh string) (string, error, bool) {
	var out Tokens
	var apiErr apiError
	resp, err := c.http.R().SetContext(ctx).
		SetBody(map[string]string{"refresh": refresh}).
		SetResult(&out).
		SetResultError(&apiErr).
		Post(refreshPath)
	if err != nil {
		// Transport-level failure — a dropped connection or a bare EOF, both of
		// which this endpoint produces. Retry unless the caller gave up.
		return "", fmt.Errorf("refresh request: %w", err), ctx.Err() == nil
	}
	if resp.IsStatusFailure() {
		return "", fmt.Errorf("%w: %s", ErrRefreshFailed, apiErr.message(resp.Status())),
			resp.StatusCode() >= serverErrorFloor
	}
	return out.Access, nil, false
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
