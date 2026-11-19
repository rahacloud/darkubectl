package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
	"testing"
)

func TestPrintableFilterInputDoesNotTriggerGlobalKeys(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24, filterEditing: true, filterBeforeEdit: "a", filterQuery: "a"}
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !m.filterEditing || m.filterQuery != "aq" {
		t.Fatalf("q should remain literal filter input, editing=%v query=%q", m.filterEditing, m.filterQuery)
	}
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if !m.filterEditing || m.filterQuery != "aq1" {
		t.Fatalf("pane shortcuts should remain literal filter input, editing=%v query=%q", m.filterEditing, m.filterQuery)
	}
}

func TestSortOverlayConsumesKeysUntilClosed(t *testing.T) {
	t.Parallel()
	m := &browser{
		width: 80, height: 24, sorting: true, sortChoice: int(sortName),
		apps: []client.App{{ID: "a", Name: "api"}},
	}
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.sortChoice != int(sortNamespace) {
		t.Fatal("j should move through the sort fields")
	}
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.actionMenu || !m.sorting {
		t.Fatal("global action shortcuts should not escape the sort overlay")
	}
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !m.sorting {
		t.Fatal("q should not quit or dismiss the sort overlay")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sorting || m.sortBy != sortNamespace {
		t.Fatal("Enter should apply the selected sort field")
	}
}

func TestHelpOverlayConsumesNavigationUntilClosed(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24, help: true, contextFocused: true, contextRow: 0}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.help || !m.contextFocused || m.contextRow != 0 {
		t.Fatal("pane navigation should not change focus behind the help overlay")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.help {
		t.Fatal("Escape should close the help overlay")
	}
}

func TestSummaryUsageScalingAndLogsViewNavigation(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24}
	applyAppsResult(m, []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
	}}, nil)
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if !m.detail {
		t.Fatal("l should open the selected app summary")
	}
	if m.activeAppView != appViewSummary {
		t.Fatal("opening app details should start on Summary")
	}
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.activeAppView != appViewUsage || !m.detail {
		t.Fatal("] should switch to Usage without changing app focus")
	}
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.activeAppView != appViewScaling {
		t.Fatal("] should switch to Scaling")
	}
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.activeAppView != appViewUsage {
		t.Fatal("[ should switch backward to Usage")
	}
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.activeAppView != appViewSummary {
		t.Fatal("[ should wrap backward to Summary")
	}
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.activeAppView != appViewLogs {
		t.Fatal("[ should wrap backward to Logs")
	}
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.activeAppView != appViewScaling {
		t.Fatal("[ should wrap backward to Scaling")
	}
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m.detail || m.activeAppView != appViewScaling {
		t.Fatal("h should return focus to Apps while preserving the selected app view")
	}
}
