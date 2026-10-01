package cmd

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const (
	flagOrphaned = "orphaned"

	// noApp marks a disk that no app in its namespace accounts for.
	noApp = "<none>"
)

// diskRow is one persistent volume claim and the app it belongs to.
type diskRow struct {
	Namespace    string `json:"namespace"    yaml:"namespace"`
	Cluster      string `json:"cluster"      yaml:"cluster"`
	Name         string `json:"name"         yaml:"name"`
	Size         string `json:"size"         yaml:"size"`
	StorageClass string `json:"storageClass" yaml:"storageClass"`
	Status       string `json:"status"       yaml:"status"`
	// App is the app whose name the claim is named after, or "" if none.
	App   string `json:"app"             yaml:"app"`
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
}

func newGetDisksCommand() *cli.Command {
	return &cli.Command{
		Name:    "disks",
		Aliases: []string{"disk", "pvcs", "pvc", "volumes"},
		Usage:   "List the persistent disks in the tenant, and which app each belongs to",
		Description: "  darkubectl get disks\n" +
			"  darkubectl get disks --orphaned       # disks no app accounts for\n\n" +
			"Deleting an app leaves its disk behind, bound and billed, and neither the app\n" +
			"list nor the console's app pages show it again. A disk is matched to an app by\n" +
			"name: the platform names an app's claim after the app (<app>-data), and a\n" +
			"managed database's after the database (shown as db/<name>). APP is <none>\n" +
			"when nothing in that namespace matches, which is usually a deleted app's\n" +
			"leftover, but can also be a workload installed outside Darkube: check before\n" +
			"removing anything.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagNamespace, Usage: "only this namespace (project)"},
			&cli.BoolFlag{Name: flagOrphaned, Usage: "only disks no app accounts for"},
		},
		Action: getDisksAction,
	}
}

func getDisksAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	namespaces, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}
	if ns := cmd.String(flagNamespace); ns != "" {
		namespaces = slices.DeleteFunc(namespaces, func(n client.Namespace) bool { return n.Name != ns })
	}
	apps, err := c.ListApps(ctx)
	if err != nil {
		return err
	}
	// Databases are a separate product whose disks share the namespaces. A
	// tenant without the product, or a failed read, only costs attribution.
	dbs, _ := c.ListDatabases(ctx)

	perNS := make([][]diskRow, len(namespaces))
	sem := make(chan struct{}, topParallel)
	var wg sync.WaitGroup
	for i, ns := range namespaces {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			perNS[i] = namespaceDisks(ctx, c, ns, apps, dbs)
		})
	}
	wg.Wait()

	var rows []diskRow
	for _, r := range perNS {
		rows = append(rows, r...)
	}
	if cmd.Bool(flagOrphaned) {
		rows = slices.DeleteFunc(rows, func(r diskRow) bool { return r.App != "" || r.Error != "" })
	}
	slices.SortStableFunc(rows, func(a, b diskRow) int {
		return cmp.Or(strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})

	if handled, err := output.Structured(os.Stdout, format, rows); handled {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "no disks")
		return nil
	}
	table := make([][]string, 0, len(rows))
	orphans := 0
	for _, r := range rows {
		if r.Error != "" {
			table = append(table, []string{r.Namespace, "-", "-", "-", "-", r.Error})
			continue
		}
		app := r.App
		if app == "" {
			app = noApp
			orphans++
		}
		table = append(table, []string{r.Namespace, r.Name, r.Size, dash(r.StorageClass), dash(r.Status), app})
	}
	if err := output.StyledTable(os.Stdout, []string{colNamespace, colName, colSize, "CLASS", colStatus, "APP"}, table, nil); err != nil {
		return err
	}
	if orphans > 0 {
		fmt.Fprintf(os.Stderr, "\n%d disk(s) match no app; a deleted app's disk stays bound and billed.\n", orphans)
	}
	return nil
}

// namespaceDisks lists one namespace's claims and matches each to an app.
func namespaceDisks(ctx context.Context, c *client.Client, ns client.Namespace, apps []client.App, dbs []client.Database) []diskRow {
	pvcs, err := c.ListPVCs(ctx, ns.Name, ns.Cluster.ID)
	if err != nil {
		return []diskRow{{Namespace: ns.Name, Cluster: ns.Cluster.Name, Error: "disks unavailable: " + err.Error()}}
	}
	var names []string
	for _, a := range apps {
		if a.Namespace.ID == ns.ID {
			names = append(names, a.Name)
		}
	}
	var dbNames []string
	for _, d := range dbs {
		if d.NamespaceID == ns.ID {
			dbNames = append(dbNames, d.Name)
		}
	}
	rows := make([]diskRow, 0, len(pvcs))
	for _, p := range pvcs {
		owner := diskOwner(p.Name, names)
		if owner == "" {
			if db := diskOwner(p.Name, dbNames); db != "" {
				owner = "db/" + db
			}
		}
		rows = append(rows, diskRow{
			Namespace: ns.Name, Cluster: ns.Cluster.Name, Name: p.Name, Size: p.Size,
			StorageClass: p.StorageClass, Status: p.Status, App: owner,
		})
	}
	return rows
}

// diskOwner finds the app a claim belongs to: the one whose name the claim
// starts with, followed by "-" (my-api-data belongs to my-api). The longest
// such name wins, so my-api-worker-data goes to my-api-worker, not my-api.
func diskOwner(pvc string, appNames []string) string {
	best := ""
	for _, n := range appNames {
		if strings.HasPrefix(pvc, n+"-") && len(n) > len(best) {
			best = n
		}
	}
	return best
}
