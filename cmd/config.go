package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

// minRedactLen is the shortest token length shown partially rather than as ****.
const minRedactLen = 8

func newConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Manage darkubectl configuration (token and tenants)",
		Commands: []*cli.Command{
			{
				Name:   "view",
				Usage:  "Show the current configuration (token redacted)",
				Action: configViewAction,
			},
			{
				Name:      "set-token",
				Usage:     "Store the account API key",
				ArgsUsage: "TOKEN",
				Action:    configSetTokenAction,
			},
			{
				Name:      "use-tenant",
				Usage:     "Set the active tenant (organization)",
				ArgsUsage: "NAME",
				Action:    configUseTenantAction,
			},
			{
				Name:      "add-tenant",
				Usage:     "Record a tenant (organization) slug",
				ArgsUsage: "NAME",
				Action:    configAddTenantAction,
			},
		},
	}
}

func configViewAction(_ context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "config:         %s\n", cfg.Path())
	fmt.Fprintf(os.Stdout, "token:          %s\n", redact(cfg.Token))
	fmt.Fprintf(os.Stdout, "current-tenant: %s\n", orDash(cfg.CurrentTenant))
	fmt.Fprintf(os.Stdout, "base-url:       %s\n", orDash(cfg.BaseURL))
	fmt.Fprintf(os.Stdout, "tenants:        %v\n", cfg.Tenants)
	return nil
}

func configSetTokenAction(_ context.Context, cmd *cli.Command) error {
	token := cmd.Args().First()
	if token == "" {
		return errors.New("a TOKEN argument is required")
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	cfg.Token = token
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "token saved to %s\n", cfg.Path())
	return nil
}

func configUseTenantAction(_ context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errors.New("a tenant NAME argument is required")
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	cfg.CurrentTenant = name
	cfg.AddTenant(name)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "switched to tenant %q\n", name)
	return nil
}

func configAddTenantAction(_ context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errors.New("a tenant NAME argument is required")
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	cfg.AddTenant(name)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "added tenant %q\n", name)
	return nil
}

func redact(s string) string {
	switch {
	case s == "":
		return "(unset)"
	case len(s) <= minRedactLen:
		return "****"
	default:
		return s[:4] + "…" + s[len(s)-4:]
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
