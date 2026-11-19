package browser

import (
	tea "charm.land/bubbletea/v2"
	"time"
)

func (m *browser) handleBrowserMouse(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.help || m.selectingTenant || m.selectingNamespace || m.filterEditing {
		return nil
	}
	if m.page == pageActivity {
		return m.handleActivityMouse(msg)
	}
	if m.page == pageDB {
		return m.handleDatabaseMouse(msg)
	}
	if m.page == pageRegistry {
		return m.handleRegistryMouse(msg)
	}
	if m.detail && m.appViewMaximized() {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	appsLeft := contextWidth + 3
	appsRight := appsLeft + appsWidth + 2
	if msg.X >= appsLeft && msg.X < appsRight {
		nodes := m.treeNodes()
		nodeTop := len(m.appPaneLines(0))
		row := msg.Y - bodyY - 1 - nodeTop
		if row < 0 || row >= len(nodes) {
			m.detail = false
			m.contextFocused = false
			return nil
		}
		node := nodes[row]
		m.contextFocused = false
		if node.app < 0 {
			if m.collapsed == nil {
				m.collapsed = make(map[namespaceKey]bool)
			}
			m.collapsed[node.key] = !m.collapsed[node.key]
			return nil
		}
		previousTarget := m.selectedDetailTarget()
		m.detail = true
		return m.selectAppIndex(node.app, true, previousTarget)
	}
	viewLeft := appsRight + 1
	if msg.X >= viewLeft && msg.X < viewLeft+summaryWidth+2 && msg.Y == bodyY {
		m.detail = true
		return m.selectClickedAppView(msg.X-(viewLeft+1), summaryWidth)
	}
	if m.detail && m.activeAppView == appViewBuilds && m.selectedApp().BuildMethod == "webhook" && m.buildDetail == nil && msg.X >= viewLeft && msg.X < viewLeft+summaryWidth+2 && msg.Y > bodyY {
		line := msg.Y - bodyY - 1
		prefix := m.buildListPrefixLines()
		if line+m.summaryScroll < prefix {
			notices := 0
			if m.buildActionNotice != "" {
				notices++
			}
			if m.buildNotice != "" {
				notices++
			}
			local := line + m.summaryScroll - notices
			if local >= 1 && local <= cicdSettingsFields {
				m.cicdSettingsCursor = local - 1
				m.cicdHistoryFocus = false
			} else if local >= 11 && local < 11+len(m.cicdSettingsDraft.Args) {
				m.cicdSettingsCursor = cicdSettingsFields + local - 11
				m.cicdHistoryFocus = false
			} else if local == 12 && len(m.cicdSettingsDraft.Args) == 0 || local == 11+len(m.cicdSettingsDraft.Args) && len(m.cicdSettingsDraft.Args) > 0 {
				m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args)
				m.cicdHistoryFocus = false
			}
			return nil
		}
		m.cicdHistoryFocus = true
		row := (line + m.summaryScroll - prefix) / 2
		if row >= 0 && row < len(m.builds) {
			m.buildListCursor = row
			m.buildSelectedID = m.builds[row].ID
			now := time.Now()
			if m.buildLastClickRow == row && now.Sub(m.buildLastClickAt) < 500*time.Millisecond {
				m.buildLastClickAt = time.Time{}
				return m.openSelectedBuild()
			}
			m.buildLastClickRow = row
			m.buildLastClickAt = now
		}
		return nil
	}
	if msg.X < contextWidth+2 {
		m.contextFocused = true
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(4, (bodyHeight-1)/2)
		if msg.Y >= bodyY+tenantHeight+1 {
			m.contextRow = 1
			return nil
		}
		m.contextRow = 0
		capacity := max(1, tenantHeight-2)
		first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
		index := first + msg.Y - bodyY - 1
		if index >= 0 && index < len(m.tenants) && m.tenants[index].name != m.tenant {
			m.tenantCursor = index
			return m.chooseTenant(m.tenants[index].name)
		}
	}
	return nil
}

func (m *browser) handleActivityMouse(msg tea.MouseClickMsg) tea.Cmd {
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	listWidth, detailWidth, contextWidth := m.paneWidths()
	listLeft := contextWidth + 3
	listRight := listLeft + listWidth + 2
	detailLeft := listRight + 1
	if msg.X < contextWidth+2 {
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(4, (bodyHeight-1)/2)
		if msg.Y >= bodyY+tenantHeight+1 {
			row := msg.Y - (bodyY + tenantHeight + 2) + 1
			if row >= 1 && row <= 6 {
				m.contextFocused = true
				m.contextRow = row
				m.detail = false
				return m.activatePageRow(row)
			}
			return nil
		}
		capacity := max(1, tenantHeight-2)
		first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
		index := first + msg.Y - bodyY - 1
		if index >= 0 && index < len(m.tenants) {
			m.contextFocused = true
			m.contextRow = 0
			m.selectingTenant = true
			m.tenantCursor = index
			if m.tenants[index].name != m.tenant {
				return m.chooseTenant(m.tenants[index].name)
			}
		}
		return nil
	}
	if msg.X >= listLeft && msg.X < listRight {
		m.contextFocused = false
		row := msg.Y - bodyY - 2
		if m.activityTab == activityNotifications {
			row-- // account-wide banner above the column heading
		}
		stateLines := 0
		if m.activityTab == activityAlerts {
			stateLines = len(m.activityStateLines(m.activityAlertsState, "Alerts"))
		} else {
			stateLines = len(m.activityStateLines(m.activityNotificationsState, "Notifications"))
		}
		row -= stateLines
		row += m.activityScroll
		if row >= 0 {
			m.moveActivitySelection(row - m.activityCursor())
			m.detail = false
		}
		return nil
	}
	if msg.X >= detailLeft && msg.X < detailLeft+detailWidth+2 {
		m.contextFocused = false
		m.detail = true
	}
	return nil
}

