package browser

import (
	tea "charm.land/bubbletea/v2"
)

func (m *browser) switchAppView(delta int) tea.Cmd {
	if !m.detail || !m.selectedAppActive() {
		return nil
	}
	m.activeAppView = appView((int(m.activeAppView) + delta + 3) % 3)
	m.usageScroll = 0
	m.summaryScroll = 0
	if m.activeAppView == appViewUsage && m.usageState.status == requestIdle {
		return tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory())
	}
	if m.activeAppView == appViewUsage && m.historyState.status == requestIdle {
		return m.loadUsageHistory()
	}
	return nil
}
