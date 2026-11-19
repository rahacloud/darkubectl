package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
	"strings"
	"testing"
)

func TestPaneBordersStayAlignedBetweenTenantAndPages(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := &browser{width: width, height: 30}
		appsWidth, _, contextWidth := m.paneWidths()
		lines := m.splitPanes(nil, nil, 24)
		separator := (24 - 1) / 2
		for _, row := range []int{separator - 1, separator, separator + 1} {
			plain := ansi.Strip(lines[row])
			if got := ansi.StringWidth(plain); got != width {
				t.Errorf("width %d row %d: rendered width %d", width, row, got)
			}
			for _, column := range []int{contextWidth + 3, contextWidth + 3 + appsWidth + 3} {
				if got := ansi.Cut(plain, column, column+1); !strings.ContainsAny(got, "│╭╰") {
					t.Errorf("width %d row %d column %d: missing pane border, got %q", width, row, column, got)
				}
			}
		}
	}
}

func TestFooterShowsOnlyQuitAndHelpWhileHelpModalListsShortcuts(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 24}
	applyAppsResult(m, []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
	}}, nil)

	footer := ansi.Strip(m.footerLine())
	if footer != "q quit · ? help" {
		t.Fatalf("footer = %q, want only quit and help", footer)
	}
	m.help = true
	background := make([]string, m.height)
	help := ansi.Strip(strings.Join(m.helpModal(background), "\n"))
	for _, hint := range m.keyHints(true) {
		if !strings.Contains(help, hint.key) || !strings.Contains(help, hint.action) {
			t.Errorf("help is missing hint %q %q", hint.key, hint.action)
		}
	}
	for _, key := range []string{"q", "?"} {
		if !strings.Contains(help, key) {
			t.Errorf("help is missing global footer key %q", key)
		}
	}
	for _, unavailable := range []string{"Pods", "Terminal", "Domains"} {
		if strings.Contains(help, unavailable) {
			t.Errorf("help advertises unimplemented app view %q", unavailable)
		}
	}
}

func TestFooterFitsSupportedTerminalWidths(t *testing.T) {
	t.Parallel()
	for _, width := range []int{60, 72, 80, 99, 100, 120} {
		states := []struct {
			name string
			set  func(*browser)
		}{
			{name: "default"},
			{name: "action", set: func(m *browser) { m.actionMenu = true }},
			{name: "restart", set: func(m *browser) { m.confirmAction = appActionRestart }},
			{name: "delete", set: func(m *browser) { m.confirmAction = appActionDelete }},
			{name: "filter", set: func(m *browser) { m.filterEditing = true }},
			{name: "sort", set: func(m *browser) { m.sorting = true }},
			{name: "namespace", set: func(m *browser) { m.selectingNamespace = true }},
			{name: "tenant", set: func(m *browser) { m.selectingTenant = true }},
			{name: "context", set: func(m *browser) { m.contextFocused = true }},
			{name: "help", set: func(m *browser) { m.help = true }},
		}
		for _, state := range states {
			m := &browser{width: width, height: 24}
			if state.set != nil {
				state.set(m)
			}
			if got := ansi.StringWidth(ansi.Strip(m.footerLine())); got > width {
				t.Errorf("%s footer width %d exceeds terminal width %d", state.name, got, width)
			}
		}
	}
}

func TestResizeBelowMinimumPreservesCurrentOverlayState(t *testing.T) {
	t.Parallel()
	m := &browser{width: 100, height: 30, actionMenu: true, confirmAction: appActionDelete, confirmName: "api"}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if !m.actionMenu || m.confirmAction != appActionDelete || m.confirmName != "api" {
		t.Fatalf("resize discarded overlay state: menu=%v action=%v input=%q", m.actionMenu, m.confirmAction, m.confirmName)
	}
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "Resize terminal") {
		t.Fatalf("small terminal should show resize guidance, got %q", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !m.actionMenu || m.confirmAction != appActionDelete || m.confirmName != "api" {
		t.Fatal("resizing back should restore the same overlay state")
	}
}

func TestResizePreservesFilterAndSelection(t *testing.T) {
	t.Parallel()
	m := &browser{width: 100, height: 30, tenant: "acme", filterQuery: "prod/api", filterEditing: true, selected: 3}
	m.Update(tea.WindowSizeMsg{Width: 45, Height: 14})
	if m.tenant != "acme" || m.filterQuery != "prod/api" || !m.filterEditing || m.selected != 3 {
		t.Fatalf("resize changed browsing state: tenant=%q filter=%q editing=%v selected=%d", m.tenant, m.filterQuery, m.filterEditing, m.selected)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.filterQuery != "prod/api" || !m.filterEditing || m.selected != 3 {
		t.Fatal("resizing back should resume the same filter and selection")
	}
}
