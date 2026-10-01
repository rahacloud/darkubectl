package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// flagAdd names a domain to route to an app.
const (
	flagAdd           = "add"
	flagChallenge     = "challenge"
	flagRedirectHTTPS = "redirect-https"
	flagNoReconcile   = "no-reconcile"

	flagTLS = "tls"

	challengeHTTP01 = "http01"
	challengeDNS01  = "dns01"
)

var (
	errNoDomainChange    = errors.New("nothing to do: pass --add and/or --remove")
	errPlatformSubdomain = errors.New("that is a platform subdomain, not an external host")
	errBadChallenge      = errors.New("--challenge must be http01 or dns01")
)

func newGetDomainsCommand() *cli.Command {
	return &cli.Command{
		Name:      "domains",
		Aliases:   []string{"domain", "ingress"},
		Usage:     "Show the domains and ingress settings of an app",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl get domains my-api\n" +
			"  darkubectl get domains my-api --tls   # also fetch the certificate each host serves\n\n" +
			"Domains live in the app's external_hosts list. Point each one's DNS at the\n" +
			"cluster's CNAME target, shown below the table.\n\n" +
			"DNS says whether the platform sees the name pointing at it. PLATFORM-CERT is the\n" +
			"certificate the platform issued, which is not necessarily what visitors get: a\n" +
			"domain fronted by another proxy keeps a stale or missing one here while being\n" +
			"served fine. --tls connects to each host and reports the certificate it\n" +
			"actually serves.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagTLS, Usage: "connect to each host on 443 and report the certificate it serves"},
		},
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

	if format == output.Name {
		for _, h := range report.Hosts {
			fmt.Fprintln(os.Stdout, h)
		}
		return nil
	}
	report.Status = hostStatuses(ctx, c, app.ID, report.Hosts, cmd.Bool(flagTLS))
	if handled, err := output.Structured(os.Stdout, format, report); handled {
		return err
	}

	if len(report.Hosts) == 0 {
		fmt.Fprintf(os.Stderr, "app %q serves no custom domains\n", app.Name)
	} else {
		header := []string{"DOMAIN", "DNS", "PLATFORM-CERT"}
		if cmd.Bool(flagTLS) {
			header = append(header, "SERVED-CERT")
		}
		rows := make([][]string, 0, len(report.Status))
		for _, s := range report.Status {
			row := []string{s.Host, dnsLabel(s.DNSPointsHere), certLabel(s.PlatformCertExpires, time.Now())}
			if cmd.Bool(flagTLS) {
				row = append(row, servedLabel(s.Served, time.Now()))
			}
			rows = append(rows, row)
		}
		if err := output.StyledTable(os.Stdout, header, rows, nil); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "\nPoint DNS at: %s\n", dash(report.CNAMETarget))
	}

	fmt.Fprintf(os.Stdout, "SSL: %s   redirect-to-https: %s   http/2: %s   challenge: %s   class: %s\n",
		yesNo(report.EnableSSL), yesNo(report.RedirectSSL), yesNo(report.EnableHTTPV2),
		dash(report.SSLChallengeType), dash(report.IngressClassName))
	return nil
}

// ingressReport is the -o json|yaml shape of `get domains`.
type ingressReport struct {
	Hosts            []string     `json:"hosts"                      yaml:"hosts"`
	CNAMETarget      string       `json:"cnameTarget"                yaml:"cnameTarget"`
	IngressClassName string       `json:"ingressClassName,omitempty" yaml:"ingressClassName,omitempty"`
	SSLChallengeType string       `json:"sslChallengeType"           yaml:"sslChallengeType"`
	EnableSSL        bool         `json:"sslEnabled"                 yaml:"sslEnabled"`
	RedirectSSL      bool         `json:"sslRedirect"                yaml:"sslRedirect"`
	EnableHTTPV2     bool         `json:"http2Enabled"               yaml:"http2Enabled"`
	Status           []hostStatus `json:"status,omitempty"           yaml:"status,omitempty"`
}

