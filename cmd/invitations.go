package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const (
	cmdInvitation = "invitation"
	colTenant     = "TENANT"
	colEmail      = "EMAIL"
)

var (
	errMissingEmail      = errors.New("at least one EMAIL is required")
	errMissingInviteLink = errors.New("the invitation LINK from the email is required")
	errBadInviteLink     = errors.New("not an invitation link: it needs token and organization_id query parameters")
	errInviteNotStored   = errors.New("the platform accepted the request but does not list the invitation")
	errNoReceivedInvite  = errors.New("no invitation from that tenant in the account's notifications")
)

const (
	flagReceived = "received"
	// invitationPath is the console page an invitation's link opens.
	invitationPath = "/organization-invitation"
	// receivedScanLimit is how far back the notification feed is searched.
	receivedScanLimit = 200
)

// invitation is one pending invitation, as `get invitations` prints it.
type invitation struct {
	Tenant string `json:"tenant" yaml:"tenant"`
	Email  string `json:"email"  yaml:"email"`
}

// receivedInvitation is an invitation the signed-in account was sent, as
// `get invitations --received` prints it. The link's token is left out: it is
// what joins, and `join <tenant>` finds it again by itself.
type receivedInvitation struct {
	Tenant         string `json:"tenant"         yaml:"tenant"`
	OrganizationID string `json:"organizationId" yaml:"organizationId"`
	Email          string `json:"email"          yaml:"email"`
	Received       string `json:"received"       yaml:"received"`
	Joined         bool   `json:"joined"         yaml:"joined"`
	link           inviteLink
}

func newGetInvitationsCommand() *cli.Command {
	return &cli.Command{
		Name:    "invitations",
		Aliases: []string{cmdInvitation, "invites", "invite"},
		Usage:   "List pending member invitations, sent by the tenant or received by this account",
		Description: "  darkubectl get invitations\n" +
			"  darkubectl get invitations --all        # sent by every tenant this account belongs to\n" +
			"  darkubectl get invitations --received   # sent to this account by other tenants\n\n" +
			"An invitation stays pending until the invitee opens the link in the email and\n" +
			"joins; it is then a member (`get members`). The platform has no way to cancel one.\n" +
			"A received invitation also arrives in the account's notifications, which is where\n" +
			"--received reads it; accept one with `darkubectl join <tenant>`.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagAll, Aliases: []string{"A"}, Usage: "every tenant, not just the current one"},
			&cli.BoolFlag{Name: flagReceived, Aliases: []string{"r"}, Usage: "invitations this account received (no tenant needed)"},
		},
		Action: getInvitationsAction,
	}
}

func newCreateInvitationCommand() *cli.Command {
	return &cli.Command{
		Name:      cmdInvitation,
		Aliases:   []string{"invitations", "invite"},
		Usage:     "Invite people to the tenant by email",
		ArgsUsage: "EMAIL...",
		Description: "  darkubectl create invitation dev@example.com\n\n" +
			"Hamravesh emails each address a link; whoever opens it joins the tenant with\n" +
			"the developer role (`join` accepts such a link from the command line). The\n" +
			"platform has no route to cancel an invitation, so check the address first.",
		Flags:  []cli.Flag{&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}},
		Action: createInvitationAction,
	}
}

func newJoinCommand() *cli.Command {
	return &cli.Command{
		Name:      "join",
		Usage:     "Accept an invitation to a tenant",
		ArgsUsage: "TENANT|ORG-ID|LINK",
		Description: "  darkubectl join talano\n" +
			"  darkubectl join 'https://console.hamravesh.com/organization-invitation?token=…&organization_id=…'\n\n" +
			"The signed-in account joins the tenant that sent the invitation. Given a tenant\n" +
			"name or id, the newest invitation from it is taken from the account's\n" +
			"notifications (`get invitations --received`). Quote a link: it holds an '&'.",
		Flags:  []cli.Flag{&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}},
		Action: joinAction,
	}
}

func getInvitationsAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool(flagReceived) {
		return getReceivedInvitations(ctx, cmd, format)
	}
	orgs, err := invitationOrgs(ctx, cmd)
	if err != nil {
		return err
	}
	invitations := pendingInvitations(orgs)
	if handled, err := output.Structured(os.Stdout, format, invitations); handled {
		return err
	}
	if len(invitations) == 0 {
		fmt.Fprintln(os.Stderr, "No pending invitations.")
		return nil
	}
	rows := make([][]string, 0, len(invitations))
	for _, inv := range invitations {
		rows = append(rows, []string{inv.Tenant, inv.Email})
	}
	return output.StyledTable(os.Stdout, []string{colTenant, colEmail}, rows, nil)
}

