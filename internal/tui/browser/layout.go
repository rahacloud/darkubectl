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
		left := m.appPaneLines(bodyHeight)
		right := m.summaryPaneLines()
		if m.activeAppView == appViewUsage {
			right = m.usageViewport(right, bodyHeight)
		}
		context := m.contextPaneLines(bodyHeight)
		lines = append(lines, m.splitPanes(context, left, right, bodyHeight)...)
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
	} else if m.help && m.width >= 60 && m.height >= 20 {
		lines = m.helpModal(lines)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *browser) appPaneLines(height int) []string {
	heading := "  [3] APPS"
	if !m.detail {
		heading = "▸ [3] APPS"
	}
	if m.appsState.active() {
		heading += " " + paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	lines := []string{paint(heading, output.ColorAccent, true)}
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

func (m *browser) appViewTabs() string {
	focus := "  "
	if m.detail {
		focus = "▸ "
	}
	if !m.selectedAppActive() {
		label := "SUMMARY"
		if m.activeAppView == appViewUsage {
			label = "USAGE"
		} else if m.activeAppView == appViewScaling {
			label = "SCALING"
		}
		return paint(focus+"[4] "+label, output.ColorAccent, true)
	}
	selectedStyle := func(label string, selected bool) string {
		if selected {
			return paint("["+label+"]", output.ColorSuccess, true)
		}
		return paint(label, output.ColorMuted, false)
	}
	heading := paint(focus+"[4] ", output.ColorAccent, true) + selectedStyle("Summary", m.activeAppView == appViewSummary) + paint("  ", output.ColorBorder, false) + selectedStyle("Usage", m.activeAppView == appViewUsage) + paint("  ", output.ColorBorder, false) + selectedStyle("Scaling", m.activeAppView == appViewScaling)
	if m.detailState.active() || m.usageState.active() || m.podsState.active() {
		heading += " " + paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	return heading
}

func (m *browser) paneWidths() (int, int, int) {
	contextWidth := min(28, max(18, m.width/5))
	available := max(3, m.width-contextWidth-6)
	appsWidth := available / 2
	return appsWidth, available - appsWidth, contextWidth
}

func (m *browser) splitPanes(context, apps, summary []string, height int) []string {
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	if m.detail && (m.activeAppView == appViewSummary || m.activeAppView == appViewScaling) {
		summary = m.summaryViewport(summary, height)
	}
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		appLine, summaryLine, contextLine := "", "", ""
		if i < len(apps) {
			appLine = apps[i]
		}
		if i < len(summary) {
			summaryLine = summary[i]
		}
		if i < len(context) {
			contextLine = context[i]
		}
		contextLine = ansi.Truncate(contextLine, contextWidth, "…")
		appLine = ansi.Truncate(appLine, appsWidth, "…")
		summaryLine = ansi.Truncate(summaryLine, summaryWidth, "…")
		contextLine += strings.Repeat(" ", max(0, contextWidth-ansi.StringWidth(contextLine)))
		appLine += strings.Repeat(" ", max(0, appsWidth-ansi.StringWidth(appLine)))
		summaryLine += strings.Repeat(" ", max(0, summaryWidth-ansi.StringWidth(summaryLine)))
		dividerOne := paint("│", output.ColorBorder, false)
		if m.contextFocused {
			dividerOne = paint("┃", output.ColorBranch, true)
		}
		dividerTwo := paint("│", output.ColorBorder, false)
		if m.detail {
			dividerTwo = paint("┃", output.ColorBranch, true)
		}
		lines[i] = contextLine + " " + dividerOne + " " + appLine + " " + dividerTwo + " " + summaryLine
	}
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
		if m.activeAppView == appViewUsage || m.activeAppView == appViewSummary || m.activeAppView == appViewScaling {
			hints = appendUniqueKeyHints(hints, keyHint{"PgUp/Dn", "scroll"})
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