// hostStatus is what is known about one domain: whether its DNS points at the
// platform, the platform's certificate, and, with --tls, the served one.
type hostStatus struct {
	Host string `json:"host" yaml:"host"`
	// DNSPointsHere is nil when the check itself failed.
	DNSPointsHere *bool `json:"dnsPointsHere" yaml:"dnsPointsHere"`
	// PlatformCertExpires is nil when the platform has issued no certificate.
	PlatformCertExpires *time.Time  `json:"platformCertExpires"  yaml:"platformCertExpires"`
	Served              *servedCert `json:"servedCert,omitempty" yaml:"servedCert,omitempty"`
}

// hostStatuses gathers each host's status. A failed check costs its column
// a "-", not the command: the domain list is worth showing regardless.
func hostStatuses(ctx context.Context, c *client.Client, appID string, hosts []string, probeTLS bool) []hostStatus {
	if len(hosts) == 0 {
		return nil
	}
	dns, dnsErr := c.CheckDNS(ctx, hosts)
	certs, _ := c.CertificateStatus(ctx, appID)
	out := make([]hostStatus, 0, len(hosts))
	for _, h := range hosts {
		s := hostStatus{Host: h, PlatformCertExpires: certs[h]}
		if ok, found := dns[h]; dnsErr == nil && found {
			s.DNSPointsHere = &ok
		}
		if probeTLS {
			served := probeServedCert(ctx, h)
			s.Served = &served
		}
		out = append(out, s)
	}
	return out
}

func dnsLabel(ok *bool) string {
	switch {
	case ok == nil:
		return "-"
	case *ok:
		return "ok"
	default:
		return "not pointed here"
	}
}

