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
					`  darkubectl patch app my-api -p '{"enable_SSL": true, "redirect_SSL": true}'` + "\n\n" +
					"The API implements no partial update, so this is a read-modify-write of the\n" +
					"whole app rather than an HTTP PATCH. A concurrent console edit between the\n" +
					"read and the write is therefore lost.\n\n" +
					"--dry-run shows which fields the write would actually change and sends nothing.\n" +
					"That is worth more here than on an API with a real partial update: the request\n" +
					"body is the whole app, so it does not show which of its seventy fields moved.\n\n" +
					"After the write every changed field is read back. The platform drops fields it\n" +
					"does not store without an error (custom_config is filtered against the chart,\n" +
					"and ram_limit/cpu_request on a fixed plan follow the plan), and such a patch\n" +
					"fails instead of reporting success. Use `set resources` for the app's size.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "patch",
						Aliases:  []string{"p"},
						Required: true,
						Usage:    "JSON object to merge-patch onto the app",
					},
					&cli.BoolFlag{
						Name:  flagDryRun,
						Usage: usageDryRun,
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

	apply := func(raw map[string]any) error {
		maps.Copy(raw, patch)
		return nil
	}

	if cmd.Bool(flagDryRun) {
		before, after, err := c.PrepareAppUpdate(ctx, app.ID, apply)
		if err != nil {
			return err
		}
		changes := diffApps(before, after)
		if handled, err := output.Structured(os.Stdout, format, changes); handled {
			return err
		}
		fmt.Fprintf(os.Stdout, "app/%s: dry run, nothing was sent\n", app.Name)
		writeDiff(os.Stdout, changes)
		return nil
	}

	wrote, err := runAppChange(ctx, cmd, c, app, appChange{what: "patch", apply: apply, verifyDiff: true})
	if err != nil || !wrote {
		return err
	}
	if format == output.JSON || format == output.YAML {
		updated, err := c.GetApp(ctx, app.ID)
		if err != nil {
			return err
		}
		_, err = output.Structured(os.Stdout, format, updated)
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s patched\n", app.Name)
	return nil
}
