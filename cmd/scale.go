package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

// errNegativeReplicas is returned when --replicas is below zero.
var errNegativeReplicas = errors.New("--replicas must be >= 0")

func newScaleCommand() *cli.Command {
	return &cli.Command{
		Name:  "scale",
		Usage: "Set the replica count of an app",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Scale an app to a given replica count",
				ArgsUsage: argRefUsage,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:     flagReplicas,
						Required: true,
						Usage:    "desired number of replicas",
					},
					&cli.BoolFlag{
						Name:    flagYes,
						Aliases: []string{aliasYes},
						Usage:   usageSkipConfirm,
					},
				},
				Action: scaleAppAction,
			},
		},
	}
}

func scaleAppAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	replicas := cmd.Int(flagReplicas)
	if replicas < 0 {
		return errNegativeReplicas
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to scale app %q (%s) in tenant %q: %d -> %d replicas.\n",
		app.Name, app.ID, c.Org, app.Replicas, replicas)
	if !cmd.Bool(flagYes) && !confirm("Proceed?") {
		return errAborted
	}

	if _, err := c.PatchApp(ctx, app.ID, map[string]any{"replicas": replicas}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s scaled to %d replicas\n", app.Name, replicas)
	return nil
}