// certLabel describes a certificate expiry relative to now.
func certLabel(expires *time.Time, now time.Time) string {
	if expires == nil {
		return "none"
	}
	left := expires.Sub(now)
	if left < 0 {
		return "expired " + age(*expires) + " ago"
	}
	return fmt.Sprintf("%dd left", int(left.Hours()/hoursPerDay))
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
			"adding it, or certificate issuance will not complete.\n\n" +
			"Adding a domain also sets the ACME challenge to http01 unless you ask otherwise,\n" +
			"because the platform default of dns01 fails SILENTLY on any zone Hamravesh does\n" +
			"not host: it waits for an _acme-challenge TXT record nobody will write, the site\n" +
			"keeps serving plain HTTP, and nothing reports an error. http01 validates over the\n" +
			"domain you just pointed at the cluster. A wildcard (*.example.com) can only be\n" +
			"issued over dns01, so wildcards keep it and say so.\n\n" +
			"The ingress is only re-rendered when the app is deployed, so a change here is\n" +
			"followed by a no-op write that triggers one. --no-reconcile skips that if you are\n" +
			"about to deploy anyway.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: flagAdd, Usage: "domain to route to this app (repeatable)"},
			&cli.StringSliceFlag{Name: flagRemove, Usage: "domain to stop routing (repeatable)"},
			&cli.StringFlag{Name: flagChallenge, Usage: "ACME challenge: http01 or dns01"},
			&cli.BoolFlag{Name: flagRedirectHTTPS, Usage: "redirect plain HTTP to HTTPS"},
			&cli.BoolFlag{Name: flagNoReconcile, Usage: "do not trigger the deploy that re-renders the ingress"},
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

	challenge, challengeNote, err := challengeFor(
		cmd.String(flagChallenge), additions, rawString(current, "ssl_challenge_type"))
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to update domains on app %q (%s) in tenant %q: %s\n",
		app.Name, app.ID, c.Org, describeDomainChange(additions, removals))
	if challenge != "" {
		fmt.Fprintf(os.Stderr, "  ssl challenge: %s -> %s%s\n",
			rawString(current, "ssl_challenge_type"), challenge, challengeNote)
	}
	if cmd.Bool(flagRedirectHTTPS) && !rawBool(current, "redirect_SSL") {
		fmt.Fprintf(os.Stderr, "  redirect http -> https: enabled\n")
	}
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	updated, err := c.UpdateApp(ctx, app.ID, func(raw map[string]any) error {
		if err := applyDomainChange(raw, additions, removals); err != nil {
			return err
		}
		applyIngressSettings(raw, challenge, cmd.Bool(flagRedirectHTTPS))
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s domains updated\n", app.Name)
	if challenge != "" {
		fmt.Fprintf(os.Stderr, "ssl challenge: %s%s\n", challenge, challengeNote)
	}
	if target := rawString(updated, "ingress_cname_address"); target != "" && len(additions) > 0 {
		fmt.Fprintf(os.Stderr, "note: point each added domain's DNS at %s\n", target)
	}

	// The ingress is rendered at deploy time, so the change above is inert until
	// the app is deployed again. A no-op write is the cheapest trigger; without
	// it a corrected challenge type sits in the API doing nothing, which is
	// exactly the silent failure this command exists to avoid.
	return reconcileIngress(ctx, c, app.ID, cmd.Bool(flagNoReconcile))
}

// reconcileIngress triggers the deploy that re-renders the ingress.
//
// Every write here is a full-object PUT rebuilt from a read, so a mutation that
// changes nothing still sends the whole app back and counts as a deploy. That
// is the entire trick: without it a corrected challenge type sits in the API
// doing nothing while the site keeps serving plain HTTP.
func reconcileIngress(ctx context.Context, c *client.Client, id string, skip bool) error {
	if skip {
		fmt.Fprintf(os.Stderr, "note: not reconciled — the ingress re-renders on the app's next deploy\n")
		return nil
	}
	if _, err := c.UpdateApp(ctx, id, func(map[string]any) error { return nil }); err != nil {
		return fmt.Errorf("domains updated but the reconcile deploy failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "reconciled: ingress re-rendered\n")
	return nil
}

// challengeFor decides which ACME challenge an app should use after these
// domains are added.
//
// The platform defaults new apps to dns01, which is the wrong default for any
// zone Hamravesh does not host: issuance waits on an _acme-challenge TXT record
// that nobody will write, and reports nothing while the site serves plain HTTP.
// http01 validates over the domain that must already point at the cluster for
// the ingress to serve it at all, so it is correct whenever the domain works.
//
// The exception is a wildcard, which Let's Encrypt will only issue over dns01 —
// there is no host to answer an HTTP challenge on. Those keep dns01 and the
// caller is told why.
//
// An explicit --challenge always wins; returning "" means leave the app alone.
func challengeFor(explicit string, additions []string, current string) (string, string, error) {
	switch explicit {
	case challengeHTTP01, challengeDNS01:
		return explicit, "", nil
	case "":
	default:
		return "", "", fmt.Errorf("%w: got %q", errBadChallenge, explicit)
	}
	if len(additions) == 0 {
		return "", "", nil
	}
	for _, h := range additions {
		if strings.HasPrefix(h, "*.") {
			if current == challengeDNS01 {
				return "", "", nil
			}
			return challengeDNS01, " (a wildcard can only be issued over dns01)", nil
		}
	}
	if current == challengeHTTP01 {
		return "", "", nil
	}
	return challengeHTTP01, " (dns01 would wait for a TXT record nobody writes)", nil
}

// applyIngressSettings writes the ingress fields that decide whether a domain
// ever gets a certificate. Both are no-ops when unset, so this is safe to call
// on every update.
func applyIngressSettings(raw map[string]any, challenge string, redirect bool) {
	if challenge != "" {
		raw["ssl_challenge_type"] = challenge
		raw["enable_SSL"] = true
	}
	if redirect {
		raw["redirect_SSL"] = true
	}
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
