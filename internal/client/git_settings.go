package client

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

// GitBuildArg is one Docker build argument returned by the app API.
type GitBuildArg struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GitBuildSettings contains mutable Git build configuration. Build method is
// intentionally excluded because changing deployment modes is a workflow
// migration rather than a routine builder setting.
type GitBuildSettings struct {
	BuildMethod string
	RepoURL     string
	Branch      string
	Provider    string
	Dockerfile  string
	Context     string
	Workdir     string
	Builder     string
	Autodeploy  *bool
	Args        []GitBuildArg
}

// GitBuildSettingsFromApp reads the editable settings from a raw API object.
func GitBuildSettingsFromApp(app map[string]any) GitBuildSettings {
	s := GitBuildSettings{}
	s.BuildMethod, _ = app["build_method"].(string)
	s.RepoURL, _ = app["git_repo_url"].(string)
	s.Branch, _ = app["git_branch_name"].(string)
	s.Provider, _ = app["git_provider_type"].(string)
	s.Dockerfile, _ = app["git_build_dockerfile"].(string)
	s.Context, _ = app["git_build_context"].(string)
	s.Workdir, _ = app["git_build_workdir"].(string)
	s.Builder, _ = app["builder"].(string)
	if value, ok := app["autodeploy_on_git_push"].(bool); ok {
		s.Autodeploy = &value
	}
	if rows, ok := app["git_build_args"].([]any); ok {
		for _, item := range rows {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			key, _ := row["key"].(string)
			value, _ := row["value"].(string)
			s.Args = append(s.Args, GitBuildArg{Key: key, Value: value})
		}
	}
	return s
}

// Equal reports whether the editable settings, including argument order, match.
func (s GitBuildSettings) Equal(other GitBuildSettings) bool {
	if len(s.Args) == 0 {
		s.Args = nil
	}
	if len(other.Args) == 0 {
		other.Args = nil
	}
	return reflect.DeepEqual(s, other)
}

func setGitBuildSettings(raw map[string]any, s GitBuildSettings) {
	raw["build_method"] = s.BuildMethod
	raw["git_repo_url"] = s.RepoURL
	raw["git_branch_name"] = s.Branch
	raw["git_provider_type"] = s.Provider
	raw["git_build_dockerfile"] = s.Dockerfile
	raw["git_build_context"] = s.Context
	raw["git_build_workdir"] = s.Workdir
	raw["builder"] = s.Builder
	if s.Autodeploy == nil {
		delete(raw, "autodeploy_on_git_push")
	} else {
		raw["autodeploy_on_git_push"] = *s.Autodeploy
	}
	args := make([]any, 0, len(s.Args))
	for _, arg := range s.Args {
		args = append(args, map[string]any{"key": arg.Key, "value": arg.Value})
	}
	raw["git_build_args"] = args
}

// UpdateGitBuildSettings checks the staged baseline again inside the app's
// read-modify-write operation, saves only these fields, and verifies them from
// a fresh read. A concurrent edit to these settings is reported as a conflict.
func (c *Client) UpdateGitBuildSettings(ctx context.Context, id string, expected, desired GitBuildSettings) error {
	mutate := func(raw map[string]any) error {
		if raw["build_method"] != expected.BuildMethod {
			return fmt.Errorf("deployment type changed since it was loaded; refresh before saving")
		}
		if got := GitBuildSettingsFromApp(raw); !got.Equal(expected) {
			return fmt.Errorf("Git build settings changed since they were loaded; refresh before saving")
		}
		setGitBuildSettings(raw, desired)
		return nil
	}
	if _, err := c.UpdateApp(ctx, id, mutate); err != nil {
		return err
	}
	for attempt := range 5 {
		if attempt > 0 {
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		raw, err := c.GetApp(ctx, id)
		if err != nil {
			if attempt == 4 {
				return err
			}
			continue
		}
		if GitBuildSettingsFromApp(raw).Equal(desired) {
			return nil
		}
	}
	return fmt.Errorf("platform accepted the Git settings update but did not store all requested values")
}
