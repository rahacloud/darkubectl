package client

import (
	"context"
	"strconv"
)

// Member is one member of an organization, from GET
// /api/v1/organizations/<numeric id>/members (a bare list; confirmed
// 2026-10-01). full_name falls back to the email when the account has no
// name. The console changes membership with a PUT of the whole list, which is
// not wired.
type Member struct {
	ID       int      `json:"id"          yaml:"id"`
	Email    string   `json:"email"       yaml:"email"`
	FullName string   `json:"full_name"   yaml:"full_name"`
	Roles    []string `json:"roles"       yaml:"roles"`
	Verified bool     `json:"is_verified" yaml:"is_verified"`
}

// Members returns the members of the organization with the numeric id orgID.
func (c *Client) Members(ctx context.Context, orgID int) ([]Member, error) {
	var out []Member
	if err := c.getJSON(ctx, "/api/v1/organizations/"+strconv.Itoa(orgID)+"/members", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
