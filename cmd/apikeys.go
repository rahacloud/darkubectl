package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const cmdAPIKey = "apikey"

var (
	errMissingKeyName = errors.New("an API key NAME is required")
	errNoSuchKey      = errors.New("no API key with that name or id")
	errAmbiguousKey   = errors.New("several API keys have that name; pass the id")
)

func newGetAPIKeysCommand() *cli.Command {
	return &cli.Command{
		Name:        "apikeys",
		Aliases:     []string{cmdAPIKey, "api-keys", "keys"},
		Usage:       "List the account's API keys (no tenant needed)",
		Description: "Names and expiry only: a key's value is shown once, when it is created.",
		Action:      getAPIKeysAction,
	}
}

func newCreateAPIKeyCommand() *cli.Command {
	return &cli.Command{
		Name:      cmdAPIKey,
		Aliases:   []string{"api-key", "key"},
		Usage:     "Create an account API key and print it (prints a secret)",
		ArgsUsage: colName,
		Description: "  darkubectl create apikey ci-pipeline\n\n" +
			"The key is printed once, on stdout, and cannot be read back. The platform sets\n" +
			"its expiry, two years out, and accepts no other: revoke it with\n" +
			"`delete apikey` when it is no longer needed. It acts as the\n" +
			"whole account, across every tenant; for deploying one app from CI the app's\n" +
			"deploy token (`get deploy-token`, `deploy`) is the narrower credential.",
		Action: createAPIKeyAction,
	}
}

func newDeleteAPIKeyCommand() *cli.Command {
	return &cli.Command{
		Name:      cmdAPIKey,
		Aliases:   []string{"api-key", "key"},
		Usage:     "Revoke an account API key",
		ArgsUsage: "NAME|ID",
		Flags:     []cli.Flag{&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}},
		Action:    deleteAPIKeyAction,
	}
}

func getAPIKeysAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newGlobalClient(ctx, cmd)
	if err != nil {
		return err
	}
	keys, err := c.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, keys); handled {
		return err
	}
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{k.Name, ageOf(k.CreationTime), shortDate(k.ExpirationTime), k.ID})
	}
	return output.StyledTable(os.Stdout, []string{colName, colAge, "EXPIRES", "ID"}, rows, nil)
}

func createAPIKeyAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingKeyName
	}
	c, err := newGlobalClient(ctx, cmd)
	if err != nil {
		return err
	}
	value, err := c.CreateAPIKey(ctx, name)
	if err != nil {
		return err
	}
	// The expiry is the platform's choice; read it back rather than assume it.
	expires := "-"
	if keys, err := c.ListAPIKeys(ctx); err == nil {
		for _, k := range keys {
			if k.Name == name {
				expires = shortDate(k.ExpirationTime)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "API key %q created, expiring %s. This is the only time it is shown:\n", name, expires)
	fmt.Fprintln(os.Stdout, value)
	return nil
}

func deleteAPIKeyAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingKeyName
	}
	c, err := newGlobalClient(ctx, cmd)
	if err != nil {
		return err
	}
	keys, err := c.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	key, err := findAPIKey(keys, ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "About to revoke API key %q (%s). Anything using it stops working.\n", key.Name, key.ID)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.DeleteAPIKey(ctx, key.ID); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "apikey/%s revoked\n", key.Name)
	return nil
}

// findAPIKey resolves a key by id, or by name when the name is unique.
func findAPIKey(keys []client.APIKeyInfo, ref string) (*client.APIKeyInfo, error) {
	if i := slices.IndexFunc(keys, func(k client.APIKeyInfo) bool { return k.ID == ref }); i >= 0 {
		return &keys[i], nil
	}
	var hits []int
	for i, k := range keys {
		if k.Name == ref {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("%w: %q", errNoSuchKey, ref)
	case 1:
		return &keys[hits[0]], nil
	default:
		return nil, fmt.Errorf("%w: %q", errAmbiguousKey, ref)
	}
}
