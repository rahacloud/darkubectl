package client

import (
	"context"
	"fmt"
)

// profilePathV2 is the signed-in user's account, including tenant memberships.
//
// It lives under /api/v2/users/, not the /api/v1/darkube/ tree the rest of the
// API uses, and it is the only endpoint that lists organizations: there is no
// /organizations/ route at any version (all 404). It is also not tenant-scoped,
// so it answers "which tenants exist" before an X-Organization is known.
const profilePathV2 = "/api/v2/users/profile"

// Organization is a tenant the signed-in user belongs to.
//
// ID is the numeric primary key the app create/update payload calls
// "organization"; Name is the slug sent as the X-Organization header.
type Organization struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Roles []string `json:"current_user_roles"`
}

// Profile is the signed-in user's account.
type Profile struct {
	ID            int            `json:"id"`
	Email         string         `json:"email"`
	FullName      string         `json:"full_name"`
	Organizations []Organization `json:"organizations"`
}

// Profile returns the signed-in user and the tenants they belong to.
func (c *Client) Profile(ctx context.Context) (*Profile, error) {
	var p Profile
	if err := c.getJSON(ctx, profilePathV2, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Organization returns the current tenant's entry from the user's profile,
// matching on the slug carried in the X-Organization header.
func (c *Client) Organization(ctx context.Context) (*Organization, error) {
	p, err := c.Profile(ctx)
	if err != nil {
		return nil, err
	}
	for i := range p.Organizations {
		if p.Organizations[i].Name == c.Org {
			return &p.Organizations[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %q is not among this account's organizations", ErrNoOrganizationID, c.Org)
}
