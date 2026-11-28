package client

import (
	"context"
	"net/http"
	"strconv"
)

// Member is one member of an organization, from GET
// /api/v1/organizations/<numeric id>/members (a bare list; confirmed
// 2026-10-01). full_name falls back to the email when the account has no
// name. The console changes membership with a PUT of the whole list to the
// same route: it appends {email} to invite someone and drops a row to remove
// a member (console-account's team page, read 2026-11-28).
type Member struct {
	ID       int      `json:"id"          yaml:"id"`
	Email    string   `json:"email"       yaml:"email"`
	FullName string   `json:"full_name"   yaml:"full_name"`
	Roles    []string `json:"roles"       yaml:"roles"`
	Verified bool     `json:"is_verified" yaml:"is_verified"`
}

// joinByTokenPath accepts an invitation. Its token and organization id come
// from the link in the invitation email (/organization-invitation?token=…&
// organization_id=…&email=…), and the console sends both as strings.
const joinByTokenPath = "/api/v1/organizations/join_by_token" //nolint:gosec // a route, not a credential

const fieldEmail = "email"

func membersPath(orgID int) string {
	return "/api/v1/organizations/" + strconv.Itoa(orgID) + "/members"
}

// Members returns the members of the organization with the numeric id orgID.
func (c *Client) Members(ctx context.Context, orgID int) ([]Member, error) {
	var out []Member
	if err := c.getJSON(ctx, membersPath(orgID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InviteMembers invites emails to the organization with the numeric id orgID.
// There is no invite route: the console PUTs the member list it read with
// {email} appended, so this does the same. The rows are echoed as read, not
// rebuilt from Member, so a field the struct does not know is not dropped.
// A concurrent change to the member list between the read and the write is
// lost, as it is in the console.
func (c *Client) InviteMembers(ctx context.Context, orgID int, emails []string) error {
	var rows []map[string]any
	if err := c.getJSON(ctx, membersPath(orgID), nil, &rows); err != nil {
		return err
	}
	for _, email := range emails {
		rows = append(rows, map[string]any{fieldEmail: email})
	}
	_, err := c.do(ctx, http.MethodPut, membersPath(orgID), nil, rows)
	return err
}

// JoinOrganization accepts the invitation that token and organizationID, both
// taken from the invitation link, name. It joins the signed-in account.
func (c *Client) JoinOrganization(ctx context.Context, token, organizationID string) error {
	body := map[string]string{"token": token, "organization_id": organizationID}
	return c.postJSON(ctx, joinByTokenPath, body, nil)
}
