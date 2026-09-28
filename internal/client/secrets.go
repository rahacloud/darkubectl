package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Secret environment variables live in a vault, not in the app object: a read of
// the app returns their names with the values blank, and until 2026-09-28 this
// client assumed that was all the API would ever give back. It is not. The
// console reads the values through two actions on the app, found in its own
// bundle (console-paas, the environmentVariables_v2 tab):
//
//	GET  /api/v1/darkube/apps/<id>/read_all_vault_secret_envs/  -> [{name, value}]
//	POST /api/v1/darkube/apps/<id>/read_vault_secret_env/ {name} -> {data: {name, value}}
//
// and it writes them with the ordinary PUT, sending secret_envs as a full list
// of {name, value}. There is no separate write route. Verified end to end on a
// throwaway app in the rahacloud org on 2026-09-28: changing one value, adding
// one, and removing one all stuck and read back through the vault route.

// SecretEnvs reads an app's secret environment variables with their values.
func (c *Client) SecretEnvs(ctx context.Context, id string) ([]EnvVar, error) {
	data, err := c.do(ctx, http.MethodGet, appsPathV1+url.PathEscape(id)+"/read_all_vault_secret_envs/", nil, nil)
	if err != nil {
		return nil, err
	}
	var out []EnvVar
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode secret envs: %w", err)
	}
	return out, nil
}

// UpdateSecretEnvs changes an app's secret environment variables.
//
// The PUT replaces the whole list, and an entry sent without its value is not
// safe to rely on, so this reads every current value first, hands the complete
// list to mutate, and writes back the complete result. Plain fields go through
// the same read-modify-write as UpdateApp, so nothing else on the app changes.
//
// Like every PUT, this rolls the app's pods; a Secret edit made this way is also
// what a later deploy re-renders, which is the point of doing it here rather
// than in the Kubernetes Secret.
func (c *Client) UpdateSecretEnvs(
	ctx context.Context, id string, mutate func(secrets []EnvVar) ([]EnvVar, error),
) ([]EnvVar, error) {
	current, err := c.SecretEnvs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read current secret envs: %w", err)
	}
	next, err := mutate(current)
	if err != nil {
		return nil, err
	}
	_, err = c.UpdateApp(ctx, id, func(app map[string]any) error {
		app["secret_envs"] = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}
