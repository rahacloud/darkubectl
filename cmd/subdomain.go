package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// A Darkube app has two quite different kinds of hostname, and mixing them up
// costs an opaque rejection:
//
//   - external_hosts — domains you own and point at the cluster with a CNAME.
//     `set domain --add` manages these.
//   - custom_subdomain_addr — a label under the cluster's own
//     apps_custom_base_domain (usually darkube.app), which the platform hosts
//     and issues a certificate for over a dns01 challenge. This command.
//
// Adding a platform subdomain through external_hosts fails with
// `400 InvalidExternalHost` and a Persian detail that does not explain which of
// the two you wanted. Confirmed against the live API on 2026-08-27.

const flagSSL = "ssl"

var (
	errSubdomainArgs = errors.New("usage: darkubectl set subdomain NAME|ID LABEL (or --remove to clear it)")
	errSubdomainDots = errors.New(
		"a subdomain is the bare label, not a full hostname: pass `my-app`, not `my-app.darkube.app`")
)

func newSetSubdomainCommand() *cli.Command {
	return &cli.Command{
		Name:      "subdomain",
		Usage:     "Set the app's subdomain on the cluster's own domain",
		ArgsUsage: "NAME|ID LABEL",
		Description: "  darkubectl set subdomain my-api my-api-prod\n" +
			"      -> my-api-prod.darkube.app, with a certificate\n" +
			"  darkubectl set subdomain my-api --remove\n\n" +
			"This is the platform's own subdomain (custom_subdomain_addr), not a domain you\n" +
			"own. For a domain you own, point its DNS at the CNAME target from `get domains`\n" +
			"and add it with `set domain --add` instead — passing a *.darkube.app name there\n" +
			"is rejected with 400 InvalidExternalHost.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagRemove, Usage: "clear the subdomain instead of setting one"},
			&cli.BoolFlag{Name: flagSSL, Value: true, Usage: "also enable SSL (the point of having the subdomain)"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: setSubdomainAction,
	}
}

func setSubdomainAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	remove := cmd.Bool(flagRemove)
	label := cmd.Args().Get(1)
	switch {
	case remove && label != "":
		return errSubdomainArgs
	case !remove && label == "":
		return errSubdomainArgs
	case strings.Contains(label, "."):
		// Passing the full hostname is the obvious mistake, and the API would
		// take it and build a nonsense name from it rather than refuse.
		return errSubdomainDots
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
	base := clusterBaseDomain(raw)

	action := "set subdomain to " + hostname(label, base)
	if remove {
		action = "clear the subdomain"
	}
	fmt.Fprintf(os.Stderr, "About to %s on app %q (%s) in tenant %q\n", action, app.Name, app.ID, c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	updated, err := c.UpdateApp(ctx, app.ID, func(a map[string]any) error {
		if remove {
			a["custom_subdomain_addr"] = nil
			return nil
		}
		a["custom_subdomain_addr"] = label
		if cmd.Bool(flagSSL) {
			a["enable_SSL"] = true
		}
		return nil
	})
	if err != nil {
		return err
	}

	if remove {
		fmt.Fprintf(os.Stdout, "app/%s subdomain cleared\n", app.Name)
		return nil
	}
	host := rawString(updated, "custom_domain_address")
	if host == "" {
		host = hostname(label, base)
	}
	fmt.Fprintf(os.Stdout, "app/%s subdomain set to %s\n", app.Name, host)
	fmt.Fprintf(os.Stderr,
		"note: DNS and the certificate are issued by the platform and take a few minutes.\n"+
			"      `darkubectl wait app %s --for ready` waits for the app; the hostname may lag it.\n",
		app.Name)
	return nil
}

// clusterBaseDomain reads apps_custom_base_domain off the app's nested cluster,
// e.g. "darkube.app". Empty if the field is absent.
func clusterBaseDomain(raw map[string]any) string {
	cluster, ok := raw["cluster"].(map[string]any)
	if !ok {
		return ""
	}
	base, _ := cluster["apps_custom_base_domain"].(string)
	return base
}

// hostname joins a subdomain label to the cluster base domain for display.
func hostname(label, base string) string {
	if base == "" {
		return label
	}
	return label + "." + base
}
