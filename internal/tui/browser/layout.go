package browser

import (
	"slices"

	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	viewValue100                   = 100
	viewValue2                     = 2
	layoutAvailableColumnDivisor   = 2
	layoutContextMinimumWidth      = 18
	layoutPaneRightPadding         = 8
	layoutContextHeightDivisor     = 2
	layoutBorderTitleRulePadding   = 3
	layoutFooterActionSegmentGap   = 3
	layoutCenteringDivisor         = 2
	layoutContextWidthDivisor      = 5
	activityPaneBorderTitleValue24 = 24
	appPaneScopeControlValue24     = 24
	appPaneScopeControlValue42     = 42
	appPaneScopeControlValue5      = 5
	appViewTabLabelsValue42        = 42
	appViewTabLabelsValue58        = 58
	appsPaneBorderTitleValue24     = 24
	appsPaneBorderTitleValue36     = 36
	borderTitleWithControlsValue6  = 6
	centerFooterSegmentValue3      = 3
	fitKeyHintsValue2              = 2
	keyHintsValue100               = 100
	keyHintsValue72                = 72
	paneWidthsValue2               = 2
	paneWidthsValue28              = 28
	paneWidthsValue3               = 3
	renderPaneValue2               = 2
	renderPaneValue3               = 3
	splitActivityPanesValue2       = 2
	splitActivityPanesValue4       = 4
	splitDatabaseCompactValue2     = 2
	splitDatabaseCompactValue4     = 4
	splitPanesValue2               = 2
	splitPanesValue4               = 4
)

const browserFooterHeight = 1

func (m *browser) View() tea.View {
	var lines []string
	footer := m.footerLine()
	if m.width < 60 || m.height < 20 {
		lines = []string{"Resize terminal to at least 60 x 20"}
	} else {
		lines = m.viewContentLines()
	}
	if len(lines) > m.height-browserFooterHeight {
		lines = lines[:max(0, m.height-browserFooterHeight)]
	}
	for len(lines) < max(0, m.height-browserFooterHeight) {
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	lines = m.viewOverlayLines(lines)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "")
		lines[i] += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(lines[i])))
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *browser) viewContentLines() []string {
	lines := m.viewHeaderLines()
	bodyHeight := max(1, m.height-len(lines)-browserFooterHeight-1)
	return append(lines, m.viewBodyLines(bodyHeight)...)
}

func (m *browser) viewHeaderLines() []string {
	header := m.breadcrumb()
	rightIndicators := ""
	right := rightIndicators
	if m.userEmail != "" {
		email := paint(safeText(m.userEmail), browserMuted, false)
		maxRightWidth := m.width / layoutAvailableColumnDivisor
		emailWidth := min(ansi.StringWidth(email), maxRightWidth)
		indicatorWidth := max(0, maxRightWidth-emailWidth-renderPaneValue2)
		rightIndicators = ansi.Truncate(rightIndicators, indicatorWidth, "…")
		if rightIndicators != "" {
			right = rightIndicators + "  "
		} else {
			right = ""
		}
		right += ansi.Truncate(email, maxRightWidth-ansi.StringWidth(right), "…")
	}
	leftWidth := max(0, m.width-ansi.StringWidth(right)-1)
	header = ansi.Truncate(header, leftWidth, "…")
	padding := max(1, m.width-ansi.StringWidth(header)-ansi.StringWidth(right))
	header += strings.Repeat(" ", padding) + right
	lines := []string{header, ""}
	if m.actionNotice != "" {
		lines = append(lines, paint(safeText(m.actionNotice), browserNumber, false))
	}
	if m.profileState.status == requestFailed || m.profileState.status == requestStale {
		lines = append(lines, "Account profile unavailable; tenant apps remain available. Press t then r to retry profile.")
		if m.profileState.status == requestStale {
			lines = append(lines, staleRequestTime("Profile", m.profileState))
		}
	}
	return wrapDiagnosticLines(lines, m.width)
}

func (m *browser) viewBodyLines(bodyHeight int) []string {
	paneHeight := max(1, bodyHeight-viewValue2)
	left, right := m.appPaneLines(paneHeight), m.summaryPaneLines()
	switch m.page {
	case pageActivity:
		left = m.activityPaneLines(paneHeight)
		right = m.activityDetailLines(paneHeight)
		return m.splitActivityPanes(left, right, bodyHeight)
	case pageTickets:
		left, right = m.ticketPaneLines(paneHeight), m.ticketConversationLines(paneHeight)
		return m.splitPanes(left, right, bodyHeight)
	case pageRegistry:
		left = m.registryPaneLines(paneHeight)
		right = m.registryDetailLines(paneHeight)
		return m.splitPanes(left, right, bodyHeight)
	case pageDB:
		left = m.databasePaneLines(paneHeight)
		right = m.databaseDetailLines(paneHeight)
		if m.width < viewValue100 {
			return m.splitDatabaseCompact(left, right, bodyHeight)
		}
		return m.splitPanes(left, right, bodyHeight)
	default:
		return m.appViewBodyLines(left, right, paneHeight, bodyHeight)
	}
}

func (m *browser) appViewBodyLines(left, right []string, paneHeight, bodyHeight int) []string {
	switch m.activeAppView {
	case appViewUsage:
		right = m.usageViewport(right, paneHeight)
	case appViewLogs:
		right = m.logsViewport(right, paneHeight)
	case appViewEnv:
		right = m.summaryViewport(right, paneHeight)
	case appViewPorts:
		viewHeight := paneHeight
		if m.portMaximized {
			viewHeight = max(1, bodyHeight-viewValue2)
		}
		right = m.summaryViewport(right, viewHeight)
	default:
		break
	}
	if m.detail && m.appViewMaximized() {
		return renderPane(right, max(1, m.width-renderPaneValue2), bodyHeight-viewValue2, m.appViewBorderTitle(m.width-renderPaneValue2), true)
	}
	return m.splitPanes(left, right, bodyHeight)
}

func (m *browser) viewOverlayLines(lines []string) []string {
	if m.width < 60 || m.height < 20 {
		return lines
	}
	for _, overlay := range []struct {
		active bool
		render func([]string) []string
	}{
		{m.buildActionMenu, m.renderBuildActionMenu},
		{m.buildConfirm != buildActionNone, m.renderBuildActionConfirmation},
		{m.actionMenu, m.renderActionMenu},
		{m.confirmAction != appActionNone, m.renderActionConfirmation},
		{m.selectingNamespace, m.namespaceModal},
		{m.logTargetPicker && m.terminalChoosing, m.logTargetModal},
		{m.help, m.helpModal},
		{m.registryTagMode, m.registryTagOverlay},
		{m.registryConfirm, m.registryConfirmation},
	} {
		if overlay.active {
			return overlay.render(lines)
		}
	}
	return lines
}

