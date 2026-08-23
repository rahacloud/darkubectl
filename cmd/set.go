package cmd

import "github.com/urfave/cli/v3"

// newSetCommand groups the field-level mutations.
//
// Every subcommand is a read-modify-write over the whole app object, because the
// API implements no partial update; see client.UpdateApp.
func newSetCommand() *cli.Command {
	return &cli.Command{
		Name:  "set",
		Usage: "Update specific fields of a resource",
		Commands: []*cli.Command{
			newSetEnvCommand(),
		},
	}
}
