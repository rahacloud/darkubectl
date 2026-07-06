package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

func newPatchCommand() *cli.Command {
	return &cli.Command{
		Name:  "patch",
		Usage: "Apply a raw JSON merge patch to a resource",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Patch an app with a JSON object (HTTP PATCH)",
				ArgsUsage: argRefUsage,
				Description: "The JSON is sent verbatim as an HTTP PATCH to the app. Use this for\n" +
					"fields not covered by dedicated commands, e.g.:\n\n" +
					`  darkubectl patch app my-api -p '{"ram_limit": 1024, "cpu_request": 500}'`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "patch",
						Aliases:  []string{"p"},
						Required: true,
						Usage:    "JSON object to merge-patch onto the app",
					},
					&cli.BoolFlag{
						Name:    flagYes,
						Aliases: []string{aliasYes},
						Usage:   usageSkipConfirm,
					},
				},
				Action: patchAppAction,
			},
		},
	}
}

func patchAppAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	patchJSON := cmd.String("patch")
	var patch map[string]any
	if err := json.Unmarshal([]byte(patchJSON), &patch); err != nil {
		return fmt.Errorf("invalid --patch JSON: %w", err)
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to PATCH app %q (%s) in tenant %q with: %s\n",
		app.Name, app.ID, c.Org, patchJSON)
	if !cmd.Bool(flagYes) && !confirm("Proceed?") {
		return errAborted
	}

	updated, err := c.PatchApp(ctx, app.ID, patch)
	if err != nil {
		return err
	}
	if format == output.JSON || format == output.YAML {
		_, err := output.Structured(os.Stdout, format, updated)
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s patched\n", app.Name)
	return nil
}