func (m *browser) appPaneLines(height int) []string {
	lines := []string{}
	if m.appsState.err != nil {
		if len(m.apps) == 0 {
			lines = append(lines, "Could not load apps: "+safeText(m.appsState.err.Error()))
		} else {
			lines = append(lines, paint("Stale · refresh failed: "+safeText(m.appsState.err.Error()), browserNumber, false))
			lines = append(lines, staleRequestTime("Apps", m.appsState))
		}
	}
	if m.appsState.status == requestUnavailable && len(m.apps) == 0 {
		lines = append(lines, paint("App listing is unavailable.", browserMuted, false))
	}
	if !m.appsState.active() && m.appsState.err == nil && len(m.apps) == 0 {
		label := "No apps in this tenant."
		if strings.HasPrefix(m.mode, "MOCK") {
			label = "No apps in this mock profile."
		}
		if m.namespaceScoped {
			label = fmt.Sprintf("No apps in namespace %q.", safeText(m.namespaceScope.name))
		}
		lines = append(lines, label)
	}
	if !m.appsState.active() && m.appsState.err == nil && len(m.apps) > 0 && len(m.visibleAppIndices()) == 0 {
		if m.namespaceScoped && m.filterQuery == "" {
			lines = append(lines, fmt.Sprintf("No apps in namespace %q.", safeText(m.namespaceScope.name)))
		} else {
			lines = append(lines, "No apps match this filter.")
		}
	}
	room := max(0, height-len(lines))
	appWidth, _, _ := m.paneWidths()
	return append(lines, m.appTreeForWidth(room, appWidth)...)
}

func (m *browser) appViewBorderTitle(width int) string {
	labels := appViewTabLabels(width)
	parts := make([]string, len(labels))
	views := visibleAppViews()
	for i, label := range labels {
		if i < len(views) && views[i] == m.activeAppView {
			parts[i] = paint("["+label+"]", browserBranch, true)
		} else {
			parts[i] = paint(label, browserBranch, false)
		}
	}
	return paint("[4] ", browserAccent, true) + strings.Join(parts, " ")
}

func (m *browser) appsPaneBorderTitle(label string, width int) string {
	if width < appsPaneBorderTitleValue24 {
		label = "[3]"
	} else if width < appsPaneBorderTitleValue36 {
		label = "[3] APPS"
	}
	fullSort := m.sortLine()
	compactSort := compactSortIndicator(m.sortDescending)
	controls := m.appPaneScopeControl(width) + " " + m.appFilterBorderLine()
	if width < appsPaneBorderTitleValue36 {
		fullSort = compactSort
	}
	return borderTitleWithControls(label, controls, fullSort, compactSort, width)
}

func (m *browser) appPaneScopeControl(width int) string {
	if width >= appPaneScopeControlValue42 {
		return m.namespaceTopLine()
	}
	if width < appPaneScopeControlValue24 {
		return paint("⎈", browserMuted, true)
	}
	name := labelAll
	if m.namespaceScoped {
		name = m.namespaceScope.name
	}
	return paint("⎈", browserMuted, true) + " " + paint(ansi.Truncate(safeText(name), appPaneScopeControlValue5, "…"), browserBranch, true)
}

func (m *browser) activityPaneBorderTitle(label string, width int) string {
	if width < activityPaneBorderTitleValue24 {
		if m.activityTab == activityNotifications {
			label = "[3] N"
		} else {
			label = "[3] A"
		}
	}
	_, _, descending := m.activitySortOptions()
	fullSort := m.activitySortLine()
	compactSort := compactSortIndicator(descending)
	return borderTitleWithControls(label, m.activityFilterLine(), fullSort, compactSort, width)
}

func (m *browser) appFilterBorderLine() string {
	query := safeText(m.filterQuery)
	if query == "" && !m.filterEditing {
		return paint("⌕", browserMuted, false)
	}
	line := paint("⌕", browserMuted, false) + " " + paint(query, browserBranch, true)
	if m.filterEditing {
		line += paint("▏", browserNumber, true)
	}
	return line
}

func compactSortIndicator(descending bool) string {
	direction := "↑"
	if descending {
		direction = "↓"
	}
	return paint("⇅", browserMuted, false) + paint(direction, browserNumber, true)
}

// borderTitleWithControls keeps the sort state at the far right and gives
// remaining title space to the filter, truncating that query first.
func borderTitleWithControls(label, filter, fullSort, compactSort string, width int) string {
	// Leave a visible run of border after the right-aligned controls.
	available := max(1, width-borderTitleWithControlsValue6)
	label = ansi.Truncate(label, available, "…")
	room := max(0, available-ansi.StringWidth(label)-1)
	if room == 0 {
		return label
	}
	sort := fullSort
	if ansi.StringWidth(sort) > room-2 {
		sort = compactSort
	}
	sort = ansi.Truncate(sort, room, "…")
	filterRoom := max(0, room-ansi.StringWidth(sort)-1)
	if filterRoom > 0 {
		filter = ansi.Truncate(filter, filterRoom, "…")
	} else {
		filter = ""
	}
	controls := filter
	if filter != "" && sort != "" {
		controls += " "
	}
	controls += sort
	gap := max(1, available-ansi.StringWidth(label)-ansi.StringWidth(controls))
	return label + strings.Repeat(" ", gap) + controls
}

func appViewTabLabels(width int) []string {
	labels := []string{"Summary", "Resources", "Logs", "CI/CD", "Env", labelDomains, "Ports"}
	if width < appViewTabLabelsValue58 {
		labels = []string{"Sum", "Res", "Logs", "CI/CD", "Env", "Domains", "Ports"}
	}
	if width < appViewTabLabelsValue42 {
		labels = []string{"S", "R", "L", "B", "E", "D", "P"}
	}
	return labels
}

func visibleAppViews() []appView {
	return []appView{appViewSummary, appViewUsage, appViewLogs, appViewBuilds, appViewEnv, appViewDomains, appViewPorts}
}

func (m *browser) appViewMaximized() bool {
	switch m.activeAppView {
	case appViewLogs:
		return m.logMaximized
	case appViewEnv:
		return m.envMaximized
	case appViewDomains:
		return m.domainMaximized
	case appViewPorts:
		return m.portMaximized
	case appViewSummary, appViewUsage, appViewScaling, appViewBuilds:
		return false
	}
	return false
}

func (m *browser) paneWidths() (int, int, int) {
	contextWidth := min(paneWidthsValue28, max(layoutContextMinimumWidth, m.width/layoutContextWidthDivisor))
	available := max(paneWidthsValue3, m.width-contextWidth-layoutPaneRightPadding)
	appsWidth := available / paneWidthsValue2
	return appsWidth, available - appsWidth, contextWidth
}

func (m *browser) splitPanes(apps, summary []string, height int) []string {
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	if m.page == pageApps && m.detail && (m.activeAppView == appViewSummary || m.activeAppView == appViewScaling) {
		summary = m.summaryViewport(summary, max(1, height-splitPanesValue2))
	}
	contextSeparator := 1
	tenantHeight := max(splitPanesValue4, (height-contextSeparator)/layoutContextHeightDivisor)
	pagesHeight := max(splitPanesValue4, height-contextSeparator-tenantHeight)
	tenant := renderPane(m.tenantPaneLines(max(1, tenantHeight-renderPaneValue2)), contextWidth, tenantHeight-renderPaneValue2,
		"[1] TENANTS", m.contextFocused && m.contextRow == 0)
	pages := renderPane(m.pagesPaneLines(max(1, pagesHeight-renderPaneValue2)), contextWidth, pagesHeight-renderPaneValue2,
		"[2] PAGES", m.contextFocused && m.contextRow > 0)
	contextPane := tenant
	contextPane = append(contextPane, strings.Repeat(" ", contextWidth+renderPaneValue2))
	contextPane = append(contextPane, pages...)
	appTitle, viewTitle := m.paneTitles(appsWidth, summaryWidth)
	appsPane := renderPane(apps, appsWidth, height-splitPanesValue2, appTitle, !m.contextFocused && !m.detail)
	viewPane := renderPaneWithFooter(summary, summaryWidth, height-splitPanesValue2, viewTitle, m.detail, m.paneMessage)
	lines := make([]string, height)
	for i := range lines {
		left, center, right := contextPane[i], appsPane[i], viewPane[i]
		lines[i] = left + " " + center + " " + right
	}
	return lines
}

