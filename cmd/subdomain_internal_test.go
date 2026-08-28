package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestRejectPlatformSubdomains(t *testing.T) {
	t.Parallel()

	// external_hosts is for domains you own and CNAME in. A <label>.darkube.app
	// name is the platform's own subdomain and belongs in custom_subdomain_addr;
	// sending it as an external host returns 400 InvalidExternalHost with a
	// Persian detail that does not say which of the two was wanted.
	err := rejectPlatformSubdomains([]string{"blackbox-sepid-org.darkube.app"}, "darkube.app")
	if !errors.Is(err, errPlatformSubdomain) {
		t.Fatalf("err = %v, want errPlatformSubdomain", err)
	}
	// The whole value of catching it early is naming the command that works.
	if !strings.Contains(err.Error(), "set subdomain") {
		t.Errorf("err should point at `set subdomain`; got: %v", err)
	}
	if !strings.Contains(err.Error(), "blackbox-sepid-org") {
		t.Errorf("err should suggest the bare label; got: %v", err)
	}
}

func TestRejectPlatformSubdomainsAllowsOwnedDomains(t *testing.T) {
	t.Parallel()

	err := rejectPlatformSubdomains([]string{"api.example.com", "www.example.com"}, "darkube.app")
	if err != nil {
		t.Errorf("err = %v, want nil for domains the user owns", err)
	}
}

func TestRejectPlatformSubdomainsIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	if err := rejectPlatformSubdomains([]string{"App.DARKUBE.App"}, "darkube.app"); err == nil {
		t.Error("expected rejection regardless of case")
	}
}

func TestRejectPlatformSubdomainsNoBaseDomain(t *testing.T) {
	t.Parallel()

	// If the cluster did not report a base domain there is nothing to compare
	// against, and guessing would block a legitimate custom domain.
	if err := rejectPlatformSubdomains([]string{"anything.darkube.app"}, ""); err != nil {
		t.Errorf("err = %v, want nil when the base domain is unknown", err)
	}
}

func TestClusterBaseDomain(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"cluster": map[string]any{"apps_custom_base_domain": "darkube.app"},
	}
	if got := clusterBaseDomain(raw); got != "darkube.app" {
		t.Errorf("clusterBaseDomain = %q, want darkube.app", got)
	}
	if got := clusterBaseDomain(map[string]any{}); got != "" {
		t.Errorf("clusterBaseDomain(no cluster) = %q, want empty", got)
	}
	if got := clusterBaseDomain(map[string]any{"cluster": nil}); got != "" {
		t.Errorf("clusterBaseDomain(null cluster) = %q, want empty", got)
	}
}

func TestHostname(t *testing.T) {
	t.Parallel()

	if got := hostname("api", "darkube.app"); got != "api.darkube.app" {
		t.Errorf("hostname = %q, want api.darkube.app", got)
	}
	if got := hostname("api", ""); got != "api" {
		t.Errorf("hostname with no base = %q, want api", got)
	}
}
