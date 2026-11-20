package browser

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	animationCmdValue120      = 120
	paneMessageTimeoutSeconds = 3
)

func (m *browser) Init() tea.Cmd {
	profileCmd := m.loadProfile()
	if m.mode != connectionLabelLive {
		return m.load()
	}
	if m.tenant == "" && m.mode == connectionLabelLive {
		m.contextFocused = true
		return tea.Batch(profileCmd, animationCmd())
	}
	if m.mode == "LIVE" {
		return tea.Batch(profileCmd, m.load(), animationCmd())
	}
	return m.load()
}

func animationCmd() tea.Cmd {
	return tea.Tick(animationCmdValue120*time.Millisecond, func(time.Time) tea.Msg { return spinnerMsg{} })
}

//nolint:nonamedreturns // The deferred pane-message update must adjust both returned values.
func (m *browser) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	previousPaneMessage := m.currentAppPaneMessage()
	defer func() {
		current := m.currentAppPaneMessage()
		if current == previousPaneMessage {
			return
		}
		m.paneMessage = current
		m.paneMessageID++
		model = m
		if current != "" && isShortPaneMessage(current) {
			id := m.paneMessageID
			cmd = tea.Batch(cmd, tea.Tick(paneMessageTimeoutSeconds*time.Second, func(time.Time) tea.Msg { return paneMessageTimeout{id: id} }))
		}
	}()
	if model, command, handled := m.handleDataResult(msg); handled {
		return model, command
	}
	if model, command, handled := m.handleConfigResult(msg); handled {
		return model, command
	}
	if model, command, handled := m.handlePeripheralResult(msg); handled {
		return model, command
	}
	return m.handleUpdateInput(msg)
}

func (m *browser) handleUpdateInput(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case paneMessageTimeout:
		if msg.id == m.paneMessageID {
			m.clearCurrentAppPaneMessage(m.paneMessage)
			m.paneMessage = ""
		}
		return m, nil
	case spinnerMsg:
		if m.mode == "LIVE" {
			m.spinnerFrame++
			return m, animationCmd()
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		m.handlePaste(msg.Content)
		return m, nil
	}
	return m, nil
}

func (m *browser) handlePaste(content string) {
	if m.detail && m.activeAppView == appViewUsage && m.resourceEditing {
		if resourceDigits(content) {
			m.resourceEditValue += content
		}
		return
	}
	if m.detail && m.activeAppView == appViewEnv && m.envEditMode != envEditNone {
		m.envEditValue += content
	}
}

func (m *browser) handleDataResult(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if model, command, handled := m.handleTicketResult(msg); handled {
		return model, command, true
	}
	switch msg := msg.(type) {
	case appsResult:
		return asHandledUpdateResult(m.handleAppsResult(msg))
	case registrySnapshot:
		return asHandledModel(m.handleRegistrySnapshot(msg))
	case databasesResult:
		return asHandledUpdateResult(m.handleDatabasesResult(msg))
	case databaseDetailResult:
		return asHandledModel(m.handleDatabaseDetailResult(msg))
	case databaseCopyResult:
		selected := m.selectedDatabase()
		if msg.tenant == m.tenant && selected != nil && msg.databaseID == selected.ID {
			if msg.err != nil {
				m.databaseNotice = messageClipboardUnavailable
			} else {
				m.databaseNotice = "URI copied · replace REPLACE_PASSWORD with the Console password"
			}
		}
		return m, nil, true
	case profileResult:
		return asHandledModel(m.handleProfileResult(msg))
	case appDetailResult:
		return asHandledUpdateResult(m.handleAppDetailResult(msg))
	}
	return m.handleRemainingDataResult(msg)
}

func (m *browser) handleRemainingDataResult(message tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case registryDigestsResult:
		return asHandledModel(m.handleRegistryDigests(msg))
	case registryDeleteResult:
		return m, m.handleRegistryDeleteResult(msg), true
	case registryCopyResult:
		m.handleRegistryCopyResult(msg)
		return m, nil, true
	case appUsageResult:
		return asHandledModel(m.handleAppUsageResult(msg))
	case usageHistoryResult:
		return asHandledModel(m.handleUsageHistoryResult(msg))
	case podsResult:
		return asHandledUpdateResult(m.handlePodsResult(msg))
	case logsResult:
		return asHandledUpdateResult(m.handleLogsResult(msg))
	case buildsResult:
		return asHandledUpdateResult(m.handleBuildsResult(msg))
	case buildResult:
		return asHandledModel(m.handleBuildResult(msg))
	case buildCopyResult:
		if msg.err != nil {
			m.buildCopyStatus = messageClipboardUnavailable
		} else {
			m.buildCopyStatus = fmt.Sprintf("Copied %d bytes", msg.bytes)
		}
		return m, nil, true
	case cicdTokenResult:
		return m.handleCICDTokenResult(msg)
	case cicdCopyResult:
		if msg.tenant == m.tenant && msg.appID == m.selectedDetailTarget() {
			if msg.err != nil {
				m.cicdNotice = "Clipboard unavailable"
			} else {
				m.cicdNotice = "Copied to clipboard"
			}
		}
		return m, nil, true
	}
	return nil, nil, false
}

