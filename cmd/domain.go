package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// flagAdd names a domain to route to an app.
const flagAdd = "add"

var (
	errNoDomainChange    = errors.New("nothing to do: pass --add and/or --remove")
	errPlatformSubdomain = errors.New("that is a platform subdomain, not an external host")
)

func newGetDomainsCommand() *cli.Command {
	return &cli.Command{
		Name:      "domains",
		Aliases:   []string{"domain", "ingress"},
		Usage:     "Show the domains and ingress settings of an app",
		ArgsUsage: argRefUsage,
		Description: "Domains live in the app's external_hosts list. Point each one's DNS at the\n" +
			"cluster's CNAME target, shown here as CNAME-TARGET.",
		Action: getDomainsAction,
	}
}

func getDomainsAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}

	report := ingressReport{
		Hosts:            client.ExternalHosts(raw),
		CNAMETarget:      rawString(raw, "ingress_cname_address"),
		IngressClassName: rawString(raw, "ingress_class_name"),
		SSLChallengeType: rawString(raw, "ssl_challenge_type"),
		EnableSSL:        rawBool(raw, "enable_SSL"),
		RedirectSSL:      rawBool(raw, "redirect_SSL"),
		EnableHTTPV2:     rawBool(raw, "enable_httpv2"),
	}

	if handled, err := output.Structured(os.Stdout, format, report); handled {
		return err
	}
	if format == output.Name {
		for _, h := range report.Hosts {
			fmt.Fprintln(os.Stdout, h)
		}
		return nil
	}

	if len(report.Hosts) == 0 {
		fmt.Fprintf(os.Stderr, "app %q serves no custom domains\n", app.Name)
	} else {
		rows := make([][]string, 0, len(report.Hosts))
		for _, h := range report.Hosts {
			rows = append(rows, []string{h, dash(report.CNAMETarget)})
		}
		if err := output.StyledTable(os.Stdout, []string{"DOMAIN", "CNAME-TARGET"}, rows, nil); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stdout, "\nSSL: %s   redirect-to-https: %s   http/2: %s   challenge: %s   class: %s\n",
		yesNo(report.EnableSSL), yesNo(report.RedirectSSL), yesNo(report.EnableHTTPV2),
		dash(report.SSLChallengeType), dash(report.IngressClassName))
	return nil
}

// ingressReport is the -o json|yaml shape of `get domains`.
type ingressReport struct {
	Hosts            []string `json:"hosts"                      yaml:"hosts"`
	CNAMETarget      string   `json:"cnameTarget"                yaml:"cnameTarget"`
	IngressClassName string   `json:"ingressClassName,omitempty" yaml:"ingressClassName,omitempty"`
	SSLChallengeType string   `json:"sslChallengeType"           yaml:"sslChallengeType"`
	EnableSSL        bool     `json:"sslEnabled"                 yaml:"sslEnabled"`
	RedirectSSL      bool     `json:"sslRedirect"                yaml:"sslRedirect"`
	EnableHTTPV2     bool     `json:"http2Enabled"               yaml:"http2Enabled"`
}

func newSetDomainCommand() *cli.Command {
	return &cli.Command{
		Name:      "domain",
		Aliases:   []string{"domains", "ingress"},
		Usage:     "Add or remove domains routed to an app",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl set domain my-api --add api.example.com\n" +
			"  darkubectl set domain my-api --remove old.example.com\n\n" +
			"Point the domain's DNS at the cluster CNAME target from `get domains` before\n" +
			"adding it, or certificate issuance will not complete.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: flagAdd, Usage: "domain to route to this app (repeatable)"},
			&cli.StringSliceFlag{Name: flagRemove, Usage: "domain to stop routing (repeatable)"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: setDomainAction,
	}
}

func setDomainAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	additions := cmd.StringSlice(flagAdd)
	removals := cmd.StringSlice(flagRemove)
	if len(additions) == 0 && len(removals) == 0 {
		return errNoDomainChange
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}
	current, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}
	// Catch the platform-subdomain mistake here, rather than letting the API
	// answer it with an opaque 400 InvalidExternalHost.
	if err := rejectPlatformSubdomains(additions, clusterBaseDomain(current)); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to update domains on app %q (%s) in tenant %q: %s\n",
		app.Name, app.ID, c.Org, describeDomainChange(additions, removals))
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	updated, err := c.UpdateApp(ctx, app.ID, func(raw map[string]any) error {
		return applyDomainChange(raw, additions, removals)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s domains updated\n", app.Name)
	if target := rawString(updated, "ingress_cname_address"); target != "" && len(additions) > 0 {
		fmt.Fprintf(os.Stderr, "note: point each added domain's DNS at %s\n", target)
	}
	return nil
}

// rejectPlatformSubdomains refuses hosts under the cluster's own base domain.
//
// external_hosts is for domains you own and CNAME in; a <label>.darkube.app name
// is the platform's own subdomain and belongs in custom_subdomain_addr. The API
// distinguishes them but reports the difference only as 400 InvalidExternalHost
// with a Persian detail, which is not enough to act on.
func rejectPlatformSubdomains(additions []string, base string) error {
	if base == "" {
		return nil
	}
	for _, h := range additions {
		if !strings.HasSuffix(strings.ToLower(h), "."+strings.ToLower(base)) {
			continue
		}
		label := strings.TrimSuffix(h, "."+base)
		return fmt.Errorf(
			"%w: %q is a subdomain of the cluster's own domain %q, which the API will not accept as an "+
				"external host (400 InvalidExternalHost).\n"+
				"  Use the dedicated command instead:\n"+
				"      darkubectl set subdomain <app> %s\n"+
				"  `set domain --add` is for domains you own and point at the cluster with a CNAME",
			errPlatformSubdomain, h, base, label)
	}
	return nil
}

// applyDomainChange merges domain additions and removals into external_hosts.
func applyDomainChange(raw map[string]any, additions, removals []string) error {
	hosts := client.ExternalHosts(raw)
	for _, h := range additions {
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	for _, h := range removals {
		idx := slices.Index(hosts, h)
		if idx < 0 {
			return fmt.Errorf("%w: %q", client.ErrNoSuchHost, h)
		}
		hosts = slices.Delete(hosts, idx, idx+1)
	}
	client.SetExternalHosts(raw, hosts)
	return nil
}

func describeDomainChange(additions, removals []string) string {
	var parts []string
	if len(additions) > 0 {
		parts = append(parts, "add "+strings.Join(additions, ", "))
	}
	if len(removals) > 0 {
		parts = append(parts, "remove "+strings.Join(removals, ", "))
	}
	return strings.Join(parts, "; ")
}

// rawString reads a string field from a raw app object, tolerating null.
func rawString(raw map[string]any, key string) string {
	s, _ := raw[key].(string)
	return s
}

// rawBool reads a bool field from a raw app object, tolerating null.
func rawBool(raw map[string]any, key string) bool {
	b, _ := raw[key].(bool)
	return b
}
