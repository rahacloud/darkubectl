package browser

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const browserFooterHeight = 1

func (m *browser) View() tea.View {
	var lines []string
	footer := m.footerLine()
	if m.width < 60 || m.height < 20 {
		lines = []string{"Resize terminal to at least 60 x 20"}
	} else {
		header := m.breadcrumb()
		rightIndicators := ""
		if m.page != pageActivity && m.page != pageApps {
			if m.page == pageDB {
				rightIndicators = m.sortLine()
			} else {
				rightIndicators = m.namespaceTopLine()
			}
		}
		leftWidth := max(0, m.width-ansi.StringWidth(rightIndicators)-1)
		header = ansi.Truncate(header, leftWidth, "…")
		padding := max(1, m.width-ansi.StringWidth(header)-ansi.StringWidth(rightIndicators))
		header += strings.Repeat(" ", padding) + rightIndicators
		lines = append(lines, header, "")
		if m.actionNotice != "" {
			lines = append(lines, paint(safeText(m.actionNotice), browserNumber, false))
		}
		if m.profileState.status == requestFailed || m.profileState.status == requestStale {
			lines = append(lines, "Account profile unavailable; tenant apps remain available. Press t then r to retry profile.")
			if m.profileState.status == requestStale {
				lines = append(lines, staleRequestTime("Profile", m.profileState))
			}
		}
		bodyHeight := max(1, m.height-len(lines)-browserFooterHeight-1)
		paneHeight := max(1, bodyHeight-2)
		left := m.appPaneLines(paneHeight)
		right := m.summaryPaneLines()
		if m.page == pageActivity {
			left = m.activityPaneLines(paneHeight)
			right = m.activityDetailLines(paneHeight)
			lines = append(lines, m.splitActivityPanes(left, right, bodyHeight)...)
		} else if m.page == pageRegistry {
			left = m.registryPaneLines(paneHeight)
			right = m.registryDetailLines(paneHeight)
			lines = append(lines, m.splitPanes(left, right, bodyHeight)...)
		} else if m.page == pageDB {
			left = m.databasePaneLines(paneHeight)
			right = m.databaseDetailLines(paneHeight)
			if m.width < 100 {
				lines = append(lines, m.splitDatabaseCompact(left, right, bodyHeight)...)
			} else {
				lines = append(lines, m.splitPanes(left, right, bodyHeight)...)
			}
		} else {
			if m.activeAppView == appViewUsage {
				right = m.usageViewport(right, paneHeight)
			} else if m.activeAppView == appViewLogs {
				right = m.logsViewport(right, paneHeight)
			} else if m.activeAppView == appViewEnv {
				right = m.summaryViewport(right, paneHeight)
			} else if m.activeAppView == appViewBuilds {
				right = m.summaryViewport(right, paneHeight)
			} else if m.activeAppView == appViewDomains {
				right = m.summaryViewport(right, paneHeight)
			} else if m.activeAppView == appViewPorts {
				viewHeight := paneHeight
				if m.portMaximized {
					viewHeight = max(1, bodyHeight-2)
				}
				right = m.summaryViewport(right, viewHeight)
			}
			if m.detail && m.appViewMaximized() {
				lines = append(lines, renderPane(right, max(1, m.width-2), bodyHeight-2, m.appViewBorderTitle(m.width-2), true)...)
			} else {
				lines = append(lines, m.splitPanes(left, right, bodyHeight)...)
			}
		}
	}
	if len(lines) > m.height-browserFooterHeight {
		lines = lines[:max(0, m.height-browserFooterHeight)]
	}
	for len(lines) < max(0, m.height-browserFooterHeight) {
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	if m.buildActionMenu && m.width >= 60 && m.height >= 20 {
		lines = m.renderBuildActionMenu(lines)
	} else if m.buildConfirm != buildActionNone && m.width >= 60 && m.height >= 20 {
		lines = m.renderBuildActionConfirmation(lines)
	} else if m.actionMenu && m.width >= 60 && m.height >= 20 {
		lines = m.renderActionMenu(lines)
	} else if m.confirmAction != appActionNone && m.width >= 60 && m.height >= 20 {
		lines = m.renderActionConfirmation(lines)
	} else if m.selectingNamespace && m.width >= 60 && m.height >= 20 {
		lines = m.namespaceModal(lines)
	} else if m.logTargetPicker && m.terminalChoosing && m.width >= 60 && m.height >= 20 {
		lines = m.logTargetModal(lines)
	} else if m.help && m.width >= 60 && m.height >= 20 {
		lines = m.helpModal(lines)
	} else if m.registryTagMode && m.width >= 60 && m.height >= 20 {
		lines = m.registryTagOverlay(lines)
	} else if m.registryConfirm && m.width >= 60 && m.height >= 20 {
		lines = m.registryConfirmation(lines)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "")
		lines[i] += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(lines[i])))
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
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
	if width < 24 {
		label = "[3]"
	} else if width < 36 {
		label = "[3] APPS"
	}
	fullSort := m.sortLine()
	compactSort := compactSortIndicator(m.sortDescending)
	controls := m.appPaneScopeControl(width) + " " + m.appFilterBorderLine()
	if width < 36 {
		fullSort = compactSort
	}
	return borderTitleWithControls(label, controls, fullSort, compactSort, width)
}

