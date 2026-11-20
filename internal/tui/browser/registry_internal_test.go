package browser

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestActivateRegistryKeepsPagePaneFocused(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageApps, tenant: "acme", contextFocused: true}
	m.activatePageRow(5)
	if m.page != pageRegistry || !m.contextFocused || m.contextRow != 5 || m.selectingTenant {
		t.Fatalf("registry activation should keep Pages pane focused: page=%v context=%t row=%d tenant=%t", m.page, m.contextFocused,
			m.contextRow, m.selectingTenant)
	}
}

func TestRegistryRightLoadsImageManifests(t *testing.T) {
	t.Parallel()
	reg := client.Registry{ID: "registry-id", Name: "hamdocker"}
	image := client.Repository{Name: "org/api"}
	m := &browser{page: pageRegistry, tenant: "acme", ctx: context.Background(), registries: []client.Registry{reg},
		registryRepos: map[string][]client.Repository{string(reg.ID): {image}}, registryDigests: map[string][]client.Digest{},
		registryRows: []registryTreeRow{{registry: 0, repo: -1, digest: -1}, {registry: 0, repo: 0, digest: -1}}, registryCursor: 1}
	m.listRegistryDigests = func(context.Context, string, client.FlexID, string) ([]client.Digest, error) {
		return []client.Digest{{Digest: "sha256:123", Tags: []string{"release-53", "latest"}}}, nil
	}
	cmd := m.handleRegistryKey("right")
	if cmd == nil {
		t.Fatal("Right on an image did not start a manifest request")
	}
	raw := cmd()
	msg, ok := raw.(registryDigestsResult)
	if !ok {
		t.Fatalf("request returned %T", raw)
	}
	m.handleRegistryDigests(msg)
	if len(m.registryRows) != 3 || m.registryRows[2].label != "release-53, latest" {
		t.Fatalf("manifest row did not show both aliases together: %+v", m.registryRows)
	}
}

func TestRegistryImageReferenceIncludesAddressAndTag(t *testing.T) {
	t.Parallel()
	reg := client.Registry{Address: "registry.example.test", Username: "acme", IsProvidedAsSaaS: true}
	got := registryImageReference(reg, "team/api", "release-53", "")
	if want := "registry.example.test/acme/team/api:release-53"; got != want {
		t.Fatalf("registry reference=%q, want %q", got, want)
	}
	got = registryImageReference(reg, "team/api", "", "sha256:abcd")
	if want := "registry.example.test/acme/team/api@sha256:abcd"; got != want {
		t.Fatalf("digest reference=%q, want %q", got, want)
	}
}

func TestRegistryAliasesShareOneTreeRowAndWheelMovesSelection(t *testing.T) {
	t.Parallel()
	merged := mergeRegistryDigestAliases([]client.Digest{{Digest: "sha256:same", Tags: []string{"release-53"}},
		{Digest: "sha256:same", Tags: []string{"latest"}}, {Digest: "sha256:other", Tags: []string{"older"}}})
	if len(merged) != 2 || len(merged[0].Tags) != 2 {
		t.Fatalf("same digest aliases were not merged: %+v", merged)
	}
	start := registryViewportStart(30, 8, 15)
	if start != 8 || 15 < start || 15 >= start+8 {
		t.Fatalf("viewport start=%d does not contain selection 15", start)
	}
	m := &browser{page: pageRegistry, width: 100, height: 30, registryRows: make([]registryTreeRow, 20), registryCursor: 5}
	m.handleBrowserWheel(tea.MouseWheelMsg{X: 24, Y: 3, Button: tea.MouseWheelDown})
	if m.registryCursor != 8 {
		t.Fatalf("wheel moved registry cursor to %d, want 8", m.registryCursor)
	}
}
