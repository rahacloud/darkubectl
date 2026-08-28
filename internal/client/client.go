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
	"errors"
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

// Auth is an HTTP Authorization header value. The Darkube API accepts two
// schemes: an account Api-key, or a Console JWT (Bearer) obtained from login.
type Auth string

// APIKey builds Api-key–scheme authorization from an account token.
func APIKey(token string) Auth { return Auth("Api-key " + token) }

// BearerToken builds Bearer-scheme (JWT) authorization from a login access token.
func BearerToken(jwt string) Auth { return Auth("Bearer " + jwt) }

// Client talks to the Darkube API for a single tenant.
type Client struct {
	BaseURL string
	Org     string

	http *resty.Client
}

// New builds a Client. baseURL may be empty to use DefaultBaseURL.
func New(baseURL string, auth Auth, org string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	rc := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(requestTimeout).
		SetHeader("Accept", "application/json").
		SetHeader("Authorization", string(auth))
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

// API error codes worth handling by name. The `detail` that accompanies them is
// Persian prose, so matching the code is both more robust and more readable.
const (
	// CodeSameHelmReleaseName means the name is taken by a Helm release in the
	// target namespace. Deleting an app drops its record immediately but can
	// leave the release behind, so this also fires for names with no app.
	CodeSameHelmReleaseName = "SameHelmReleaseNameExists"
	// CodeTerminatingApp means an app of this name is still being deleted.
	CodeTerminatingApp = "TerminatingAppException"
	// CodeDuplicateReleaseAndNamespace means a live app of this name already
	// exists in the namespace. The API distinguishes this from
	// CodeSameHelmReleaseName, which is the orphaned-release case.
	CodeDuplicateReleaseAndNamespace = "DuplicateReleaseAndNamespaceException"
	// CodeGithubAuth and CodeGitlabAuth mean the git provider has not been
	// connected to this Hamravesh account, so Darkube cannot read the repository
	// it is being asked to build. Creating a git-backed app has an account-level
	// prerequisite that creating an image-backed one does not.
	CodeGithubAuth = "GithubAuthException"
	CodeGitlabAuth = "GitlabAuthException"
)

// ErrorCode returns the API error code carried by err, or "" if err is not an
// *APIError.
func ErrorCode(err error) string {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr.Code
	}
	return ""
}

// IsNotFound reports whether err is an API 404. This is the distinction
// `wait --for deleted` turns on: "the app is gone" versus "the request failed".
func IsNotFound(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

// IsTransient reports whether err is worth retrying: a transport failure, or a
// 5xx from the API. Polling loops use it so that a flaky minute — which this API
// does produce — does not abort a wait that would otherwise have succeeded.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr.StatusCode >= http.StatusInternalServerError
	}
	// Not an APIError at all, so the request never produced a response.
	return true
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
