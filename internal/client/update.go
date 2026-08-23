package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// Writes go through PUT on the v1 detail route.
//
// PATCH is not merely broken, it is not implemented: OPTIONS on the v1 detail
// advertises {"actions": ["PUT"]} and the v2 detail advertises none, so the
// viewset defines update but not partial_update. That is why every PATCH
// returns a bodyless 500 rather than a validation error — hence the
// read-modify-write below rather than a merge patch.

// putDropFields are server-owned fields that must not be echoed back in a PUT.
//
// They are either read-only projections (state, latest_build, workflows) or
// nested objects the write serializer does not accept. secret_envs is the
// important one: it is read-only here and its values never come back from a
// read, so dropping it preserves the stored secrets, while sending it back
// would at best be ignored and at worst clear them.
var putDropFields = []string{
	"cluster",
	"creation_time",
	"creator",
	"deploy_context",
	"latest_build",
	"secret_envs",
	"state",
	"updated_at",
	"version",
	"workflows",
}

// putRelationFields are relations the read returns as nested objects but the
// write serializer wants as bare primary keys ("plan: … is not a valid UUID").
var putRelationFields = []string{"plan", "namespace", "organization"}

// UpdateApp applies mutate to an app's current state and writes it back.
//
// The API has no partial update, so this is a read-modify-write: the current
// object is fetched, normalized into the shape the write serializer accepts,
// handed to mutate, and PUT in full. Callers therefore change one field without
// having to reconstruct the other seventy.
func (c *Client) UpdateApp(ctx context.Context, id string, mutate func(app map[string]any) error) (map[string]any, error) {
	path := appsPathV1 + url.PathEscape(id) + "/"

	var app map[string]any
	if err := c.getJSON(ctx, path, nil, &app); err != nil {
		return nil, err
	}
	normalizeForPut(app)

	if err := mutate(app); err != nil {
		return nil, err
	}

	data, err := c.do(ctx, http.MethodPut, path, nil, app)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(data) > 0 {
		_ = decodeInto(data, &out)
	}
	return out, nil
}

// normalizeForPut rewrites a freshly read app into the shape PUT accepts:
// nested relations collapsed to their ids, server-owned fields removed.
func normalizeForPut(app map[string]any) {
	for _, k := range putRelationFields {
		nested, ok := app[k].(map[string]any)
		if !ok {
			continue
		}
		if id, found := nested["id"]; found {
			app[k] = id
		}
	}
	for _, k := range putDropFields {
		delete(app, k)
	}
}

// appField reads a typed field out of a raw app object, tolerating absence.
func appField[T any](app map[string]any, key string) (T, bool) {
	v, ok := app[key].(T)
	return v, ok
}

// EnvVars reads an app's plain (non-secret) environment variables.
func EnvVars(app map[string]any) []EnvVar {
	raw, _ := appField[[]any](app, "envs")
	out := make([]EnvVar, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		value, _ := entry["value"].(string)
		out = append(out, EnvVar{Name: name, Value: value})
	}
	return out
}

// SecretEnvNames reads the names of an app's secret environment variables.
//
// Only names are ever available: the API stores the values in a vault and
// returns them empty on every read, so there is nothing to unmask.
func SecretEnvNames(app map[string]any) []string {
	raw, ok := appField[[]any](app, "secret_envs")
	if !ok {
		// The v2 list route types secret_envs as a bare vault path string and
		// carries the names in secret_envs_keys instead.
		raw, _ = appField[[]any](app, "secret_envs_keys")
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := entry["name"].(string); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// SetEnvVars replaces an app's plain environment variables.
func SetEnvVars(app map[string]any, envs []EnvVar) {
	out := make([]any, 0, len(envs))
	for _, e := range envs {
		out = append(out, map[string]any{"name": e.Name, "value": e.Value})
	}
	app["envs"] = out
}

// ExternalHosts reads the domains routed to an app. This is where custom
// domains actually live; custom_domain_address is a separate, usually empty
// field and is not the ingress host list.
func ExternalHosts(app map[string]any) []string {
	raw, _ := appField[[]any](app, "external_hosts")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if host, ok := item.(string); ok {
			out = append(out, host)
		}
	}
	return out
}

// SetExternalHosts replaces the domains routed to an app.
func SetExternalHosts(app map[string]any, hosts []string) {
	out := make([]any, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, h)
	}
	app["external_hosts"] = out
}

// ErrNoSuchEnv is returned when removing an environment variable that is absent.
var ErrNoSuchEnv = errors.New("no such environment variable")

// ErrNoSuchHost is returned when removing a domain the app does not serve.
var ErrNoSuchHost = errors.New("no such domain")
