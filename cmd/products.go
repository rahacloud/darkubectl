package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// Column positions colored in the database and service tables.
const (
	dbStatusCol   = 2
	svcStatusCol  = 4
	svcEnabledCol = 5
)

func newGetDatabasesCommand() *cli.Command {
	return &cli.Command{
		Name:    "databases",
		Aliases: []string{"database", "db", "dbs", "dbaas"},
		Usage:   "List the tenant's managed databases (DBaaS)",
		Description: "Managed databases are a separate product from apps and do not appear in\n" +
			"`get apps`. SIZE is per node.",
		Action: getDatabasesAction,
	}
}

func newGetServicesCommand() *cli.Command {
	return &cli.Command{
		Name:        "services",
		Aliases:     []string{"service", "marketplace", "saas"},
		Usage:       "List the tenant's marketplace services (Jira, Rocket.Chat, n8n, …)",
		Description: "Marketplace services are a separate product from apps and do not appear in `get apps`.",
		Action:      getServicesAction,
	}
}

func getDatabasesAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	dbs, err := c.ListDatabases(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, dbs); handled {
		return err
	}
	if len(dbs) == 0 {
		fmt.Fprintln(os.Stderr, "no databases")
		return nil
	}
	rows := make([][]string, 0, len(dbs))
	for _, d := range dbs {
		rows = append(rows, []string{
			d.Name, d.Engine + " " + d.Version, dash(d.Status.Status), dash(d.Status.Detail),
			strconv.Itoa(d.NumNodes), databaseSize(d),
		})
	}
	return output.StyledTable(os.Stdout, []string{colName, "ENGINE", colStatus, "DETAIL", "NODES", colSize}, rows,
		output.StateCells(dbStatusCol))
}

func getServicesAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	svcs, err := c.ListServices(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, svcs); handled {
		return err
	}
	if len(svcs) == 0 {
		fmt.Fprintln(os.Stderr, "no marketplace services")
		return nil
	}
	rows := make([][]string, 0, len(svcs))
	for _, s := range svcs {
		rows = append(rows, []string{s.Name, dash(s.Label), dash(s.ProductType), dash(s.Cluster), dash(s.PodStatus), yesNo(s.IsEnabled)})
	}
	return output.StyledTable(os.Stdout, []string{colName, "LABEL", "PRODUCT", colCluster, colStatus, colEnabled}, rows,
		output.StatusCells(svcStatusCol, svcEnabledCol))
}

// databaseSize is one node's size, as the nodes report it.
func databaseSize(d client.Database) string {
	if len(d.Nodes) == 0 {
		return "-"
	}
	n := d.Nodes[0]
	return fmt.Sprintf("%dm / %dM / %dGi", n.CPU, n.RAM, n.Disk)
}
