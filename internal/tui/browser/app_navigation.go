package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) switchAppView(delta int) tea.Cmd {
	if !m.detail || !m.selectedAppActive() {
		return nil
	}
	wasLogs := m.activeAppView == appViewLogs
	if m.activeAppView == appViewBuilds {
		m.ensureCICDSettings()
		m.cicdToken, m.cicdNotice = "", ""
		m.cicdReveal, m.cicdTokenReveal, m.cicdLoading = false, false, false
		m.cicdGeneration++
		m.cicdSettingsOriginal, m.cicdSettingsDraft = client.GitBuildSettings{}, client.GitBuildSettings{}
		m.cicdSettingsLoaded, m.cicdReview, m.cicdSaving = false, false, false
		m.cicdSettingsNotice, m.cicdHistoryFocus = "", false
		m.cicdSettingsCursor, m.cicdEditField = 0, -1
	}
	if m.activeAppView == appViewEnv {
		m.envEditMode = envEditNone
		m.envEditName, m.envEditValue = "", ""
		m.envConfirm = false
		m.envRevealName, m.envRevealValue = "", ""
	}
	if m.activeAppView == appViewPorts {
		m.portEditing, m.portConfirm, m.portBusy = false, false, false
		m.portEditValue = ""
	}
	if m.activeAppView == appViewUsage {
		m.resourceEditing, m.resourceReview, m.resourcePlanPicker = false, false, false
		m.resourceEditValue = ""
	}
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
	if m.activeAppView == appViewEnv && m.envState.status == requestIdle {
		return m.loadEnv()
	}
	if m.activeAppView == appViewDomains && m.domainState.status == requestIdle {
		return m.loadDomains()
	}
	if m.activeAppView == appViewPorts && m.portState.status == requestIdle {
		return m.loadPorts()
	}
	if m.activeAppView == appViewBuilds {
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
	}
	return nil
}
