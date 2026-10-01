package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
)

// Per-app access control, the console's Permissions tab (console-remote,
// 2026-09-30):
//
//	GET permissions/app/<uuid>/?just_me=false  [{user:{id,email,full_name,…}, permissions:[…]}]
//	PUT permissions/app/<uuid>/                the same rows, for every member who keeps any
//
// The read lists every member of the organization, those without access with
// an empty permissions list. A member left out of the PUT keeps whatever they
// had: revoking takes the member's row with an explicit empty list, which is
// what the console sends once a box is unchecked (confirmed 2026-10-01 on a
// throwaway app, where omitting the row left the member with "view"). It is
// only enforced in an organization whose is_acl_enabled is true; elsewhere the
// console disables the tab.
const permissionsPathV1 = "/api/v1/darkube/permissions/app/"

// ErrNoSuchOrganization means the account does not belong to the organization.
var ErrNoSuchOrganization = errors.New("the account is not a member of that organization")

// App permissions, as the console names them.
const (
	PermView   = "view"
	PermChange = "change"
	PermDelete = "delete"
)

// AllPermissions is every permission an app grants, in display order.
var AllPermissions = []string{PermView, PermChange, PermDelete}

// PermissionUser is the member a permission row is about.
type PermissionUser struct {
	ID       int    `json:"id"        yaml:"id"`
	Email    string `json:"email"     yaml:"email"`
	FullName string `json:"full_name" yaml:"full_name"`
}

// AppPermission is one member's access to an app.
type AppPermission struct {
	User        PermissionUser `json:"user"        yaml:"user"`
	Permissions []string       `json:"permissions" yaml:"permissions"`

	// raw is the row as read, sent back on a write so that no field the
	// console echoes is lost.
	raw map[string]any
}

// Has reports whether the member holds perm.
func (p AppPermission) Has(perm string) bool { return slices.Contains(p.Permissions, perm) }

// AppPermissions returns every organization member's access to an app.
func (c *Client) AppPermissions(ctx context.Context, appID string) ([]AppPermission, error) {
	q := url.Values{}
	q.Set("just_me", "false")
	data, err := c.do(ctx, http.MethodGet, permissionsPathV1+url.PathEscape(appID)+"/", q, nil)
	if err != nil {
		return nil, err
	}
	var rows []AppPermission
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	var raws []map[string]any
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	for i := range rows {
		if i < len(raws) {
			rows[i].raw = raws[i]
		}
	}
	return rows, nil
}

// SetAppPermissions writes an app's access list. Every row is sent, those
// with no permissions included: an empty list is what revokes access.
func (c *Client) SetAppPermissions(ctx context.Context, appID string, rows []AppPermission) error {
	body := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		perms := r.Permissions
		if perms == nil {
			perms = []string{}
		}
		row := map[string]any{}
		maps.Copy(row, r.raw)
		if _, ok := row["user"]; !ok {
			row["user"] = r.User
		}
		row["permissions"] = perms
		body = append(body, row)
	}
	_, err := c.do(ctx, http.MethodPut, permissionsPathV1+url.PathEscape(appID)+"/", nil, body)
	return err
}

// IsACLEnabled reports whether per-app permissions are enforced in an
// organization, from the account profile.
func (c *Client) IsACLEnabled(ctx context.Context, org string) (bool, error) {
	var p struct {
		Organizations []struct {
			Name         string `json:"name"`
			IsACLEnabled bool   `json:"is_acl_enabled"`
		} `json:"organizations"`
	}
	if err := c.getJSON(ctx, profilePathV2, nil, &p); err != nil {
		return false, err
	}
	for _, o := range p.Organizations {
		if o.Name == org {
			return o.IsACLEnabled, nil
		}
	}
	return false, fmt.Errorf("%w: %s", ErrNoSuchOrganization, org)
}
