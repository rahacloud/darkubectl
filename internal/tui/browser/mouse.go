package browser

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	appViewTabAtValue2            = 2
	handleActivityMouseValue2     = 2
	handleActivityMouseValue3     = 3
	handleActivityMouseValue4     = 4
	handleBrowserMouseValue11     = 11
	handleBrowserMouseValue2      = 2
	handleBrowserMouseValue3      = 3
	handleBrowserMouseValue4      = 4
	handleBrowserWheelValue2      = 2
	handleBrowserWheelValue3      = 3
	handleBrowserWheelValue4      = 4
	handleBuildActionMouseValue2  = 2
	ticketMouseHeaderHeight       = 2
	ticketMouseRowOffset          = 2
	ticketMouseTitleRightInset    = 6
	buildActionMenuVerticalOffset = 4
	buildActionMenuContentPadding = 7
	contextPaneHeightDivisor      = 2
)

func (m *browser) handleBrowserMouse(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.help || m.selectingTenant || m.selectingNamespace || m.filterEditing {
		return nil
	}
	if m.page == pageActivity {
		return m.handleActivityMouse(msg)
	}
	if m.page == pageTickets {
		return m.handleTicketMouse(msg)
	}
	if m.page == pageDB {
		return m.handleDatabaseMouse(msg)
	}
	if m.page == pageRegistry {
		return m.handleRegistryMouse(msg)
	}
	if m.detail && m.appViewMaximized() && !m.writeDisabledNotice() {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	appsLeft := contextWidth + handleBrowserMouseValue3
	appsRight := appsLeft + appsWidth + handleBrowserMouseValue2
	if cmd, handled := m.handleAppTreeClick(msg, bodyY, appsLeft, appsRight); handled {
		return cmd
	}
	viewLeft := appsRight + 1
	if msg.X >= viewLeft && msg.X < viewLeft+summaryWidth+2 && msg.Y == bodyY {
		m.detail = true
		return m.selectClickedAppView(msg.X-(viewLeft+1), summaryWidth)
	}
	if m.isBuildListClick(msg, viewLeft, summaryWidth) {
		return m.handleBuildListClick(msg, bodyY)
	}
	if msg.X < contextWidth+2 {
		return m.handleContextClick(msg, bodyY)
	}
	return nil
}

func (m *browser) handleTicketMouse(msg tea.MouseClickMsg) tea.Cmd {
	if m.ticketCompose != "" {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	listWidth, detailWidth, contextWidth := m.paneWidths()
	listLeft := contextWidth + handleBrowserMouseValue3
	listRight := listLeft + listWidth + handleBrowserMouseValue2
	detailLeft := listRight + 1
	if msg.X < contextWidth+2 {
		return m.handleContextPaneMouse(msg, bodyY, false)
	}
	if msg.X >= listLeft && msg.X < listRight {
		return m.handleTicketListMouse(msg, bodyY, listRight, listWidth)
	}
	if msg.X >= detailLeft && msg.X < detailLeft+detailWidth+2 {
		m.contextFocused = false
		m.detail = true
		return nil
	}
	return nil
}

func (m *browser) handleTicketListMouse(msg tea.MouseClickMsg, bodyY, listRight, listWidth int) tea.Cmd {
	if msg.Y == bodyY {
		return m.handleTicketHeaderMouse(msg, listRight, listWidth)
	}
	return m.selectTicketMouseRow(msg, bodyY)
}

func (m *browser) handleTicketHeaderMouse(msg tea.MouseClickMsg, listRight, listWidth int) tea.Cmd {
	m.contextFocused = false
	scope := ticketScopeControl(m.ticketMine)
	scopeStart := listRight - ticketMouseTitleRightInset - ansi.StringWidth(scope) + 1
	if msg.X >= scopeStart && msg.X < scopeStart+ansi.StringWidth(scope) {
		m.ticketMine = !m.ticketMine
		m.ticketCursor = 0
		return m.loadTickets(true)
	}
	statusEnd := scopeStart - ticketControlSeparatorWidth(listWidth, m.ticketStatus, m.ticketMine)
	statusStart := statusEnd - ansi.StringWidth(ticketStatusControl(m.ticketStatus))
	if msg.X >= statusStart && msg.X < statusEnd {
		return m.cycleTicketStatus()
	}
	return nil
}

func (m *browser) selectTicketMouseRow(msg tea.MouseClickMsg, bodyY int) tea.Cmd {
	first, _ := ticketVisibleRange(len(m.visibleTickets()), m.ticketCursor, max(1, m.height-bodyY-browserFooterHeight-ticketMouseHeaderHeight))
	row := msg.Y - bodyY - ticketMouseRowOffset
	if row < 0 {
		return nil
	}
	row += first
	if row < 0 || row >= len(m.visibleTickets()) {
		return nil
	}
	m.contextFocused = false
	m.ticketCursor = row
	m.detail = false
	return m.loadTicketThread()
}

func (m *browser) handleAppTreeClick(msg tea.MouseClickMsg, bodyY, appsLeft, appsRight int) (tea.Cmd, bool) {
	if msg.X < appsLeft || msg.X >= appsRight {
		return nil, false
	}
	nodes := m.treeNodes()
	nodeTop := len(m.appPaneLines(0))
	row := msg.Y - bodyY - 1 - nodeTop
	if row < 0 || row >= len(nodes) {
		m.detail = false
		m.contextFocused = false
		return nil, true
	}
	node := nodes[row]
	m.contextFocused = false
	if node.app < 0 {
		if m.collapsed == nil {
			m.collapsed = make(map[namespaceKey]bool)
		}
		m.collapsed[node.key] = !m.collapsed[node.key]
		return nil, true
	}
	previousTarget := m.selectedDetailTarget()
	m.detail = true
	return m.selectAppIndex(node.app, true, previousTarget), true
}

func (m *browser) isBuildListClick(msg tea.MouseClickMsg, viewLeft, summaryWidth int) bool {
	return m.detail && m.activeAppView == appViewBuilds && m.selectedApp().BuildMethod == buildModeWebhook &&
		m.buildDetail == nil && msg.X >= viewLeft && msg.X < viewLeft+summaryWidth+2
}

func (m *browser) handleBuildListClick(msg tea.MouseClickMsg, bodyY int) tea.Cmd {
	line := msg.Y - bodyY - 1
	prefix := m.buildListPrefixLines()
	if line+m.summaryScroll < prefix {
		m.selectBuildSettingsLine(line)
		return nil
	}
	m.cicdHistoryFocus = true
	row := (line + m.summaryScroll - prefix) / handleBrowserMouseValue2
	if row < 0 || row >= len(m.builds) {
		return nil
	}
	m.buildListCursor = row
	m.buildSelectedID = m.builds[row].ID
	now := time.Now()
	if m.buildLastClickRow == row && now.Sub(m.buildLastClickAt) < 500*time.Millisecond {
		m.buildLastClickAt = time.Time{}
		return m.openSelectedBuild()
	}
	m.buildLastClickRow = row
	m.buildLastClickAt = now
	return nil
}

func (m *browser) selectBuildSettingsLine(line int) {
	notices := 0
	if m.buildActionNotice != "" {
		notices++
	}
	if m.buildNotice != "" {
		notices++
	}
	local := line + m.summaryScroll - notices
	const argumentFirstLine = 12
	removedStart := argumentFirstLine + max(1, len(m.cicdSettingsDraft.Args))
	switch {
	case local >= 1 && local <= cicdSettingsFields:
		m.cicdSettingsCursor = local - 1
		m.cicdHistoryFocus = false
	case local >= argumentFirstLine && local < argumentFirstLine+len(m.cicdSettingsDraft.Args):
		m.cicdSettingsCursor = cicdSettingsFields + local - argumentFirstLine
		m.cicdHistoryFocus = false
	case local == 13 && len(m.cicdSettingsDraft.Args) == 0 || local == 12+len(m.cicdSettingsDraft.Args) && len(m.cicdSettingsDraft.Args) > 0:
		m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args)
		m.cicdHistoryFocus = false
	case m.allowWrites && local > removedStart && local <= removedStart+len(m.cicdRemovedArgs()):
		m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args) + local - removedStart
		m.cicdHistoryFocus = false
	}
}