func (m *browser) handleCICDTokenResult(msg cicdTokenResult) (tea.Model, tea.Cmd, bool) {
	if msg.tenant != m.tenant || msg.appID != m.selectedDetailTarget() || msg.generation != m.cicdGeneration ||
		m.activeAppView != appViewBuilds {
		return m, nil, true
	}
	m.cicdLoading = false
	if msg.err != nil {
		m.cicdNotice = "Could not load deploy token."
		return m, nil, true
	}
	if msg.token == "" {
		m.cicdNotice = "This app has no deploy token."
		return m, nil, true
	}
	m.cicdToken, m.cicdNotice = msg.token, ""
	if msg.copy {
		return m, m.copyCICDValue(msg.token), true
	}
	m.cicdTokenReveal = msg.reveal
	return m, nil, true
}

func (m *browser) handleConfigResult(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case cicdSettingsWriteResult:
		return m, m.handleCICDSettingsWrite(msg), true
	case envResult:
		return asHandledModel(m.handleEnvResult(msg))
	case envWriteResult:
		return asHandledUpdateResult(m.handleEnvWrite(msg))
	case envCopyResult:
		return asHandledModel(m.handleEnvCopy(msg))
	case envRevealResult:
		return asHandledModel(m.handleEnvReveal(msg))
	case domainsResult:
		return asHandledModel(m.handleDomainsResult(msg))
	case portsResult:
		return asHandledModel(m.handlePortsResult(msg))
	case portsWriteResult:
		return m, m.handlePortsWrite(msg), true
	case resourceWriteResult:
		return m, m.handleResourcesWrite(msg), true
	case resourcePlansResult:
		m.handleResourcePlans(msg)
		if len(m.resourcePlans) > 0 {
			m.resourcePlanCursor = findResourcePlan(m.resourcePlans, m.resourceDraft.Plan)
			m.resourcePlanPicker = true
		}
		return m, nil, true
	case clipboardResult:
		if msg.err != nil {
			m.portMessage = "Clipboard unavailable"
		} else {
			m.portMessage = "Copied to clipboard"
		}
		return m, nil, true
	case domainsWriteResult:
		return m, m.handleDomainsWrite(msg), true
	case domainCopyResult:
		if msg.tenant == m.tenant && msg.appID == m.selectedDetailTarget() {
			if msg.err != nil {
				m.domainMessage = "Copy failed · check clipboard access"
			} else {
				m.domainMessage = "Record " + msg.field + " copied to clipboard"
			}
		}
		return m, nil, true
	}
	return nil, nil, false
}

func findResourcePlan(plans []client.Plan, id string) int {
	for i, plan := range plans {
		if plan.ID == id {
			return i
		}
	}
	return 0
}

func (m *browser) handlePeripheralResult(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case terminalFinishedMsg:
		return asHandledModel(m.handleTerminalFinished(msg))
	case logsPollMsg:
		if msg.appID == m.selectedDetailTarget() && msg.pod == m.logPod && msg.container == m.logContainer &&
			msg.previous == m.logPrevious && m.logFollowing {
			return m, m.loadSelectedLogs(), true
		}
		return m, nil, true
	case logCopyResult:
		return asHandledModel(m.handleLogsCopied(msg))
	case egressRoutesResult:
		return asHandledModel(m.handleEgressRoutesResult(msg))
	case namespacesResult:
		return asHandledModel(m.handleNamespacesResult(msg))
	case alertsResult:
		return asHandledModel(m.handleAlertsResult(msg))
	case notificationsResult:
		return asHandledModel(m.handleNotificationsResult(msg))
	case appActionResult:
		return m, m.applyActionResult(msg), true
	case buildActionResult:
		return asHandledUpdateResult(m.handleBuildActionResult(msg))
	}
	return nil, nil, false
}

func asHandledUpdateResult(model tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd, bool) {
	return model, cmd, true
}

func asHandledModel(model tea.Model) (tea.Model, tea.Cmd, bool) {
	return model, nil, true
}
