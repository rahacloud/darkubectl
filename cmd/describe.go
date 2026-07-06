package cmd

import (
	"context"
	"os"

	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

func newDescribeCommand() *cli.Command {
	return &cli.Command{
		Name:  "describe",
		Usage: "Show details of a specific resource",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Show the full detail of an app",
				ArgsUsage: argRefUsage,
				Action:    describeAppAction,
			},
		},
	}
}

func describeAppAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	c, err := newClient(cmd)
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
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}

	// describe defaults to YAML (rich object); a flat table isn't meaningful here.
	if format == output.Table || format == output.Wide {
		format = output.YAML
	}
	if format == output.Name {
		_, err := os.Stdout.WriteString(app.ID + "\n")
		return err
	}
	_, err = output.Structured(os.Stdout, format, raw)
	return err
}
