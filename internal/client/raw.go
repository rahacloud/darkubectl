package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// ErrInvalidJSON reports a raw request body that is not JSON.
var ErrInvalidJSON = errors.New("request body is not valid JSON")

// Raw sends one request to an arbitrary API path with the client's auth and
// X-Organization, and returns the response body. A body, when given, must be
// JSON and is sent as-is.
//
// This is the escape hatch for routes darkubectl does not model yet: the API is
// reverse-engineered, and the fastest way to learn a route is to call it.
func (c *Client) Raw(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	return c.RawWithHeaders(ctx, method, path, query, body, nil)
}

// RawWithHeaders is Raw with extra request headers. Some writes need one the
// client does not send by default: Hamravesh object storage, for instance,
// demands the account's TOTP code in `x-otp` before it creates a key or makes a
// bucket public, and answers 500 "2-fa verification error" without it.
func (c *Client) RawWithHeaders(
	ctx context.Context, method, path string, query url.Values, body []byte, header http.Header,
) ([]byte, error) {
	var payload any
	if len(body) > 0 {
		if !json.Valid(body) {
			return nil, ErrInvalidJSON
		}
		payload = json.RawMessage(body)
	}
	return c.doWithHeaders(ctx, method, path, query, payload, header)
}
