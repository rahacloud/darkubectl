package client

import (
	"context"
	"net/url"
)

// CICDScripts is the pipeline configuration the console's CI/CD tab generates
// for an app, from GET stateless_apps/<uuid>/generate_cicd_scripts (no
// trailing slash; confirmed 2026-10-01). The GitLab and GitHub jobs build with
// darkube-cli and deploy with the app's trigger deploy token, which they read
// from the CI variables in Envs.
//
// Envs and Curl carry secrets: the deploy token and, in DOCKER_AUTH_CONFIG,
// the tenant's registry credential.
type CICDScripts struct {
	Envs          map[string]string `json:"envs"           yaml:"envs"`
	GitLabCI      string            `json:"gitlabci"       yaml:"gitlabci"`
	GitHubActions string            `json:"github_actions" yaml:"github_actions"`
	Curl          string            `json:"curl"           yaml:"curl"`
}

// CICDScripts returns the generated pipeline configuration for an app.
func (c *Client) CICDScripts(ctx context.Context, appID string) (*CICDScripts, error) {
	var out CICDScripts
	if err := c.getJSON(ctx, "/api/v1/darkube/stateless_apps/"+url.PathEscape(appID)+"/generate_cicd_scripts", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
