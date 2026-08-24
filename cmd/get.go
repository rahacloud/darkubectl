package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/kube"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// colName is the shared first column header across the get tables.
const colName = "NAME"

// colState is the shared state column header across the get tables.
const colState = "STATE"

// colNamespace is the shared namespace column header across the get tables.
const colNamespace = "NAMESPACE"

// flagNamespace filters `get apps` to a single namespace (name or id).
const flagNamespace = "namespace"

// Column indices used for status-aware coloring. The grouped-by-namespace
// table drops the NAMESPACE column, shifting STATE/ENABLED left by one.
const (
	appStateCol   = 2
	appEnabledCol = 4
	certStateCol  = 3
	podStatusCol  = 2

	appGroupedStateCol   = 1
	appGroupedEnabledCol = 3
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
				Flags: []cli.Flag{
					&cli.StringFlag{Name: flagNamespace, Aliases: []string{"ns"}, Usage: "only show apps in this namespace (name or id)"},
				},
				Action: getAppsAction,
			},
			newGetEnvCommand(),
			newGetDomainsCommand(),
			newGetNotificationsCommand(),
			newGetAlertsCommand(),
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
				Name:      "deploy-token",
				Aliases:   []string{"deploy-tokens", "deploytoken"},
				Usage:     "Print an app's CI deploy token and app id (prints a secret)",
				ArgsUsage: argRefUsage,
				Description: "The two values `darkube deploy --app-id ... --token ...` needs in a pipeline.\n" +
					"Use -o name for the bare token, to interpolate into a CI variable.",
				Action: getDeployTokenAction,
			},
			{
				Name:  "orphans",
				Usage: "Find workloads whose Darkube app no longer exists (shells out to kubectl)",
				Description: "Compares the tenant's apps against the Deployments in a cluster, using the\n" +
					"darkube.hamravesh.com/app-id label. Catches Helm releases left behind by a\n" +
					"delete, which hold the name and make `create app` fail, and which neither\n" +
					"side can show on its own. Only namespaces the cluster returns are compared.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: flagNamespace, Aliases: []string{"ns"}, Usage: "only check this namespace (default: all)"},
					&cli.StringFlag{Name: flagKubeContext, Usage: "kubectl context to read the cluster from"},
					&cli.StringFlag{Name: flagKubeconfig, Usage: "path to a kubeconfig (default: kubectl's own resolution)"},
					&cli.StringFlag{Name: flagKubectl, Usage: "kubectl binary to run", Value: kube.DefaultBinary},
				},
				Action: getOrphansAction,
			},
			{
				Name:    "certificates",
				Aliases: []string{"certificate", "certs", "cert"},
				Usage:   "List TLS certificates in the current tenant",
				Action:  getCertificatesAction,
			},
			{
				Name:        "plans",
				Usage:       "List app plans available for `create app` (--all for every plan)",
				Description: "The plan catalogue is global, so this needs no tenant selected.",
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
	if ns := cmd.String(flagNamespace); ns != "" {
		apps = filterAppsByNamespace(apps, ns)
		if len(apps) == 0 {
			return fmt.Errorf("no app in namespace %q in tenant %q", ns, c.Org)
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

func filterAppsByNamespace(apps []client.App, ns string) []client.App {
	var out []client.App
	for _, a := range apps {
		if a.Namespace.Name == ns || strconv.Itoa(a.Namespace.ID) == ns {
			out = append(out, a)
		}
	}
	return out
}

// printAppsTable renders apps grouped under a per-namespace heading on a
// terminal, and as a single flat (pipe-safe) table otherwise.
func printAppsTable(apps []client.App, wide bool) error {
	if !output.IsTerminal(os.Stdout) {
		return printAppsFlatTable(apps, wide)
	}
	for i, g := range groupAppsByNamespace(apps) {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		if err := output.PrintSectionHeader(os.Stdout, "NAMESPACE: "+g.namespace); err != nil {
			return err
		}
		if err := printAppsGroupTable(g.apps, wide); err != nil {
			return err
		}
	}
	return nil
}

func printAppsFlatTable(apps []client.App, wide bool) error {
	header := []string{colName, colNamespace, colState, "REPLICAS", "ENABLED"}
	if wide {
		header = append(header, "CLUSTER", "IMAGE", "RAM", "CPU", "DOMAIN", "UPDATED", "ID")
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
				dash(a.Image()),
				dash(a.RAMLimit),
				dash(a.CPURequest),
				dash(a.CustomDomainAddress),
				ageOf(a.UpdatedAt),
				a.ID,
			)
		}
		rows = append(rows, row)
	}
	return output.StyledTable(os.Stdout, header, rows, output.StatusCells(appStateCol, appEnabledCol))
}

// printAppsGroupTable is printAppsFlatTable without the (now redundant)
// NAMESPACE column, for use under a per-namespace section header.
func printAppsGroupTable(apps []client.App, wide bool) error {
	header := []string{colName, colState, "REPLICAS", "ENABLED"}
	if wide {
		header = append(header, "CLUSTER", "IMAGE", "RAM", "CPU", "DOMAIN", "UPDATED", "ID")
	}
	rows := make([][]string, 0, len(apps))
	for _, a := range apps {
		row := []string{
			a.Name,
			stateLabel(a.State),
			strconv.Itoa(a.Replicas),
			yesNo(a.IsEnabled),
		}
		if wide {
			row = append(row,
				a.Namespace.Cluster.Name,
				dash(a.Image()),
				dash(a.RAMLimit),
				dash(a.CPURequest),
				dash(a.CustomDomainAddress),
				ageOf(a.UpdatedAt),
				a.ID,
			)
		}
		rows = append(rows, row)
	}
	return output.StyledTable(os.Stdout, header, rows, output.StatusCells(appGroupedStateCol, appGroupedEnabledCol))
}

// appGroup is a namespace and the apps in it, in `get apps` list order.
type appGroup struct {
	namespace string
	apps      []client.App
}

// groupAppsByNamespace buckets apps by namespace name, sorted alphabetically
// by namespace for a stable, readable listing.
func groupAppsByNamespace(apps []client.App) []appGroup {
	idx := make(map[string]int)
	var groups []appGroup
	for _, a := range apps {
		name := a.Namespace.Name
		if i, ok := idx[name]; ok {
			groups[i].apps = append(groups[i].apps, a)
			continue
		}
		idx[name] = len(groups)
		groups = append(groups, appGroup{namespace: name, apps: []client.App{a}})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].namespace < groups[j].namespace })
	return groups
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
	return printPodsTable(pods, format == output.Wide)
}

