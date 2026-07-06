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

// PatchApp applies a partial update (PATCH) to an app and returns the updated raw object.
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

// Pod is a running pod of an app.
type Pod struct {
	Name string `json:"name"`
}

// ListPods returns an app's running pods, read from its live state.
//
// Note: the console sources pods from the app-state websocket, so this REST
// state may be empty; callers should fall back to an explicit pod name.
func (c *Client) ListPods(ctx context.Context, id string) ([]Pod, error) {
	var app struct {
		State struct {
			Pods []Pod `json:"pods"`
		} `json:"state"`
	}
	if err := c.getJSON(ctx, appsPathV2+url.PathEscape(id)+"/", nil, &app); err != nil {
		return nil, err
	}
	return app.State.Pods, nil
}

// DeleteApp deletes an app by UUID.
func (c *Client) DeleteApp(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, appsPathV2+url.PathEscape(id)+"/", nil, nil)
	return err
}