func (m *browser) handleContextClick(msg tea.MouseClickMsg, bodyY int) tea.Cmd {
	m.contextFocused = true
	bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
	tenantHeight := max(handleBrowserMouseValue4, (bodyHeight-1)/contextPaneHeightDivisor)
	if msg.Y >= bodyY+tenantHeight+1 {
		m.contextRow = 1
		return nil
	}
	m.contextRow = 0
	capacity := max(1, tenantHeight-handleBrowserMouseValue2)
	first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
	index := first + msg.Y - bodyY - 1
	if index >= 0 && index < len(m.tenants) && m.tenants[index].name != m.tenant {
		m.tenantCursor = index
		return m.chooseTenant(m.tenants[index].name)
	}
	return nil
}

func (m *browser) handleActivityMouse(msg tea.MouseClickMsg) tea.Cmd {
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	listWidth, detailWidth, contextWidth := m.paneWidths()
	listLeft := contextWidth + handleActivityMouseValue3
	listRight := listLeft + listWidth + handleActivityMouseValue2
	detailLeft := listRight + 1
	if msg.X < contextWidth+2 {
		return m.handleContextPaneMouse(msg, bodyY, true)
	}
	if msg.X >= listLeft && msg.X < listRight {
		m.contextFocused = false
		row := msg.Y - bodyY - handleActivityMouseValue2
		if m.activityTab == activityNotifications {
			row-- // account-wide banner above the column heading
		}
		stateLines := len(m.activityStateLines(m.activityNotificationsState, "Notifications"))
		if m.activityTab == activityAlerts {
			stateLines = len(m.activityStateLines(m.activityAlertsState, "Alerts"))
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

func (m *browser) handleContextPaneMouse(msg tea.MouseClickMsg, bodyY int, selectTenant bool) tea.Cmd {
	bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
	tenantHeight := max(handleActivityMouseValue4, (bodyHeight-1)/contextPaneHeightDivisor)
	if msg.Y >= bodyY+tenantHeight+1 {
		row := msg.Y - (bodyY + tenantHeight + handleActivityMouseValue2) + 1
		if row < 1 || row > 5 {
			return nil
		}
		m.contextFocused = true
		m.contextRow = row
		m.detail = false
		return m.activatePageRow(row)
	}
	capacity := max(1, tenantHeight-handleActivityMouseValue2)
	first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
	index := first + msg.Y - bodyY - 1
	if index < 0 || index >= len(m.tenants) {
		return nil
	}
	m.contextFocused = true
	m.contextRow = 0
	if selectTenant {
		m.selectingTenant = true
	}
	m.tenantCursor = index
	if m.tenants[index].name == m.tenant {
		return nil
	}
	return m.chooseTenant(m.tenants[index].name)
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
		start := (m.height-(len(items)+buildActionMenuContentPadding))/handleBuildActionMouseValue2 + buildActionMenuVerticalOffset
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
	position := appViewTabAtValue2 + len("[4] ") // border prefix and pane number
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
	if m.actionNotice != "" && !m.writeDisabledNotice() {
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
	delta, ok := browserWheelDelta(msg.Button)
	if !ok {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY || msg.Y >= m.height-browserFooterHeight {
		return nil
	}
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	switch m.page {
	case pageActivity:
		return m.handleActivityWheel(msg, delta, appsWidth, summaryWidth, contextWidth)
	case pageTickets:
		if m.ticketCompose != "" {
			return nil
		}
		listLeft, listRight := browserWheelListBounds(appsWidth, contextWidth)
		if msg.X >= listLeft && msg.X < listRight {
			m.contextFocused = false
			m.detail = false
			_, cmd := m.handleTicketMovement(delta)
			return cmd
		}
		if msg.X >= listRight+1 && msg.X < listRight+1+summaryWidth+2 {
			m.contextFocused = false
			m.detail = true
			m.ticketScroll = max(0, m.ticketScroll+delta*3)
		}
		return nil
	case pageDB:
		return m.handleDatabaseWheel(msg, delta, bodyY, appsWidth, contextWidth)
	case pageRegistry:
		return m.handleRegistryWheel(msg, delta, appsWidth, contextWidth)
	default:
		return m.handleAppsWheel(msg, delta, bodyY, appsWidth, summaryWidth, contextWidth)
	}
}

func browserWheelDelta(button tea.MouseButton) (int, bool) {
	switch button {
	case tea.MouseWheelUp:
		return -1, true
	case tea.MouseWheelDown:
		return 1, true
	default:
		return 0, false
	}
}

func (m *browser) handleActivityWheel(msg tea.MouseWheelMsg, delta, appsWidth, summaryWidth, contextWidth int) tea.Cmd {
	listLeft, listRight := browserWheelListBounds(appsWidth, contextWidth)
	if msg.X >= listLeft && msg.X < listRight {
		m.moveActivitySelection(delta)
		return nil
	}
	if msg.X >= listRight+1 && msg.X < listRight+1+summaryWidth+2 {
		m.activityDetailScroll = max(0, m.activityDetailScroll-delta*3)
	}
	return nil
}

func (m *browser) handleDatabaseWheel(msg tea.MouseWheelMsg, delta, bodyY, appsWidth, contextWidth int) tea.Cmd {
	if m.width < 100 && m.detail {
		m.databaseScroll = max(0, m.databaseScroll-delta*3)
		return nil
	}
	listLeft, listRight := browserWheelListBounds(appsWidth, contextWidth)
	if msg.X >= listLeft && msg.X < listRight {
		rows := m.visibleDatabases()
		row := (msg.Y - bodyY - 1) / handleBrowserWheelValue2
		if row >= 0 && row < len(rows) {
			m.databaseCursor = row
			return m.updateDatabaseSelection()
		}
	} else if msg.X >= listRight+1 {
		m.databaseScroll = max(0, m.databaseScroll-delta*3)
	}
	return nil
}

func (m *browser) handleRegistryWheel(msg tea.MouseWheelMsg, delta, appsWidth, contextWidth int) tea.Cmd {
	listLeft, listRight := browserWheelListBounds(appsWidth, contextWidth)
	if msg.X >= listLeft && msg.X < listRight {
		m.moveRegistrySelection(delta * handleBrowserWheelValue3)
	}
	return nil
}

func browserWheelListBounds(appsWidth, contextWidth int) (int, int) {
	left := contextWidth + handleBrowserWheelValue3
	return left, left + appsWidth + handleBrowserWheelValue2
}

func (m *browser) handleAppsWheel(msg tea.MouseWheelMsg, delta, bodyY, appsWidth, summaryWidth, contextWidth int) tea.Cmd {
	appsLeft := contextWidth + handleBrowserWheelValue3
	appsRight := appsLeft + appsWidth + handleBrowserWheelValue2
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
		switch m.activeAppView {
		case appViewBuilds:
			if m.buildDetail != nil {
				m.buildScroll = max(0, m.buildScroll+delta*3)
			} else {
				m.summaryScroll = max(0, m.summaryScroll+delta*3)
			}
		case appViewLogs:
			m.logScroll = max(0, m.logScroll-delta*3)
		case appViewUsage:
			m.usageScroll = max(0, m.usageScroll-delta*3)
		default:
			m.summaryScroll = max(0, m.summaryScroll-delta*3)
		}
	}
	if msg.X < contextWidth+2 {
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(handleBrowserWheelValue4, (bodyHeight-1)/contextPaneHeightDivisor)
		m.contextFocused = true
		m.contextRow = 0
		if msg.Y >= bodyY+tenantHeight+1 {
			m.contextRow = 1
		}
	}
	return nil
}
