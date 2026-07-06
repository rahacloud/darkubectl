package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/rahacloud/darkubectl/internal/auth"
	"github.com/urfave/cli/v3"
)

func newLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Authenticate for terminal/exec (email + password + TOTP)",
		Description: "Mints a Console JWT and stores the refresh token so `terminal` and `exec`\n" +
			"can open the websocket. This is separate from the account API key, which\n" +
			"cannot open the terminal.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "email", Usage: "account email (prompted if omitted)"},
		},
		Action: loginAction,
	}
}

func loginAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

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

	a := auth.New(resolveBaseURL(cmd, cfg))
	defer func() { _ = a.Close() }()

	tokens, err := a.Login(ctx, email, password, otp)
	if err != nil {
		return err
	}
	cfg.RefreshToken = tokens.Refresh
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "login successful; refresh token stored in", cfg.Path())
	return nil
}
