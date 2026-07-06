package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// appsPathV1 is the create endpoint (v1, not v2 — v2 POST 500s).
const appsPathV1 = "/api/v1/darkube/apps/"

// ErrNoOrganizationID is returned when the numeric org id can't be derived.
var ErrNoOrganizationID = errors.New("could not determine the organization id (no existing app to read it from)")

// CreateAppInput describes a Docker-image app to create. Names have already been
// resolved to ids by the caller.
type CreateAppInput struct {
	Name           string
	NamespaceID    int
	OrganizationID int
	PlanID         string
	ImageRepo      string
	ImageTag       string
	Command        string
	Args           string
	Replicas       int
}

// CreateApp creates a Docker-image app and returns the created object. The
// payload mirrors the console's confirmed POST /api/v1/darkube/apps/ request.
func (c *Client) CreateApp(ctx context.Context, in CreateAppInput) (map[string]any, error) {
	data, err := c.do(ctx, http.MethodPost, appsPathV1, nil, buildCreatePayload(in))
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
	return map[string]any{
		"name":                 in.Name,
		"namespace":            in.NamespaceID,
		"organization":         in.OrganizationID,
		"plan":                 in.PlanID,
		"creation_method":      "docker_image",
		"image_repo":           in.ImageRepo,
		"image_tag":            in.ImageTag,
		"builder":              "dockerfile",
		"command":              in.Command,
		"args":                 in.Args,
		"replicas":             in.Replicas,
		"svc":                  map[string]any{"type": "ClusterIP", "ports": map[string]any{}},
		"custom_config":        map[string]any{},
		"readiness_probe_path": "",
		"backup_config":        nil,
		"deploy_context":       nil,
		"ssl_challenge_type":   "dns01",
	}
}

// OrganizationID returns the current tenant's numeric organization id, which the
// create payload requires. It is read from an existing app's v1 detail (the app
// list does not include it).
func (c *Client) OrganizationID(ctx context.Context) (int, error) {
	q := url.Values{}
	q.Set("limit", "1")
	q.Set("fields", "id")

	var list page[App]
	if err := c.getJSON(ctx, appsPathV2, q, &list); err != nil {
		return 0, err
	}
	if len(list.Results) == 0 {
		return 0, ErrNoOrganizationID
	}

	var detail struct {
		Organization struct {
			ID int `json:"id"`
		} `json:"organization"`
	}
	if err := c.getJSON(ctx, appsPathV1+url.PathEscape(list.Results[0].ID)+"/", nil, &detail); err != nil {
		return 0, err
	}
	if detail.Organization.ID == 0 {
		return 0, ErrNoOrganizationID
	}
	return detail.Organization.ID, nil
}
