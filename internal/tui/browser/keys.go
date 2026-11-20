package browser

import tea "charm.land/bubbletea/v2"

const (
	handleKeyPressValue2 = 2
	handleKeyPressValue3 = 3
	lastPageRow          = 5
	handleKeyPressValue8 = 8
)

func (m *browser) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if model, cmd, handled := m.handleKeyPriorityMode(msg); handled {
		return model, cmd
	}
	if model, cmd, handled := m.handleKeyModalMode(msg); handled {
		return model, cmd
	}
	if model, cmd, handled := m.handleKeyCICDMode(msg); handled {
		return model, cmd
	}
	if model, cmd, handled := m.handleKeyPageMode(msg); handled {
		return model, cmd
	}
	if model, cmd, handled := m.handleKeyEditorMode(msg); handled {
		return model, cmd
	}

	return m.handleKeyPressRoutes(msg)
}

func (m *browser) handleKeyPressRoutes(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1", "2", "3", "4", inputKeyTab, inputKeyShiftTab, inputKeyBacktab, inputKeyCtrlC, "q", "a", "[", "]", browserKeyEscape,
		keyEnter, "r":
		return m.handleKeyPressBasic(msg)
	case "w", "+", "-", "p", "z", "m":
		return m.handleKeyPressView(msg)
	case "c":
		return m.handleKeyPressCopy(msg)
	case "v", "V", "y", "t":
		return m.handleKeyPressBuild(msg)
	case "f", "N", "n", ":":
		return m.handleKeyPressFilter(msg)
	case "?":
		return m.handleKeyPressHelp(msg)
	case "/":
		return m.handleKeyPressFilteredit(msg)
	case "s":
		return m.handleKeyPressSort(msg)
	case keyLeft, "h", keyRight, "l":
		return m.handleKeyPressHorizontal(msg)
	case "up", "k", keyDown, "j":
		return m.handleKeyPressVertical(msg)
	case inputKeyHome, "g":
		return m.handleKeyPressHome(msg)
	case inputKeyEnd, "G":
		return m.handleKeyPressEnd(msg)
	case "pgup":
		return m.handleKeyPressPgup(msg)
	case inputKeyPgDown:
		return m.handleKeyPressPgdown(msg)
	}

	return m, nil
}

func (m *browser) handleKeyPriorityMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.actionBusy {
		if msg.String() == inputKeyCtrlC || msg.String() == "q" {
			return m, tea.Quit, true
		}
		return m, nil, true
	}
	if m.detail && m.activeAppView == appViewBuilds && (m.cicdEditing || m.cicdReview) {
		cmd, _ := m.handleCICDSettingsKey(msg)
		return m, cmd, true
	}
	if m.actionMenu {
		return m, m.handleActionMenuKey(msg), true
	}
	if m.buildActionMenu {
		m.handleBuildActionMenuKey(msg)
		return m, nil, true
	}
	if m.buildConfirm != buildActionNone {
		return m, m.handleBuildConfirmKey(msg), true
	}
	if m.logTargetPicker {
		if m.terminalChoosing {
			return m, m.handleTerminalPickerKey(msg), true
		}
		return m, m.handleLogTargetKey(msg), true
	}
	if m.logSearchEditing {
		m.handleLogSearchKey(msg)
		return m, nil, true
	}
	if m.buildSearchEditing {
		m.handleBuildSearchKey(msg)
		return m, nil, true
	}
	if m.confirmAction != appActionNone {
		return m, m.handleActionConfirmationKey(msg), true
	}
	return nil, nil, false
}

func (m *browser) handleKeyModalMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.selectingNamespace {
		return asHandledKeyResult(m.handleNamespaceKey(msg))
	}
	if m.filterEditing {
		return asHandledKeyResult(m.handleFilterKey(msg))
	}
	if m.activityFilterEditing {
		m.handleActivityFilterKey(msg)
		return m, nil, true
	}
	if m.databaseFilterEditing {
		return asHandledKeyResult(m.handleDatabaseKey(msg))
	}
	if m.sorting {
		return asHandledKeyResult(m.handleSortKey(msg))
	}
	if m.help {
		return asHandledKeyResult(m.handleHelpKey(msg))
	}
	return nil, nil, false
}

