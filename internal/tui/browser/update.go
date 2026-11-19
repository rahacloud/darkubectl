package browser

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"time"
)

func (m *browser) Init() tea.Cmd {
	profileCmd := m.loadProfile()
	if m.mode != "LIVE" {
		return m.load()
	}
	if m.tenant == "" && m.mode == "LIVE" {
		m.contextFocused = true
		return tea.Batch(profileCmd, animationCmd())
	}
	if m.mode == "LIVE" {
		return tea.Batch(profileCmd, m.load(), animationCmd())
	}
	return m.load()
}

func animationCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerMsg{} })
}

func (m *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerMsg:
		if m.mode == "LIVE" {
			m.spinnerFrame++
			return m, animationCmd()
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case appsResult:
		return m.handleAppsResult(msg)
	case registrySnapshot:
		return m.handleRegistrySnapshot(msg)
	case databasesResult:
		return m.handleDatabasesResult(msg)
	case databaseDetailResult:
		return m.handleDatabaseDetailResult(msg)
	case databaseCopyResult:
		selected := m.selectedDatabase()
		if msg.tenant == m.tenant && selected != nil && msg.databaseID == selected.ID {
			if msg.err != nil {
				m.databaseNotice = "Clipboard unavailable"
			} else {
				m.databaseNotice = "URI copied · replace REPLACE_PASSWORD with the Console password"
			}
		}
		return m, nil
	case registryDigestsResult:
		return m.handleRegistryDigests(msg)
	case registryDeleteResult:
		return m, m.handleRegistryDeleteResult(msg)
	case registryCopyResult:
		m.handleRegistryCopyResult(msg)
		return m, nil
	case profileResult:
		return m.handleProfileResult(msg)
	case appDetailResult:
		return m.handleAppDetailResult(msg)
	case appUsageResult:
		return m.handleAppUsageResult(msg)
	case usageHistoryResult:
		return m.handleUsageHistoryResult(msg)
	case podsResult:
		return m.handlePodsResult(msg)
	case logsResult:
		return m.handleLogsResult(msg)
	case buildsResult:
		return m.handleBuildsResult(msg)
	case buildResult:
		return m.handleBuildResult(msg)
	case buildCopyResult:
		if msg.err != nil {
			m.buildCopyStatus = "Clipboard unavailable"
		} else {
			m.buildCopyStatus = fmt.Sprintf("Copied %d bytes", msg.bytes)
		}
		return m, nil
	case cicdTokenResult:
		if msg.tenant != m.tenant || msg.appID != m.selectedDetailTarget() || msg.generation != m.cicdGeneration || m.activeAppView != appViewBuilds {
			return m, nil
		}
		m.cicdLoading = false
		if msg.err != nil {
			m.cicdNotice = "Could not load deploy token."
			return m, nil
		}
		if msg.token == "" {
			m.cicdNotice = "This app has no deploy token."
			return m, nil
		}
		m.cicdToken, m.cicdNotice = msg.token, ""
		if msg.copy {
			return m, m.copyCICDValue(msg.token)
		}
		m.cicdTokenReveal = msg.reveal
		return m, nil
	case cicdCopyResult:
		if msg.tenant == m.tenant && msg.appID == m.selectedDetailTarget() {
			if msg.err != nil {
				m.cicdNotice = "Clipboard unavailable"
			} else {
				m.cicdNotice = "Copied to clipboard"
			}
		}
		return m, nil
	case cicdSettingsWriteResult:
		return m, m.handleCICDSettingsWrite(msg)
	case envResult:
		return m.handleEnvResult(msg)
	case envWriteResult:
		return m.handleEnvWrite(msg)
	case envCopyResult:
		return m.handleEnvCopy(msg)
	case envRevealResult:
		return m.handleEnvReveal(msg)
	case domainsResult:
		return m.handleDomainsResult(msg)
	case portsResult:
		return m.handlePortsResult(msg)
	case portsWriteResult:
		return m, m.handlePortsWrite(msg)
	case resourceWriteResult:
		return m, m.handleResourcesWrite(msg)
	case resourcePlansResult:
		m.handleResourcePlans(msg)
		if len(m.resourcePlans) > 0 {
			m.resourcePlanCursor = 0
			for i, p := range m.resourcePlans {
				if p.ID == m.resourceDraft.Plan {
					m.resourcePlanCursor = i
					break
				}
			}
			m.resourcePlanPicker = true
		}
		return m, nil
	case clipboardResult:
		if msg.err != nil {
			m.portMessage = "Clipboard unavailable"
		} else {
			m.portMessage = "Copied to clipboard"
		}
		return m, nil
	case domainsWriteResult:
		return m, m.handleDomainsWrite(msg)
	case domainCopyResult:
		if msg.tenant == m.tenant && msg.appID == m.selectedDetailTarget() {
			if msg.err != nil {
				m.domainMessage = "Copy failed · check clipboard access"
			} else {
				m.domainMessage = "Record " + msg.field + " copied to clipboard"
			}
		}
		return m, nil
	case terminalFinishedMsg:
		return m.handleTerminalFinished(msg)
	case logsPollMsg:
		if msg.appID == m.selectedDetailTarget() && msg.pod == m.logPod &&
			msg.container == m.logContainer && msg.previous == m.logPrevious && m.logFollowing {
			return m, m.loadSelectedLogs()
		}
	case logCopyResult:
		return m.handleLogsCopied(msg)
	case egressRoutesResult:
		return m.handleEgressRoutesResult(msg)
	case namespacesResult:
		return m.handleNamespacesResult(msg)
	case alertsResult:
		return m.handleAlertsResult(msg)
	case notificationsResult:
		return m.handleNotificationsResult(msg)
	case appActionResult:
		return m, m.applyActionResult(msg)
	case buildActionResult:
		return m.handleBuildActionResult(msg)
	case tea.MouseClickMsg:
		if m.buildActionMenu || m.buildConfirm != buildActionNone {
			return m, m.handleBuildActionMouse(msg)
		}
		if m.actionMenu || m.confirmAction != appActionNone {
			return m, m.handleActionMouse(msg)
		}
		return m, m.handleBrowserMouse(msg)
	case tea.MouseWheelMsg:
		return m, m.handleBrowserWheel(msg)
	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	case tea.PasteMsg:
		if m.detail && m.activeAppView == appViewUsage && m.resourceEditing {
			if resourceDigits(msg.Content) {
				m.resourceEditValue += msg.Content
			}
			return m, nil
		}
		if m.detail && m.activeAppView == appViewEnv && m.envEditMode != envEditNone {
			m.envEditValue += msg.Content
			return m, nil
		}
	}
	return m, nil
}