func (m *browser) activityCursor() int {
	if m.activityTab == activityAlerts {
		return m.activityAlertCursor
	}
	return m.activityNotificationCursor
}

func (m *browser) selectClickedAppView(x, width int) tea.Cmd {
	if !m.selectedAppActive() {
		return nil
	}
	view, ok := appViewTabAt(x, width, m.activeAppView)
	if !ok || view == m.activeAppView {
		return nil
	}
	views := visibleAppViews()
	from, to := 0, 0
	for i, v := range views {
		if v == m.activeAppView {
			from = i
		}
		if v == view {
			to = i
		}
	}
	return m.switchAppView(to - from)
}

func (m *browser) handleBuildActionMouse(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if m.buildActionMenu {
		items := m.buildActionItems()
		start := (m.height-(len(items)+7))/2 + 4
		index := msg.Y - start
		if index >= 0 && index < len(items) {
			m.buildActionCursor = index
			m.beginBuildActionConfirmation(items[index])
		}
		return nil
	}
	if m.buildConfirm != buildActionNone {
		if msg.Y >= m.height/2+2 {
			if msg.X < m.width/2 {
				m.buildConfirmButton = 0
				m.buildConfirm = buildActionNone
			} else {
				m.buildConfirmButton = 1
				return m.submitBuildAction()
			}
		}
	}
	return nil
}

func appViewTabAt(x, width int, active appView) (appView, bool) {
	labels := appViewTabLabels(width)
	views := visibleAppViews()
	position := 2 + len("[4] ") // border prefix and pane number
	for index, label := range labels {
		start := position
		span := len(label)
		if views[index] == active {
			span += 2 // selected tab includes its brackets
		}
		if x >= start && x < start+span {
			return views[index], true
		}
		position += span + 1 // one separating space
	}
	return 0, false
}

func (m *browser) browserBodyY() int {
	y := 2 // breadcrumb and spacer
	if m.actionNotice != "" {
		y++
	}
	if m.profileState.status == requestFailed || m.profileState.status == requestStale {
		y++
		if m.profileState.status == requestStale {
			y++
		}
	}
	return y
}

func (m *browser) handleBrowserWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.help || m.selectingTenant || m.selectingNamespace || m.filterEditing {
		return nil
	}
	delta := 1
	if msg.Button == tea.MouseWheelUp {
		delta = -1
	} else if msg.Button != tea.MouseWheelDown {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY || msg.Y >= m.height-browserFooterHeight {
		return nil
	}
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	if m.page == pageActivity {
		listLeft := contextWidth + 3
		listRight := listLeft + appsWidth + 2
		if msg.X >= listLeft && msg.X < listRight {
			m.moveActivitySelection(delta)
			return nil
		}
		if msg.X >= listRight+1 && msg.X < listRight+1+summaryWidth+2 {
			m.activityDetailScroll = max(0, m.activityDetailScroll-delta*3)
		}
		return nil
	}
	if m.page == pageDB {
		if m.width < 100 && m.detail {
			m.databaseScroll = max(0, m.databaseScroll-delta*3)
			return nil
		}
		listLeft := contextWidth + 3
		listRight := listLeft + appsWidth + 2
		if msg.X >= listLeft && msg.X < listRight {
			rows := m.visibleDatabases()
			row := (msg.Y - bodyY - 1) / 2
			if row >= 0 && row < len(rows) {
				m.databaseCursor = row
				return m.updateDatabaseSelection()
			}
		} else if msg.X >= listRight+1 {
			m.databaseScroll = max(0, m.databaseScroll-delta*3)
		}
		return nil
	}
	if m.page == pageRegistry {
		listLeft := contextWidth + 3
		listRight := listLeft + appsWidth + 2
		if msg.X >= listLeft && msg.X < listRight {
			m.moveRegistrySelection(delta * 3)
		}
		return nil
	}
	appsLeft := contextWidth + 3
	appsRight := appsLeft + appsWidth + 2
	if msg.X >= appsLeft && msg.X < appsRight {
		visible := m.visibleAppIndices()
		if len(visible) == 0 {
			return nil
		}
		position := 0
		for i, index := range visible {
			if index == m.selected {
				position = i
				break
			}
		}
		position = min(len(visible)-1, max(0, position+delta))
		return m.selectAppIndex(visible[position], false, m.selectedDetailTarget())
	}
	if msg.X >= appsRight+1 && msg.X < appsRight+1+summaryWidth+2 {
		if m.activeAppView == appViewLogs {
			m.logScroll = max(0, m.logScroll-delta*3)
		} else if m.activeAppView == appViewUsage {
			m.usageScroll = max(0, m.usageScroll-delta*3)
		} else {
			m.summaryScroll = max(0, m.summaryScroll-delta*3)
		}
	}
	if msg.X < contextWidth+2 {
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(4, (bodyHeight-1)/2)
		m.contextFocused = true
		m.contextRow = 0
		if msg.Y >= bodyY+tenantHeight+1 {
			m.contextRow = 1
		}
	}
	return nil
}