func (m *browser) paneTitles(appsWidth, summaryWidth int) (string, string) {
	appTitle := "[3] APPS"
	if m.appsState.active() {
		appTitle += " " + m.spinnerGlyph()
	}
	if m.page == pageApps {
		appTitle = m.appsPaneBorderTitle(appTitle, appsWidth)
	}
	viewTitle := m.appViewBorderTitle(summaryWidth)
	if m.page == pageRegistry {
		appTitle = "[3] REGISTRY TREE"
		viewTitle = "[4] IMAGE DETAILS"
		if len(m.registryRows) > 0 && m.registryRows[m.registryCursor].digest >= 0 {
			viewTitle = "[4] MANIFEST DETAILS"
		}
	}
	if m.page == pageDB {
		appTitle = "[3] DATABASES"
		viewTitle = "[4] DATABASE DETAILS"
		appTitle = borderTitleWithControls(appTitle, m.databaseFilterLine(), m.databaseSortLine(),
			compactSortIndicator(m.databaseSortDescending), appsWidth)
	}
	if m.page == pageTickets {
		controls := ticketStatusControl(m.ticketStatus) + "  " + ticketScopeControl(m.ticketMine)
		compactControls := ticketStatusControl(m.ticketStatus) + " " + ticketScopeControl(m.ticketMine)
		appTitle = borderTitleWithControls("[3] TICKETS", "⌕ "+safeText(m.ticketFilter), controls, compactControls, appsWidth)
		viewTitle = "[4] CONVERSATION"
		switch m.ticketCompose {
		case ticketComposeNew:
			viewTitle = "[4] NEW TICKET"
		case ticketComposeReview:
			viewTitle = "[4] REVIEW TICKET"
		case ticketComposeReply:
			viewTitle = "[4] REPLY"
		}
	}
	return appTitle, viewTitle
}

func (m *browser) splitDatabaseCompact(list, details []string, height int) []string {
	_, _, contextWidth := m.paneWidths()
	contentWidth := max(1, m.width-contextWidth-splitDatabaseCompactValue4)
	tenantHeight := max(splitDatabaseCompactValue4, (height-1)/layoutContextHeightDivisor)
	pagesHeight := max(splitDatabaseCompactValue4, height-1-tenantHeight)
	tenant := renderPane(m.tenantPaneLines(max(1, tenantHeight-renderPaneValue2)), contextWidth, tenantHeight-renderPaneValue2,
		"[1] TENANTS", m.contextFocused && m.contextRow == 0)
	pages := renderPane(m.pagesPaneLines(max(1, pagesHeight-renderPaneValue2)), contextWidth, pagesHeight-renderPaneValue2,
		"[2] PAGES", m.contextFocused && m.contextRow > 0)
	context := tenant
	context = append(context, strings.Repeat(" ", contextWidth+renderPaneValue2))
	context = append(context, pages...)
	content, title := list, "[3] DATABASES"
	focused := !m.contextFocused && !m.detail
	if m.detail {
		content, title, focused = details, "[4] DATABASE DETAILS", true
	}
	if !m.detail {
		title = borderTitleWithControls(title, m.databaseFilterLine(), m.databaseSortLine(),
			compactSortIndicator(m.databaseSortDescending), contentWidth)
	}
	pane := renderPane(content, contentWidth, height-splitDatabaseCompactValue2, title, focused)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = context[i] + " " + pane[i]
	}
	return lines
}

func (m *browser) databaseFilterLine() string {
	query := safeText(m.databaseFilter)
	if query == "" && !m.databaseFilterEditing {
		return paint("⌕", browserMuted, false)
	}
	if m.databaseFilterEditing {
		query += "█"
	}
	return paint("⌕ "+query, browserBranch, true)
}

func (m *browser) splitActivityPanes(list, details []string, height int) []string {
	listWidth, detailWidth, contextWidth := m.paneWidths()
	contextSeparator := 1
	tenantHeight := max(splitActivityPanesValue4, (height-contextSeparator)/layoutContextHeightDivisor)
	pagesHeight := max(splitActivityPanesValue4, height-contextSeparator-tenantHeight)
	tenant := renderPane(m.tenantPaneLines(max(1, tenantHeight-renderPaneValue2)), contextWidth, tenantHeight-renderPaneValue2,
		"[1] TENANTS", m.contextFocused && m.contextRow == 0)
	pages := renderPane(m.pagesPaneLines(max(1, pagesHeight-renderPaneValue2)), contextWidth, pagesHeight-renderPaneValue2,
		"[2] PAGES", m.contextFocused && m.contextRow > 0)
	context := tenant
	context = append(context, strings.Repeat(" ", contextWidth+renderPaneValue2))
	context = append(context, pages...)
	listTitle := "[3] ALERTS"
	if m.activityTab == activityNotifications {
		listTitle = "[3] NOTIFICATIONS"
	}
	listTitle = m.activityPaneBorderTitle(listTitle, listWidth)
	listPane := renderPane(list, listWidth, height-splitActivityPanesValue2, listTitle, !m.contextFocused && !m.detail)
	detailTitle := "[4] ALERT DETAILS"
	if m.activityTab == activityNotifications {
		detailTitle = "[4] MESSAGE"
	}
	detailPane := renderPane(details, detailWidth, height-splitActivityPanesValue2, detailTitle, m.detail)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = context[i] + " " + listPane[i] + " " + detailPane[i]
	}
	return lines
}

func renderPane(content []string, width, height int, title string, focused bool) []string {
	return renderPaneWithFooter(content, width, height, title, focused, "")
}

func renderPaneWithFooter(content []string, width, height int, title string, focused bool, footer string) []string {
	borderColor := browserBorder
	if focused {
		borderColor = browserBranch
	}
	innerWidth := max(1, width)
	content = wrapDiagnosticLines(content, innerWidth)
	// Keep ANSI styling out of the measured border string. Embedded styles
	// around title controls can confuse terminal width accounting and break the
	// top edge in some terminal emulators.
	title = ansi.Truncate(ansi.Strip(title), max(1, innerWidth-renderPaneValue3), "…")
	top := "╭─ " + title + " " + strings.Repeat("─", max(0, innerWidth-ansi.StringWidth(title)-layoutBorderTitleRulePadding)) + "╮"
	lines := make([]string, 0, height+renderPaneValue2)
	lines = append(lines, paint(top, borderColor, focused))
	for row := range height {
		line := ""
		if row < len(content) {
			line = content[row]
		}
		line = ansi.Truncate(line, innerWidth, "…")
		line += strings.Repeat(" ", max(0, innerWidth-ansi.StringWidth(line)))
		lines = append(lines, paint("│", borderColor, focused)+line+paint("│", borderColor, focused))
	}
	bottom := "╰" + strings.Repeat("─", innerWidth) + "╯"
	if footer != "" && innerWidth > 4 {
		label := ansi.Truncate(" "+safeText(footer)+" ", innerWidth-renderPaneValue2, "…")
		bottom = "╰─" + label + strings.Repeat("─", max(0, innerWidth-ansi.StringWidth(label)-1)) + "╯"
	}
	lines = append(lines, paint(bottom, borderColor, focused))
	return lines
}

