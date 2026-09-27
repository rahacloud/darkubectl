package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

// `set command` exists because the command/args asymmetry (see appargs.go) is
// most often discovered on an app that is already crash-looping, and until now
// the warnings that explain it only ran at `create`. This is where they help.

const (
	flagCommand = "command"
	flagArgs    = "args"
	flagClear   = "clear"

	keyCommand = "command"
	keyArgs    = "args"
)

var errEntrypointArgs = errors.New("give --command and/or --args, or --clear to return to the image's own entrypoint")

func newSetCommandCommand() *cli.Command {
	return &cli.Command{
		Name:      "command",
		Aliases:   []string{"entrypoint"},
		Usage:     "Change the container command and args of an app",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl set command my-worker --command \"/bin/sh -c\" --args 'cd /app && exec ./worker'\n" +
			"  darkubectl set command my-worker --clear\n\n" +
			"The two fields are NOT symmetric, and this is the sharpest edge in the API:\n" +
			"  command is SPLIT on whitespace   -> [\"/bin/sh\", \"-c\"]\n" +
			"  args    is NOT split, ever       -> [\"<the whole string>\"]\n" +
			"so a flag belongs in --command, and a script belongs whole in --args after a\n" +
			"`sh -c` in --command. Warnings are printed before the prompt.\n\n" +
			"An empty value clears that field (--args \"\"), and --clear clears both, handing\n" +
			"the container back to the image's ENTRYPOINT and CMD.",
		Flags: append(mutationFlags(),
			&cli.StringFlag{Name: flagCommand, Usage: "container command; SPLIT on whitespace"},
			&cli.StringFlag{Name: flagArgs, Usage: "container args; passed as ONE argument, never split"},
			&cli.BoolFlag{Name: flagClear, Usage: "clear both, returning to the image's entrypoint"},
		),
		Action: setCommandAction,
	}
}

func setCommandAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	clearBoth := cmd.Bool(flagClear)
	setCmd, setArgs := cmd.IsSet(flagCommand), cmd.IsSet(flagArgs)
	if clearBoth == (setCmd || setArgs) {
		return errEntrypointArgs
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}

	// What the container will end up with, so the warnings judge the pair
	// rather than whichever half was given.
	command, _ := raw[keyCommand].(string)
	args, _ := raw[keyArgs].(string)
	switch {
	case clearBoth:
		command, args = "", ""
	default:
		if setCmd {
			command = cmd.String(flagCommand)
		}
		if setArgs {
			args = cmd.String(flagArgs)
		}
	}
	for _, w := range entrypointWarnings(command, args) {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	changed, err := runAppChange(ctx, cmd, c, app, appChange{
		what: "change the entrypoint of",
		apply: func(raw map[string]any) error {
			raw[keyCommand], raw[keyArgs] = command, args
			return nil
		},
		took: func(raw map[string]any) bool {
			gotCmd, _ := raw[keyCommand].(string)
			gotArgs, _ := raw[keyArgs].(string)
			return gotCmd == command && gotArgs == args
		},
	})
	if err != nil || !changed {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s entrypoint set: command=%q args=%q\n", app.Name, command, args)
	return nil
}
