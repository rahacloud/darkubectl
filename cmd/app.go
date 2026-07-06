// Package cmd implements the darkubectl command tree on urfave/cli/v3.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/config"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// Persistent flag names, shared across commands.
const (
	flagConfig  = "config"
	flagToken   = "token"
	flagOrg     = "org"
	flagBaseURL = "base-url"
	flagOutput  = "output"
)

// Shared command, argument, and flag literals reused across the app-oriented
// command trees (describe/scale/patch/delete and get apps).
const (
	cmdApp      = "app"
	aliasApp    = "application"
	argRefUsage = "NAME|ID"
	flagYes     = "yes"
	aliasYes    = "y"
)

// Sentinel errors for command-level validation.
var (
	errNoCredentials = errors.New("no credentials: set an API key with `darkubectl config set-token` " +
		"(or --token/$DARKUBE_TOKEN), or run `darkubectl login`")
	errNoTenant = errors.New("no tenant selected: set one with `darkubectl config use-tenant <name>`, --org, or $DARKUBE_ORG")
)

// NewApp builds the root command with its persistent flags and subcommands.
func NewApp() *cli.Command {
	return &cli.Command{
		Name:  "darkubectl",
		Usage: "kubectl-like access to the Hamravesh Darkube platform",
		Description: "Tenants are Darkube organizations, selected with --org or a config context.\n" +
			"Authentication uses an account API key (Authorization: Api-key) plus the\n" +
			"active tenant (X-Organization).",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  flagConfig,
				Usage: "config file (default $DARKUBE_CONFIG or ~/.darkube/config.yaml)",
			},
			&cli.StringFlag{
				Name:  flagToken,
				Usage: "account API key (overrides config)",
			},
			&cli.StringFlag{
				Name:    flagOrg,
				Aliases: []string{"n"},
				Usage:   "tenant/organization slug (overrides current-tenant)",
			},
			&cli.StringFlag{
				Name:  flagBaseURL,
				Usage: "API base URL (advanced)",
			},
			&cli.StringFlag{
				Name:    flagOutput,
				Aliases: []string{"o"},
				Value:   string(output.Table),
				Usage:   "output format: table|wide|json|yaml|name",
			},
		},
		Commands: []*cli.Command{
			newGetCommand(),
			newDescribeCommand(),
			newScaleCommand(),
			newPatchCommand(),
			newDeleteCommand(),
			newLoginCommand(),
			newExecCommand(),
			newTerminalCommand(),
			newConfigCommand(),
			newVersionCommand(),
		},
	}
}

func newVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print the darkubectl version",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Fprintf(os.Stdout, "darkubectl %s\n", version)
			return nil
		},
	}
}

// loadConfig resolves the config path (from --config or the default) and reads it.
func loadConfig(cmd *cli.Command) (*config.Config, error) {
	path := cmd.String(flagConfig)
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	return config.Load(path)
}

// resolveToken picks the token: --token overrides the (file+env) config.
func resolveToken(cmd *cli.Command, cfg *config.Config) string {
	if v := cmd.String(flagToken); v != "" {
		return v
	}
	return cfg.Token
}

// resolveOrg picks the tenant: --org overrides current-tenant from config.
func resolveOrg(cmd *cli.Command, cfg *config.Config) string {
	if v := cmd.String(flagOrg); v != "" {
		return v
	}
	return cfg.CurrentTenant
}

// resolveBaseURL picks the base URL: --base-url > config base-url > default.
func resolveBaseURL(cmd *cli.Command, cfg *config.Config) string {
	if v := cmd.String(flagBaseURL); v != "" {
		return v
	}
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	return client.DefaultBaseURL
}

// newClient builds an API client for the active tenant, validating required inputs.
func newClient(ctx context.Context, cmd *cli.Command) (*client.Client, error) {
	c, _, err := buildClient(ctx, cmd)
	return c, err
}

// buildClient is like newClient but also returns the loaded config, for commands
// (login/exec/terminal) that need the JWT credentials alongside the REST client.
func buildClient(ctx context.Context, cmd *cli.Command) (*client.Client, *config.Config, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return nil, nil, err
	}
	org := resolveOrg(cmd, cfg)
	if org == "" {
		return nil, nil, errNoTenant
	}
	auth, err := resolveAuth(ctx, cmd, cfg)
	if err != nil {
		return nil, nil, err
	}
	return client.New(resolveBaseURL(cmd, cfg), auth, org), cfg, nil
}

// resolveAuth chooses REST authentication: an Api-key if one is configured,
// otherwise a Console JWT (Bearer) minted from a login/refresh token. Either
// credential can drive the whole API, so a login is a full alternative to the
// Api-key (and the only credential that can also open the terminal).
func resolveAuth(ctx context.Context, cmd *cli.Command, cfg *config.Config) (client.Auth, error) {
	if token := resolveToken(cmd, cfg); token != "" {
		return client.APIKey(token), nil
	}
	access, err := accessToken(ctx, cmd, cfg)
	if err != nil {
		if errors.Is(err, errNotLoggedIn) {
			return "", errNoCredentials
		}
		return "", err
	}
	return client.BearerToken(access), nil
}

// outputFormat parses the -o flag.
func outputFormat(cmd *cli.Command) (output.Format, error) {
	return output.Parse(cmd.String(flagOutput))
}