func (m *browser) summaryViewport(lines []string, height int) []string {
	scroll := &m.summaryScroll
	if m.activeAppView == appViewBuilds && m.buildDetail != nil {
		scroll = &m.buildScroll
	}
	if len(lines) <= height || height <= 0 {
		*scroll = 0
		return lines
	}
	stickyLines := 3
	switch {
	case m.activeAppView == appViewUsage:
		stickyLines = 1
	case m.activeAppView == appViewBuilds && m.buildDetail != nil:
		stickyLines = buildDetailStickyLines(lines)
	case m.activeAppView == appViewBuilds:
		stickyLines = 0
	}
	sticky := min(stickyLines, min(len(lines), height))
	bodyHeight := max(0, height-sticky)
	maxScroll := max(0, len(lines)-sticky-bodyHeight)
	for _, cursor := range m.viewportCursors(sticky, bodyHeight) {
		keepRowVisible(scroll, bodyHeight, cursor)
	}
	if m.activeAppView == appViewBuilds && m.selectedAppActive() && m.selectedApp().BuildMethod == buildModeWebhook &&
		!m.cicdHistoryFocus && !m.cicdReview && bodyHeight > 0 {
		keepRowVisible(scroll, bodyHeight, max(0, m.cicdCursorLine-sticky))
	}
	*scroll = min(*scroll, maxScroll)
	end := min(len(lines), sticky+*scroll+bodyHeight)
	out := append([]string(nil), lines[:sticky]...)
	return append(out, lines[sticky+*scroll:end]...)
}

func (m *browser) viewportCursors(sticky, bodyHeight int) []int {
	if bodyHeight <= 0 {
		return nil
	}
	var cursors []int
	if m.activeAppView == appViewEnv && !m.envConfirm && m.envCursorLine >= sticky {
		cursors = append(cursors, m.envCursorLine-sticky)
	}
	if m.activeAppView == appViewDomains && !m.domainConfirm && m.domainCursorLine >= sticky {
		cursors = append(cursors, m.domainCursorLine-sticky)
	}
	if m.activeAppView == appViewPorts && !m.portConfirm && m.portCursorLine >= sticky {
		cursors = append(cursors, m.portCursorLine-sticky)
	}
	return cursors
}

func keepRowVisible(scroll *int, bodyHeight, selectedRow int) {
	if selectedRow < *scroll {
		*scroll = selectedRow
	} else if selectedRow >= *scroll+bodyHeight {
		*scroll = selectedRow - bodyHeight + 1
	}
}

func (m *browser) footerLine() string {
	left := m.footerBaseLine()
	if m.contextFocused {
		return m.contextFooterLine(left)
	}
	modalHints, modal := m.modalFooterHints()
	if !modal && m.page == pageApps && m.selectedAppActive() {
		left = m.appendFooterActionHint(left)
	}
	if modal {
		return centerFooterSegment(left, m.fitFooterHints(left, modalHints), m.width)
	}
	switch m.page {
	case pageActivity:
		return m.activityFooterLine(left)
	case pageTickets:
		return m.ticketFooterLine(left)
	case pageRegistry:
		return m.registryFooterLine(left)
	case pageDB:
		return m.databaseFooterLine(left)
	default:
		return m.appFooterLine(left)
	}
}

func (m *browser) footerBaseLine() string {
	quit := paint("q", browserBranch, true) + " " + paint("quit", browserMuted, false)
	help := paint("?", browserBranch, true) + " " + paint("help", browserMuted, false)
	return quit + paint(" · ", browserBorder, false) + help
}

func (m *browser) contextFooterLine(left string) string {
	if m.contextRow == 0 {
		return left + paint(" · ", browserBorder, false) + paint("r", browserBranch, true) + " " + paint("retry profile", browserMuted, false)
	}
	return left
}

func (m *browser) appendFooterActionHint(left string) string {
	label := labelActions
	available := len(m.actionMenuItems()) > 0
	if m.detail && m.activeAppView == appViewBuilds && m.selectedApp().BuildMethod == buildModeWebhook {
		label = hintBuildActions
		available = m.actionIsMock() || m.allowWrites
	}
	if !available {
		return left
	}
	return left + paint(" · ", browserBorder, false) + paint("a", browserBranch, true) + " " + paint(label, browserMuted, false)
}

func (m *browser) fitFooterHints(left string, hints []keyHint) string {
	available := max(0, m.width-ansi.StringWidth(ansi.Strip(left))-layoutFooterActionSegmentGap)
	return footerHintText(fitKeyHints(hints, available))
}

func (m *browser) activityFooterLine(left string) string {
	hints := []keyHint{{hintVerticalSelect, hintSelect}, {hintEnter, hintDetails}, {hintHorizontalTabs, inputKeyTab}, {"f", "status"},
		{"s", hintSort}, {"/", hintFilter}, {"r", hintRefresh}}
	if m.detail {
		hints = []keyHint{{hintVerticalPageScroll, hintScroll}, {hintEscape, hintList}, {hintHorizontalTabs, inputKeyTab}, {"r", hintRefresh}}
	}
	return m.rightFooterLine(left, m.fitFooterHints(left, hints))
}

func (m *browser) registryFooterLine(left string) string {
	hints := []keyHint{{hintVerticalSelect, hintSelect}, {"←→/↵", "collapse / load"}, {"y", "copy image reference"}, {"r", hintRefresh}}
	if m.allowWrites {
		hints = append(hints, keyHint{"d", "delete image/digest"}, keyHint{"t", "remove tag"})
	}
	if m.registryConfirm {
		hints = []keyHint{{"type delete", "confirm target"}, {hintEscape, hintCancel}}
	} else if m.registryTagMode {
		if m.registryTagCopy {
			hints = []keyHint{{"↑↓", "choose tag"}, {"↵", "copy reference"}, {hintEscape, hintCancel}}
		} else {
			hints = []keyHint{{"↑↓", "choose tag"}, {"↵", "remove selected"}, {hintEscape, hintCancel}}
		}
	}
	right := m.fitFooterHints(left, hints)
	if m.registryConfirm {
		return centerFooterSegment(left, right, m.width)
	}
	return m.rightFooterLine(left, right)
}

func (m *browser) databaseFooterLine(left string) string {
	hints := []keyHint{{hintVerticalSelect, hintSelect}, {hintEnter, hintDetails}, {"s", hintSort}, {"/", hintFilter}, {"r", hintRefresh}}
	if m.detail {
		hints = []keyHint{{hintVerticalPageScroll, hintScroll}, {hintEscape, hintList}, {"y", "copy URI"}, {"r", hintRefresh}}
		if databaseHasStandby(m.databaseDetail) {
			hints = append(hints, keyHint{"m/b", "master/standby"})
		}
	}
	return m.rightFooterLine(left, m.fitFooterHints(left, hints))
}

