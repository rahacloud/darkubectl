package cmd

import (
	"errors"
	"slices"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestParsePermissions(t *testing.T) {
	t.Parallel()

	got, err := parsePermissions([]string{"view, Change", "delete"})
	if err != nil || !slices.Equal(got, []string{"view", "change", "delete"}) {
		t.Errorf("parsePermissions = %v, %v", got, err)
	}
	if _, err := parsePermissions([]string{"admin"}); !errors.Is(err, errBadPermission) {
		t.Errorf("admin: err = %v", err)
	}
}

func TestApplyPermissionChange(t *testing.T) {
	t.Parallel()

	got := applyPermissionChange([]string{"view"}, []string{"delete", "change"}, nil)
	if !slices.Equal(got, []string{"change", "delete", "view"}) {
		t.Errorf("grant = %v", got)
	}
	got = applyPermissionChange(got, nil, []string{"change"})
	if !slices.Equal(got, []string{"delete", "view"}) {
		t.Errorf("revoke = %v", got)
	}
	if got = applyPermissionChange(got, nil, client.AllPermissions); len(got) != 0 {
		t.Errorf("none = %v", got)
	}
}

func TestDisplayNameHidesEmailFallback(t *testing.T) {
	t.Parallel()

	if got := displayName(client.PermissionUser{Email: "a@example.com", FullName: "a@example.com"}); got != "" {
		t.Errorf("fallback = %q", got)
	}
	if got := displayName(client.PermissionUser{Email: "a@example.com", FullName: "Ada"}); got != "Ada" {
		t.Errorf("name = %q", got)
	}
}
