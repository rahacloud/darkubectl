package browser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestWriteDisabledNoticeClearsOnNextKey(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 120} {
		m := &browser{width: width, height: 24, apps: []client.App{{ID: "app", Name: "api"}}}
		key := tea.KeyPressMsg{Code: 'a', Text: "a"}
		m.Update(key)
		if m.actionNotice != errWriteActionsAreDisabledRelaunchWithAllow.Error() {
			t.Fatalf("width %d: missing notice", width)
		}
		lines := m.viewContentLines()
		joined := ansi.Strip(strings.Join(lines, "\n"))
		if strings.Count(joined, "Use --allow-writes") != 1 || strings.Contains(joined, m.actionNotice) {
			t.Fatalf("width %d: incorrect notice placement: %s", width, joined)
		}
		bottom := ansi.Strip(lines[len(lines)-1])
		if ansi.StringWidth(bottom) != width {
			t.Fatalf("width %d: bottom border width = %d: %q", width, ansi.StringWidth(bottom), bottom)
		}
		appsWidth, _, contextWidth := m.paneWidths()
		appsStart := contextWidth + 3
		appsBottom := []rune(bottom)[appsStart : appsStart+appsWidth+2]
		if !strings.Contains(string(appsBottom), "Use --allow-writes") {
			t.Fatalf("width %d: notice not on Apps bottom border: %q", width, string(appsBottom))
		}
		m.Update(key)
		if m.actionNotice != errWriteActionsAreDisabledRelaunchWithAllow.Error() {
			t.Fatalf("width %d: repeated action should show a fresh notice", width)
		}
		m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		if m.page == pageApps || m.actionNotice != "" || m.currentAppPaneMessage() != "" {
			t.Fatalf("width %d: notice persisted after navigation", width)
		}
		if strings.Contains(ansi.Strip(strings.Join(m.viewContentLines(), "\n")), errWriteActionsAreDisabledRelaunchWithAllow.Error()) {
			t.Fatalf("width %d: notice remains visible", width)
		}
	}
}

func TestWriteDisabledNoticeShowsAppsWhileViewMaximized(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 30, detail: true, activeAppView: appViewLogs, logMaximized: true,
		apps: []client.App{{ID: "app", Name: "api"}}}
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	view := ansi.Strip(strings.Join(m.viewContentLines(), "\n"))
	if !strings.Contains(view, "Use --allow-writes") || !strings.Contains(view, "[3] APPS") {
		t.Fatal("Apps pane and its notice should be visible")
	}
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if !m.logMaximized || m.writeDisabledNotice() {
		t.Fatal("the maximized view should remain enabled after the notice clears")
	}
}

func TestWriteDisabledNoticeClearsOnMouseTenantChange(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 30, tenant: "alpha", tenants: []tenantOption{{name: "alpha"}, {name: "beta"}},
		apps: []client.App{{ID: "app", Name: "api"}}}
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m.Update(tea.MouseClickMsg{X: 1, Y: m.browserBodyY() + 2, Button: tea.MouseLeft})
	if m.tenant != "beta" || m.actionNotice != "" {
		t.Fatalf("tenant=%q notice=%q", m.tenant, m.actionNotice)
	}
}

func TestActionResultsRemainOnNextKey(t *testing.T) {
	t.Parallel()
	m := &browser{actionNotice: "Restart requested for api"}
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.actionNotice != "Restart requested for api" {
		t.Fatal("action result was cleared")
	}
	m.page = pageActivity
	if m.currentAppPaneMessage() != "" {
		t.Fatal("app action notice projected onto another page")
	}
}

func TestWriteDisabledNoticeClearsOnMousePageChange(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 30, page: pageActivity,
		actionNotice: errWriteActionsAreDisabledRelaunchWithAllow.Error()}
	bodyY := m.browserBodyY()
	bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
	tenantHeight := max(handleActivityMouseValue4, (bodyHeight-1)/contextPaneHeightDivisor)
	m.Update(tea.MouseClickMsg{X: 1, Y: bodyY + tenantHeight + handleActivityMouseValue2, Button: tea.MouseLeft})
	if m.page != pageApps || m.actionNotice != "" {
		t.Fatalf("page=%v notice=%q", m.page, m.actionNotice)
	}
}
