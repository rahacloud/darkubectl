package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// colName is the shared first column header across the get tables.
const colName = "NAME"

// Column indices used for status-aware coloring.
const (
	appStateCol   = 2
	appEnabledCol = 4
	certStateCol  = 3
)

func newGetCommand() *cli.Command {
	return &cli.Command{
		Name:  "get",
		Usage: "Display one or many resources",
		Commands: []*cli.Command{
			{
				Name:    "apps",
				Aliases: []string{cmdApp, "applications"},
				Usage:   "List apps in the current tenant",
				Action:  getAppsAction,
			},
			{
				Name:    "tenants",
				Aliases: []string{"tenant", "orgs", "org", "organizations"},
				Usage:   "List known tenants (organizations)",
				Action:  getTenantsAction,
			},
			{
				Name:    "namespaces",
				Aliases: []string{"namespace", "ns", "projects", "project"},
				Usage:   "List namespaces (projects) in the current tenant",
				Action:  getNamespacesAction,
			},
			{
				Name:      "pods",
				Aliases:   []string{"pod"},
				Usage:     "List an app's running pods",
				ArgsUsage: "APP|ID",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: flagDebug, Usage: "dump raw app-state JSON to stderr"},
				},
				Action: getPodsAction,
			},
			{
				Name:    "certificates",
				Aliases: []string{"certificate", "certs", "cert"},
				Usage:   "List TLS certificates in the current tenant",
				Action:  getCertificatesAction,
			},
			{
				Name:  "plans",
				Usage: "List app plans available for `create app` (--all for every plan)",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "all", Usage: "show every plan, not just create-eligible app plans"},
				},
				Action: getPlansAction,
			},
		},
	}
}

func getAppsAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}

	apps, err := c.ListApps(ctx)
	if err != nil {
		return err
	}
	if name := cmd.Args().First(); name != "" {
		apps = filterAppsByName(apps, name)
		if len(apps) == 0 {
			return fmt.Errorf("no app matching %q in tenant %q", name, c.Org)
		}
	}

	if handled, err := output.Structured(os.Stdout, format, apps); handled {
		return err
	}
	if format == output.Name {
		for _, a := range apps {
			fmt.Fprintln(os.Stdout, a.Name)
		}
		return nil
	}
	return printAppsTable(apps, format == output.Wide)
}

func filterAppsByName(apps []client.App, name string) []client.App {
	var out []client.App
	for _, a := range apps {
		if a.Name == name || a.ID == name {
			out = append(out, a)
		}
	}
	return out
}

func printAppsTable(apps []client.App, wide bool) error {
	header := []string{colName, "NAMESPACE", "STATE", "REPLICAS", "ENABLED"}
	if wide {
		header = append(header, "CLUSTER", "RAM", "CPU", "DOMAIN", "ID")
	}
	rows := make([][]string, 0, len(apps))
	for _, a := range apps {
		row := []string{
			a.Name,
			a.Namespace.Name,
			stateLabel(a.State),
			strconv.Itoa(a.Replicas),
			yesNo(a.IsEnabled),
		}
		if wide {
			row = append(row,
				a.Namespace.Cluster.Name,
				dash(a.RAMLimit),
				dash(a.CPURequest),
				dash(a.CustomDomainAddress),
				a.ID,
			)
		}
		rows = append(rows, row)
	}
	return output.StyledTable(os.Stdout, header, rows, output.StatusCells(appStateCol, appEnabledCol))
}

func getTenantsAction(_ context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	current := resolveOrg(cmd, cfg)
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, cfg.Tenants); handled {
		return err
	}
	if len(cfg.Tenants) == 0 {
		fmt.Fprintln(os.Stderr, "no tenants configured; add one with `darkubectl config add-tenant <name>`")
		return nil
	}
	rows := make([][]string, 0, len(cfg.Tenants))
	for _, t := range cfg.Tenants {
		marker := ""
		if t == current {
			marker = "*"
		}
		rows = append(rows, []string{marker, t})
	}
	return output.StyledTable(os.Stdout, []string{"CURRENT", colName}, rows, nil)
}

func getPodsAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	c, cfg, err := buildClient(ctx, cmd)
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
	access, err := accessToken(ctx, cmd, cfg)
	if err != nil {
		return err
	}

	pods, _, err := appstate.FetchPods(ctx, appstate.Options{
		BaseURL:     resolveBaseURL(cmd, cfg),
		AccessToken: access,
		Org:         resolveOrg(cmd, cfg),
		AppID:       app.ID,
		Debug:       cmd.Bool(flagDebug),
	})
	if err != nil {
		return err
	}

	if handled, err := output.Structured(os.Stdout, format, pods); handled {
		return err
	}
	if len(pods) == 0 {
		fmt.Fprintln(os.Stderr, "no running pods for", app.Name)
		return nil
	}
	rows := make([][]string, 0, len(pods))
	for _, p := range pods {
		rows = append(rows, []string{p.Name, dash(strings.Join(p.Containers, ","))})
	}
	return output.StyledTable(os.Stdout, []string{colName, "CONTAINERS"}, rows, nil)
}

func getNamespacesAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	ns, err := c.NamespacesFromApps(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, ns); handled {
		return err
	}
	rows := make([][]string, 0, len(ns))
	for _, n := range ns {
		rows = append(rows, []string{n.Name, n.Cluster.Name, n.Cluster.LocationCountry})
	}
	return output.StyledTable(os.Stdout, []string{colName, "CLUSTER", "LOCATION"}, rows, nil)
}

func getCertificatesAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	certs, err := c.ListCertificates(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, certs); handled {
		return err
	}
	rows := make([][]string, 0, len(certs))
	for _, ct := range certs {
		rows = append(rows, []string{dash(ct.Name), dash(ct.CommonName), dash(ct.Domain), dash(ct.State)})
	}
	return output.StyledTable(os.Stdout, []string{colName, "COMMON-NAME", "DOMAIN", "STATE"}, rows, output.StatusCells(certStateCol))
}

func getPlansAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	plans, err := c.ListPlans(ctx)
	if err != nil {
		return err
	}
	if !cmd.Bool("all") {
		plans = filterCreatablePlans(plans)
	}
	if handled, err := output.Structured(os.Stdout, format, plans); handled {
		return err
	}
	rows := make([][]string, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, []string{
			dash(planRef(p)),
			ramMB(p.Detail.RAMLimit),
			cpuM(p.Detail.CPURequest),
			dash(clusterLabel(p.Cluster)),
			p.ID,
		})
	}
	return output.StyledTable(os.Stdout, []string{colName, "RAM", "CPU", "CLUSTER", "ID"}, rows, nil)
}

func filterCreatablePlans(plans []client.Plan) []client.Plan {
	out := make([]client.Plan, 0, len(plans))
	for _, p := range plans {
		if p.IsCreatable() {
			out = append(out, p)
		}
	}
	return out
}

// planRef is the value to pass to `create app --plan` (code name, else name).
func planRef(p client.Plan) string {
	if p.CodeName != "" {
		return p.CodeName
	}
	return p.Name
}

func ramMB(mb int) string {
	if mb == 0 {
		return "-"
	}
	return strconv.Itoa(mb) + "M"
}

func cpuM(m int) string {
	if m == 0 {
		return "-"
	}
	return strconv.Itoa(m) + "m"
}

func clusterLabel(c *client.Cluster) string {
	if c == nil {
		return ""
	}
	if c.LocationCountry != "" {
		return c.Name + " (" + c.LocationCountry + ")"
	}
	return c.Name
}
