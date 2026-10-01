package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

const (
	flagAppID       = "app-id"
	flagDeployToken = "deploy-token"
	flagTag         = "tag"
	flagJobID       = "job-id"
)

var errDeployArgs = errors.New("deploy needs --app-id, --deploy-token and --tag (or their environment variables)")

func newDeployCommand() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "Roll an app onto a new image tag using only its deploy token, for CI",
		Description: "  darkubectl deploy --app-id $APP_ID --deploy-token $DEPLOY_TOKEN --tag $CI_COMMIT_SHORT_SHA\n\n" +
			"What `darkube deploy` does in a pipeline, with no login and no config file: the\n" +
			"app id and its deploy token are the whole credential, and both come from\n" +
			"`darkubectl get deploy-token` (or `get ci-config --for env`). Each flag also\n" +
			"reads an environment variable, so a job can set them as masked CI variables.\n" +
			"--job-id defaults to GitLab's CI_JOB_ID or GitHub's GITHUB_RUN_ID.\n\n" +
			"The platform answers once the change is accepted; the rollout continues\n" +
			"after this command exits.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagAppID, Sources: cli.EnvVars("DARKUBE_APP_ID"), Usage: "the app's id"},
			&cli.StringFlag{Name: flagDeployToken, Sources: cli.EnvVars("DARKUBE_DEPLOY_TOKEN"), Usage: "the app's deploy token"},
			&cli.StringFlag{Name: flagTag, Sources: cli.EnvVars("DARKUBE_IMAGE_TAG"), Usage: "image tag to deploy"},
			&cli.StringFlag{Name: flagJobID, Sources: cli.EnvVars("CI_JOB_ID", "GITHUB_RUN_ID"), Usage: "CI job id to report"},
		},
		Action: deployAction,
	}
}

func deployAction(ctx context.Context, cmd *cli.Command) error {
	in := client.DeployInput{
		AppID:       cmd.String(flagAppID),
		DeployToken: cmd.String(flagDeployToken),
		ImageTag:    cmd.String(flagTag),
		JobID:       cmd.String(flagJobID),
	}
	if in.AppID == "" || in.DeployToken == "" || in.ImageTag == "" {
		return errDeployArgs
	}
	// No account credential: the token in the body is the authentication, so
	// this works on a CI runner that has never logged in.
	c := client.New(cmd.String(flagBaseURL), "", "")
	if err := c.DeployWithToken(ctx, in); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app %s deploying image tag %s\n", in.AppID, in.ImageTag)
	return nil
}
