package client

import (
	"context"
	"net/http"
)

// CreateAppInput describes a Docker-image app to create. Names have already been
// resolved to ids by the caller.
type CreateAppInput struct {
	Name        string
	NamespaceID int
	PlanID      string
	ImageRepo   string
	ImageTag    string
	Port        int
	Replicas    int
	EnableSSL   bool
}

// CreateApp creates a new app (Docker-image mode) and returns the created object.
//
// TODO(create): the exact POST body (field names, which are required, whether
// namespace/plan are ids or nested objects) is not yet confirmed against a
// captured console "create app" request — the endpoint 500s on partial bodies.
// buildCreatePayload below is a best guess from an existing docker_image app's
// fields; adjust it once the real request is captured.
func (c *Client) CreateApp(ctx context.Context, in CreateAppInput) (map[string]any, error) {
	data, err := c.do(ctx, http.MethodPost, appsPathV2, nil, buildCreatePayload(in))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(data) > 0 {
		_ = decodeInto(data, &out)
	}
	return out, nil
}

func buildCreatePayload(in CreateAppInput) map[string]any {
	payload := map[string]any{
		"name":            in.Name,
		"namespace":       in.NamespaceID,
		"plan":            in.PlanID,
		"creation_method": "docker_image",
		"image_repo":      in.ImageRepo,
		"image_tag":       in.ImageTag,
		"replicas":        in.Replicas,
		"enable_SSL":      in.EnableSSL,
	}
	if in.Port > 0 {
		payload["port"] = in.Port
	}
	return payload
}
