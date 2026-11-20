package browser

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestMouseClickSelectsBorderViewTab(t *testing.T) {
	t.Parallel()
	m := &browser{
		width: 120, height: 30, detail: true, activeAppView: appViewSummary,
		apps: []client.App{{ID: "app-1"}}, selected: 0,
	}
	_, summaryWidth, contextWidth := m.paneWidths()
	appsWidth, _, _ := m.paneWidths()
	viewLeft := contextWidth + 3 + appsWidth + 2 + 1
	// Resolve the tab position from the labels actually rendered at this width.
	tabs := appViewTabLabels(summaryWidth)
	position := len("[4] ")
	for i := range tabs {
		span := len(tabs[i])
		if visibleAppViews()[i] == appViewSummary {
			span += 2
		}
		if visibleAppViews()[i] == appViewUsage {
			break
		}
		position += span + 1
	}
	resourcesX := viewLeft + 1 + 2 + position
	m.handleBrowserMouse(tea.MouseClickMsg{X: resourcesX, Y: m.browserBodyY(), Button: tea.MouseLeft})
	if m.activeAppView != appViewUsage {
		t.Fatalf("active view = %v, want Resources (pane width %d)", m.activeAppView, summaryWidth)
	}
}
