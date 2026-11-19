package browser

import tea "charm.land/bubbletea/v2"

func (m *browser) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.actionBusy {
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.detail && m.activeAppView == appViewBuilds && (m.cicdEditing || m.cicdReview) {
		cmd, _ := m.handleCICDSettingsKey(msg)
		return m, cmd
	}
	if m.actionMenu {
		return m, m.handleActionMenuKey(msg)
	}
	if m.buildActionMenu {
		return m, m.handleBuildActionMenuKey(msg)
	}
	if m.buildConfirm != buildActionNone {
		return m, m.handleBuildConfirmKey(msg)
	}
	if m.logTargetPicker {
		if m.terminalChoosing {
			return m, m.handleTerminalPickerKey(msg)
		}
		return m, m.handleLogTargetKey(msg)
	}
	if m.logSearchEditing {
		m.handleLogSearchKey(msg)
		return m, nil
	}
	if m.buildSearchEditing {
		m.handleBuildSearchKey(msg)
		return m, nil
	}
	if m.confirmAction != appActionNone {
		return m, m.handleActionConfirmationKey(msg)
	}
	if m.selectingNamespace {
		return m.handleNamespaceKey(msg)
	}
	if m.filterEditing {
		return m.handleFilterKey(msg)
	}
	if m.activityFilterEditing {
		m.handleActivityFilterKey(msg)
		return m, nil
	}
	if m.databaseFilterEditing {
		return m.handleDatabaseKey(msg)
	}
	if m.sorting {
		return m.handleSortKey(msg)
	}
	if m.help {
		return m.handleHelpKey(msg)
	}
	if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && (m.selectedApp().BuildMethod == "webhook" || m.selectedApp().BuildMethod == "gitlabci") {
		if cmd, handled := m.handleCICDSettingsKey(msg); handled {
			return m, cmd
		}
	}
	if m.page == pageActivity && !m.contextFocused && !m.selectingTenant {
		return m.handleActivityKey(msg)
	}
	if m.page == pageDB && !m.contextFocused && !m.selectingTenant {
		if m.sorting {
			return m.handleDatabaseKey(msg)
		}
		switch msg.String() {
		case "1":
			return m, m.focusPane(0)
		case "2":
			return m, m.focusPane(1)
		case "3":
			return m, m.focusPane(2)
		case "4":
			return m, m.focusPane(3)
		case "tab":
			return m, m.cycleFocus(false)
		case "shift+tab", "backtab":
			return m, m.cycleFocus(true)
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.help = true
			return m, nil
		}
		return m.handleDatabaseKey(msg)
	}
	if m.page == pageRegistry && !m.contextFocused && !m.selectingTenant {
		return m, m.handleRegistryKey(msg.String())
	}
	if m.detail && m.activeAppView == appViewEnv && m.selectedAppActive() {
		if m.envEditMode != envEditNone || m.envConfirm {
			return m, m.handleEnvKey(msg)
		}
		switch msg.String() {
		case "[", "]", "ctrl+c", "q", "?", "1", "2", "3", "4", "shift+tab", "backtab", "a", "pgup", "pgdown":
		default:
			return m, m.handleEnvKey(msg)
		}
	}
	if m.detail && m.activeAppView == appViewDomains && m.selectedAppActive() {
		if m.domainEditing || m.domainConfirm {
			return m, m.handleDomainsKey(msg)
		}
		switch msg.String() {
		case "[", "]", "ctrl+c", "q", "?", "1", "2", "3", "4", "shift+tab", "backtab", "a":
		default:
			return m, m.handleDomainsKey(msg)
		}
	}
	if m.detail && m.activeAppView == appViewPorts && m.selectedAppActive() {
		if m.portEditing || m.portConfirm {
			return m, m.handlePortsKey(msg)
		}
		switch msg.String() {
		case "[", "]", "ctrl+c", "q", "?", "1", "2", "3", "4", "shift+tab", "backtab", "a":
		default:
			return m, m.handlePortsKey(msg)
		}
	}
	if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
		if m.resourceEditing || m.resourceReview {
			return m, m.handleResourcesKey(msg)
		}
		switch msg.String() {
		case "[", "]", "ctrl+c", "q", "?", "1", "2", "3", "4", "shift+tab", "backtab", "a":
		default:
			return m, m.handleResourcesKey(msg)
		}
	}

	switch msg.String() {
	case "1":
		if !m.help && !m.sorting {
			return m, m.focusPane(0)
		}
	case "2":
		if !m.help && !m.sorting {
			return m, m.focusPane(1)
		}
	case "3":
		if !m.help && !m.sorting {
			return m, m.focusPane(2)
		}
	case "4":
		if !m.help && !m.sorting {
			return m, m.focusPane(3)
		}
	case "tab":
		return m, m.cycleFocus(false)
	case "shift+tab", "backtab":
		return m, m.cycleFocus(true)
	case "ctrl+c", "q":
		return m, tea.Quit
	case "a":
		if m.page == pageActivity {
			return m, nil
		}
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && m.selectedApp().BuildMethod == "webhook" {
			m.openBuildActionMenu()
			return m, nil
		}
		m.openActionMenu()
	case "[":
		return m, m.switchAppView(-1)
	case "]":
		return m, m.switchAppView(1)
	case "w":
		if m.detail && m.activeAppView == appViewLogs {
			m.logWrap = !m.logWrap
			return m, nil
		}
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			m.historyRange = (m.historyRange + 1) % len(usageRanges)
			m.cancelRequest(requestKey{kind: requestUsageHistory})
			m.historyState.reset()
			m.history = nil
			return m, m.loadUsageHistory()
		}
	case "+":
		if m.detail && m.activeAppView == appViewLogs {
			return m, m.adjustLogTail(logTailStep)
		}
	case "-":
		if m.detail && m.activeAppView == appViewLogs {
			return m, m.adjustLogTail(-logTailStep)
		}
	case "c":
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && (m.selectedApp().BuildMethod == "gitlabci" || m.selectedApp().BuildMethod == "webhook") {
			return m, m.requestCICDToken(true)
		}
		if m.detail && m.activeAppView == appViewLogs {
			if len(m.logEntries) == 0 {
				m.logCopyStatus = "No logs to copy"
				return m, nil
			}
			m.logCopyStatus = "Copying logs…"
			return m, m.copyLogs()
		}
		if m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil {
			m.buildCopyStatus = "Copying build output…"
			return m, m.copyBuildOutput()
		}
	case "p":
		if m.detail && m.activeAppView == appViewLogs && m.logPod != "" {
			return m, m.toggleLogPrevious()
		}
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			m.nextHistoryPod()
		}
	case "z":
		if m.detail && m.activeAppView == appViewLogs {
			m.logUTC = !m.logUTC
			return m, nil
		}
	case "m":
		if m.detail && m.activeAppView == appViewLogs {
			m.logMaximized = !m.logMaximized
			return m, nil
		}
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			app := m.apps[m.selected]
			if m.detailApp != nil && m.detailApp.ID == app.ID {
				app = *m.detailApp
			}
			metrics := availableHistoryMetrics(app)
			if len(metrics) > 0 {
				m.historyMetric = (m.historyMetric + 1) % len(metrics)
			}
		}
		if m.detail && m.activeAppView == appViewEnv {
			m.envMaximized = !m.envMaximized
		}
	case "v":
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && m.buildDetail == nil {
			m.ensureCICDSettings()
			if m.cicdToken != "" {
				m.cicdTokenReveal = !m.cicdTokenReveal
				return m, nil
			}
			m.cicdTokenReveal = true
			return m, m.requestCICDToken(false)
		}
		if m.detail && m.activeAppView == appViewLogs {
			return m, m.cycleLogPod(1)
		}
	case "V":
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() {
			m.cicdReveal = !m.cicdReveal
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			return m, m.cycleLogPod(-1)
		}
	case "y":
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && (m.selectedApp().BuildMethod == "gitlabci" || m.selectedApp().BuildMethod == "webhook") {
			return m, m.copyCICDValue(m.selectedApp().ID)
		}
	case "t":
		if m.detail && m.activeAppView == appViewLogs {
			if len(m.podList) == 0 {
				return m, m.loadSelectedPods()
			}
			return m, m.cycleLogPod(1)
		}
	case "f":
		if m.page == pageActivity {
			m.cycleActivityStatusFilter()
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs && m.logPod != "" && !m.logPrevious {
			m.logFollowing = !m.logFollowing
			m.logFollowPausedByUser = !m.logFollowing
			if m.logFollowing {
				return m, m.loadSelectedLogs()
			}
			m.cancelRequest(requestKey{kind: requestLogs})
			m.logPollInFlight = false
			m.logState.reset()
			return m, nil
		}
	case "N":
		if m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil {
			m.moveBuildMatch(-1)
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			m.moveLogMatch(-1)
			return m, nil
		}
	case "n", ":":
		if m.page == pageActivity {
			return m, nil
		}
		if m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil && m.buildSearch != "" {
			m.moveBuildMatch(1)
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs && m.logSearch != "" {
			m.moveLogMatch(1)
			return m, nil
		}
		if !m.help && !m.selectingTenant {
			return m, m.openNamespacePicker()
		}
	case "?":
		m.help = !m.help
	case "/":
		if m.page == pageActivity && !m.contextFocused {
			m.activityFilterBefore = m.activityFilter()
			m.activityFilterEditing = true
			return m, nil
		}
		if m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil {
			m.buildSearchBefore = m.buildSearch
			m.buildSearchEditing = true
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs && m.logPod != "" {
			m.logSearchBefore = m.logSearch
			m.logSearchEditing = true
			return m, nil
		}
		if !m.help && !m.selectingTenant {
			previousTarget := m.selectedDetailTarget()
			m.filterEditing = true
			m.contextFocused = false
			m.detail = false
			m.filterBeforeEdit = m.filterQuery
			m.namespaceSelected = false
			m.filterSelection = ""
			if m.selected >= 0 && m.selected < len(m.apps) {
				m.filterSelection = m.apps[m.selected].ID
			}
			m.syncDetailTarget(previousTarget)
		}
	case "s":
		if !m.help && !m.selectingTenant && !m.contextFocused {
			m.sorting = true
			if m.page == pageActivity {
				_, sortBy, descending := m.activitySortOptions()
				m.sortChoice, m.sortDescending = int(sortBy), descending
			} else {
				m.sortChoice = int(m.sortBy)
			}
		}
	case "left", "h":
		if m.sorting {
			if m.page == pageActivity {
				m.setActivitySortDirection(false)
			} else {
				m.sortDescending = false
			}
		} else if m.page == pageActivity && !m.contextFocused {
			return m, m.switchActivityTab(-1)
		} else if !m.help && m.contextFocused {
			m.selectingTenant = false
		} else if !m.help && !m.selectingTenant && !m.detail && m.selectedAppVisible() {
			m.setCollapsed(true)
		} else if !m.help && !m.selectingTenant && m.detail {
			m.detail = false
		}
	case "right", "l":
		if m.sorting {
			if m.page == pageActivity {
				m.setActivitySortDirection(true)
			} else {
				m.sortDescending = true
			}
		} else if m.page == pageActivity && !m.contextFocused {
			return m, m.switchActivityTab(1)
		} else if !m.help && m.contextFocused {
			m.selectingTenant = false
			return m, m.activatePageRow(m.contextRow)
		} else if !m.help && !m.selectingTenant && !m.detail && m.namespaceSelected && m.selectedAppVisible() {
			m.setCollapsed(false)
		} else if !m.help && !m.selectingTenant && !m.detail && !m.namespaceSelected && len(m.apps) > 0 {
			m.detail = true
			return m, m.loadSelectedSummary()
		}
	case browserKeyEscape:
		if m.sorting {
			m.sorting = false
		} else if m.selectingTenant {
			m.selectingTenant = false
		} else if m.help {
			m.help = false
		} else if m.page == pageActivity && m.detail {
			m.detail = false
		} else if m.page == pageActivity {
			m.page = pageApps
			m.detail = false
		} else if m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil {
			m.cancelRequest(requestKey{kind: requestBuild})
			m.buildDetail = nil
			m.buildDetailState.reset()
			m.buildSearchEditing = false
			m.buildSearch = ""
			m.summaryScroll = 0
		} else {
			if m.detail {
				m.detail = false
			} else if m.filterQuery != "" {
				m.filterQuery = ""
				m.ensureFilterSelection()
			}
		}
	case "up", "k":
		switch {
		case m.page == pageActivity && !m.contextFocused && m.detail:
			m.activityDetailScroll = max(0, m.activityDetailScroll-1)
		case m.page == pageActivity && !m.contextFocused:
			m.moveActivitySelection(-1)
		case m.detail && m.activeAppView == appViewBuilds && m.buildDetail == nil:
			m.moveBuildSelection(-1)
		case m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil:
			m.summaryScroll++
		case m.detail && m.activeAppView == appViewLogs:
			m.logScroll++
			m.logNewEntries = 0
		case m.sorting:
			m.sortChoice = (m.sortChoice + len(appSortNames) - 1) % len(appSortNames)
		case m.selectingTenant:
			return m, m.moveTenantSelection(-1)
		case m.contextFocused && m.contextRow == 0:
			return m, m.moveTenantSelection(-1)
		case m.contextFocused:
			m.contextRow = max(0, m.contextRow-1)
			if m.contextRow == 1 || m.contextRow == 2 || m.contextRow == 4 || m.contextRow == 5 {
				return m, m.activatePageRow(m.contextRow)
			}
		case !m.help && !m.detail:
			return m, m.moveTree(-1)
		case !m.help && m.detail:
			return m, m.moveTree(-1)
		}
	case "down", "j":
		switch {
		case m.page == pageActivity && !m.contextFocused && m.detail:
			m.activityDetailScroll++
		case m.page == pageActivity && !m.contextFocused:
			m.moveActivitySelection(1)
		case m.detail && m.activeAppView == appViewBuilds && m.buildDetail == nil:
			m.moveBuildSelection(1)
		case m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil:
			m.summaryScroll = max(0, m.summaryScroll-1)
		case m.detail && m.activeAppView == appViewLogs:
			m.logScroll = max(0, m.logScroll-1)
			if m.logScroll == 0 {
				m.logNewEntries = 0
			}
		case m.sorting:
			m.sortChoice = (m.sortChoice + 1) % len(appSortNames)
		case m.selectingTenant:
			return m, m.moveTenantSelection(1)
		case m.contextFocused && m.contextRow == 0:
			return m, m.moveTenantSelection(1)
		case m.contextFocused:
			m.contextRow = min(6, m.contextRow+1)
			if m.contextRow == 1 || m.contextRow == 2 || m.contextRow == 4 || m.contextRow == 5 {
				return m, m.activatePageRow(m.contextRow)
			}
		case !m.help && !m.detail:
			return m, m.moveTree(1)
		case !m.help && m.detail:
			return m, m.moveTree(1)
		}
	case "home", "g":
		if m.page == pageActivity {
			if m.detail {
				m.activityDetailScroll = 0
			} else if m.activityTab == activityAlerts {
				m.activityAlertCursor = 0
				m.updateActivityAlertSelection()
			} else {
				m.activityNotificationCursor = 0
				m.updateActivityNotificationSelection()
			}
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			m.logScroll = len(m.logEntries)
			m.logNewEntries = 0
			return m, nil
		}
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.selectTree(0)
		}
	case "end", "G":
		if m.page == pageActivity {
			if m.detail {
				m.activityDetailScroll = len(m.activityDetails())
			} else if m.activityTab == activityAlerts {
				m.activityAlertCursor = max(0, len(m.activityAlertsVisible())-1)
				m.updateActivityAlertSelection()
			} else {
				m.activityNotificationCursor = max(0, len(m.activityNotificationsVisible())-1)
				m.updateActivityNotificationSelection()
			}
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			m.logScroll = 0
			m.logNewEntries = 0
			return m, nil
		}
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.selectTree(len(m.treeNodes()) - 1)
		}
	case "pgup":
		if m.page == pageActivity {
			if m.detail {
				m.activityDetailScroll = max(0, m.activityDetailScroll-max(1, m.height-8))
			} else {
				m.moveActivitySelection(-max(1, m.height-8))
			}
			return m, nil
		}
		if m.detail && m.activeAppView == appViewBuilds {
			m.summaryScroll = max(0, m.summaryScroll-max(1, m.height-8))
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			m.logScroll += max(1, m.height-appViewPageHeightPadding)
			m.logNewEntries = 0
			return m, nil
		}
		if m.detail && (m.activeAppView == appViewSummary || m.activeAppView == appViewScaling) && m.selectedAppActive() {
			m.summaryScroll = max(0, m.summaryScroll-max(1, m.height-8))
			return m, nil
		}
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			m.usageScroll = max(0, m.usageScroll-max(1, m.height-8))
			return m, nil
		}
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.moveTree(-max(1, m.height-8))
		}
	case "pgdown":
		if m.page == pageActivity {
			if m.detail {
				m.activityDetailScroll += max(1, m.height-8)
			} else {
				m.moveActivitySelection(max(1, m.height-8))
			}
			return m, nil
		}
		if m.detail && m.activeAppView == appViewBuilds {
			m.summaryScroll += max(1, m.height-8)
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			m.logScroll = max(0, m.logScroll-max(1, m.height-appViewPageHeightPadding))
			if m.logScroll == 0 {
				m.logNewEntries = 0
			}
			return m, nil
		}
		if m.detail && (m.activeAppView == appViewSummary || m.activeAppView == appViewScaling) && m.selectedAppActive() {
			m.summaryScroll += max(1, m.height-8)
			return m, nil
		}
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			m.usageScroll += max(1, m.height-8)
			return m, nil
		}
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.moveTree(max(1, m.height-8))
		}
	case "enter":
		if m.sorting {
			if m.page == pageActivity {
				_, _, descending := m.activitySortOptions()
				m.applyActivitySort(m.sortChoice, descending)
			} else {
				m.sortBy = appSort(m.sortChoice)
				m.sortApps()
			}
			m.sorting = false
		} else if m.page == pageActivity && m.contextFocused {
			return m, m.activatePageRow(m.contextRow)
		} else if m.page == pageActivity {
			m.detail = !m.detail
			m.activityScroll = 0
		} else if m.detail && m.activeAppView == appViewBuilds && m.buildDetail == nil {
			return m, m.openSelectedBuild()
		} else if m.selectingTenant || m.contextFocused && m.contextRow == 0 {
			// Tenant changes are applied as soon as the highlighted row moves.
		} else if m.contextFocused {
			return m, m.activatePageRow(m.contextRow)
		} else if !m.help && len(m.visibleAppIndices()) > 0 {
			if m.namespaceSelected {
				if m.filterQuery == "" {
					m.toggleNamespace()
				} else {
					m.namespaceSelected = false
					m.detail = true
					return m, m.loadSelectedSummary()
				}
			} else {
				m.detail = true
				return m, m.loadSelectedSummary()
			}
		}
	case "r":
		if m.selectingTenant {
			return m, m.loadProfile()
		}
		if m.page == pageActivity {
			return m, m.loadActivityFeed(true)
		}
		if m.page == pageDB {
			return m, m.loadDatabases(true)
		}
		if m.detail {
			if m.activeAppView == appViewBuilds {
				if m.selectedApp().BuildMethod != "webhook" {
					m.detailState.reset()
					return m, m.loadSelectedSummary()
				}
				if m.buildDetailState.err != nil && m.buildSelectedID != "" {
					return m, m.openSelectedBuild()
				}
				if m.buildDetail != nil {
					return m, m.openSelectedBuild()
				}
				return m, m.loadSelectedBuilds()
			}
			if m.activeAppView == appViewLogs {
				if m.logPod == "" {
					return m, m.loadSelectedPods()
				}
				return m, m.loadSelectedLogs()
			}
			if m.activeAppView == appViewUsage {
				return m, tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory())
			}
			return m, tea.Batch(m.loadSelectedSummary(), m.loadEgressRoutes(true))
		}
		if !m.help && !m.appsState.active() {
			return m, tea.Batch(m.load(), m.loadEgressRoutes(true))
		}
	}
	return m, nil
}
