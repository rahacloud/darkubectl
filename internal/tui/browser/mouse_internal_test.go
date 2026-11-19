package browser

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestMouseAppSelectionInvalidatesAppSpecificViews(t *testing.T) {
	views := []struct {
		name string
		view appView
	}{
		{name: "summary", view: appViewSummary},
		{name: "usage", view: appViewUsage},
		{name: "logs", view: appViewLogs},
	}
	inputs := []struct {
		name  string
		click bool
	}{
		{name: "click app row", click: true},
		{name: "wheel app list"},
	}
	for _, view := range views {
		for _, input := range inputs {
			t.Run(view.name+"/"+input.name, func(t *testing.T) {
				m := &browser{
					width: 120, height: 30, tenant: "tenant", detail: true,
					activeAppView: view.view, selected: 0,
					apps:   []client.App{{ID: "app-a", Name: "a"}, {ID: "app-b", Name: "b"}},
					logPod: "pod-a", logContainer: "main",
					logEntries: []client.LogEntry{{Timestamp: "1", Text: "belongs to app A"}},
					usageAppID: "app-a", detailUsage: &AppUsage{},
				}
				m.detailApp = &m.apps[0]
				generation := m.selectionGeneration
				_, _, contextWidth := m.paneWidths()
				appsLeft := contextWidth + 3
				if input.click {
					appRow := -1
					for i, node := range m.treeNodes() {
						if node.app == 1 {
							appRow = i
							break
						}
					}
					if appRow < 0 {
						t.Fatal("second app is missing from the tree")
					}
					nodeTop := len(m.appPaneLines(0))
					y := m.browserBodyY() + 1 + nodeTop + appRow
					m.handleBrowserMouse(tea.MouseClickMsg{X: appsLeft + 1, Y: y, Button: tea.MouseLeft})
				} else {
					m.handleBrowserWheel(tea.MouseWheelMsg{
						X: appsLeft + 1, Y: m.browserBodyY() + 1, Button: tea.MouseWheelDown,
					})
				}
				if m.selected != 1 || m.detailApp == nil || m.detailApp.ID != "app-b" {
					t.Fatalf("selected app = %d, detail = %+v", m.selected, m.detailApp)
				}
				if m.activeAppView != appViewSummary || m.selectionGeneration <= generation {
					t.Fatalf("view=%v selection generation=%d, want reset Summary after app change", m.activeAppView, m.selectionGeneration)
				}
				if m.logPod != "" || len(m.logEntries) != 0 || m.usageAppID != "" || m.detailUsage != nil {
					t.Fatalf("old app data survived selection: pod=%q logs=%d usage=%q detailUsage=%v", m.logPod, len(m.logEntries), m.usageAppID, m.detailUsage)
				}
			})
		}
	}
}