func (m *browser) appFooterLine(left string) string {
	if !m.detail {
		if m.page != pageApps || m.contextFocused {
			return left
		}
		hints := []keyHint{{hintVerticalSelect, labelApps}, {hintEnter, hintDetails}, {"n", hintScope}, {"/", hintFilter}, {"s",
			hintSort}, {"r", hintRefresh}}
		return m.rightFooterLine(left, m.fitFooterHints(left, hints))
	}
	if !m.selectedAppActive() {
		return left
	}
	right := m.fitFooterHints(left, m.appViewFooterHints())
	if right == "" {
		return left
	}
	return m.rightFooterLine(left, right)
}

func (m *browser) rightFooterLine(left, right string) string {
	leftWidth := ansi.StringWidth(ansi.Strip(left))
	rightWidth := ansi.StringWidth(ansi.Strip(right))
	if leftWidth+rightWidth+3 > m.width {
		return left
	}
	return left + strings.Repeat(" ", max(1, m.width-leftWidth-rightWidth)) + right
}

func (m *browser) modalFooterHints() ([]keyHint, bool) {
	for _, state := range m.modalFooterHintStates() {
		if state.active {
			return state.hints, true
		}
	}
	return nil, false
}

type modalFooterHintState struct {
	active bool
	hints  []keyHint
}

func (m *browser) modalFooterHintStates() []modalFooterHintState {
	return []modalFooterHintState{
		{m.cicdReview, []keyHint{{"y/Enter", hintSave}, {hintNoEscape, hintBack}}},
		{m.actionBusy, []keyHint{{"…", "action in progress"}}},
		{m.actionMenu, []keyHint{{hintEscape, hintClose}}},
		{m.buildActionMenu, []keyHint{{"↑↓", hintChoose}, {hintEnter, hintSelect}, {hintClick, hintSelect}, {hintEscape, hintClose}}},
		{m.buildConfirm != buildActionNone, []keyHint{{"←→", "cancel / confirm"}, {"↵", "continue"}, {hintEscape, hintCancel}}},
		{m.confirmAction == appActionRestart, []keyHint{{"y", "restart"}, {"n", hintCancel}, {"↵", "confirm"}, {hintEscape, hintCancel}}},
		{m.confirmAction == appActionDelete, []keyHint{{hintTextType, "app name"}, {hintTab, actionDelete}, {hintClick, actionDelete},
			{hintEscape, hintCancel}}},
		{m.help, []keyHint{{"Shift+drag", "select text"}, {"terminal shortcut", hintCopy}, {hintEscape, hintCloseHelp}}},
		{m.filterEditing, []keyHint{{hintTextType, hintFilter}, {hintEnter, hintOpen}, {hintCtrlU, hintClear}, {hintEscape, hintCancel}}},
		{m.activityFilterEditing, []keyHint{{hintTextType, hintFilter}, {hintCtrlU, hintClear}, {hintEscape, hintCancel}}},
		{m.selectingNamespace, []keyHint{{hintTextType, hintFilter}, {hintEnter, hintScope}, {hintClick, hintScope}, {hintCtrlU,
			hintClear}, {hintEscape, hintClose}}},
		{m.terminalChoosing, []keyHint{{hintEnter, "connect"}, {hintEscape, hintCancel}}},
		{m.logTargetPicker, []keyHint{{hintEnter, "select target"}, {hintEscape, hintClose}}},
		{m.logSearchEditing, []keyHint{{hintTextType, hintSearch}, {hintEnter, hintFind}, {hintCtrlU, hintClear}, {hintEscape, hintCancel}}},
		{m.buildSearchEditing, []keyHint{{hintTextType, hintSearch}, {hintEnter, hintFind}, {hintCtrlU, hintClear}, {hintEscape, hintCancel}}},
		{m.sorting, []keyHint{{hintEnter, "apply sort"}, {hintEscape, hintClose}}},
		{m.envConfirm, []keyHint{{hintVerticalPageScroll, hintScrollDiff}, {"y", hintSave}, {hintNoEscape, hintBack}}},
		{m.detail && m.activeAppView == appViewDomains && m.domainConfirm, []keyHint{{hintPageScroll, hintScrollDiff}, {hintEnter,
			hintSave}, {hintEscape, hintBack}}},
		{m.detail && m.activeAppView == appViewDomains && m.domainEditing, []keyHint{{hintEnter, hintStage}, {hintCtrlU, hintClear},
			{hintEscape, hintCancel}}},
		{m.detail && m.activeAppView == appViewPorts && m.portConfirm, []keyHint{{"↑↓", hintScrollDiff}, {hintEnter, hintSave},
			{hintEscape, hintBack}}},
		{m.detail && m.activeAppView == appViewPorts && m.portEditing, []keyHint{{hintTextType, labelValue}, {hintEnter, "stage cell"},
			{hintCtrlU, hintClear}, {hintEscape, hintCancel}}},
		{m.detail && m.activeAppView == appViewUsage && m.resourceReview, []keyHint{{hintEnterOrY, "save"}, {hintEscOrNo, hintBack}}},
		{m.detail && m.activeAppView == appViewUsage && m.resourceEditing, []keyHint{{hintTextType, labelValue}, {hintEnter, hintStage},
			{hintCtrlU, hintClear}, {hintEscape, hintCancel}}},
		{m.detail && m.activeAppView == appViewUsage && m.resourcePlanPicker, []keyHint{{hintVerticalSelect, hintChoosePlan}, {hintEnter,
			hintSelect}, {hintEscape, hintCancel}}},
		{m.envEditMode != envEditNone, []keyHint{{hintEnter, hintStage}, {hintCtrlU, hintClear}, {hintEscape, hintCancelEdit}}},
	}
}

func footerHintText(hints []keyHint) string {
	parts := make([]string, len(hints))
	for i, hint := range hints {
		parts[i] = paint(hint.key, browserBranch, true) + " " + paint(hint.action, browserMuted, false)
	}
	return strings.Join(parts, paint(" · ", browserBorder, false))
}

func centerFooterSegment(left, center string, width int) string {
	if center == "" {
		return left
	}
	leftWidth := ansi.StringWidth(ansi.Strip(left))
	centerWidth := ansi.StringWidth(ansi.Strip(center))
	start := max(leftWidth+centerFooterSegmentValue3, (width-centerWidth)/layoutCenteringDivisor)
	if start+centerWidth > width {
		return left
	}
	return left + strings.Repeat(" ", start-leftWidth) + center
}

func (m *browser) appViewFooterHints() []keyHint {
	var hints []keyHint
	switch m.activeAppView {
	case appViewSummary:
		hints = []keyHint{{"↑↓", labelApps}, {hintEnter, hintOpen}, {hintPageScroll, hintScroll}, {"r", hintRefresh}}
	case appViewUsage:
		hints = m.usageFooterHints()
	case appViewLogs:
		hints = []keyHint{{"t", "next pod"}, {"v/V", "next / previous pod"}, {"f", "follow"}, {"/", hintSearch}, {"c", hintCopy}, {"p",
			"previous"}, {"m", hintMaximize}, {"End", "latest"}, {"r", hintRefresh}}
	case appViewBuilds:
		hints = m.buildsFooterHints()
	case appViewDomains:
		hints = m.domainsFooterHints()
	case appViewPorts:
		hints = m.portsFooterHints()
	case appViewEnv:
		hints = m.envFooterHints()
	default:
		return nil
	}
	hints = appendUniqueKeyHints(hints, keyHint{hintViews, "previous / next view"})
	if m.selectedAppActive() && len(m.actionMenuItems()) > 0 {
		hints = appendUniqueKeyHints(hints, keyHint{"a", "actions"})
	}
	return hints
}

