package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// Builds are the image builds Darkube runs for a git-backed app. The routes
// come from the console's build tab (console-paas, 2026-09-30):
//
//	GET  build/app/<app-id>/?limit=&offset=   paginated list, newest first
//	GET  build/<build-id>/                    one build, including its log
//	GET  build/<build-id>/retry/              rebuild that commit (a GET that writes)
//	POST build/<build-id>/stop/               cancel a running build
//	GET  build/build_last_commit/?app_id=&deploy=true   build and deploy the branch head
const buildsPathV1 = "/api/v1/darkube/build/"

// Build statuses, as the console's build tab names them.
const (
	BuildInitiated = "initiated"
	BuildRunning   = "running"
	BuildSucceeded = "succeeded"
	BuildFailed    = "failed"
	BuildCanceled  = "canceled"
)

// BuildID identifies a build.
type BuildID = FlexID

// Build is one image build of a git-backed app.
type Build struct {
	ID              BuildID `json:"id"                yaml:"id"`
	Status          string  `json:"status"            yaml:"status"`
	GitBranch       string  `json:"git_branch"        yaml:"git_branch"`
	ShortCommitHash string  `json:"short_commit_hash" yaml:"short_commit_hash"`
	CommitMessage   string  `json:"commit_message"    yaml:"commit_message"`
	CreationTime    string  `json:"creation_time"     yaml:"creation_time"`
	EndTime         string  `json:"end_time"          yaml:"end_time"`
	// Errors is the platform's explanation of a failed build. The console shows
	// it as prose above the log.
	Errors any `json:"errors,omitempty" yaml:"errors,omitempty"`
	// Log is the build output with ANSI colors, present on the detail route only.
	Log string `json:"log,omitempty" yaml:"log,omitempty"`
}

// InProgress reports whether the build has yet to finish.
func (b Build) InProgress() bool {
	return b.Status == BuildInitiated || b.Status == BuildRunning
}

// ListBuilds returns an app's most recent builds, newest first, up to limit.
func (c *Client) ListBuilds(ctx context.Context, appID string, limit int) ([]Build, error) {
	const pageSize = 50
	var all []Build
	offset := 0
	for len(all) < limit {
		want := min(pageSize, limit-len(all))

		q := url.Values{}
		q.Set("limit", strconv.Itoa(want))
		q.Set("offset", strconv.Itoa(offset))

		var p page[Build]
		if err := c.getJSON(ctx, buildsPathV1+"app/"+url.PathEscape(appID)+"/", q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		offset += want
		if p.Next == "" || len(p.Results) == 0 || len(all) >= p.Count {
			break
		}
	}
	return all, nil
}

// GetBuild returns one build with its log.
func (c *Client) GetBuild(ctx context.Context, id BuildID) (*Build, error) {
	var b Build
	if err := c.getJSON(ctx, buildsPathV1+url.PathEscape(string(id))+"/", nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// RetryBuild rebuilds the commit of an earlier build and returns the new build.
// The route is a GET even though it starts a build; that is how the console
// calls it. The console reads the new build out of the answer; if the answer
// is not a build, the retry still happened and the returned build has no ID.
func (c *Client) RetryBuild(ctx context.Context, id BuildID) (*Build, error) {
	data, err := c.do(ctx, http.MethodGet, buildsPathV1+url.PathEscape(string(id))+"/retry/", nil, nil)
	if err != nil {
		return nil, err
	}
	var b Build
	if json.Unmarshal(data, &b) != nil {
		b = Build{}
	}
	return &b, nil
}

// StopBuild cancels a running build.
func (c *Client) StopBuild(ctx context.Context, id BuildID) error {
	_, err := c.do(ctx, http.MethodPost, buildsPathV1+url.PathEscape(string(id))+"/stop/", nil, map[string]any{})
	return err
}

// BuildLatestCommit builds the head of the app's branch and deploys the result,
// the console's "build and deploy the last commit" button.
func (c *Client) BuildLatestCommit(ctx context.Context, appID string) error {
	q := url.Values{}
	q.Set("app_id", appID)
	q.Set("deploy", "true")
	_, err := c.do(ctx, http.MethodGet, buildsPathV1+"build_last_commit/", q, nil)
	return err
}
