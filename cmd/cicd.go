package cmd

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

const (
	ciGitLab = "gitlab"
	ciGitHub = "github"
	ciCurl   = "curl"
	ciEnv    = "env"
)

var errBadCITarget = errors.New("--for must be gitlab, github, curl or env")

func newGetCIConfigCommand() *cli.Command {
	return &cli.Command{
		Name:      "ci-config",
		Aliases:   []string{"cicd", "ci"},
		Usage:     "Print the pipeline that builds and deploys an app, as the console generates it",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl get ci-config my-api                 # a .gitlab-ci.yml job pair\n" +
			"  darkubectl get ci-config my-api --for github     # a GitHub Actions workflow\n" +
			"  darkubectl get ci-config my-api --for env        # the CI variables the jobs read (prints secrets)\n" +
			"  darkubectl get ci-config my-api --for curl       # a one-line deploy (prints a secret)\n\n" +
			"The jobs build with darkube-cli and deploy with the app's deploy token, which\n" +
			"they expect as CI variables: set those from --for env, as masked variables.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFor, Value: ciGitLab, Usage: "gitlab, github, curl or env"},
		},
		Action: getCIConfigAction,
	}
}

func getCIConfigAction(ctx context.Context, cmd *cli.Command) error {
	target := cmd.String(flagFor)
	if !slices.Contains([]string{ciGitLab, ciGitHub, ciCurl, ciEnv}, target) {
		return fmt.Errorf("%w (got %q)", errBadCITarget, target)
	}
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	s, err := c.CICDScripts(ctx, app.ID)
	if err != nil {
		return err
	}
	fmt.Fprint(os.Stdout, ciOutput(s, target))
	return nil
}

// ciOutput renders one form of the generated configuration, newline-ended.
func ciOutput(s *client.CICDScripts, target string) string {
	var out string
	switch target {
	case ciGitHub:
		out = s.GitHubActions
	case ciCurl:
		out = s.Curl
	case ciEnv:
		var b strings.Builder
		for _, k := range slices.Sorted(maps.Keys(s.Envs)) {
			fmt.Fprintf(&b, "%s=%s\n", k, envValue(s.Envs[k]))
		}
		out = b.String()
	default:
		out = s.GitLabCI
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// envValue quotes a value only when it holds anything a shell would
// interpret, so that the env form can be pasted or sourced as is.
func envValue(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n\"'$`\\{}[]*?;&|<>()!#~") {
		return v
	}
	return shellQuote(v)
}
