package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

// `set image` is the deploy step of a pipeline that builds its own images:
// change image_repo/image_tag in place and, with --wait, block until the new
// pods are serving. It is the same write the console makes when a tag is
// edited by hand, and it needs no `darkube` binary and no deploy token.

const (
	keyImageRepo = "image_repo"
	keyImageTag  = "image_tag"
)

var errImageArgs = errors.New("usage: darkubectl set image NAME|ID IMAGE[:TAG]")

func newSetImageCommand() *cli.Command {
	return &cli.Command{
		Name:      "image",
		Usage:     "Change the container image an app runs",
		ArgsUsage: "NAME|ID IMAGE[:TAG]",
		Description: "  darkubectl set image my-api registry.hamdocker.ir/acme/my-api:1.4.2\n" +
			"  darkubectl set image my-api nginx:1.27-alpine --wait --timeout 5m\n\n" +
			"Rewrites image_repo and image_tag and lets the platform roll the Deployment.\n" +
			"A missing tag means `latest`, as with docker.\n\n" +
			"--wait follows the rollout by pod rather than by app state, so it does not\n" +
			"return while the old pod is still serving and the new one is crash-looping.\n\n" +
			"On an app Darkube builds from git, the next push rebuilds and replaces whatever\n" +
			"image is set here.",
		Flags:  append(mutationFlags(), rolloutWaitFlags()...),
		Action: setImageAction,
	}
}

func setImageAction(ctx context.Context, cmd *cli.Command) error {
	ref, image := cmd.Args().First(), strings.TrimSpace(cmd.Args().Get(1))
	if ref == "" || image == "" {
		return errImageArgs
	}
	repo, tag := splitImage(image)

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}

	var notes []string
	if app.CreationMethod == client.CreationMethodGitRepoURL {
		notes = append(notes, "this app is built from git: the next push rebuilds it and replaces this image")
	}

	var watcher *podWatcher
	if cmd.Bool(flagWait) && !cmd.Bool(flagDryRun) {
		if watcher, err = newPodWatcher(ctx, cmd, app); err != nil {
			return err
		}
	}

	changed, err := runAppChange(ctx, cmd, c, app, appChange{
		what:  "change the image of",
		apply: func(raw map[string]any) error { return applyImage(raw, repo, tag) },
		took:  func(raw map[string]any) bool { return imageOf(raw) == repo+":"+tag },
		notes: notes,
	})
	if err != nil || !changed {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s image set to %s:%s\n", app.Name, repo, tag)
	if watcher == nil {
		return nil
	}
	return watcher.wait(ctx, cmd.Duration(flagTimeout))
}

// applyImage sets the image fields on a normalized app object.
func applyImage(raw map[string]any, repo, tag string) error {
	raw[keyImageRepo] = repo
	raw[keyImageTag] = tag
	return nil
}

// imageOf renders an app object's image as repo:tag.
func imageOf(raw map[string]any) string {
	repo, _ := raw[keyImageRepo].(string)
	tag, _ := raw[keyImageTag].(string)
	return repo + ":" + tag
}
