package browser

import (
	tea "charm.land/bubbletea/v2"
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
	case appActionResult:
		return m, m.applyActionResult(msg)
	case tea.MouseClickMsg:
		if m.actionMenu || m.confirmAction != appActionNone {
			return m, m.handleActionMouse(msg)
		}
		return m, m.handleBrowserMouse(msg)
	case tea.MouseWheelMsg:
		return m, m.handleBrowserWheel(msg)
	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	}
	return m, nil
}