func (m *browser) usageFooterHints() []keyHint {
	if m.resourcePlanPicker {
		return []keyHint{{hintVerticalSelect, hintChoosePlan}, {hintEnter, hintSelect}, {hintEscape, hintCancel}}
	}
	if m.resourceReview {
		return []keyHint{{hintEnterOrY, hintSave}, {hintEscOrNo, hintBack}}
	}
	if m.resourceEditing {
		return []keyHint{{hintTextType, labelValue}, {hintEnter, hintStage}, {hintCtrlU, hintClear}, {hintEscape, hintCancel}}
	}
	hints := []keyHint{{"w", "window"}, {"m", "metric"}}
	if len(historyPods(m.history)) > 0 {
		hints = append(hints, keyHint{"p", "pod filter"})
	}
	if m.allowWrites {
		hints = append(hints, keyHint{hintVerticalSelect, hintSelectSetting}, keyHint{"e", hintEdit}, keyHint{"s", hintReviewSave})
	}
	return append(hints, keyHint{hintPageScroll, hintScroll}, keyHint{"r", hintRefresh})
}

func (m *browser) buildsFooterHints() []keyHint {
	if m.cicdEditing {
		return []keyHint{{hintTextType, labelValue}, {hintEnter, hintStage}, {hintEscape, hintCancelEdit}}
	}
	if m.selectedApp().BuildMethod != buildModeCustom && m.selectedApp().BuildMethod != buildModeWebhook {
		return []keyHint{{"r", hintRefresh}}
	}
	hints := []keyHint{{"v", "reveal token"}, {"c", "copy token"}, {"y", "copy app ID"}}
	if m.allowWrites {
		hints = append(hints, keyHint{"t", "toggle deployment type"}, keyHint{"s", "review/save"})
	}
	if m.cicdSettingsDraft.BuildMethod == buildModeWebhook {
		hints = append(hints, keyHint{"↑↓", "settings"}, keyHint{hintTab, "history"}, keyHint{"r", hintRefresh})
		if m.allowWrites {
			hints = append(hints, keyHint{"e", "edit"}, keyHint{"+", "add arg"}, keyHint{"d", "delete arg"})
		}
	}
	if m.cicdHistoryFocus {
		hints = []keyHint{{"↑↓", "builds"}, {hintEnter, "output"}, {"Home/Tab", "settings"}, {"r", hintRefresh}}
	}
	if m.allowWrites && m.cicdSettingsChanged() {
		hints = append(hints, keyHint{"x", ticketComposeDiscard})
	}
	if len(buildArgs(m.selectedApp().GitBuildArgs)) > 0 {
		hints = append(hints, keyHint{"V", "reveal args"})
	}
	if m.buildDetail != nil {
		hints = []keyHint{{"/", hintSearch}, {hintNextPrevious, "match"}, {"c", hintCopy}, {hintEscape, hintList}, {"r", hintRefresh}}
	}
	return hints
}

func (m *browser) domainsFooterHints() []keyHint {
	hints := []keyHint{{"↑↓", hintSelect}, {"n", "copy name"}, {"y", "copy target"}, {"r", hintRefresh}, {"m", hintMaximize}}
	if m.allowWrites {
		hints = append(hints, keyHint{"+", "add external domain"}, keyHint{"d", "remove / clear"}, keyHint{"e",
			"edit setting or subdomain"}, keyHint{"s", hintReviewSave})
		if len(domainDiff(m.domainOriginal, m.domainConfig)) > 0 {
			hints = append(hints, keyHint{"x", hintDiscardStaged})
		}
	}
	return hints
}

func (m *browser) portsFooterHints() []keyHint {
	hints := []keyHint{{hintVerticalSelect, "row"}, {hintHorizontalMovement, "cell"}, {"c", "copy address"}, {"y", "copy cell"}, {"r",
		hintRefresh}, {"m", hintMaximize}}
	if m.allowWrites {
		hints = append(hints, keyHint{"e", hintEditToggle}, keyHint{"+", "add port"}, keyHint{"d", "delete port"}, keyHint{"s", hintReviewSave})
		if m.portsChanged() {
			hints = append(hints, keyHint{"x", hintDiscardStaged})
		}
	}
	return hints
}

func (m *browser) envFooterHints() []keyHint {
	hints := []keyHint{{"↑↓", hintSelect}, {"y", hintCopy}, {"v", "reveal"}, {"r", hintRefresh}}
	if m.allowWrites {
		hints = append(hints, keyHint{"e", "edit"}, keyHint{hintAddEnv, "add plain / secret"}, keyHint{"d", "stage delete"}, keyHint{"s",
			hintReviewSave})
		if m.envHasChanges() {
			hints = append(hints, keyHint{"x", "discard staged"})
		}
	}
	return hints
}

func fitKeyHints(hints []keyHint, width int) []keyHint {
	hints = append([]keyHint(nil), hints...)
	for len(hints) > 1 {
		parts := make([]string, len(hints))
		for i, hint := range hints {
			parts[i] = hint.key + " " + hint.action
		}
		if ansi.StringWidth(strings.Join(parts, " · ")) <= width {
			break
		}
		remove := -1
		for i, hint := range slices.Backward(hints) {
			switch hint.key {
			case "q", "?", hintEscape, "Esc/?", "q/Esc", hintEnter, hintTextType, hintClick, hintCtrlU, "y", hintNoEscape,
				"↑↓", hintVerticalSelect, "←→", hintHorizontalMovement, "r", "d", hintViews, "w", "p", hintPageScroll:
				continue
			default:
				remove = i
			}
			break
		}
		if remove < 0 {
			remove = len(hints) - fitKeyHintsValue2
		}
		hints = append(hints[:remove], hints[remove+1:]...)
	}
	return hints
}

// keyHints is the detailed shortcut registry used by the help modal. The
// footer selects a compact, view-specific subset; key dispatch remains in the
// update and focused modal handlers.

func (m *browser) keyHints(forHelp bool) []keyHint {
	hints := m.contextKeyHints(forHelp)
	if len(hints) == 0 {
		hints = m.defaultKeyHints(forHelp)
	}
	if forHelp {
		hints = appendUniqueKeyHints(hints, keyHint{hintEscape, hintCloseHelp}, keyHint{"?", hintCloseHelp}, keyHint{"q", hintQuit})
	}
	if m.showSelectedAppKeyHints(forHelp) {
		hints = m.appendSelectedAppKeyHints(hints, forHelp)
	}
	return hints
}

func (m *browser) contextKeyHints(forHelp bool) []keyHint {
	for _, state := range m.contextKeyHintStates(forHelp) {
		if state.active {
			return state.hints
		}
	}
	return nil
}

