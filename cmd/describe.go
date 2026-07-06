package cmd

import (
	"context"
	"errors"
	"os"

	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/rahacloud/darkubectl/internal/tui"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

const flagInteractive = "interactive"

// errNotATerminal is returned when -i is requested but stdout is not a TTY.
var errNotATerminal = errors.New("--interactive requires an interactive terminal (stdout is not a TTY)")

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
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    flagInteractive,
						Aliases: []string{"i"},
						Usage:   "open an interactive, scrollable, searchable viewer",
					},
				},
				Action: describeAppAction,
			},
		},
	}
}

func describeAppAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
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
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}

	// Explicit machine formats pass through unchanged.
	switch format {
	case output.JSON, output.YAML:
		_, err = output.Structured(os.Stdout, format, raw)
		return err
	case output.Name:
		_, err = os.Stdout.WriteString(app.ID + "\n")
		return err
	case output.Table, output.Wide:
		// default describe view (colorized / interactive) handled below
	}

	title := "app/" + app.Name
	rows := output.FlattenObject(raw)

	if cmd.Bool(flagInteractive) {
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return errNotATerminal
		}
		return tui.RunDescribe(title, rows)
	}
	return output.RenderDescribe(os.Stdout, title, rows)
}