func getReceivedInvitations(ctx context.Context, cmd *cli.Command, format output.Format) error {
	c, err := newGlobalClient(ctx, cmd)
	if err != nil {
		return err
	}
	received, err := fetchReceivedInvitations(ctx, c)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, received); handled {
		return err
	}
	if len(received) == 0 {
		fmt.Fprintln(os.Stderr, "No invitations received.")
		return nil
	}
	rows := make([][]string, 0, len(received))
	for _, r := range received {
		status := "pending"
		if r.Joined {
			status = "joined"
		}
		rows = append(rows, []string{r.Tenant, r.OrganizationID, r.Email, ageOf(r.Received), status})
	}
	return output.StyledTable(os.Stdout, []string{colTenant, "ORG-ID", colEmail, colAge, "STATUS"}, rows, nil)
}

func fetchReceivedInvitations(ctx context.Context, c *client.Client) ([]receivedInvitation, error) {
	notes, err := c.Notifications(ctx, receivedScanLimit)
	if err != nil {
		return nil, err
	}
	profile, err := c.Profile(ctx)
	if err != nil {
		return nil, err
	}
	return receivedInvitations(notes, profile.Organizations), nil
}

// receivedInvitations picks the invitations out of the notification feed,
// newest first and one per organization, marking those already joined.
func receivedInvitations(notes []client.Notification, orgs []client.Organization) []receivedInvitation {
	out := []receivedInvitation{}
	seen := map[string]bool{}
	for _, n := range notes {
		u, err := url.Parse(n.ActionURL)
		if err != nil || !strings.HasSuffix(u.Path, invitationPath) {
			continue
		}
		link, err := parseInvitationLink(n.ActionURL)
		if err != nil || seen[link.orgID] {
			continue
		}
		seen[link.orgID] = true
		joined := slices.ContainsFunc(orgs, func(o client.Organization) bool { return strconv.Itoa(o.ID) == link.orgID })
		out = append(out, receivedInvitation{
			Tenant: link.orgName, OrganizationID: link.orgID, Email: link.email,
			Received: n.Timestamp, Joined: joined, link: link,
		})
	}
	return out
}

// invitationOrgs is the current tenant, or with --all every tenant of the account.
func invitationOrgs(ctx context.Context, cmd *cli.Command) ([]client.Organization, error) {
	if !cmd.Bool(flagAll) {
		c, err := newClient(ctx, cmd)
		if err != nil {
			return nil, err
		}
		org, err := c.Organization(ctx)
		if err != nil {
			return nil, err
		}
		return []client.Organization{*org}, nil
	}
	c, err := newGlobalClient(ctx, cmd)
	if err != nil {
		return nil, err
	}
	profile, err := c.Profile(ctx)
	if err != nil {
		return nil, err
	}
	return profile.Organizations, nil
}

// pendingInvitations flattens the organizations' pending invitations.
func pendingInvitations(orgs []client.Organization) []invitation {
	out := []invitation{}
	for _, org := range orgs {
		for _, email := range org.PendingInvitations {
			out = append(out, invitation{Tenant: org.Name, Email: email})
		}
	}
	return out
}

func createInvitationAction(ctx context.Context, cmd *cli.Command) error {
	emails, err := parseEmails(cmd.Args().Slice())
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	org, err := c.Organization(ctx)
	if err != nil {
		return err
	}
	members, err := c.Members(ctx, org.ID)
	if err != nil {
		return err
	}
	if err := checkInvitees(emails, members, org.PendingInvitations); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "About to invite %s to tenant %q. Each address gets a link that joins it as a developer.\n",
		strings.Join(emails, ", "), org.Name)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.InviteMembers(ctx, org.ID, emails); err != nil {
		return err
	}
	// The PUT answering 2xx does not prove anything was stored; read it back.
	after, err := c.Organization(ctx)
	if err != nil {
		return fmt.Errorf("invitation sent, but reading it back failed: %w", err)
	}
	membersAfter, err := c.Members(ctx, org.ID)
	if err != nil {
		return fmt.Errorf("invitation sent, but reading it back failed: %w", err)
	}
	var missing []string
	for _, email := range emails {
		switch {
		case containsFold(after.PendingInvitations, email):
			fmt.Fprintf(os.Stdout, "invitation/%s sent\n", email)
		case slices.ContainsFunc(membersAfter, func(m client.Member) bool { return strings.EqualFold(m.Email, email) }):
			fmt.Fprintf(os.Stdout, "member/%s added\n", email)
		default:
			missing = append(missing, email)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", errInviteNotStored, strings.Join(missing, ", "))
	}
	return nil
}

