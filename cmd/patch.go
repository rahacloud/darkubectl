package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"

	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

func newPatchCommand() *cli.Command {
	return &cli.Command{
		Name:  "patch",
		Usage: "Merge a JSON object into a resource",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Merge a JSON object into an app",
				ArgsUsage: argRefUsage,
				Description: "The JSON is merged into the app's current object and written back. Use\n" +
					"this for fields not covered by dedicated commands, e.g.:\n\n" +
					`  darkubectl patch app my-api -p '{"ram_limit": "1024M", "cpu_request": "500m"}'` + "\n\n" +
					"The API implements no partial update, so this is a read-modify-write of the\n" +
					"whole app rather than an HTTP PATCH. A concurrent console edit between the\n" +
					"read and the write is therefore lost.",
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

	fmt.Fprintf(os.Stderr, "About to update app %q (%s) in tenant %q with: %s\n",
		app.Name, app.ID, c.Org, patchJSON)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	updated, err := c.UpdateApp(ctx, app.ID, func(raw map[string]any) error {
		maps.Copy(raw, patch)
		return nil
	})
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
