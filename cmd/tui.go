package cmd

import (
	"context"
	"errors"
	"os"

	"github.com/rahacloud/darkubectl/internal/tui"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func newTUICommand() *cli.Command {
	return &cli.Command{
		Name:   "tui",
		Usage:  "Browse Darkube apps interactively (experimental)",
		Action: tuiAction,
	}
}

func tuiAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 0 {
		return errors.New("tui does not accept positional arguments")
	}
	if cmd.String(flagOutput) != "table" {
		return errors.New("tui only supports table output")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("tui requires a terminal on stdin and stdout")
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return tui.RunBrowser(ctx, c.Org, c.ListApps)
}
