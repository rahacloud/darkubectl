package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// Completing subcommands and flags is urfave/cli's job. What it cannot know is
// the app names, and those are what every other word on the line is: `set image
// <TAB>` wants an app. So every command whose first argument is an app gets a
// completer that lists them — one API call per TAB, bounded so a slow or
// unreachable API makes completion quietly offer nothing rather than hang the
// shell.

const completionTimeout = 3 * time.Second

// withAppCompletion walks the tree and attaches completeAppNames to every
// command whose first argument is an app reference.
func withAppCompletion(cmd *cli.Command) *cli.Command {
	if strings.HasPrefix(cmd.ArgsUsage, argRefUsage) || strings.HasPrefix(cmd.ArgsUsage, "NAME|ID") ||
		strings.HasPrefix(cmd.ArgsUsage, "APP|ID") {
		cmd.ShellComplete = completeAppNames
	}
	for _, sub := range cmd.Commands {
		withAppCompletion(sub)
	}
	return cmd
}

// completeAppNames offers app names for the first argument and falls back to
// flag completion once one has been given, or when a flag is being typed.
func completeAppNames(ctx context.Context, cmd *cli.Command) {
	if cmd.NArg() > 0 || lastArgIsFlag() {
		cli.DefaultCompleteWithFlags(ctx, cmd)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, completionTimeout)
	defer cancel()

	c, err := newClient(ctx, cmd)
	if err != nil {
		return
	}
	apps, err := c.ListApps(ctx)
	if err != nil {
		return
	}
	for _, a := range apps {
		fmt.Fprintln(os.Stdout, a.Name)
	}
}

// lastArgIsFlag reports whether the word being completed starts a flag. The
// completion flag itself is the final element of os.Args, so the word the user
// is typing is the one before it.
func lastArgIsFlag() bool {
	// Program name, the word being typed, and the completion flag.
	const minArgs = 3
	if len(os.Args) < minArgs {
		return false
	}
	return strings.HasPrefix(os.Args[len(os.Args)-2], "-")
}