func (m *browser) appPaneScopeControl(width int) string {
	if width >= 42 {
		return m.namespaceTopLine()
	}
	if width < 24 {
		return paint("⎈", browserMuted, true)
	}
	name := "All"
	if m.namespaceScoped {
		name = m.namespaceScope.name
	}
	return paint("⎈", browserMuted, true) + " " + paint(ansi.Truncate(safeText(name), 5, "…"), browserBranch, true)
}

func (m *browser) activityPaneBorderTitle(label string, width int) string {
	if width < 24 {
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
	available := max(1, width-6)
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
	labels := []string{"Summary", "Resources", "Logs", "CI/CD", "Env", "Domains", "Ports"}
	if width < 58 {
		labels = []string{"Sum", "Res", "Logs", "CI/CD", "Env", "Domains", "Ports"}
	}
	if width < 42 {
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
	}
	return false
}

func (m *browser) paneWidths() (int, int, int) {
	contextWidth := min(28, max(18, m.width/5))
	available := max(3, m.width-contextWidth-8)
	appsWidth := available / 2
	return appsWidth, available - appsWidth, contextWidth
}

func (m *browser) splitPanes(apps, summary []string, height int) []string {
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	if m.detail && (m.activeAppView == appViewSummary || m.activeAppView == appViewScaling) {
		summary = m.summaryViewport(summary, max(1, height-2))
	}
	contextSeparator := 1
	tenantHeight := max(4, (height-contextSeparator)/2)
	pagesHeight := max(4, height-contextSeparator-tenantHeight)
	tenant := renderPane(m.tenantPaneLines(max(1, tenantHeight-2)), contextWidth, tenantHeight-2, "[1] TENANTS", m.contextFocused && m.contextRow == 0)
	pages := renderPane(m.pagesPaneLines(max(1, pagesHeight-2)), contextWidth, pagesHeight-2, "[2] PAGES", m.contextFocused && m.contextRow > 0)
	contextPane := append(tenant, strings.Repeat(" ", contextWidth+2))
	contextPane = append(contextPane, pages...)
	appTitle := "[3] APPS"
	if m.appsState.active() {
		appTitle += " " + m.spinnerGlyph()
	}
	if m.page == pageApps {
		appTitle = m.appsPaneBorderTitle(appTitle, appsWidth)
	}
	appsPane := renderPane(apps, appsWidth, height-2, appTitle, !m.contextFocused && !m.detail)
	viewTitle := m.appViewBorderTitle(summaryWidth)
	if m.page == pageRegistry {
		appTitle = "[3] REGISTRY TREE"
		viewTitle = "[4] IMAGE DETAILS"
		if len(m.registryRows) > 0 && m.registryRows[m.registryCursor].digest >= 0 {
			viewTitle = "[4] MANIFEST DETAILS"
		}
		appsPane = renderPane(apps, appsWidth, height-2, appTitle, !m.contextFocused && !m.detail)
	}
	if m.page == pageDB {
		appTitle = "[3] DATABASES"
		viewTitle = "[4] DATABASE DETAILS"
		appTitle = borderTitleWithControls(appTitle, m.databaseFilterLine(), "", "", appsWidth)
		appsPane = renderPane(apps, appsWidth, height-2, appTitle, !m.contextFocused && !m.detail)
	}
	viewPane := renderPane(summary, summaryWidth, height-2, viewTitle, m.detail)
	lines := make([]string, height)
	for i := range lines {
		left, center, right := contextPane[i], appsPane[i], viewPane[i]
		lines[i] = left + " " + center + " " + right
	}
	return lines
}

func (m *browser) splitDatabaseCompact(list, details []string, height int) []string {
	_, _, contextWidth := m.paneWidths()
	contentWidth := max(1, m.width-contextWidth-4)
	tenantHeight := max(4, (height-1)/2)
	pagesHeight := max(4, height-1-tenantHeight)
	tenant := renderPane(m.tenantPaneLines(max(1, tenantHeight-2)), contextWidth, tenantHeight-2, "[1] TENANTS", m.contextFocused && m.contextRow == 0)
	pages := renderPane(m.pagesPaneLines(max(1, pagesHeight-2)), contextWidth, pagesHeight-2, "[2] PAGES", m.contextFocused && m.contextRow > 0)
	context := append(tenant, strings.Repeat(" ", contextWidth+2))
	context = append(context, pages...)
	content, title := list, "[3] DATABASES"
	focused := !m.contextFocused && !m.detail
	if m.detail {
		content, title, focused = details, "[4] DATABASE DETAILS", true
	}
	if !m.detail {
		title = borderTitleWithControls(title, m.databaseFilterLine(), "", "", contentWidth)
	}
	pane := renderPane(content, contentWidth, height-2, title, focused)
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
	tenantHeight := max(4, (height-contextSeparator)/2)
	pagesHeight := max(4, height-contextSeparator-tenantHeight)
	tenant := renderPane(m.tenantPaneLines(max(1, tenantHeight-2)), contextWidth, tenantHeight-2, "[1] TENANTS", m.contextFocused && m.contextRow == 0)
	pages := renderPane(m.pagesPaneLines(max(1, pagesHeight-2)), contextWidth, pagesHeight-2, "[2] PAGES", m.contextFocused && m.contextRow > 0)
	context := append(tenant, strings.Repeat(" ", contextWidth+2))
	context = append(context, pages...)
	listTitle := "[3] ALERTS"
	if m.activityTab == activityNotifications {
		listTitle = "[3] NOTIFICATIONS"
	}
	listTitle = m.activityPaneBorderTitle(listTitle, listWidth)
	listPane := renderPane(list, listWidth, height-2, listTitle, !m.contextFocused && !m.detail)
	detailTitle := "[4] ALERT DETAILS"
	if m.activityTab == activityNotifications {
		detailTitle = "[4] MESSAGE"
	}
	detailPane := renderPane(details, detailWidth, height-2, detailTitle, m.detail)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = context[i] + " " + listPane[i] + " " + detailPane[i]
	}
	return lines
}

