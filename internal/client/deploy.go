package client

import (
	"context"
	"net/http"
)

// deployFromCLIPath is what `darkube deploy` calls in a pipeline: it sets an
// app's image tag and rolls it out, authenticated by the app's trigger deploy
// token in the body rather than by an account. It answers 202
// {"status":"ok"}, and 400 IncorrectBuildParameters for a wrong token or id.
// Confirmed 2026-10-01 with no Authorization header at all, on a throwaway
// app, and found in the curl form of the console's generated CI scripts.
const deployFromCLIPath = "/api/v1/darkube/apps/update_from_cli/"

// DeployInput is one token-authenticated deploy.
type DeployInput struct {
	AppID       string
	DeployToken string
	ImageTag    string
	// JobID is the CI job, which darkube-cli reports alongside; optional.
	JobID string
}

// DeployWithToken sets the app's image tag using only its deploy token. The
// client needs no credential for this, and normally has none.
func (c *Client) DeployWithToken(ctx context.Context, in DeployInput) error {
	body := map[string]any{
		"app_id":               in.AppID,
		"trigger_deploy_token": in.DeployToken,
		"image_tag":            in.ImageTag,
	}
	if in.JobID != "" {
		body["job_id"] = in.JobID
	}
	_, err := c.do(ctx, http.MethodPut, deployFromCLIPath, nil, body)
	return err
}