func (m *browser) handleKeyCICDMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && (m.selectedApp().BuildMethod == buildModeWebhook ||
		m.selectedApp().BuildMethod == buildModeCustom) {
		if cmd, handled := m.handleCICDSettingsKey(msg); handled {
			return m, cmd, true
		}
	}
	return nil, nil, false
}

func (m *browser) handleKeyPageMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.page == pageActivity && !m.contextFocused && !m.selectingTenant {
		return asHandledKeyResult(m.handleActivityKey(msg))
	}
	if m.page == pageTickets && !m.contextFocused && !m.selectingTenant {
		return asHandledKeyResult(m.handleTicketKey(msg))
	}
	if model, cmd, handled := m.handleKeyDatabasePageMode(msg); handled {
		return model, cmd, true
	}
	if m.page == pageRegistry && !m.contextFocused && !m.selectingTenant {
		return m, m.handleRegistryKey(msg.String()), true
	}
	return nil, nil, false
}

func (m *browser) handleKeyDatabasePageMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.page != pageDB || m.contextFocused || m.selectingTenant {
		return nil, nil, false
	}
	if m.sorting {
		return asHandledKeyResult(m.handleDatabaseKey(msg))
	}
	switch msg.String() {
	case "1", "2", "3", "4":
		return m, m.focusPane(int(msg.String()[0] - '1')), true
	case inputKeyTab:
		return m, m.cycleFocus(false), true
	case inputKeyShiftTab, inputKeyBacktab:
		return m, m.cycleFocus(true), true
	case inputKeyCtrlC, "q":
		return m, tea.Quit, true
	case "?":
		m.help = true
		return m, nil, true
	default:
		return asHandledKeyResult(m.handleDatabaseKey(msg))
	}
}

func (m *browser) handleKeyEditorMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	for _, handler := range []func(tea.KeyPressMsg) (tea.Model, tea.Cmd, bool){
		m.handleKeyEnvironmentEditor,
		m.handleKeyDomainsEditor,
		m.handleKeyPortsEditor,
		m.handleKeyResourcesEditor,
	} {
		if model, cmd, handled := handler(msg); handled {
			return model, cmd, true
		}
	}
	return nil, nil, false
}

func (m *browser) handleKeyEnvironmentEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	return m.handleKeyAppEditor(msg, appViewEnv, m.envEditMode != envEditNone || m.envConfirm, m.handleEnvKey,
		[]string{"[", "]", inputKeyCtrlC, "q", "?", "1", "2", "3", "4", inputKeyShiftTab, inputKeyBacktab, "a", inputKeyPgUp, inputKeyPgDown})
}

func (m *browser) handleKeyDomainsEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	return m.handleKeyAppEditor(msg, appViewDomains, m.domainEditing || m.domainConfirm, m.handleDomainsKey,
		[]string{"[", "]", inputKeyCtrlC, "q", "?", "1", "2", "3", "4", inputKeyShiftTab, inputKeyBacktab, "a"})
}

func (m *browser) handleKeyPortsEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	return m.handleKeyAppEditor(msg, appViewPorts, m.portEditing || m.portConfirm, m.handlePortsKey,
		[]string{"[", "]", inputKeyCtrlC, "q", "?", "1", "2", "3", "4", inputKeyShiftTab, inputKeyBacktab, "a"})
}

func (m *browser) handleKeyResourcesEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	return m.handleKeyAppEditor(msg, appViewUsage, m.resourceEditing || m.resourceReview, m.handleResourcesKey,
		[]string{"[", "]", inputKeyCtrlC, "q", "?", "1", "2", "3", "4", inputKeyShiftTab, inputKeyBacktab, "a"})
}

func (m *browser) handleKeyAppEditor(msg tea.KeyPressMsg, view appView, editing bool, handler func(tea.KeyPressMsg) tea.Cmd,
	globalKeys []string) (tea.Model, tea.Cmd, bool) {
	if !m.detail || m.activeAppView != view || !m.selectedAppActive() {
		return nil, nil, false
	}
	if editing {
		return m, handler(msg), true
	}
	for _, key := range globalKeys {
		if msg.String() == key {
			return nil, nil, false
		}
	}
	return m, handler(msg), true
}

func asHandledKeyResult(model tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd, bool) {
	return model, cmd, true
}

