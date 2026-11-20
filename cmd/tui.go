package cmd

import (
	"context"
	"errors"
	"os"
	"strings"

	browserUI "github.com/rahacloud/darkubectl/internal/tui/browser"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func newTUICommand() *cli.Command {
	return &cli.Command{
		Name:  "tui",
		Usage: "Browse Darkube apps interactively (experimental)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagMockFile, Usage: "load a local YAML profile without connecting to Darkube"},
			&cli.BoolFlag{Name: flagTUIAllowWrites, Usage: "enable confirmed write actions in the TUI"},
		},
		Action: tuiAction,
	}
}

const (
	flagMockFile       = "mock-file"
	flagTUIAllowWrites = "allow-writes"
)

func tuiAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 0 {
		return errors.New("tui does not accept positional arguments")
	}
	if cmd.String(flagOutput) != "table" {
		return errors.New("tui only supports table output")
	}
	mockFileSet := cmd.IsSet(flagMockFile)
	mockPath := strings.TrimSpace(cmd.String(flagMockFile))
	if mockFileSet {
		if mockPath == "" {
			return errors.New("--mock-file requires a YAML file path")
		}
		if err := rejectMockConnectionFlags(cmd); err != nil {
			return err
		}
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("tui requires a terminal on stdin and stdout")
	}
	if mockFileSet {
		profile, err := browserUI.LoadMockProfile(mockPath)
		if err != nil {
			return err
		}
		return browserUI.RunMockBrowser(ctx, profile)
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	credential, err := newTUICredential(ctx, cmd, cfg)
	if err != nil {
		return err
	}
	baseURL := resolveBaseURL(cmd, cfg)
	org := resolveOrg(cmd, cfg)
	return browserUI.RunBrowser(ctx, newTUIBrowserOptions(cmd, cfg, credential, baseURL, org))
}

func rejectMockConnectionFlags(cmd *cli.Command) error {
	for _, name := range []string{flagConfig, flagToken, flagOrg, flagBaseURL} {
		if cmd.IsSet(name) {
			return errors.New("--mock-file cannot be combined with --" + name + "; fixture mode is offline")
		}
	}
	return nil
}
