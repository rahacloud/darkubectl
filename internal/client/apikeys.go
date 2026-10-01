package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Account API keys, the console's account settings (console-account,
// confirmed 2026-10-01). Not tenant-scoped: the routes answer without
// X-Organization.
//
//	GET    /api/v1/apikeys/                       DRF list of {id,name,creation_time,expiration_time}
//	POST   /api/v1/apikeys/ {name}                → {"value": "<key>"}, the only time it is shown
//	DELETE /api/v1/apikeys/<id>/
//
// OPTIONS marks expiration_time required and writable, but the server ignores
// it: every key expires two years after it is made, whatever is sent (an RFC
// 3339 time, a date, or nothing; 2026-10-01). The console sends only the name.
const apiKeysPath = "/api/v1/apikeys/" //nolint:gosec // a route, not a credential

// APIKeyInfo is an API key as listed; the key itself is never returned again.
type APIKeyInfo struct {
	ID             string `json:"id"              yaml:"id"`
	Name           string `json:"name"            yaml:"name"`
	CreationTime   string `json:"creation_time"   yaml:"creation_time"`
	ExpirationTime string `json:"expiration_time" yaml:"expiration_time"`
}

// ErrNoAPIKeyValue means the platform created a key but did not return it.
var ErrNoAPIKeyValue = fmt.Errorf("%w: value", ErrMissingField)

// ListAPIKeys returns the account's API keys.
func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKeyInfo, error) {
	var p page[APIKeyInfo]
	if err := c.getJSON(ctx, apiKeysPath, nil, &p); err != nil {
		return nil, err
	}
	return p.Results, nil
}

// CreateAPIKey creates a key and returns its value, which cannot be read
// back later. The platform decides its expiry.
func (c *Client) CreateAPIKey(ctx context.Context, name string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	body := map[string]any{"name": name}
	if err := c.postJSON(ctx, apiKeysPath, body, &out); err != nil {
		return "", err
	}
	if out.Value == "" {
		return "", ErrNoAPIKeyValue
	}
	return out.Value, nil
}

// DeleteAPIKey revokes a key.
func (c *Client) DeleteAPIKey(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, apiKeysPath+url.PathEscape(id)+"/", nil, nil)
	return err
}
