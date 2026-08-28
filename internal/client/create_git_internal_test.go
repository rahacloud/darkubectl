package client

import (
	"testing"
)

func TestBuildCreatePayloadDockerImageUnchanged(t *testing.T) {
	t.Parallel()

	got := buildCreatePayload(CreateAppInput{
		Name: "api", ImageRepo: "nginx", ImageTag: "1.27", Replicas: 1,
	})
	if got["creation_method"] != CreationMethodDockerImage {
		t.Errorf("creation_method = %v, want %s", got["creation_method"], CreationMethodDockerImage)
	}
	if got["image_repo"] != "nginx" || got["image_tag"] != "1.27" {
		t.Errorf("image = %v:%v, want nginx:1.27", got["image_repo"], got["image_tag"])
	}
	if _, ok := got["git_repo_url"]; ok {
		t.Error("a docker-image app must not carry git fields")
	}
}

func TestBuildCreatePayloadGitDefaults(t *testing.T) {
	t.Parallel()

	got := buildCreatePayload(CreateAppInput{
		Name:     "api",
		Replicas: 1,
		Git:      &GitSource{RepoURL: "https://github.com/acme/api"},
	})

	want := map[string]any{
		"creation_method":        CreationMethodGitRepoURL,
		"builder":                BuilderDockerfile,
		"git_repo_url":           "https://github.com/acme/api",
		"git_branch_name":        "main",
		"git_provider_type":      ProviderGitlab,
		"git_build_dockerfile":   "./Dockerfile",
		"git_build_context":      ".",
		"git_build_workdir":      ".",
		"build_method":           BuildMethodGitlabCI,
		"autodeploy_on_git_push": true,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	// Darkube builds and pushes the image itself, so naming one would conflict.
	if _, ok := got["image_repo"]; ok {
		t.Error("git-built app must not send image_repo")
	}
	if _, ok := got["image_tag"]; ok {
		t.Error("git-built app must not send image_tag")
	}
}

func TestBuildCreatePayloadGitHonorsOverrides(t *testing.T) {
	t.Parallel()

	no := false
	got := buildCreatePayload(CreateAppInput{
		Name: "api",
		Git: &GitSource{
			RepoURL:     "https://github.com/acme/api",
			Branch:      "develop",
			Provider:    ProviderGithub,
			Dockerfile:  "./build/Dockerfile",
			Context:     "./build",
			Workdir:     "./svc",
			Builder:     BuilderHerokuBuildpack,
			BuildMethod: BuildMethodWebhook,
			Autodeploy:  &no,
		},
	})

	want := map[string]any{
		"git_branch_name":        "develop",
		"git_provider_type":      ProviderGithub,
		"git_build_dockerfile":   "./build/Dockerfile",
		"git_build_context":      "./build",
		"git_build_workdir":      "./svc",
		"builder":                BuilderHerokuBuildpack,
		"build_method":           BuildMethodWebhook,
		"autodeploy_on_git_push": false,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestGitSourceAutodeployFalseSurvivesDefaults(t *testing.T) {
	t.Parallel()

	// The pointer type exists so an explicit false is distinguishable from unset;
	// a plain bool would silently become true here.
	no := false
	if got := (GitSource{RepoURL: "u", Autodeploy: &no}).withDefaults(); *got.Autodeploy {
		t.Error("explicit autodeploy=false was overwritten by the default")
	}
	if got := (GitSource{RepoURL: "u"}).withDefaults(); !*got.Autodeploy {
		t.Error("unset autodeploy should default to true")
	}
}