func renderPane(content []string, width, height int, title string, focused bool) []string {
	borderColor := browserBorder
	if focused {
		borderColor = browserBranch
	}
	innerWidth := max(1, width)
	// Keep ANSI styling out of the measured border string. Embedded styles
	// around title controls can confuse terminal width accounting and break the
	// top edge in some terminal emulators.
	title = ansi.Truncate(ansi.Strip(title), max(1, innerWidth-3), "…")
	top := "╭─ " + title + " " + strings.Repeat("─", max(0, innerWidth-ansi.StringWidth(title)-3)) + "╮"
	lines := make([]string, 0, height+2)
	lines = append(lines, paint(top, borderColor, focused))
	for row := 0; row < height; row++ {
		line := ""
		if row < len(content) {
			line = content[row]
		}
		line = ansi.Truncate(line, innerWidth, "…")
		line += strings.Repeat(" ", max(0, innerWidth-ansi.StringWidth(line)))
		lines = append(lines, paint("│", borderColor, focused)+line+paint("│", borderColor, focused))
	}
	lines = append(lines, paint("╰"+strings.Repeat("─", innerWidth)+"╯", borderColor, focused))
	return lines
}

func (m *browser) summaryViewport(lines []string, height int) []string {
	if len(lines) <= height || height <= 0 {
		m.summaryScroll = 0
		return lines
	}
	stickyLines := 3
	if m.activeAppView == appViewUsage {
		stickyLines = 1
	} else if m.activeAppView == appViewEnv {
		stickyLines = 1
	} else if m.activeAppView == appViewDomains {
		stickyLines = 1
	} else if m.activeAppView == appViewPorts {
		stickyLines = 1
	} else if m.activeAppView == appViewBuilds && m.buildDetail != nil {
		stickyLines = buildDetailStickyLines(lines)
	} else if m.activeAppView == appViewBuilds {
		stickyLines = 0
	}
	sticky := min(stickyLines, min(len(lines), height))
	bodyHeight := max(0, height-sticky)
	maxScroll := max(0, len(lines)-sticky-bodyHeight)
	if m.activeAppView == appViewEnv && !m.envConfirm && bodyHeight > 0 && m.envCursorLine >= sticky {
		selectedRow := m.envCursorLine - sticky
		if selectedRow < m.summaryScroll {
			m.summaryScroll = selectedRow
		} else if selectedRow >= m.summaryScroll+bodyHeight {
			m.summaryScroll = selectedRow - bodyHeight + 1
		}
	}
	if m.activeAppView == appViewDomains && !m.domainConfirm && bodyHeight > 0 && m.domainCursorLine >= sticky {
		selectedRow := m.domainCursorLine - sticky
		if selectedRow < m.summaryScroll {
			m.summaryScroll = selectedRow
		} else if selectedRow >= m.summaryScroll+bodyHeight {
			m.summaryScroll = selectedRow - bodyHeight + 1
		}
	}
	if m.activeAppView == appViewPorts && !m.portConfirm && bodyHeight > 0 && m.portCursorLine >= sticky {
		selectedRow := m.portCursorLine - sticky
		if selectedRow < m.summaryScroll {
			m.summaryScroll = selectedRow
		} else if selectedRow >= m.summaryScroll+bodyHeight {
			m.summaryScroll = selectedRow - bodyHeight + 1
		}
	}
	if m.activeAppView == appViewBuilds && m.selectedAppActive() && m.selectedApp().BuildMethod == "webhook" && !m.cicdHistoryFocus && !m.cicdReview && bodyHeight > 0 {
		selectedRow := max(0, m.cicdCursorLine-sticky)
		if selectedRow < m.summaryScroll {
			m.summaryScroll = selectedRow
		} else if selectedRow >= m.summaryScroll+bodyHeight {
			m.summaryScroll = selectedRow - bodyHeight + 1
		}
	}
	m.summaryScroll = min(m.summaryScroll, maxScroll)
	end := min(len(lines), sticky+m.summaryScroll+bodyHeight)
	out := append([]string(nil), lines[:sticky]...)
	return append(out, lines[sticky+m.summaryScroll:end]...)
}

