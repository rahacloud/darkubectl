package client

import (
	"context"
	"net/http"
	"net/url"
)

// AppManifests returns the Kubernetes objects the platform renders for an app:
// the Helm output of its chart, as multi-document YAML with "# Source:"
// comments, from GET apps/<uuid>/manifests/ (the console downloads it as a
// file). Confirmed 2026-10-01. The secrets template renders empty, so secret
// values are not in it.
func (c *Client) AppManifests(ctx context.Context, appID string) (string, error) {
	data, err := c.do(ctx, http.MethodGet, appsPathV1+url.PathEscape(appID)+"/manifests/", nil, nil)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
