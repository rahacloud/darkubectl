package browser

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestMouseClickSelectsBorderViewTab(t *testing.T) {
	m := &browser{
		width: 120, height: 30, detail: true, activeAppView: appViewSummary,
		apps: []client.App{{ID: "app-1"}}, selected: 0,
	}
	_, summaryWidth, contextWidth := m.paneWidths()
	appsWidth, _, _ := m.paneWidths()
	viewLeft := contextWidth + 3 + appsWidth + 2 + 1
	// At this width the title is "[4] [Summary] Usage Scaling Logs".
	// Scaling starts after the prefix, selected Summary, and Usage labels.
	scalingX := viewLeft + 1 + 2 + len("[4] ") + len("[Summary] ") + len("Usage ")
	m.handleBrowserMouse(tea.MouseClickMsg{X: scalingX, Y: m.browserBodyY(), Button: tea.MouseLeft})
	if m.activeAppView != appViewScaling {
		t.Fatalf("active view = %v, want Scaling (pane width %d)", m.activeAppView, summaryWidth)
	}
}
