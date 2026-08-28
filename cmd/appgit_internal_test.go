package cmd

import (
	"errors"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
	"gopkg.in/yaml.v3"
)

func TestGitSpecValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec gitSpec
		want error
	}{
		{name: "repo only", spec: gitSpec{RepoURL: "https://github.com/a/b"}},
		{name: "no repo", spec: gitSpec{Branch: "main"}, want: errGitNoRepo},
		{
			name: "bad provider",
			spec: gitSpec{RepoURL: "u", Provider: "bitbucket"},
			want: errGitProvider,
		},
		{
			name: "bad builder",
			spec: gitSpec{RepoURL: "u", Builder: "buildkit"},
			want: errGitBuilder,
		},
		{
			name: "bad build method",
			spec: gitSpec{RepoURL: "u", BuildMethod: "tekton"},
			want: errGitBuildMeth,
		},
		{
			name: "valid enums",
			spec: gitSpec{
				RepoURL:     "u",
				Provider:    client.ProviderGithub,
				Builder:     client.BuilderHerokuBuildpack,
				BuildMethod: client.BuildMethodWebhook,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.spec.validate()
			if tc.want == nil && err != nil {
				t.Fatalf("validate = %v, want nil", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGuessProvider(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://github.com/acme/api":   client.ProviderGithub,
		"git@github.com:acme/api.git":   client.ProviderGithub,
		"https://gitlab.com/acme/api":   client.ProviderGitlab,
		"https://hamgit.ir/lms/backend": client.ProviderGitlab,
		"https://git.example.com/a/b":   "",
	}
	for url, want := range cases {
		if got := guessProvider(url); got != want {
			t.Errorf("guessProvider(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestAppSpecCompleteAcceptsEitherSource(t *testing.T) {
	t.Parallel()

	base := appSpec{Name: "a", Namespace: "1", Plan: "1"}

	if base.complete() {
		t.Error("spec with neither image nor git should be incomplete")
	}

	withImage := base
	withImage.Image = "nginx:latest"
	if !withImage.complete() {
		t.Error("spec with an image should be complete")
	}

	withGit := base
	withGit.Git = &gitSpec{RepoURL: "https://github.com/a/b"}
	if !withGit.complete() {
		t.Error("spec with a git repo should be complete")
	}
}

func TestAppSpecValidateRejectsImageAndGitTogether(t *testing.T) {
	t.Parallel()

	spec := appSpec{
		Name: "a", Namespace: "1", Plan: "1",
		Image: "nginx:latest",
		Git:   &gitSpec{RepoURL: "https://github.com/a/b"},
	}
	if err := spec.validate(); !errors.Is(err, errGitAndImage) {
		t.Errorf("validate = %v, want errGitAndImage", err)
	}
}

func TestAppSpecParsesGitBlock(t *testing.T) {
	t.Parallel()

	const doc = `
name: my-api
namespace: "171708"
plan: "1"
git:
  repoUrl: https://github.com/acme/my-api
  branch: develop
  dockerfile: ./build/Dockerfile
`
	var spec appSpec
	if err := yaml.Unmarshal([]byte(doc), &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if spec.Git == nil {
		t.Fatal("git block did not parse")
	}
	if spec.Git.RepoURL != "https://github.com/acme/my-api" || spec.Git.Branch != "develop" {
		t.Errorf("git = %+v", spec.Git)
	}
	if !spec.complete() {
		t.Error("spec should be complete without an image")
	}
	if err := spec.validate(); err != nil {
		t.Errorf("validate = %v, want nil", err)
	}
}