func (m *browser) contextKeyHintStates(forHelp bool) []modalFooterHintState {
	return []modalFooterHintState{
		{m.detail && m.activeAppView == appViewUsage && m.resourcePlanPicker, []keyHint{{hintVerticalSelect, hintChoosePlan}, {hintEnter,
			"select plan"}, {hintEscape, hintCancel}}},
		{m.detail && m.activeAppView == appViewUsage && m.resourceReview, []keyHint{{"↵/y", "save"}, {"Esc/n", "back"}}},
		{m.detail && m.activeAppView == appViewUsage && m.resourceEditing, []keyHint{{hintTextType, labelValue}, {hintEnter, hintStage},
			{hintCtrlU, hintClear}, {hintEscape, hintCancelEdit}}},
		{m.actionBusy, []keyHint{{"…", "action in progress"}, {"q", hintQuit}}},
		{m.buildActionMenu, []keyHint{{hintVerticalSelect, "choose build action"}, {hintEnter, hintSelect}, {hintClick, hintSelect},
			{hintEscape, hintClose}}},
		{m.buildConfirm != buildActionNone, []keyHint{{"←→", "cancel / confirm"}, {hintEnter, "continue"}, {hintEscape, hintCancel}}},
		{m.buildSearchEditing, []keyHint{{hintTextType, hintSearch}, {hintEnter, hintFind}, {hintCtrlU, hintClear}, {hintEscape, hintCancel}}},
		{m.actionMenu, m.actionMenuContextHints()},
		{m.confirmAction == appActionRestart, []keyHint{{"←→", "choose"}, {"y", "yes"}, {"n/Esc", "no"}, {"↵", "confirm"}}},
		{m.confirmAction == appActionDelete, []keyHint{{hintTextType, "app name"}, {hintTab, "Delete button"}, {hintClick, actionDelete},
			{hintEscape, hintCancel}}},
		{m.help && !forHelp, []keyHint{{"Esc/?", "close help"}, {"q", hintQuit}}},
		{m.filterEditing, []keyHint{{hintTextType, hintFilter}, {"↑↓", hintMove}, {hintEnter, hintOpen}, {hintEscape, hintCancel},
			{hintCtrlU, hintClear}}},
		{m.selectingNamespace, []keyHint{{hintTextType, hintFilter}, {"↑↓", hintMove}, {hintEnter, hintScope}, {hintEscape, hintClose},
			{hintCtrlU, hintClear}}},
		{m.logTargetPicker, []keyHint{{"↑↓", hintChoose}, {"←→", "column"}, {hintEnter, hintSelect}, {hintEscape, hintCancel}}},
		{m.logSearchEditing, []keyHint{{"type", "search"}, {"↵", "find"}, {hintEscape, hintCancel}, {"Ctrl+U", "clear"}}},
		{m.selectingTenant, []keyHint{{hintVerticalSelect, "switch tenant"}, {"r", "retry profile"}, {"Tab/ShiftTab", hintCycle},
			{hintPaneNumbers, hintPanes}, {"?", hintHelp}, {"q", hintQuit}}},
		{m.page == pageActivity, m.activityContextHints()},
		{m.contextFocused, []keyHint{{hintPaneNumbers, hintPanes}, {hintVerticalSelect, "control / switch tenant"}, {"Tab/ShiftTab",
			hintCycle}, {"→/l", "open Apps"}, {"n", hintScope}, {"/", hintFilter}, {"s", hintSort}, {"r", hintRefresh}, {"?", hintHelp},
			{"q", hintQuit}}},
	}
}

func (m *browser) actionMenuContextHints() []keyHint {
	hints := []keyHint{{hintVerticalSelect, hintChoose}}
	for _, action := range []struct {
		name appAction
		key  string
		text string
	}{{appActionRestart, "r", "restart"}, {appActionDelete, "d", "delete"}, {appActionTerminal, "t", "terminal"}} {
		if m.actionAvailable(action.name) {
			hints = append(hints, keyHint{action.key, action.text})
		}
	}
	return append(hints, keyHint{"↵", "select"}, keyHint{"q/Esc", "close"})
}

func (m *browser) activityContextHints() []keyHint {
	if m.detail {
		return []keyHint{{hintVerticalPageScroll, hintScrollDetails}, {hintEscape, hintList}, {hintHorizontalTabs, "switch feed"}, {"r",
			hintRefresh}}
	}
	return []keyHint{{hintVerticalSelect, "select alert"}, {hintEnter, hintDetails}, {hintHorizontalTabs, "switch feed"}, {"f",
		"status filter"}, {"s", hintSort}, {"/", hintFilter}, {"r", hintRefresh}}
}

func (m *browser) defaultKeyHints(forHelp bool) []keyHint {
	if forHelp {
		hints := []keyHint{
			{hintVerticalSelect, hintMove}, {hintHorizontalMovement, "tree / summary"},
			{hintPaneNumbers, "focus pane"}, {"Tab / Shift+Tab", "cycle panes"}, {"↵", "open / toggle"},
			{"Home/End", "first / last"}, {"g/G", "first / last"}, {hintPageScroll, hintScrollDetails},
			{hintEscape, "back / clear"}, {"n", "namespace scope"}, {"/", "fuzzy filter"}, {"s", hintSort},
		}
		if m.selectedAppActive() && len(m.actionMenuItems()) > 0 {
			hints = append(hints, keyHint{"a", "app actions"})
		}
		return hints
	}
	if m.width < keyHintsValue72 {
		if m.detail && m.selectedAppActive() {
			return []keyHint{{hintPageScroll, hintScroll}, {hintViews, "views"}, {"r", hintRefresh}, {hintEscape, labelApps}, {"?",
				hintHelp}, {"q", hintQuit}}
		}
		return []keyHint{{hintVerticalSelect, hintMove}, {"n", hintScope}, {"/", hintFilter}, {"?", hintHelp}, {"q", hintQuit}}
	}
	if m.width < keyHintsValue100 {
		if m.detail && m.selectedAppActive() {
			return []keyHint{{hintVerticalSelect, labelApps}, {hintPageScroll, hintScroll}, {hintViews, "views"}, {"r", hintRefresh},
				{hintEscape, hintBack}, {"?", hintHelp}, {"q", hintQuit}}
		}
		return []keyHint{
			{hintVerticalSelect, hintMove}, {hintPaneNumbers, hintPanes}, {hintHorizontalMovement, "tree"},
			{"n", "scope"}, {"/", hintFilter}, {"s", "sort"}, {"?", "help"}, {"q", "quit"},
		}
	}
	hints := []keyHint{
		{hintVerticalSelect, hintMove}, {hintPaneNumbers, hintPanes}, {"←→/hl", "tree"},
		{hintTab, "cycle"}, {"↵", "open"}, {"n", "scope"}, {"/", hintFilter},
		{"s", hintSort}, {"r", hintRefresh}, {"?", hintHelp}, {"q", hintQuit},
	}
	if m.selectedAppActive() && len(m.actionMenuItems()) > 0 {
		hints = append(hints[:5], append([]keyHint{{"a", "actions"}}, hints[5:]...)...)
	}
	return hints
}

func (m *browser) showSelectedAppKeyHints(forHelp bool) bool {
	return m.detail && m.selectedAppActive() && (forHelp || !m.help && !m.actionBusy && !m.actionMenu &&
		m.confirmAction == appActionNone && !m.filterEditing && !m.selectingNamespace && !m.selectingTenant)
}

