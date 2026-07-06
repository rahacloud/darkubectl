package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/auth"
	"github.com/rahacloud/darkubectl/internal/config"
	"github.com/urfave/cli/v3"
)

const (
	flagRefreshToken      = "refresh-token"
	flagRefreshTokenStdin = "refresh-token-stdin"
)

func newLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Authenticate with a JWT (email + password + TOTP, or a refresh token)",
		Description: "Stores a Console JWT refresh token so `terminal`/`exec` can open the\n" +
			"websocket, and so the JWT can also drive the REST API when no Api-key is set.\n\n" +
			"By default it logs in interactively (email, password, TOTP). Alternatively,\n" +
			"pass an existing refresh token with --refresh-token or --refresh-token-stdin;\n" +
			"a refresh token is as powerful as a full login.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "email", Usage: "account email (prompted if omitted)"},
			&cli.StringFlag{Name: flagRefreshToken, Usage: "store this refresh token instead of an interactive login"},
			&cli.BoolFlag{Name: flagRefreshTokenStdin, Usage: "read the refresh token to store from stdin"},
		},
		Action: loginAction,
	}
}

func loginAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	a := auth.New(resolveBaseURL(cmd, cfg))
	defer func() { _ = a.Close() }()

	// Method 1: a refresh token supplied directly (flag or stdin).
	refresh, err := providedRefreshToken(cmd)
	if err != nil {
		return err
	}
	if refresh != "" {
		// Validate it by minting one access token before storing.
		if _, rerr := a.Refresh(ctx, refresh); rerr != nil {
			return fmt.Errorf("the provided refresh token is not valid: %w", rerr)
		}
		return storeRefresh(cfg, refresh)
	}

	// Method 2: interactive email + password + TOTP.
	email := cmd.String("email")
	if email == "" {
		if email, err = prompt("Hamravesh email: "); err != nil {
			return err
		}
	}
	password, err := promptPassword("Password: ")
	if err != nil {
		return err
	}
	otp, err := prompt("TOTP code: ")
	if err != nil {
		return err
	}

	tokens, err := a.Login(ctx, email, password, otp)
	if err != nil {
		return err
	}
	return storeRefresh(cfg, tokens.Refresh)
}

// providedRefreshToken returns a refresh token supplied via flag or stdin, or "".
func providedRefreshToken(cmd *cli.Command) (string, error) {
	if cmd.Bool(flagRefreshTokenStdin) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read refresh token from stdin: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return strings.TrimSpace(cmd.String(flagRefreshToken)), nil
}

func storeRefresh(cfg *config.Config, refresh string) error {
	cfg.RefreshToken = refresh
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "login successful; refresh token stored in", cfg.Path())
	return nil
}
