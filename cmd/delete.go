package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func newDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a resource",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Delete an app",
				ArgsUsage: argRefUsage,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    flagYes,
						Aliases: []string{aliasYes},
						Usage:   "skip the confirmation prompt (dangerous)",
					},
				},
				Action: deleteAppAction,
			},
		},
	}
}

func deleteAppAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	c, err := newClient(cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to DELETE app %q (%s) in tenant %q. This cannot be undone.\n",
		app.Name, app.ID, c.Org)
	if !cmd.Bool(flagYes) && !confirmExact(fmt.Sprintf("Type the app name %q to confirm: ", app.Name), app.Name) {
		return errAborted
	}

	if err := c.DeleteApp(ctx, app.ID); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s deleted\n", app.Name)
	return nil
}
