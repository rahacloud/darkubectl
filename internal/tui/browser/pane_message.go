package browser

import "strings"

func (m *browser) currentAppPaneMessage() string {
	if m.actionNotice != "" {
		return m.actionNotice
	}
	if m.page != pageApps || !m.detail || !m.selectedAppActive() {
		return ""
	}
	switch m.activeAppView {
	case appViewLogs:
		return firstMessage(m.logCopyStatus, m.logNotice)
	case appViewBuilds:
		return firstMessage(m.buildCopyStatus, m.buildActionNotice, m.buildNotice, m.cicdNotice, m.cicdSettingsNotice)
	case appViewEnv:
		return m.envMessage
	case appViewDomains:
		return m.domainMessage
	case appViewPorts:
		return m.portMessage
	default:
		return ""
	}
}

func (m *browser) clearCurrentAppPaneMessage(message string) {
	if m.actionNotice == message {
		m.actionNotice = ""
		return
	}
	switch m.activeAppView {
	case appViewLogs:
		if m.logCopyStatus == message {
			m.logCopyStatus = ""
		}
	case appViewBuilds:
		if m.buildCopyStatus == message {
			m.buildCopyStatus = ""
		}
		if m.cicdNotice == message {
			m.cicdNotice = ""
		}
	case appViewEnv:
		if m.envMessage == message {
			m.envMessage = ""
		}
	case appViewDomains:
		if m.domainMessage == message {
			m.domainMessage = ""
		}
	case appViewPorts:
		if m.portMessage == message {
			m.portMessage = ""
		}
	case appViewSummary, appViewUsage, appViewScaling:
		return
	}
}

func firstMessage(messages ...string) string {
	for _, message := range messages {
		if message != "" {
			return message
		}
	}
	return ""
}

func isShortPaneMessage(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "copied") || strings.HasPrefix(lower, "match ") ||
		lower == "no matches" || lower == "secret visible" || lower == "staged secret visible"
}
