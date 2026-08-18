package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const appsPathV2 = "/api/v2/darkube/apps/"

// Sentinel errors returned by ResolveApp, comparable with errors.Is.
var (
	ErrAppNotFound  = errors.New("no app named or with that id")
	ErrAppAmbiguous = errors.New("app name is ambiguous")
)

// ListApps returns all apps in the current tenant, following pagination.
func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	var all []App
	const limit = 200
	offset := 0
	for {
		q := url.Values{}
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))

		var p page[App]
		if err := c.getJSON(ctx, appsPathV2, q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		offset += limit
		// Stop when the API says there is no next page (or nothing came back).
		if p.Next == "" || len(p.Results) == 0 || len(all) >= p.Count {
			break
		}
	}
	return all, nil
}

// GetApp returns the full raw app object by UUID. The result is a generic map so
// every field is preserved for `describe` / `-o json|yaml`.
func (c *Client) GetApp(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	if err := c.getJSON(ctx, appsPathV2+url.PathEscape(id)+"/", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAppTyped returns a single app decoded into the typed App struct.
func (c *Client) GetAppTyped(ctx context.Context, id string) (*App, error) {
	var a App
	if err := c.getJSON(ctx, appsPathV2+url.PathEscape(id)+"/", nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// ResolveApp finds an app by UUID or by exact name within the current tenant.
// Names are not guaranteed unique across namespaces; an ambiguous name is an error.
func (c *Client) ResolveApp(ctx context.Context, nameOrID string) (*App, error) {
	apps, err := c.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	var byName []App
	for i := range apps {
		if apps[i].ID == nameOrID {
			return &apps[i], nil
		}
		if apps[i].Name == nameOrID {
			byName = append(byName, apps[i])
		}
	}
	switch len(byName) {
	case 0:
		return nil, fmt.Errorf("%w: %q in tenant %q", ErrAppNotFound, nameOrID, c.Org)
	case 1:
		return &byName[0], nil
	default:
		return nil, fmt.Errorf("%w: %q has %d matches; use the app id instead", ErrAppAmbiguous, nameOrID, len(byName))
	}
}

// DeployToken returns an app's CI trigger deploy token.
//
// This is the credential `darkube deploy --token` wants in a pipeline, paired
// with the app's own id as --app-id. The console shows it on the app's CI/CD
// page and it is stored nowhere in the cluster, so the API is the only way to
// wire up CI without the web UI.
//
// It is deliberately not a field on App: `get apps -o json` would then print
// every app's deploy token, which is not what asking for a list of apps means.
func (c *Client) DeployToken(ctx context.Context, id string) (string, error) {
	var out struct {
		Token string `json:"trigger_deploy_token"`
	}
	if err := c.getJSON(ctx, appsPathV2+url.PathEscape(id)+"/", nil, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// PatchApp applies a partial update (PATCH) to an app and returns the updated raw object.
//
// Caveat observed 2026-08-11 against a freshly created app: PATCH returned 500
// with an empty body for every field tried — a scalar (`readiness_probe_path`),
// `replicas`, and the nested `svc` — on both the v1 and v2 detail routes. So the
// v1-for-writes rule that `create` follows does not rescue PATCH, and callers
// (including `scale app`) should expect it to fail. Setting `disk`, `svc.ports`
// and `envs` currently requires the console.
func (c *Client) PatchApp(ctx context.Context, id string, patch map[string]any) (map[string]any, error) {
	data, err := c.do(ctx, http.MethodPatch, appsPathV2+url.PathEscape(id)+"/", nil, patch)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	// Some endpoints return the object; tolerate an empty body.
	if len(data) > 0 {
		_ = decodeInto(data, &out)
	}
	return out, nil
}

// DeleteApp deletes an app by UUID.
func (c *Client) DeleteApp(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, appsPathV2+url.PathEscape(id)+"/", nil, nil)
	return err
}
