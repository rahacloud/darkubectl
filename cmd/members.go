package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

func newGetMembersCommand() *cli.Command {
	return &cli.Command{
		Name:        "members",
		Aliases:     []string{"member", "users"},
		Usage:       "List the tenant's members and their roles",
		Description: "Per-app access is separate: see `get permissions <app>`.",
		Action:      getMembersAction,
	}
}

func getMembersAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	orgID, err := c.OrganizationID(ctx)
	if err != nil {
		return err
	}
	members, err := c.Members(ctx, orgID)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, members); handled {
		return err
	}
	rows := make([][]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, []string{
			m.Email, dash(displayName(client.PermissionUser{Email: m.Email, FullName: m.FullName})),
			dash(strings.Join(m.Roles, ",")), yesNo(m.Verified),
		})
	}
	return output.StyledTable(os.Stdout, []string{"EMAIL", colName, "ROLES", "VERIFIED"}, rows, nil)
}
