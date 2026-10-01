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

const (
	flagGrant  = "grant"
	flagRevoke = "revoke"
	flagNone   = "none"
	flagAll    = "all"

	// noStateCol and permViewCol place the colors of the permissions table:
	// no state column, then VIEW, CHANGE and DELETE side by side.
	noStateCol    = -1
	permViewCol   = 2
	permChangeCol = 3
	permDeleteCol = 4
)

var (
	errMissingMember     = errors.New("a member's email is required after the app")
	errNoSuchMember      = errors.New("no member of the organization with that email")
	errNoPermissionDelta = errors.New("nothing to do: pass --grant, --revoke or --none")
	errBadPermission     = errors.New("unknown permission (want view, change or delete)")
	errPermissionsLost   = errors.New("the platform accepted the change but it did not read back")
)

func newGetPermissionsCommand() *cli.Command {
	return &cli.Command{
		Name:      "permissions",
		Aliases:   []string{"permission", "perms", "acl"},
		Usage:     "Show which organization members can view, change and delete an app",
		ArgsUsage: argRefUsage,
		Description: "Members without any access are hidden; --all shows them too. Permissions are\n" +
			"only enforced in an organization with access control turned on, which this\n" +
			"command checks and reports.",
		Flags:  []cli.Flag{&cli.BoolFlag{Name: flagAll, Usage: "include members with no access"}},
		Action: getPermissionsAction,
	}
}

func newSetPermissionsCommand() *cli.Command {
	return &cli.Command{
		Name:      "permissions",
		Aliases:   []string{"permission", "perms", "acl"},
		Usage:     "Grant or revoke one member's access to an app",
		ArgsUsage: argRefUsage + " EMAIL",
		Description: "  darkubectl set permissions my-api dev@example.com --grant view\n" +
			"  darkubectl set permissions my-api dev@example.com --grant change,delete\n" +
			"  darkubectl set permissions my-api dev@example.com --revoke delete\n" +
			"  darkubectl set permissions my-api dev@example.com --none\n\n" +
			"The member must already belong to the organization. The whole access list is\n" +
			"written back, as the console does, and read again to confirm the change.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: flagGrant, Usage: "permissions to add: view, change, delete"},
			&cli.StringSliceFlag{Name: flagRevoke, Usage: "permissions to remove"},
			&cli.BoolFlag{Name: flagNone, Usage: "remove every permission"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: setPermissionsAction,
	}
}

func getPermissionsAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	rows, err := c.AppPermissions(ctx, app.ID)
	if err != nil {
		return err
	}
	if !cmd.Bool(flagAll) {
		rows = slices.DeleteFunc(rows, func(p client.AppPermission) bool { return len(p.Permissions) == 0 })
	}
	if handled, err := output.Structured(os.Stdout, format, rows); handled {
		return err
	}
	warnIfACLOff(ctx, c)
	table := make([][]string, 0, len(rows))
	for _, p := range rows {
		table = append(table, []string{
			p.User.Email, dash(displayName(p.User)),
			yesNo(p.Has(client.PermView)), yesNo(p.Has(client.PermChange)), yesNo(p.Has(client.PermDelete)),
		})
	}
	return output.StyledTable(os.Stdout, []string{"EMAIL", colName, "VIEW", "CHANGE", "DELETE"}, table,
		output.StatusCells(noStateCol, permViewCol, permChangeCol, permDeleteCol))
}

func setPermissionsAction(ctx context.Context, cmd *cli.Command) error {
	email := cmd.Args().Get(1)
	if cmd.Args().First() != "" && email == "" {
		return errMissingMember
	}
	grant, err := parsePermissions(cmd.StringSlice(flagGrant))
	if err != nil {
		return err
	}
	revoke, err := parsePermissions(cmd.StringSlice(flagRevoke))
	if err != nil {
		return err
	}
	if cmd.Bool(flagNone) {
		revoke = client.AllPermissions
	}
	if len(grant) == 0 && len(revoke) == 0 {
		return errNoPermissionDelta
	}

	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	rows, err := c.AppPermissions(ctx, app.ID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(rows, func(p client.AppPermission) bool { return strings.EqualFold(p.User.Email, email) })
	if i < 0 {
		return fmt.Errorf("%w: %s", errNoSuchMember, email)
	}
	before := slices.Clone(rows[i].Permissions)
	after := applyPermissionChange(before, grant, revoke)
	if slices.Equal(sortedPerms(before), after) {
		fmt.Fprintf(os.Stdout, "no change: %s already has [%s] on app/%s\n", email, strings.Join(after, ","), app.Name)
		return nil
	}

	warnIfACLOff(ctx, c)
	fmt.Fprintf(os.Stderr, "About to change %s's access to app %q in tenant %q:\n  - [%s]\n  + [%s]\n",
		email, app.Name, c.Org, strings.Join(sortedPerms(before), ","), strings.Join(after, ","))
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	rows[i].Permissions = after
	if err := c.SetAppPermissions(ctx, app.ID, rows); err != nil {
		return err
	}

	check, err := c.AppPermissions(ctx, app.ID)
	if err != nil {
		return err
	}
	j := slices.IndexFunc(check, func(p client.AppPermission) bool { return strings.EqualFold(p.User.Email, email) })
	if j < 0 || !slices.Equal(sortedPerms(check[j].Permissions), after) {
		return fmt.Errorf("%w: %s on app/%s", errPermissionsLost, email, app.Name)
	}
	fmt.Fprintf(os.Stdout, "%s now has [%s] on app/%s\n", email, strings.Join(after, ","), app.Name)
	return nil
}

// displayName is a member's name, or "" when the account has none of its own:
// full_name falls back to the email address.
func displayName(u client.PermissionUser) string {
	if strings.EqualFold(u.FullName, u.Email) {
		return ""
	}
	return u.FullName
}

// warnIfACLOff says so when the tenant does not enforce per-app permissions,
// since then the list is bookkeeping and everyone has access.
func warnIfACLOff(ctx context.Context, c *client.Client) {
	if on, err := c.IsACLEnabled(ctx, c.Org); err == nil && !on {
		fmt.Fprintf(os.Stderr, "note: access control is off in tenant %q, so these permissions are not enforced\n", c.Org)
	}
}

// parsePermissions validates a --grant/--revoke list, which may be given as
// repeated flags or comma-separated.
func parsePermissions(values []string) ([]string, error) {
	var out []string
	for _, v := range values {
		for p := range strings.SplitSeq(v, ",") {
			p = strings.ToLower(strings.TrimSpace(p))
			if p == "" {
				continue
			}
			if !slices.Contains(client.AllPermissions, p) {
				return nil, fmt.Errorf("%w: %q", errBadPermission, p)
			}
			out = append(out, p)
		}
	}
	return out, nil
}

// applyPermissionChange grants then revokes, returning a sorted set.
func applyPermissionChange(current, grant, revoke []string) []string {
	set := map[string]bool{}
	for _, p := range current {
		set[p] = true
	}
	for _, p := range grant {
		set[p] = true
	}
	for _, p := range revoke {
		delete(set, p)
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	return sortedPerms(out)
}

// sortedPerms orders permissions as the API returns them: alphabetically.
func sortedPerms(p []string) []string {
	out := slices.Clone(p)
	slices.Sort(out)
	return out
}