func (m *browser) footerLine() string {
	quit := paint("q", browserBranch, true) + " " + paint("quit", browserMuted, false)
	help := paint("?", browserBranch, true) + " " + paint("help", browserMuted, false)
	left := quit + paint(" · ", browserBorder, false) + help
	modalHints, modal := m.modalFooterHints()
	if m.contextFocused && m.contextRow == 0 {
		left += paint(" · ", browserBorder, false) + paint("r", browserBranch, true) + " " + paint("retry profile", browserMuted, false)
		return left
	}
	if !modal && m.selectedAppActive() {
		actionLabel := "actions"
		if m.detail && m.activeAppView == appViewBuilds && m.selectedApp().BuildMethod == "webhook" {
			actionLabel = "build actions"
		}
		left += paint(" · ", browserBorder, false) + paint("a", browserBranch, true) + " " + paint(actionLabel, browserMuted, false)
	}
	if modal {
		available := max(0, m.width-ansi.StringWidth(ansi.Strip(left))-3)
		modalHints = fitKeyHints(modalHints, available)
		return centerFooterSegment(left, footerHintText(modalHints), m.width)
	}
	if m.page == pageActivity {
		hints := []keyHint{{"↑↓/jk", "select"}, {"↵", "details"}, {"h/l", "tab"}, {"f", "status"}, {"s", "sort"}, {"/", "filter"}, {"r", "refresh"}}
		if m.detail {
			hints = []keyHint{{"↑↓/PgUp/Dn", "scroll"}, {"Esc", "list"}, {"h/l", "tab"}, {"r", "refresh"}}
		}
		available := max(0, m.width-ansi.StringWidth(ansi.Strip(left))-3)
		right := footerHintText(fitKeyHints(hints, available))
		if ansi.StringWidth(ansi.Strip(left))+ansi.StringWidth(ansi.Strip(right))+3 > m.width {
			return left
		}
		return left + strings.Repeat(" ", max(1, m.width-ansi.StringWidth(ansi.Strip(left))-ansi.StringWidth(ansi.Strip(right)))) + right
	}
	if m.page == pageRegistry {
		hints := []keyHint{{"↑↓/jk", "select"}, {"←→/↵", "collapse / load"}, {"y", "copy image reference"}, {"r", "refresh"}}
		if m.allowWrites {
			hints = append(hints, keyHint{"d", "delete image/digest"}, keyHint{"t", "remove tag"})
		}
		if m.registryConfirm {
			hints = []keyHint{{"type delete", "confirm target"}, {"Esc", "cancel"}}
		} else if m.registryTagMode {
			if m.registryTagCopy {
				hints = []keyHint{{"↑↓", "choose tag"}, {"↵", "copy reference"}, {"Esc", "cancel"}}
			} else {
				hints = []keyHint{{"↑↓", "choose tag"}, {"↵", "remove selected"}, {"Esc", "cancel"}}
			}
		}
		available := max(0, m.width-ansi.StringWidth(ansi.Strip(left))-3)
		right := footerHintText(fitKeyHints(hints, available))
		if m.registryConfirm {
			return centerFooterSegment(left, right, m.width)
		}
		if ansi.StringWidth(ansi.Strip(left))+ansi.StringWidth(ansi.Strip(right))+3 > m.width {
			return left
		}
		return left + strings.Repeat(" ", max(1, m.width-ansi.StringWidth(ansi.Strip(left))-ansi.StringWidth(ansi.Strip(right)))) + right
	}
	if m.page == pageDB {
		hints := []keyHint{{"↑↓/jk", "select"}, {"↵", "details"}, {"s", "sort"}, {"/", "filter"}, {"r", "refresh"}}
		if m.detail {
			hints = []keyHint{{"↑↓/PgUp/Dn", "scroll"}, {"Esc", "list"}, {"y", "copy URI"}, {"r", "refresh"}}
			if databaseHasStandby(m.databaseDetail) {
				hints = append(hints, keyHint{"m/b", "master/standby"})
			}
		}
		available := max(0, m.width-ansi.StringWidth(ansi.Strip(left))-3)
		right := footerHintText(fitKeyHints(hints, available))
		if ansi.StringWidth(ansi.Strip(left))+ansi.StringWidth(ansi.Strip(right))+3 > m.width {
			return left
		}
		return left + strings.Repeat(" ", max(1, m.width-ansi.StringWidth(ansi.Strip(left))-ansi.StringWidth(ansi.Strip(right)))) + right
	}
	if !m.detail || !m.selectedAppActive() {
		return left
	}

	available := max(0, m.width-ansi.StringWidth(ansi.Strip(left))-3)
	hints := fitKeyHints(m.appViewFooterHints(), available)
	if len(hints) == 0 {
		return left
	}
	right := footerHintText(hints)
	if ansi.StringWidth(ansi.Strip(left))+ansi.StringWidth(ansi.Strip(right))+3 > m.width {
		return left
	}
	return left + strings.Repeat(" ", max(1, m.width-ansi.StringWidth(ansi.Strip(left))-ansi.StringWidth(ansi.Strip(right)))) + right
}

