package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) switchAppView(delta int) tea.Cmd {
	if m.page != pageApps || m.contextFocused || m.selectingTenant || !m.selectedAppActive() {
		return nil
	}
	wasLogs := m.activeAppView == appViewLogs
	m.resetAppViewEditor(m.activeAppView)
	views := visibleAppViews()
	index := 0
	for i, view := range views {
		if view == m.activeAppView {
			index = i
			break
		}
	}
	index = (index + delta + len(views)) % len(views)
	m.activeAppView = views[index]
	if wasLogs {
		m.logFollowing = false
		m.cancelRequest(requestKey{kind: requestLogs})
		m.logPollInFlight = false
		m.logState.reset()
	}
	m.usageScroll = 0
	m.summaryScroll = 0
	return m.loadActiveAppView()
}

func (m *browser) resetAppViewEditor(view appView) {
	switch view {
	case appViewBuilds:
		m.ensureCICDSettings()
		m.cicdToken, m.cicdNotice = "", ""
		m.cicdReveal, m.cicdTokenReveal, m.cicdLoading = false, false, false
		m.cicdGeneration++
		m.cicdSettingsOriginal, m.cicdSettingsDraft = client.GitBuildSettings{}, client.GitBuildSettings{}
		m.cicdSettingsLoaded, m.cicdReview, m.cicdSaving = false, false, false
		m.cicdSettingsNotice, m.cicdHistoryFocus = "", false
		m.cicdSettingsCursor, m.cicdEditField = 0, -1
	case appViewEnv:
		m.envEditMode = envEditNone
		m.envEditName, m.envEditValue = "", ""
		m.envConfirm = false
		m.envRevealName, m.envRevealValue = "", ""
	case appViewPorts:
		m.portEditing, m.portConfirm, m.portBusy = false, false, false
		m.portEditValue = ""
	case appViewUsage:
		m.resourceEditing, m.resourceReview, m.resourcePlanPicker = false, false, false
		m.resourceEditValue = ""
	default:
		return
	}
}

func (m *browser) loadActiveAppView() tea.Cmd {
	switch m.activeAppView {
	case appViewLogs:
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
	case appViewUsage:
		return tea.Batch(m.loadUsageView(), m.loadSelectedPVCs())
	case appViewEnv:
		if m.envState.status == requestIdle {
			return m.loadEnv()
		}
	case appViewDomains:
		if m.domainState.status == requestIdle {
			return m.loadDomains()
		}
	case appViewPorts:
		if m.portState.status == requestIdle {
			return m.loadPorts()
		}
	case appViewBuilds:
		return m.loadBuildsView()
	default:
		return nil
	}
	return nil
}

func (m *browser) loadUsageView() tea.Cmd {
	if m.usageState.status == requestIdle {
		return tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory())
	}
	if m.historyState.status == requestIdle {
		return m.loadUsageHistory()
	}
	return nil
}

func (m *browser) loadBuildsView() tea.Cmd {
	app := m.selectedApp()
	m.ensureCICDSettings()
	if m.cicdToken == "" {
		tokenCmd := m.requestCICDToken(false)
		if app.BuildMethod == client.BuildMethodWebhook {
			return tea.Batch(m.loadSelectedBuilds(), tokenCmd)
		}
		return tokenCmd
	}
	if app.BuildMethod == "" {
		return m.loadSelectedSummary()
	}
	if m.buildsState.status == requestIdle || m.buildsState.status == requestUnavailable {
		return m.loadSelectedBuilds()
	}
	return nil
}
