package browser

import (
	tea "charm.land/bubbletea/v2"
)

func (m *browser) switchAppView(delta int) tea.Cmd {
	if !m.detail || !m.selectedAppActive() {
		return nil
	}
	wasLogs := m.activeAppView == appViewLogs
	m.activeAppView = appView((int(m.activeAppView) + delta + appViewsCount) % appViewsCount)
	if wasLogs {
		m.logFollowing = false
		m.cancelRequest(requestKey{kind: requestLogs})
		m.logPollInFlight = false
		m.logState.reset()
	}
	m.usageScroll = 0
	m.summaryScroll = 0
	if m.activeAppView == appViewLogs {
		if !m.logPrevious {
			m.logFollowing = !m.logFollowPausedByUser
		}
		if m.logPod != "" && m.logContainer != "" {
			return m.loadSelectedLogs()
		}
		if len(m.podList) > 0 {
			return m.prepareLogTarget()
		}
		return m.loadSelectedPods()
	}
	if m.activeAppView == appViewUsage && m.usageState.status == requestIdle {
		return tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory())
	}
	if m.activeAppView == appViewUsage && m.historyState.status == requestIdle {
		return m.loadUsageHistory()
	}
	return nil
}
