package browser

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/output"
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
		if m.filterQuery != "" || m.filterEditing {
			header += "   " + m.filterLine()
		}
		rightIndicators := m.sortLine()
		if namespaceIndicator := m.namespaceTopLine(); namespaceIndicator != "" {
			rightIndicators = namespaceIndicator + "   " + rightIndicators
		}
		leftWidth := max(0, m.width-ansi.StringWidth(rightIndicators)-1)
		header = ansi.Truncate(header, leftWidth, "…")
		padding := max(1, m.width-ansi.StringWidth(header)-ansi.StringWidth(rightIndicators))
		header += strings.Repeat(" ", padding) + rightIndicators
		lines = append(lines, header, "")
		if m.actionNotice != "" {
			lines = append(lines, paint(safeText(m.actionNotice), output.ColorNumber, false))
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
		if m.activeAppView == appViewUsage {
			right = m.usageViewport(right, paneHeight)
		} else if m.activeAppView == appViewLogs {
			right = m.logsViewport(right, paneHeight)
		}
		if m.detail && m.activeAppView == appViewLogs && m.logMaximized {
			lines = append(lines, renderPane(right, max(1, m.width-2), bodyHeight-2, m.appViewBorderTitle(m.width-2), true)...)
		} else {
			lines = append(lines, m.splitPanes(left, right, bodyHeight)...)
		}
	}
	if len(lines) > m.height-browserFooterHeight {
		lines = lines[:max(0, m.height-browserFooterHeight)]
	}
	for len(lines) < max(0, m.height-browserFooterHeight) {
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	if m.actionMenu && m.width >= 60 && m.height >= 20 {
		lines = m.renderActionMenu(lines)
	} else if m.confirmAction != appActionNone && m.width >= 60 && m.height >= 20 {
		lines = m.renderActionConfirmation(lines)
	} else if m.selectingNamespace && m.width >= 60 && m.height >= 20 {
		lines = m.namespaceModal(lines)
	} else if m.logTargetPicker && m.width >= 60 && m.height >= 20 {
		lines = m.logTargetModal(lines)
	} else if m.help && m.width >= 60 && m.height >= 20 {
		lines = m.helpModal(lines)
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
			lines = append(lines, paint("Stale · refresh failed: "+safeText(m.appsState.err.Error()), output.ColorNumber, false))
			lines = append(lines, staleRequestTime("Apps", m.appsState))
		}
	}
	if m.appsState.status == requestUnavailable && len(m.apps) == 0 {
		lines = append(lines, paint("App listing is unavailable.", output.ColorMuted, false))
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
	for i, label := range labels {
		if appView(i) == m.activeAppView {
			parts[i] = paint("["+label+"]", output.ColorBranch, true)
		} else {
			parts[i] = paint(label, output.ColorBranch, false)
		}
	}
	return paint("[4] ", output.ColorAccent, true) + strings.Join(parts, " ")
}

func appViewTabLabels(width int) []string {
	labels := []string{"Summary", "Usage", "Scaling", "Logs"}
	if width < 35 {
		labels = []string{"Sum", "Use", "Scale", "Logs"}
	}
	if width < 27 {
		labels = []string{"S", "U", "Sc", "L"}
	}
	return labels
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
	pages := renderPane(m.pagesPaneLines(max(1, pagesHeight-2)), contextWidth, pagesHeight-2, "[2] PAGES", m.contextFocused && m.contextRow == 1)
	contextPane := append(tenant, strings.Repeat(" ", contextWidth+2))
	contextPane = append(contextPane, pages...)
	appTitle := "[3] APPS"
	if m.appsState.active() {
		appTitle += " " + m.spinnerGlyph()
	}
	appsPane := renderPane(apps, appsWidth, height-2, appTitle, !m.contextFocused && !m.detail)
	viewPane := renderPane(summary, summaryWidth, height-2, m.appViewBorderTitle(summaryWidth), m.detail)
	lines := make([]string, height)
	for i := range lines {
		left, center, right := contextPane[i], appsPane[i], viewPane[i]
		lines[i] = left + " " + center + " " + right
	}
	return lines
}

func renderPane(content []string, width, height int, title string, focused bool) []string {
	borderColor := output.ColorBorder
	if focused {
		borderColor = output.ColorBranch
	}
	innerWidth := max(1, width)
	title = ansi.Truncate(title, max(1, innerWidth-3), "…")
	titleText := title
	if ansi.Strip(titleText) == titleText {
		titleText = paint(titleText, borderColor, focused)
	}
	top := paint("╭─ ", borderColor, focused) + titleText + paint(" "+strings.Repeat("─", max(0, innerWidth-ansi.StringWidth(title)-3))+"╮", borderColor, focused)
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
	if m.activeAppView == appViewScaling {
		stickyLines = 2
	}
	sticky := min(stickyLines, min(len(lines), height))
	bodyHeight := max(0, height-sticky)
	maxScroll := max(0, len(lines)-sticky-bodyHeight)
	m.summaryScroll = min(m.summaryScroll, maxScroll)
	end := min(len(lines), sticky+m.summaryScroll+bodyHeight)
	out := append([]string(nil), lines[:sticky]...)
	return append(out, lines[sticky+m.summaryScroll:end]...)
}

func (m *browser) footerLine() string {
	quit := paint("q", output.ColorBranch, true) + " " + paint("quit", output.ColorMuted, false)
	help := paint("?", output.ColorBranch, true) + " " + paint("help", output.ColorMuted, false)
	return quit + paint(" · ", output.ColorBorder, false) + help
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

// keyHints is the presentation registry used by the footer and help modal.
// Key dispatch remains in Update and the focused modal handlers.

func (m *browser) keyHints(forHelp bool) []keyHint {
	var hints []keyHint
	switch {
	case m.actionBusy:
		hints = []keyHint{{"…", "action in progress"}, {"q", "quit"}}
	case m.actionMenu:
		hints = []keyHint{{"↑↓/jk", "choose"}, {"r", "restart"}, {"d", "delete"}, {"↵", "select"}, {"q/Esc", "close"}}
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
				{"1", "Tenant · arrows switch now"},
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
				keyHint{"t", "target"}, keyHint{"v/V", "next / previous pod"}, keyHint{"f", "follow"},
				keyHint{"p", "current / previous"}, keyHint{"z", "local / UTC"}, keyHint{"w", "wrap"},
				keyHint{"-/+", "tail size"}, keyHint{"c", "copy retained logs"}, keyHint{"m", "maximize / restore"},
				keyHint{"/", "search"}, keyHint{"n/N", "next / previous match"}, keyHint{"r", "refresh"},
				keyHint{"↑↓/PgUp/Dn", "scroll logs"}, keyHint{"End/G", "latest"})
		case appViewUsage, appViewSummary, appViewScaling:
			hints = appendUniqueKeyHints(hints, keyHint{"PgUp/Dn", "scroll"})
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
