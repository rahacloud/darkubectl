package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
)

// gitSpec is the `git:` block of an app spec, for apps Darkube builds from a
// repository instead of pulling a prebuilt image.
//
//	name: my-api
//	namespace: "171708"
//	plan: "1"
//	git:
//	  repoUrl: https://github.com/acme/my-api
//	  branch: main
//	  dockerfile: ./Dockerfile
//
// Only repoUrl is required; the rest default to what the console sends.
type gitSpec struct {
	RepoURL     string `yaml:"repoUrl"`
	Branch      string `yaml:"branch"`
	Provider    string `yaml:"provider"`   // Github | Gitlab
	Dockerfile  string `yaml:"dockerfile"` // path within the repo
	Context     string `yaml:"context"`    // docker build context
	Workdir     string `yaml:"workdir"`
	Builder     string `yaml:"builder"`     // dockerfile | heroku_buildpacks
	BuildMethod string `yaml:"buildMethod"` // gitlabci | webhook
	Autodeploy  *bool  `yaml:"autodeployOnPush"`
}

// Errors for the git build fields. The API rejects a bad enum with a Persian
// message that does not name the alternatives, so validate before sending.
var (
	errGitNoRepo    = errors.New("git.repoUrl is required when creating an app from a repository")
	errGitAndImage  = errors.New("an app is built from a repository or pulled from an image, not both: set git.repoUrl or image, never both") //nolint:lll // one sentence reads better whole
	errGitProvider  = fmt.Errorf("git.provider must be %s or %s", client.ProviderGithub, client.ProviderGitlab)
	errGitBuilder   = fmt.Errorf("git.builder must be %s or %s", client.BuilderDockerfile, client.BuilderHerokuBuildpack)
	errGitBuildMeth = fmt.Errorf("git.buildMethod must be %s or %s", client.BuildMethodGitlabCI, client.BuildMethodWebhook)
)

// validate checks the enumerated fields against the values OPTIONS advertises.
func (g *gitSpec) validate() error {
	if g.RepoURL == "" {
		return errGitNoRepo
	}
	if g.Provider != "" && !slices.Contains([]string{client.ProviderGithub, client.ProviderGitlab}, g.Provider) {
		return errGitProvider
	}
	if g.Builder != "" &&
		!slices.Contains([]string{client.BuilderDockerfile, client.BuilderHerokuBuildpack}, g.Builder) {
		return errGitBuilder
	}
	if g.BuildMethod != "" &&
		!slices.Contains([]string{client.BuildMethodGitlabCI, client.BuildMethodWebhook}, g.BuildMethod) {
		return errGitBuildMeth
	}
	return nil
}

// toClient converts the spec into the client's input shape.
func (g *gitSpec) toClient() *client.GitSource {
	if g == nil {
		return nil
	}
	return &client.GitSource{
		RepoURL:     g.RepoURL,
		Branch:      g.Branch,
		Provider:    g.Provider,
		Dockerfile:  g.Dockerfile,
		Context:     g.Context,
		Workdir:     g.Workdir,
		Builder:     g.Builder,
		BuildMethod: g.BuildMethod,
		Autodeploy:  g.Autodeploy,
	}
}

// describe renders the git source for the confirmation line.
func (g *gitSpec) describe() string {
	branch := g.Branch
	if branch == "" {
		branch = "main"
	}
	return fmt.Sprintf("%s@%s", g.RepoURL, branch)
}

// guessProvider infers the provider from the repository host, so the common
// cases need no flag. Anything unrecognised is left empty for the API default.
func guessProvider(repoURL string) string {
	host := strings.ToLower(repoURL)
	switch {
	case strings.Contains(host, "github.com"):
		return client.ProviderGithub
	case strings.Contains(host, "gitlab"), strings.Contains(host, "hamgit"):
		return client.ProviderGitlab
	default:
		return ""
	}
}