func (m *browser) modalFooterHints() ([]keyHint, bool) {
	switch {
	case m.cicdReview:
		return []keyHint{{"y/Enter", "save"}, {"n/Esc", "back"}}, true
	case m.actionBusy:
		return []keyHint{{"…", "action in progress"}}, true
	case m.actionMenu:
		return []keyHint{{"Esc", "close"}}, true
	case m.buildActionMenu:
		return []keyHint{{"↑↓", "choose"}, {"↵", "select"}, {"Click", "select"}, {"Esc", "close"}}, true
	case m.buildConfirm != buildActionNone:
		return []keyHint{{"←→", "cancel / confirm"}, {"↵", "continue"}, {"Esc", "cancel"}}, true
	case m.confirmAction == appActionRestart:
		return []keyHint{{"y", "restart"}, {"n", "cancel"}, {"↵", "confirm"}, {"Esc", "cancel"}}, true
	case m.confirmAction == appActionDelete:
		return []keyHint{{"type", "app name"}, {"Tab", "delete"}, {"Click", "delete"}, {"Esc", "cancel"}}, true
	case m.help:
		return []keyHint{{"Shift+drag", "select text"}, {"terminal shortcut", "copy"}, {"Esc", "close help"}}, true
	case m.filterEditing:
		return []keyHint{{"type", "filter"}, {"↵", "open"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}, true
	case m.activityFilterEditing:
		return []keyHint{{"type", "filter"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}, true
	case m.selectingNamespace:
		return []keyHint{{"type", "filter"}, {"↵", "scope"}, {"Click", "scope"}, {"Ctrl+U", "clear"}, {"Esc", "close"}}, true
	case m.terminalChoosing:
		return []keyHint{{"↵", "connect"}, {"Esc", "cancel"}}, true
	case m.logTargetPicker:
		return []keyHint{{"↵", "select target"}, {"Esc", "close"}}, true
	case m.logSearchEditing:
		return []keyHint{{"type", "search"}, {"↵", "find"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}, true
	case m.buildSearchEditing:
		return []keyHint{{"type", "search"}, {"↵", "find"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}, true
	case m.sorting:
		return []keyHint{{"↵", "apply sort"}, {"Esc", "close"}}, true
	case m.envConfirm:
		return []keyHint{{"↑↓/PgUp/Dn", "scroll diff"}, {"y", "save"}, {"n/Esc", "back"}}, true
	case m.detail && m.activeAppView == appViewDomains && m.domainConfirm:
		return []keyHint{{"PgUp/Dn", "scroll diff"}, {"Enter", "save"}, {"Esc", "back"}}, true
	case m.detail && m.activeAppView == appViewDomains && m.domainEditing:
		return []keyHint{{"Enter", "stage"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}, true
	case m.detail && m.activeAppView == appViewPorts && m.portConfirm:
		return []keyHint{{"↑↓", "scroll diff"}, {"Enter", "save"}, {"Esc", "back"}}, true
	case m.detail && m.activeAppView == appViewPorts && m.portEditing:
		return []keyHint{{"type", "value"}, {"Enter", "stage cell"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}, true
	case m.detail && m.activeAppView == appViewUsage && m.resourceReview:
		return []keyHint{{"Enter/y", "save"}, {"Esc/n", "back"}}, true
	case m.detail && m.activeAppView == appViewUsage && m.resourceEditing:
		return []keyHint{{"type", "value"}, {"Enter", "stage"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}, true
	case m.detail && m.activeAppView == appViewUsage && m.resourcePlanPicker:
		return []keyHint{{"↑↓/jk", "choose plan"}, {"Enter", "select"}, {"Esc", "cancel"}}, true
	case m.envEditMode != envEditNone:
		return []keyHint{{"↵", "stage"}, {"Ctrl+U", "clear"}, {"Esc", "cancel edit"}}, true
	default:
		return nil, false
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
	start := max(leftWidth+3, (width-centerWidth)/2)
	if start+centerWidth > width {
		return left
	}
	return left + strings.Repeat(" ", start-leftWidth) + center
}

func (m *browser) appViewFooterHints() []keyHint {
	switch m.activeAppView {
	case appViewSummary:
		return []keyHint{{"↑↓", "apps"}, {"↵", "open"}, {"PgUp/Dn", "scroll"}, {"r", "refresh"}}
	case appViewUsage:
		if m.resourcePlanPicker {
			return []keyHint{{"↑↓/jk", "choose plan"}, {"Enter", "select"}, {"Esc", "cancel"}}
		}
		if m.resourceReview {
			return []keyHint{{"Enter/y", "save"}, {"Esc/n", "back"}}
		}
		if m.resourceEditing {
			return []keyHint{{"type", "value"}, {"Enter", "stage"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}
		}
		hints := []keyHint{{"w", "window"}, {"m", "metric"}}
		if len(historyPods(m.history)) > 0 {
			hints = append(hints, keyHint{"p", "pod filter"})
		}
		if m.allowWrites {
			hints = append(hints, keyHint{"↑↓/jk", "select setting"}, keyHint{"e", "edit"}, keyHint{"s", "review / save"})
		}
		return append(hints, keyHint{"PgUp/Dn", "scroll"}, keyHint{"r", "refresh"})
	case appViewLogs:
		return []keyHint{{"t", "next pod"}, {"v/V", "next / previous pod"}, {"f", "follow"}, {"/", "search"}, {"c", "copy"}, {"p", "previous"}, {"m", "maximize"}, {"End", "latest"}, {"r", "refresh"}}
	case appViewBuilds:
		if m.cicdEditing {
			return []keyHint{{"type", "value"}, {"Enter", "stage"}, {"Esc", "cancel edit"}}
		}
		if m.selectedApp().BuildMethod != "gitlabci" && m.selectedApp().BuildMethod != "webhook" {
			return []keyHint{{"r", "refresh"}}
		}
		hints := []keyHint{{"v", "reveal token"}, {"c", "copy token"}, {"y", "copy app ID"}}
		if m.allowWrites {
			hints = append(hints, keyHint{"t", "toggle deployment type"}, keyHint{"s", "review/save"})
		}
		if m.cicdSettingsDraft.BuildMethod == "webhook" {
			hints = append(hints, keyHint{"↑↓", "settings"}, keyHint{"Tab", "history"}, keyHint{"r", "refresh"})
			if m.allowWrites {
				hints = append(hints, keyHint{"e", "edit"}, keyHint{"+", "add arg"}, keyHint{"d", "delete arg"})
			}
		}
		if m.cicdHistoryFocus {
			hints = []keyHint{{"↑↓", "builds"}, {"↵", "output"}, {"Home/Tab", "settings"}, {"r", "refresh"}}
		}
		if m.allowWrites && m.cicdSettingsChanged() {
			hints = append(hints, keyHint{"x", "discard"})
		}
		if len(buildArgs(m.selectedApp().GitBuildArgs)) > 0 {
			hints = append(hints, keyHint{"V", "reveal args"})
		}
		if m.buildDetail != nil {
			hints = []keyHint{{"/", "search"}, {"n/N", "match"}, {"c", "copy"}, {"Esc", "list"}, {"r", "refresh"}}
		}
		return hints
	case appViewDomains:
		hints := []keyHint{{"↑↓", "select"}, {"n", "copy name"}, {"y", "copy target"}, {"r", "refresh"}, {"m", "maximize"}}
		if m.allowWrites {
			hints = append(hints, keyHint{"+", "add external domain"}, keyHint{"d", "remove / clear"}, keyHint{"e", "edit setting or subdomain"}, keyHint{"s", "review / save"})
			if len(domainDiff(m.domainOriginal, m.domainConfig)) > 0 {
				hints = append(hints, keyHint{"x", "discard staged"})
			}
		}
		return hints
	case appViewPorts:
		hints := []keyHint{{"↑↓/jk", "row"}, {"←→/hl", "cell"}, {"c", "copy address"}, {"y", "copy cell"}, {"r", "refresh"}, {"m", "maximize"}}
		if m.allowWrites {
			hints = append(hints, keyHint{"e", "edit / toggle"}, keyHint{"+", "add port"}, keyHint{"d", "delete port"}, keyHint{"s", "review / save"})
			if m.portsChanged() {
				hints = append(hints, keyHint{"x", "discard staged"})
			}
		}
		return hints
	case appViewEnv:
		hints := []keyHint{{"↑↓", "select"}, {"y", "copy"}, {"v", "reveal"}, {"r", "refresh"}}
		if m.allowWrites {
			hints = append(hints, keyHint{"e", "edit"}, keyHint{"+ / A", "add plain / secret"}, keyHint{"d", "stage delete"}, keyHint{"s", "review / save"})
			if m.envHasChanges() {
				hints = append(hints, keyHint{"x", "discard staged"})
			}
		}
		return hints
	default:
		return nil
	}
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
		for i := len(hints) - 1; i >= 0; i-- {
			switch hints[i].key {
			case "q", "?", "Esc", "Esc/?", "q/Esc", "↵", "type", "Click", "Ctrl+U", "y", "n/Esc",
				"↑↓", "↑↓/jk", "←→", "←→/hl", "r", "d", "[/]", "w", "p", "PgUp/Dn":
				continue
			default:
				remove = i
			}
			break
		}
		if remove < 0 {
			remove = len(hints) - 2
		}
		hints = append(hints[:remove], hints[remove+1:]...)
	}
	return hints
}

// keyHints is the detailed shortcut registry used by the help modal. The
// footer selects a compact, view-specific subset; key dispatch remains in the
// update and focused modal handlers.

func (m *browser) keyHints(forHelp bool) []keyHint {
	var hints []keyHint
	switch {
	case m.detail && m.activeAppView == appViewUsage && m.resourcePlanPicker:
		hints = []keyHint{{"↑↓/jk", "choose plan"}, {"↵", "select plan"}, {"Esc", "cancel"}}
	case m.detail && m.activeAppView == appViewUsage && m.resourceReview:
		hints = []keyHint{{"↵/y", "save"}, {"Esc/n", "back"}}
	case m.detail && m.activeAppView == appViewUsage && m.resourceEditing:
		hints = []keyHint{{"type", "value"}, {"↵", "stage"}, {"Ctrl+U", "clear"}, {"Esc", "cancel edit"}}
	case m.actionBusy:
		hints = []keyHint{{"…", "action in progress"}, {"q", "quit"}}
	case m.buildActionMenu:
		hints = []keyHint{{"↑↓/jk", "choose build action"}, {"↵", "select"}, {"Click", "select"}, {"Esc", "close"}}
	case m.buildConfirm != buildActionNone:
		hints = []keyHint{{"←→", "cancel / confirm"}, {"↵", "continue"}, {"Esc", "cancel"}}
	case m.buildSearchEditing:
		hints = []keyHint{{"type", "search"}, {"↵", "find"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}
	case m.actionMenu:
		hints = []keyHint{{"↑↓/jk", "choose"}}
		if m.actionAvailable(appActionRestart) {
			hints = append(hints, keyHint{"r", "restart"})
		}
		if m.actionAvailable(appActionDelete) {
			hints = append(hints, keyHint{"d", "delete"})
		}
		if m.actionAvailable(appActionTerminal) {
			hints = append(hints, keyHint{"t", "terminal"})
		}
		hints = append(hints, keyHint{"↵", "select"}, keyHint{"q/Esc", "close"})
	case m.confirmAction == appActionRestart:
		hints = []keyHint{{"←→", "choose"}, {"y", "yes"}, {"n/Esc", "no"}, {"↵", "confirm"}}
	case m.confirmAction == appActionDelete:
		hints = []keyHint{{"type", "app name"}, {"Tab", "Delete button"}, {"Click", "delete"}, {"Esc", "cancel"}}
	case m.help && !forHelp:
		hints = []keyHint{{"Esc/?", "close help"}, {"q", "quit"}}
	case m.filterEditing:
		hints = []keyHint{{"type", "filter"}, {"↑↓", "move"}, {"↵", "open"}, {"Esc", "cancel"}, {"Ctrl+U", "clear"}}
	case m.selectingNamespace:
		hints = []keyHint{{"type", "filter"}, {"↑↓", "move"}, {"↵", "scope"}, {"Esc", "close"}, {"Ctrl+U", "clear"}}
	case m.logTargetPicker:
		hints = []keyHint{{"↑↓", "choose"}, {"←→", "column"}, {"↵", "select"}, {"Esc", "cancel"}}
	case m.logSearchEditing:
		hints = []keyHint{{"type", "search"}, {"↵", "find"}, {"Esc", "cancel"}, {"Ctrl+U", "clear"}}
	case m.selectingTenant:
		hints = []keyHint{{"↑↓/jk", "switch tenant"}, {"r", "retry profile"}, {"Tab/ShiftTab", "cycle"}, {"1-4", "panes"}, {"?", "help"}, {"q", "quit"}}
	case m.page == pageActivity:
		if m.detail {
			hints = []keyHint{{"↑↓/PgUp/Dn", "scroll details"}, {"Esc", "list"}, {"h/l", "switch feed"}, {"r", "refresh"}}
		} else {
			hints = []keyHint{{"↑↓/jk", "select alert"}, {"↵", "details"}, {"h/l", "switch feed"}, {"f", "status filter"}, {"s", "sort"}, {"/", "filter"}, {"r", "refresh"}}
		}
	case m.contextFocused:
		hints = []keyHint{{"1-4", "panes"}, {"↑↓/jk", "control / switch tenant"}, {"Tab/ShiftTab", "cycle"}, {"→/l", "open Apps"}, {"n", "scope"}, {"/", "filter"}, {"s", "sort"}, {"r", "refresh"}, {"?", "help"}, {"q", "quit"}}
	}
	if len(hints) == 0 {
		if forHelp {
			hints = []keyHint{
				{"↑↓/jk", "move"}, {"←→/hl", "tree / summary"},
				{"1-4", "focus pane"}, {"Tab / Shift+Tab", "cycle panes"}, {"↵", "open / toggle"},
				{"Home/End", "first / last"}, {"g/G", "first / last"}, {"PgUp/Dn", "scroll details / move by page"},
				{"Esc", "back / clear"}, {"n", "namespace scope"}, {"/", "fuzzy filter"}, {"s", "sort"},
			}
			if m.selectedAppActive() {
				hints = append(hints, keyHint{"a", "app actions"})
			}
		} else if m.width < 72 {
			if m.detail && m.selectedAppActive() {
				hints = []keyHint{{"PgUp/Dn", "scroll"}, {"[/]", "views"}, {"r", "refresh"}, {"Esc", "Apps"}, {"?", "help"}, {"q", "quit"}}
			} else {
				hints = []keyHint{{"↑↓/jk", "move"}, {"n", "scope"}, {"/", "filter"}, {"?", "help"}, {"q", "quit"}}
			}
		} else if m.width < 100 {
			if m.detail && m.selectedAppActive() {
				hints = []keyHint{{"↑↓/jk", "apps"}, {"PgUp/Dn", "scroll"}, {"[/]", "views"}, {"r", "refresh"}, {"Esc", "back"}, {"?", "help"}, {"q", "quit"}}
			} else {
				hints = []keyHint{
					{"↑↓/jk", "move"}, {"1-4", "panes"}, {"←→/hl", "tree"},
					{"n", "scope"}, {"/", "filter"}, {"s", "sort"}, {"?", "help"}, {"q", "quit"},
				}
			}
		} else {
			hints = []keyHint{
				{"↑↓/jk", "move"}, {"1-4", "panes"}, {"←→/hl", "tree"},
				{"Tab", "cycle"}, {"↵", "open"}, {"n", "scope"}, {"/", "filter"},
				{"s", "sort"}, {"r", "refresh"}, {"?", "help"}, {"q", "quit"},
			}
			if m.selectedAppActive() {
				hints = append(hints[:5], append([]keyHint{{"a", "actions"}}, hints[5:]...)...)
			}
		}
	}
	if forHelp {
		hints = appendUniqueKeyHints(hints, keyHint{"Esc", "close help"}, keyHint{"?", "close help"}, keyHint{"q", "quit"})
	}
	if m.detail && m.selectedAppActive() && (forHelp || !m.help && !m.actionBusy && !m.actionMenu && m.confirmAction == appActionNone && !m.filterEditing && !m.selectingNamespace && !m.selectingTenant) {
		hints = appendUniqueKeyHints(hints, keyHint{"[/]", "previous / next app view"})
		switch m.activeAppView {
		case appViewLogs:
			hints = appendUniqueKeyHints(hints,
				keyHint{"t/v", "next pod"}, keyHint{"V", "previous pod"}, keyHint{"f", "follow"},
				keyHint{"p", "current / previous"}, keyHint{"z", "local / UTC"}, keyHint{"w", "wrap"},
				keyHint{"-/+", "tail size"}, keyHint{"c", "copy retained logs"}, keyHint{"m", "maximize / restore"},
				keyHint{"/", "search"}, keyHint{"n/N", "next / previous match"}, keyHint{"r", "refresh"},
				keyHint{"↑↓/PgUp/Dn", "scroll logs"}, keyHint{"End/G", "latest"})
		case appViewUsage:
			hints = appendUniqueKeyHints(hints, keyHint{"PgUp/Dn", "scroll"}, keyHint{"w", "usage window"}, keyHint{"m", "metric"}, keyHint{"p", "pod filter"})
			if m.allowWrites {
				hints = appendUniqueKeyHints(hints, keyHint{"↑↓/jk", "select setting"}, keyHint{"e", "edit / toggle"}, keyHint{"s", "review / save"})
				if changedAny(resourceConfigChanged(m.resourceOriginal, m.resourceDraft)) {
					hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged"})
				}
			}
		case appViewSummary, appViewScaling:
			hints = appendUniqueKeyHints(hints, keyHint{"PgUp/Dn", "scroll"})
		case appViewBuilds:
			if m.selectedApp().BuildMethod != "gitlabci" && m.selectedApp().BuildMethod != "webhook" {
				hints = appendUniqueKeyHints(hints, keyHint{"r", "refresh"})
				break
			}
			hints = appendUniqueKeyHints(hints, keyHint{"y", "copy app ID"}, keyHint{"v", "reveal / hide token"}, keyHint{"c", "copy token"})
			if m.allowWrites {
				hints = appendUniqueKeyHints(hints, keyHint{"t", "toggle deployment type"})
			}
			hints = appendUniqueKeyHints(hints, keyHint{"↑↓", "select build"}, keyHint{"↵", "view output"}, keyHint{"r", "refresh builds"}, keyHint{"PgUp/Dn", "scroll"})
			hints = appendUniqueKeyHints(hints, keyHint{"Tab", "settings / history"})
			if m.allowWrites {
				hints = appendUniqueKeyHints(hints, keyHint{"e", "edit setting"}, keyHint{"+", "add argument"}, keyHint{"d", "delete argument"}, keyHint{"s", "review / save"})
			}
			if len(buildArgs(m.selectedApp().GitBuildArgs)) > 0 {
				hints = appendUniqueKeyHints(hints, keyHint{"V", "reveal / hide build arguments"})
			}
			if m.buildDetail != nil {
				hints = appendUniqueKeyHints(hints, keyHint{"/", "search output"}, keyHint{"n/N", "next / previous match"}, keyHint{"c", "copy build output"}, keyHint{"Esc", "return to builds"})
			}
			if m.allowWrites && !m.cicdHistoryFocus && m.cicdSettingsChanged() {
				hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged settings"})
			}
			if m.allowWrites {
				hints = appendUniqueKeyHints(hints, keyHint{"a", "build actions"})
			}
		case appViewEnv:
			if forHelp {
				hints = appendUniqueKeyHints(hints,
					keyHint{"↑/↓", "move through Plain and Secret"},
					keyHint{"y", "copy selected value"}, keyHint{"v", "reveal / hide selected secret"},
					keyHint{"r", "refresh"}, keyHint{"m", "maximize / restore"}, keyHint{"PgUp/Dn", "scroll"})
				if m.allowWrites {
					hints = appendUniqueKeyHints(hints,
						keyHint{"e", "edit selected value"},
						keyHint{"+ / A", "add Plain / Secret"},
						keyHint{"d", "stage delete"},
						keyHint{"s", "review diff and save"})
					if m.envHasChanges() {
						hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged changes"})
					}
				}
			}
		case appViewDomains:
			if forHelp {
				hints = appendUniqueKeyHints(hints, keyHint{"↑/↓", "move through domains, records and settings"}, keyHint{"n", "copy record name"}, keyHint{"y", "copy record target"}, keyHint{"r", "refresh DNS and certificates"}, keyHint{"m", "maximize / restore"})
				if m.allowWrites {
					hints = appendUniqueKeyHints(hints, keyHint{"+", "add external domain"}, keyHint{"d", "remove domain / clear subdomain"}, keyHint{"e", "edit challenge, setting or subdomain"}, keyHint{"s", "review diff and save"})
					if len(domainDiff(m.domainOriginal, m.domainConfig)) > 0 {
						hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged changes"})
					}
				}
			}
		case appViewPorts:
			if forHelp {
				hints = appendUniqueKeyHints(hints, keyHint{"↑↓/jk", "move between access and port rows"}, keyHint{"←→/hl", "select a port cell"}, keyHint{"c", "copy address"}, keyHint{"y", "copy selected cell"}, keyHint{"r", "refresh"}, keyHint{"m", "maximize / restore"})
				if m.allowWrites {
					hints = appendUniqueKeyHints(hints, keyHint{"e", "edit cell or toggle protocol / external access"}, keyHint{"+", "add port"}, keyHint{"d", "delete port"}, keyHint{"s", "review diff and save"})
					if m.portsChanged() {
						hints = appendUniqueKeyHints(hints, keyHint{"x", "discard staged changes"})
					}
				}
			}
		}
		if forHelp {
			for i := range hints {
				if hints[i].key == "PgUp/Dn" {
					hints[i].action = "scroll details / move by page"
				}
			}
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