func (m *browser) appendSelectedAppKeyHints(hints []keyHint, forHelp bool) []keyHint {
	hints = appendUniqueKeyHints(hints, keyHint{"[/]", "previous / next app view"})
	hints = m.appendAppViewKeyHints(hints, forHelp)
	if forHelp {
		for i := range hints {
			if hints[i].key == hintPageScroll {
				hints[i].action = "scroll details / move by page"
			}
		}
	}
	return hints
}

func (m *browser) appendAppViewKeyHints(hints []keyHint, forHelp bool) []keyHint {
	switch m.activeAppView {
	case appViewLogs:
		return m.appendLogKeyHints(hints)
	case appViewUsage:
		return m.appendUsageKeyHints(hints)
	case appViewSummary, appViewScaling:
		return appendUniqueKeyHints(hints, keyHint{hintPageScroll, hintScroll})
	case appViewBuilds:
		return m.appendBuildKeyHints(hints)
	case appViewEnv:
		if forHelp {
			return m.appendEnvKeyHints(hints)
		}
	case appViewDomains:
		if forHelp {
			return m.appendDomainKeyHints(hints)
		}
	case appViewPorts:
		if forHelp {
			return m.appendPortKeyHints(hints)
		}
	}
	return hints
}

func (m *browser) appendLogKeyHints(hints []keyHint) []keyHint {
	return appendUniqueKeyHints(hints,
		keyHint{"t/v", "next pod"}, keyHint{"V", "previous pod"}, keyHint{"f", "follow"},
		keyHint{"p", "current / previous"}, keyHint{"z", "local / UTC"}, keyHint{"w", "wrap"},
		keyHint{"-/+", "tail size"}, keyHint{"c", "copy retained logs"}, keyHint{"m", hintMaximizeRestore},
		keyHint{"/", hintSearch}, keyHint{hintNextPrevious, "next / previous match"}, keyHint{"r", hintRefresh},
		keyHint{"↑↓/PgUp/Dn", "scroll logs"}, keyHint{"End/G", "latest"})
}

func (m *browser) appendUsageKeyHints(hints []keyHint) []keyHint {
	hints = appendUniqueKeyHints(hints, keyHint{hintPageScroll, hintScroll}, keyHint{"w", "usage window"}, keyHint{"m", "metric"},
		keyHint{"p", "pod filter"})
	if m.allowWrites {
		hints = appendUniqueKeyHints(hints, keyHint{"↑↓/jk", "select setting"}, keyHint{"e", "edit / toggle"}, keyHint{"s", "review / save"})
		if changedAny(resourceConfigChanged(m.resourceOriginal, m.resourceDraft)) {
			hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged"})
		}
	}
	return hints
}

func (m *browser) appendBuildKeyHints(hints []keyHint) []keyHint {
	if m.selectedApp().BuildMethod != buildModeCustom && m.selectedApp().BuildMethod != buildModeWebhook {
		return appendUniqueKeyHints(hints, keyHint{"r", hintRefresh})
	}
	hints = appendUniqueKeyHints(hints, keyHint{"y", "copy app ID"}, keyHint{"v", "reveal / hide token"}, keyHint{"c", "copy token"})
	if m.allowWrites {
		hints = appendUniqueKeyHints(hints, keyHint{"t", "toggle deployment type"})
	}
	hints = appendUniqueKeyHints(hints, keyHint{"↑↓", "select build"}, keyHint{"↵", "view output"}, keyHint{"r", "refresh builds"},
		keyHint{"PgUp/Dn", hintScroll}, keyHint{hintTab, "settings / history"})
	if m.allowWrites {
		hints = appendUniqueKeyHints(hints, keyHint{"e", "edit setting"}, keyHint{"+", "add argument"}, keyHint{"d", "delete argument"},
			keyHint{"s", "review / save"})
	}
	if len(buildArgs(m.selectedApp().GitBuildArgs)) > 0 {
		hints = appendUniqueKeyHints(hints, keyHint{"V", "reveal / hide build arguments"})
	}
	if m.buildDetail != nil {
		hints = appendUniqueKeyHints(hints, keyHint{"/", "search output"}, keyHint{"n/N", "next / previous match"}, keyHint{"c",
			"copy build output"}, keyHint{hintEscape, "return to builds"})
	}
	if m.allowWrites && !m.cicdHistoryFocus && m.cicdSettingsChanged() {
		hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged settings"})
	}
	if m.allowWrites {
		hints = appendUniqueKeyHints(hints, keyHint{"a", "build actions"})
	}
	return hints
}

func (m *browser) appendEnvKeyHints(hints []keyHint) []keyHint {
	hints = appendUniqueKeyHints(hints, keyHint{hintVerticalMovement, "move through Plain and Secret"}, keyHint{"y",
		"copy selected value"}, keyHint{"v", "reveal / hide selected secret"}, keyHint{"r", hintRefresh}, keyHint{"m",
		hintMaximizeRestore}, keyHint{"PgUp/Dn", hintScroll})
	if m.allowWrites {
		hints = appendUniqueKeyHints(hints, keyHint{"e", "edit selected value"}, keyHint{"+ / A", "add Plain / Secret"}, keyHint{"d",
			"stage delete"}, keyHint{"s", hintReviewAndSave})
		if m.envHasChanges() {
			hints = appendUniqueKeyHints(hints, keyHint{"x", hintDiscardStagedChanges})
		}
	}
	return hints
}

func (m *browser) appendDomainKeyHints(hints []keyHint) []keyHint {
	hints = appendUniqueKeyHints(hints, keyHint{hintVerticalMovement, "move through domains, records and settings"}, keyHint{"n",
		"copy record name"}, keyHint{"y", "copy record target"}, keyHint{"r", "refresh DNS and certificates"}, keyHint{"m",
		"maximize / restore"})
	if m.allowWrites {
		hints = appendUniqueKeyHints(hints, keyHint{"+", "add external domain"}, keyHint{"d", "remove domain / clear subdomain"},
			keyHint{"e", "edit challenge, setting or subdomain"}, keyHint{"s", "review diff and save"})
		if len(domainDiff(m.domainOriginal, m.domainConfig)) > 0 {
			hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged changes"})
		}
	}
	return hints
}

func (m *browser) appendPortKeyHints(hints []keyHint) []keyHint {
	hints = appendUniqueKeyHints(hints, keyHint{"↑↓/jk", "move between access and port rows"}, keyHint{"←→/hl", "select a port cell"},
		keyHint{"c", "copy address"}, keyHint{"y", "copy selected cell"}, keyHint{"r", hintRefresh}, keyHint{"m", "maximize / restore"})
	if m.allowWrites {
		hints = appendUniqueKeyHints(hints, keyHint{"e", "edit cell or toggle protocol / external access"}, keyHint{"+", "add port"},
			keyHint{"d", "delete port"}, keyHint{"s", "review diff and save"})
		if m.portsChanged() {
			hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged changes"})
		}
	}
	return hints
}

func appendUniqueKeyHints(hints []keyHint, extras ...keyHint) []keyHint {
	for _, extra := range extras {
		found := false
		for i, hint := range hints {
			if hint.key == extra.key {
				hints[i].action = extra.action
				found = true
				break
			}
		}
		if !found {
			hints = append(hints, extra)
		}
	}
	return hints
}