// parseEmails requires each argument to be a bare address.
func parseEmails(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errMissingEmail
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		addr, err := mail.ParseAddress(arg)
		if err != nil || addr.Address != arg {
			return nil, fmt.Errorf("%q is not an email address", arg)
		}
		if !containsFold(out, arg) {
			out = append(out, arg)
		}
	}
	return out, nil
}

// checkInvitees refuses an address that is already a member or already invited.
func checkInvitees(emails []string, members []client.Member, pending []string) error {
	for _, email := range emails {
		if slices.ContainsFunc(members, func(m client.Member) bool { return strings.EqualFold(m.Email, email) }) {
			return fmt.Errorf("%s is already a member", email)
		}
		if containsFold(pending, email) {
			return fmt.Errorf("%s already has a pending invitation", email)
		}
	}
	return nil
}

func containsFold(list []string, s string) bool {
	return slices.ContainsFunc(list, func(v string) bool { return strings.EqualFold(v, s) })
}

func joinAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingInviteLink
	}
	c, err := newGlobalClient(ctx, cmd)
	if err != nil {
		return err
	}
	inv, err := resolveInvitation(ctx, c, ref)
	if err != nil {
		return err
	}
	profile, err := c.Profile(ctx)
	if err != nil {
		return err
	}
	if inv.email != "" && !strings.EqualFold(inv.email, profile.Email) {
		fmt.Fprintf(os.Stderr, "Warning: the invitation is for %s, but you are signed in as %s.\n", inv.email, profile.Email)
	}
	fmt.Fprintf(os.Stderr, "About to join tenant %s (organization %s) as %s.\n", dash(inv.orgName), inv.orgID, profile.Email)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.JoinOrganization(ctx, inv.token, inv.orgID); err != nil {
		return err
	}
	after, err := c.Profile(ctx)
	if err != nil {
		return fmt.Errorf("joined, but reading the profile back failed: %w", err)
	}
	for _, org := range after.Organizations {
		if strconv.Itoa(org.ID) == inv.orgID {
			fmt.Fprintf(os.Stdout, "joined tenant %s; select it with `darkubectl config use-tenant %s`\n", org.Name, org.Name)
			return nil
		}
	}
	return fmt.Errorf("%w: organization %s is not among this account's tenants after joining", errInviteNotStored, inv.orgID)
}

// resolveInvitation takes a link as it is, and finds the newest received
// invitation for a tenant name or organization id.
func resolveInvitation(ctx context.Context, c *client.Client, ref string) (inviteLink, error) {
	if strings.Contains(ref, "?") {
		return parseInvitationLink(ref)
	}
	received, err := fetchReceivedInvitations(ctx, c)
	if err != nil {
		return inviteLink{}, err
	}
	return findReceivedInvitation(received, ref)
}

func findReceivedInvitation(received []receivedInvitation, ref string) (inviteLink, error) {
	for _, r := range received {
		if r.OrganizationID == ref || strings.EqualFold(r.Tenant, ref) {
			return r.link, nil
		}
	}
	return inviteLink{}, fmt.Errorf("%w: %q", errNoReceivedInvite, ref)
}

// inviteLink is what an invitation's console link carries.
type inviteLink struct {
	token, orgID, orgName, email string
}

// parseInvitationLink reads the token, organization id and invitee address
// from the console link an invitation email carries.
func parseInvitationLink(link string) (inviteLink, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return inviteLink{}, fmt.Errorf("%w: %w", errBadInviteLink, err)
	}
	q := u.Query()
	inv := inviteLink{
		token: q.Get("token"), orgID: q.Get("organization_id"),
		orgName: q.Get("organization_name"), email: q.Get("email"),
	}
	if inv.token == "" || inv.orgID == "" {
		return inviteLink{}, errBadInviteLink
	}
	if _, err := strconv.Atoi(inv.orgID); err != nil {
		return inviteLink{}, fmt.Errorf("%w: organization_id %q is not a number", errBadInviteLink, inv.orgID)
	}
	return inv, nil
}