func (m *browser) handleKeyPressBasicPart0(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "1" {
		if !m.help && !m.sorting {
			return m, m.focusPane(0)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart1(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "2" {
		if !m.help && !m.sorting {
			return m, m.focusPane(1)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart2(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "3" {
		if !m.help && !m.sorting {
			return m, m.focusPane(handleKeyPressValue2)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart3(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "4" {
		if !m.help && !m.sorting {
			return m, m.focusPane(handleKeyPressValue3)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart4(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == inputKeyTab {
		return m, m.cycleFocus(false)
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart5(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "shift+tab" || msg.String() == inputKeyBacktab {
		return m, m.cycleFocus(true)
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart6(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == inputKeyCtrlC || msg.String() == "q" {
		return m, tea.Quit
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart7(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "a" {
		if m.page == pageActivity {
			return m, nil
		}
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && m.selectedApp().BuildMethod == buildModeWebhook {
			m.openBuildActionMenu()
			return m, nil
		}
		m.openActionMenu()
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart8(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "[" {
		return m, m.switchAppView(-1)
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart9(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "]" {
		return m, m.switchAppView(1)
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart10(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == browserKeyEscape {
		m.handleEscapeKey()
		return m, nil
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart11(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == keyEnter {
		return m, m.handleEnterKey()
	}
	return m, nil
}

func (m *browser) handleKeyPressBasicPart12(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "r" {
		return m, nil
	}
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
		return m, m.refreshDetail()
	}
	if !m.help && !m.appsState.active() {
		return m, tea.Batch(m.load(), m.loadSelectedPods(), m.loadEgressRoutes(true))
	}
	return m, nil
}

func (m *browser) handleKeyPressViewPart0(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "w" {
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
	}
	return m, nil
}

func (m *browser) handleKeyPressViewPart1(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "+" {
		if m.detail && m.activeAppView == appViewLogs {
			return m, m.adjustLogTail(logTailStep)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressViewPart2(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "-" {
		if m.detail && m.activeAppView == appViewLogs {
			return m, m.adjustLogTail(-logTailStep)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressViewPart3(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "p" {
		if m.detail && m.activeAppView == appViewLogs && m.logPod != "" {
			return m, m.toggleLogPrevious()
		}
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			m.nextHistoryPod()
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressViewPart4(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "z" {
		if m.detail && m.activeAppView == appViewLogs {
			m.logUTC = !m.logUTC
			return m, nil
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressViewPart5(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "m" {
		return m, nil
	}
	if m.detail && m.activeAppView == appViewLogs {
		m.logMaximized = !m.logMaximized
		return m, nil
	}
	if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
		m.cycleHistoryMetric()
	}
	if m.detail && m.activeAppView == appViewEnv {
		m.envMaximized = !m.envMaximized
	}
	return m, nil
}

func (m *browser) cycleHistoryMetric() {
	app := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == app.ID {
		app = *m.detailApp
	}
	metrics := availableHistoryMetrics(app)
	if len(metrics) > 0 {
		m.historyMetric = (m.historyMetric + 1) % len(metrics)
	}
}

func (m *browser) handleKeyPressBuildPart0(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "v" {
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
	}
	return m, nil
}

func (m *browser) handleKeyPressBuildPart1(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "V" {
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() {
			m.cicdReveal = !m.cicdReveal
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			return m, m.cycleLogPod(-1)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressBuildPart2(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "y" {
		if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && (m.selectedApp().BuildMethod == "gitlabci" ||
			m.selectedApp().BuildMethod == buildModeWebhook) {
			return m, m.copyCICDValue(m.selectedApp().ID)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressBuildPart3(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "t" {
		if m.detail && m.activeAppView == appViewLogs {
			if len(m.podList) == 0 {
				return m, m.loadSelectedPods()
			}
			return m, m.cycleLogPod(1)
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressFilterPart0(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "f" {
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
	}
	return m, nil
}

func (m *browser) handleKeyPressFilterPart1(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "N" {
		if m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil {
			m.moveBuildMatch(-1)
			return m, nil
		}
		if m.detail && m.activeAppView == appViewLogs {
			m.moveLogMatch(-1)
			return m, nil
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressFilterPart2(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "n" || msg.String() == ":" {
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
	}
	return m, nil
}

func (m *browser) handleKeyPressHorizontalPart0(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == keyLeft || msg.String() == "h" {
		switch {
		case m.sorting:
			m.updateSortDirection(false)
		case m.page == pageActivity && !m.contextFocused:
			return m, m.switchActivityTab(-1)
		case !m.help && m.contextFocused:
			m.selectingTenant = false
		case !m.help && !m.selectingTenant && !m.detail && m.selectedAppVisible():
			m.setCollapsed(true)
		case !m.help && !m.selectingTenant && m.detail:
			m.detail = false
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressHorizontalPart1(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == keyRight || msg.String() == "l" {
		switch {
		case m.sorting:
			m.updateSortDirection(true)
		case m.page == pageActivity && !m.contextFocused:
			return m, m.switchActivityTab(1)
		case !m.help && m.contextFocused:
			m.selectingTenant = false
			return m, m.activatePageRow(m.contextRow)
		case !m.help && !m.selectingTenant && !m.detail && m.namespaceSelected && m.selectedAppVisible():
			m.setCollapsed(false)
		case !m.help && !m.selectingTenant && !m.detail && !m.namespaceSelected && len(m.apps) > 0:
			m.detail = true
			return m, m.loadSelectedSummary()
		}
	}
	return m, nil
}

func (m *browser) updateSortDirection(descending bool) {
	if m.page == pageActivity {
		m.setActivitySortDirection(descending)
		return
	}
	m.sortDescending = descending
}

func (m *browser) handleEscapeKey() {
	switch {
	case m.sorting:
		m.sorting = false
	case m.selectingTenant:
		m.selectingTenant = false
	case m.help:
		m.help = false
	case m.page == pageActivity && m.detail:
		m.detail = false
	case m.page == pageActivity:
		m.page = pageApps
		m.detail = false
	case m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil:
		m.cancelRequest(requestKey{kind: requestBuild})
		m.buildDetail = nil
		m.buildDetailState.reset()
		m.buildSearchEditing = false
		m.buildSearch = ""
		m.summaryScroll = 0
	case m.detail:
		m.detail = false
	case m.filterQuery != "":
		m.filterQuery = ""
		m.ensureFilterSelection()
	}
}

func (m *browser) handleEnterKey() tea.Cmd {
	if m.sorting {
		if m.page == pageActivity {
			_, _, descending := m.activitySortOptions()
			m.applyActivitySort(m.sortChoice, descending)
		} else {
			m.sortBy = appSort(m.sortChoice)
			m.sortApps()
		}
		m.sorting = false
		return nil
	}
	if m.page == pageActivity && m.contextFocused {
		return m.activatePageRow(m.contextRow)
	}
	if m.page == pageActivity {
		m.detail = !m.detail
		m.activityScroll = 0
		return nil
	}
	if m.detail && m.activeAppView == appViewBuilds && m.buildDetail == nil {
		return m.openSelectedBuild()
	}
	if m.selectingTenant || m.contextFocused && m.contextRow == 0 {
		return nil
	}
	if m.contextFocused {
		return m.activatePageRow(m.contextRow)
	}
	if !m.help && len(m.visibleAppIndices()) > 0 {
		return m.enterVisibleApp()
	}
	return nil
}

func (m *browser) enterVisibleApp() tea.Cmd {
	if !m.namespaceSelected {
		m.detail = true
		return m.loadSelectedSummary()
	}
	if m.filterQuery == "" {
		m.toggleNamespace()
		return nil
	}
	m.namespaceSelected = false
	m.detail = true
	return m.loadSelectedSummary()
}

func (m *browser) refreshDetail() tea.Cmd {
	switch m.activeAppView {
	case appViewBuilds:
		if m.selectedApp().BuildMethod != "webhook" {
			m.detailState.reset()
			return m.loadSelectedSummary()
		}
		if m.buildDetailState.err != nil && m.buildSelectedID != "" || m.buildDetail != nil {
			return m.openSelectedBuild()
		}
		return m.loadSelectedBuilds()
	case appViewLogs:
		if m.logPod == "" {
			return m.loadSelectedPods()
		}
		return m.loadSelectedLogs()
	case appViewUsage:
		m.pvcsState.reset()
		return tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory(), m.loadSelectedPVCs())
	default:
		return tea.Batch(m.loadSelectedSummary(), m.loadEgressRoutes(true))
	}
}

func (m *browser) handleKeyPressBasic(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1":
		return m.handleKeyPressBasicPart0(msg)
	case "2":
		return m.handleKeyPressBasicPart1(msg)
	case "3":
		return m.handleKeyPressBasicPart2(msg)
	case "4":
		return m.handleKeyPressBasicPart3(msg)
	case inputKeyTab:
		return m.handleKeyPressBasicPart4(msg)
	case "shift+tab", inputKeyBacktab:
		return m.handleKeyPressBasicPart5(msg)
	case inputKeyCtrlC, "q":
		return m.handleKeyPressBasicPart6(msg)
	case "a":
		return m.handleKeyPressBasicPart7(msg)
	case "[":
		return m.handleKeyPressBasicPart8(msg)
	case "]":
		return m.handleKeyPressBasicPart9(msg)
	case browserKeyEscape:
		return m.handleKeyPressBasicPart10(msg)
	case keyEnter:
		return m.handleKeyPressBasicPart11(msg)
	case "r":
		return m.handleKeyPressBasicPart12(msg)
	}
	return m, nil
}

func (m *browser) handleKeyPressView(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "w":
		return m.handleKeyPressViewPart0(msg)
	case "+":
		return m.handleKeyPressViewPart1(msg)
	case "-":
		return m.handleKeyPressViewPart2(msg)
	case "p":
		return m.handleKeyPressViewPart3(msg)
	case "z":
		return m.handleKeyPressViewPart4(msg)
	case "m":
		return m.handleKeyPressViewPart5(msg)
	}
	return m, nil
}

func (m *browser) handleKeyPressCopy(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "c" {
		return m, nil
	}
	if m.detail && m.activeAppView == appViewBuilds && m.selectedAppActive() && (m.selectedApp().BuildMethod == buildModeCustom ||
		m.selectedApp().BuildMethod == buildModeWebhook) {
		return m, m.requestCICDToken(true)
	}
	if m.detail && m.activeAppView == appViewLogs {
		return m, m.copyLogEntries()
	}
	if m.detail && m.activeAppView == appViewBuilds && m.buildDetail != nil {
		m.buildCopyStatus = "Copying build output…"
		return m, m.copyBuildOutput()
	}
	return m, nil
}

func (m *browser) copyLogEntries() tea.Cmd {
	if len(m.logEntries) == 0 {
		m.logCopyStatus = "No logs to copy"
		return nil
	}
	m.logCopyStatus = "Copying logs…"
	return m.copyLogs()
}

func (m *browser) handleKeyPressBuild(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "v":
		return m.handleKeyPressBuildPart0(msg)
	case "V":
		return m.handleKeyPressBuildPart1(msg)
	case "y":
		return m.handleKeyPressBuildPart2(msg)
	case "t":
		return m.handleKeyPressBuildPart3(msg)
	}
	return m, nil
}

func (m *browser) handleKeyPressFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "f":
		return m.handleKeyPressFilterPart0(msg)
	case "N":
		return m.handleKeyPressFilterPart1(msg)
	case "n", ":":
		return m.handleKeyPressFilterPart2(msg)
	}
	return m, nil
}

func (m *browser) handleKeyPressHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "?" {
		m.help = !m.help
	}
	return m, nil
}

func (m *browser) handleKeyPressFilteredit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "/" {
		return m, nil
	}
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
	return m, nil
}

func (m *browser) handleKeyPressSort(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "s" {
		if !m.help && !m.selectingTenant && !m.contextFocused {
			m.sorting = true
			if m.page == pageActivity {
				_, sortBy, descending := m.activitySortOptions()
				m.sortChoice, m.sortDescending = int(sortBy), descending
			} else {
				m.sortChoice = int(m.sortBy)
			}
		}
	}
	return m, nil
}

func (m *browser) handleKeyPressHorizontal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyLeft, "h":
		return m.handleKeyPressHorizontalPart0(msg)
	case keyRight, "l":
		return m.handleKeyPressHorizontalPart1(msg)
	}
	return m, nil
}

func (m *browser) handleKeyPressVertical(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	direction := 1
	if msg.String() == "up" || msg.String() == "k" {
		direction = -1
	}
	if m.page == pageActivity && !m.contextFocused {
		if m.detail {
			m.activityDetailScroll = max(0, m.activityDetailScroll+direction)
		} else {
			m.moveActivitySelection(direction)
		}
		return m, nil
	}
	if m.detail && m.activeAppView == appViewBuilds {
		if m.buildDetail == nil {
			m.moveBuildSelection(direction)
		} else {
			m.buildScroll = max(0, m.buildScroll+direction)
		}
		return m, nil
	}
	if m.detail && m.activeAppView == appViewLogs {
		m.logScroll = max(0, m.logScroll-direction)
		if direction < 0 || m.logScroll == 0 {
			m.logNewEntries = 0
		}
		return m, nil
	}
	if m.sorting {
		m.sortChoice = (m.sortChoice + len(appSortNames) + direction) % len(appSortNames)
		return m, nil
	}
	if m.selectingTenant || m.contextFocused && m.contextRow == 0 {
		return m, m.moveTenantSelection(direction)
	}
	if m.contextFocused {
		m.contextRow = (m.contextRow-1+direction+lastPageRow)%lastPageRow + 1
		if m.contextRow > 0 {
			return m, m.activatePageRow(m.contextRow)
		}
		return m, nil
	}
	if !m.help {
		return m, m.moveTree(direction)
	}
	return m, nil
}

func (m *browser) handleKeyPressHome(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == inputKeyHome || msg.String() == "g" {
		if m.page == pageActivity {
			switch {
			case m.detail:
				m.activityDetailScroll = 0
			case m.activityTab == activityAlerts:
				m.activityAlertCursor = 0
				m.updateActivityAlertSelection()
			default:
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
	}
	return m, nil
}

func (m *browser) handleKeyPressEnd(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == inputKeyEnd || msg.String() == "G" {
		if m.page == pageActivity {
			switch {
			case m.detail:
				m.activityDetailScroll = len(m.activityDetails())
			case m.activityTab == activityAlerts:
				m.activityAlertCursor = max(0, len(m.activityAlertsVisible())-1)
				m.updateActivityAlertSelection()
			default:
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
	}
	return m, nil
}

func (m *browser) handleKeyPressPgup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "pgup" {
		return m, nil
	}
	if m.page == pageActivity {
		if m.detail {
			m.activityDetailScroll = max(0, m.activityDetailScroll-max(1, m.height-handleKeyPressValue8))
		} else {
			m.moveActivitySelection(-max(1, m.height-handleKeyPressValue8))
		}
		return m, nil
	}
	if m.detail && m.activeAppView == appViewBuilds {
		m.buildScroll = max(0, m.buildScroll-max(1, m.height-handleKeyPressValue8))
		return m, nil
	}
	if m.detail && m.activeAppView == appViewLogs {
		m.logScroll += max(1, m.height-appViewPageHeightPadding)
		m.logNewEntries = 0
		return m, nil
	}
	if m.detail && (m.activeAppView == appViewSummary || m.activeAppView == appViewScaling) && m.selectedAppActive() {
		m.summaryScroll = max(0, m.summaryScroll-max(1, m.height-handleKeyPressValue8))
		return m, nil
	}
	if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
		m.usageScroll = max(0, m.usageScroll-max(1, m.height-handleKeyPressValue8))
		return m, nil
	}
	if !m.help && !m.contextFocused && !m.selectingTenant {
		return m, m.moveTree(-max(1, m.height-handleKeyPressValue8))
	}
	return m, nil
}

func (m *browser) handleKeyPressPgdown(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() != inputKeyPgDown {
		return m, nil
	}
	if m.page == pageActivity {
		if m.detail {
			m.activityDetailScroll += max(1, m.height-handleKeyPressValue8)
		} else {
			m.moveActivitySelection(max(1, m.height-handleKeyPressValue8))
		}
		return m, nil
	}
	if m.detail && m.activeAppView == appViewBuilds {
		m.buildScroll += max(1, m.height-handleKeyPressValue8)
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
		m.summaryScroll += max(1, m.height-handleKeyPressValue8)
		return m, nil
	}
	if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
		m.usageScroll += max(1, m.height-handleKeyPressValue8)
		return m, nil
	}
	if !m.help && !m.contextFocused && !m.selectingTenant {
		return m, m.moveTree(max(1, m.height-handleKeyPressValue8))
	}
	return m, nil
}