// printPodsTable renders pods the way `kubectl get pods` does — READY, STATUS,
// RESTARTS and AGE — because the aggregate app state only ever says "not ready"
// and hides whether the pod is crash-looping, and if so for how long.
func printPodsTable(pods []appstate.Pod, wide bool) error {
	header := []string{colName, "READY", "STATUS", "RESTARTS", "AGE"}
	if wide {
		header = append(header, "CONTAINERS", "LAST-STATE", colNamespace)
	}
	rows := make([][]string, 0, len(pods))
	for _, p := range pods {
		ready, total := p.ReadyCount()
		row := []string{
			p.Name,
			fmt.Sprintf("%d/%d", ready, total),
			podStatus(p),
			strconv.Itoa(p.Restarts()),
			age(p.CreatedAt),
		}
		if wide {
			row = append(row,
				dash(strings.Join(p.ContainerNames(), ",")),
				dash(lastState(p)),
				dash(p.Namespace),
			)
		}
		rows = append(rows, row)
	}
	return output.StyledTable(os.Stdout, header, rows, output.StatusCells(podStatusCol))
}

// podStatus is the pod's live phase, with a pod on its way out called out as
// such: a terminating pod still reports phase Running.
func podStatus(p appstate.Pod) string {
	if p.Terminating {
		return "terminating"
	}
	if s := p.State; s != "" {
		return s
	}
	return dash(p.Phase)
}

// lastState reports why the pod's containers last ended, which is the only
// clue the stream gives about a crash loop's cause.
func lastState(p appstate.Pod) string {
	for _, c := range p.Containers {
		if c.LastState != "" {
			return c.LastState
		}
	}
	return ""
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
	ns, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, ns); handled {
		return err
	}
	rows := make([][]string, 0, len(ns))
	for _, n := range ns {
		rows = append(rows, []string{n.Name, strconv.Itoa(n.ID), n.Cluster.Name, n.Cluster.LocationCountry})
	}
	return output.StyledTable(os.Stdout, []string{colName, "ID", "CLUSTER", "LOCATION"}, rows, nil)
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
	return output.StyledTable(os.Stdout, []string{colName, "COMMON-NAME", "DOMAIN", colState}, rows, output.StatusCells(certStateCol))
}

func getPlansAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newGlobalClient(ctx, cmd)
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
