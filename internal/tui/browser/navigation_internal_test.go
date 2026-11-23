package browser

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestTenantPaneSwitchesOnCursorMovementWithoutEnter(t *testing.T) {
	t.Parallel()
	profile := MockProfile{Tenant: "alpha", Tenants: []MockTenant{
		{Name: "alpha", Apps: []client.App{{ID: "a", Name: "api"}}},
		{Name: "beta", Apps: []client.App{{ID: "b", Name: "worker"}}},
	}}
	m := newMockBrowser(profile)
	if cmd := m.Init(); cmd != nil {
		for _, batched := range requireMessage[tea.BatchMsg](t, cmd()) {
			m.Update(batched())
		}
	}
	m.focusPane(0)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.tenant != "beta" || m.tenantCursor != 1 || !m.contextFocused || !m.selectingTenant || cmd == nil {
		t.Fatalf("moving to next tenant should switch immediately and retain focus: tenant=%q cursor=%d focus=%v selecting=%v cmd=%v",
			m.tenant, m.tenantCursor, m.contextFocused, m.selectingTenant, cmd != nil)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.tenant != "alpha" || m.tenantCursor != 0 || cmd == nil {
		t.Fatalf("moving back should immediately switch tenant: tenant=%q cursor=%d cmd=%v", m.tenant, m.tenantCursor, cmd != nil)
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tenant != "alpha" {
		t.Fatalf("Enter should not be needed to apply the selected tenant; tenant=%q", m.tenant)
	}
}

func TestTenantShortcutRemovedFromDispatchAndHelp(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 30}
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.selectingTenant || m.contextFocused {
		t.Fatal("t should not open or focus tenant selection")
	}
	for _, hints := range [][]keyHint{m.keyHints(false), m.keyHints(true)} {
		for _, hint := range hints {
			if hint.key == "t" || strings.Contains(hint.action, "tenant selector") {
				t.Fatalf("obsolete tenant shortcut appears in hints: %+v", hint)
			}
		}
	}
	foundTenantPaneShortcut := false
	for _, hint := range m.keyHints(true) {
		if hint.key == "1" && strings.Contains(hint.action, "tenant") {
			foundTenantPaneShortcut = true
		}
	}
	if foundTenantPaneShortcut {
		t.Fatal("help should not show the removed tenant shortcut")
	}
}

func TestPageKeysMoveThroughTheAppsTree(t *testing.T) {
	t.Parallel()
	apps := make([]client.App, 24)
	for i := range apps {
		apps[i] = client.App{
			ID: fmt.Sprintf("app-%02d", i), Name: fmt.Sprintf("app-%02d", i),
			Namespace: client.Namespace{ID: 1, Name: "prod"},
		}
	}
	m := &browser{width: 80, height: 20}
	applyAppsResult(m, apps, nil)

	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.selected == 0 {
		t.Fatal("PageDown should advance the selected app")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.selected != 0 {
		t.Fatalf("PageUp should return to the first app, selected index is %d", m.selected)
	}
}

func TestPageMenuKeepsFocusAndWrapsWithoutEnteringTenantPane(t *testing.T) {
	t.Parallel()
	m := &browser{page: pageApps, contextFocused: true, contextRow: 1}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.page != pageStorage || !m.contextFocused || m.contextRow != 6 {
		t.Fatalf("Up from Apps should wrap to Storage and retain page focus: page=%v focused=%t row=%d", m.page, m.contextFocused, m.contextRow)
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: '2', Text: "2"})
	if !m.contextFocused || m.contextRow != 6 {
		t.Fatalf("pane 2 shortcut did not focus Pages: focused=%t row=%d", m.contextFocused, m.contextRow)
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.page != pageApps || !m.contextFocused || m.contextRow != 1 {
		t.Fatalf("Down from Storage should wrap to Apps and retain focus: page=%v focused=%t row=%d", m.page, m.contextFocused, m.contextRow)
	}
}

func TestTenantPaneHasNoCaretAndHighlightsOnlyWhenFocused(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &browser{width: 120, tenant: "alpha", tenantCursor: 0, contextRow: 1,
		tenants: []tenantOption{{name: "alpha"}, {name: "beta"}}}
	active := m.tenantPaneLines(2)[0]
	if strings.Contains(active, "›") {
		t.Fatalf("tenant pane should not show a caret: %q", active)
	}
	m.contextFocused = true
	m.contextRow = 0
	focused := m.tenantPaneLines(2)[0]
	if strings.Contains(focused, "›") || active == focused {
		t.Fatalf("tenant selection should match Pages focus styling: active=%q focused=%q", active, focused)
	}
}

func TestNamespaceSelectionDoesNotOfferAppActions(t *testing.T) {
	t.Parallel()
	m := &browser{
		width: 80, height: 24, namespaceSelected: true,
		apps: []client.App{{ID: "app-1", Name: "api"}},
	}
	for _, hint := range m.keyHints(true) {
		if hint.key == "a" {
			t.Fatal("app actions should not be advertised while a namespace row is selected")
		}
	}
	m.openActionMenu()
	if m.actionMenu {
		t.Fatal("app actions should not open while a namespace row is selected")
	}
}
