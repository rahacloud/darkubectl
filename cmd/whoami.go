package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

func newWhoamiCommand() *cli.Command {
	return &cli.Command{
		Name:  "whoami",
		Usage: "Show the signed-in account and the tenants it can reach",
		Description: "Lists the organizations this credential belongs to, straight from the API\n" +
			"rather than from the local config, along with the numeric id each one uses\n" +
			"in an app create payload.\n\n" +
			"Needs no tenant: this is the command that tells you which tenants exist.",
		Action: whoamiAction,
	}
}

func whoamiAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newGlobalClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	profile, err := c.Profile(ctx)
	if err != nil {
		return err
	}

	if handled, err := output.Structured(os.Stdout, format, profile); handled {
		return err
	}
	if format == output.Name {
		for _, o := range profile.Organizations {
			fmt.Fprintln(os.Stdout, o.Name)
		}
		return nil
	}

	fmt.Fprintf(os.Stdout, "%s <%s>\n\n", dash(profile.FullName), profile.Email)

	orgs := profile.Organizations
	sort.Slice(orgs, func(i, j int) bool { return orgs[i].Name < orgs[j].Name })

	rows := make([][]string, 0, len(orgs))
	for _, o := range orgs {
		marker := ""
		if o.Name == c.Org {
			marker = "*"
		}
		roles := o.Roles
		sort.Strings(roles)
		rows = append(rows, []string{marker, o.Name, strconv.Itoa(o.ID), dash(strings.Join(roles, ","))})
	}
	if err := output.StyledTable(os.Stdout, []string{"CURRENT", colName, "ID", "ROLES"}, rows, nil); err != nil {
		return err
	}
	if c.Org == "" && len(orgs) > 0 {
		fmt.Fprintf(os.Stderr, "\nno tenant selected; pick one with `darkubectl config use-tenant %s`\n", orgs[0].Name)
	}
	return nil
}
