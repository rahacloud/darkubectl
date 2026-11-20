package browser

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	helpModalValue2                 = 2
	helpModalValue6                 = 6
	namespaceChoiceRowValue20       = 20
	namespaceChoiceRowValue8        = 8
	namespaceModalValue10           = 10
	namespaceModalValue2            = 2
	namespaceModalValue6            = 6
	namespaceModalValue72           = 72
	namespaceModalOuterMargin       = 4
	namespaceModalVerticalMargin    = 12
	helpModalColumnGap              = 3
	helpHintGroupCapacity           = 4
	namespaceChoiceMinimumMetaWidth = 8
	namespaceChoiceNameRightPadding = 8
	namespaceChoiceRowPadding       = 6
	namespaceChoiceMetaWidthDivisor = 3
)

func (m *browser) namespaceModal(background []string) []string {
	panelWidth := min(namespaceModalValue72, m.width-namespaceModalOuterMargin)
	innerWidth := panelWidth - namespaceModalValue6
	choices := m.namespaceChoices()
	content := []string{
		paint("⎈  NAMESPACE SCOPE", browserAccent, true),
		"Filter by namespace, cluster, or ID  " + paint(m.namespaceQuery+"▏", browserBranch, true),
		paint(strings.Repeat("─", innerWidth), browserBorder, false),
	}
	if m.namespaceState.active() {
		content = append(content, m.loadingMessage("Loading namespace inventory…"))
	}
	if m.namespaceState.err != nil && m.namespaceState.status != requestStale {
		content = append(content, paint("Namespace inventory failed: "+safeText(m.namespaceState.err.Error()), browserNumber, false))
	} else if m.namespaceState.status == requestStale {
		content = append(content, paint("Cached inventory · refresh failed", browserNumber, false), staleRequestTime("Namespaces",
			m.namespaceState))
	}
	if m.namespaceFallback {
		message := "Inventory unavailable · showing namespaces with apps"
		if strings.HasPrefix(m.mode, "MOCK") {
			message = "Using mock app namespaces · empty projects need explicit fixture inventory"
		}
		content = append(content, paint(message, browserNumber, false))
	}
	capacity := max(1, min(namespaceModalValue10, m.height-namespaceModalVerticalMargin))
	first := max(0, min(m.namespaceCursor-capacity+1, len(choices)-capacity))
	last := min(len(choices), first+capacity)
	for i := first; i < last; i++ {
		content = append(content, m.namespaceChoiceRow(choices[i], i, innerWidth))
	}
	if len(choices) == 0 {
		content = append(content, paint("No namespaces match this filter.", browserMuted, false))
	}
	if first > 0 || last < len(choices) {
		content = append(content, paint(fmt.Sprintf("Showing %d–%d of %d", first+1, last, len(choices)), browserMuted, false))
	}
	panel := tenantPanelStyle().Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/namespaceModalValue2)
	startX := max(0, (m.width-panelWidth)/namespaceModalValue2)
	for i, line := range panelLines {
		y := startY + i
		if y >= len(background) {
			break
		}
		if width := ansi.StringWidth(line); width < panelWidth {
			line += strings.Repeat(" ", panelWidth-width)
		}
		base := ansi.Truncate(background[y], m.width, "")
		base += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(base)))
		background[y] = ansi.Cut(base, 0, startX) + line + ansi.Cut(base, startX+panelWidth, m.width)
	}
	return background[:m.height]
}

func (m *browser) helpModal(background []string) []string {
	panelWidth := min(helpModalMaxWidth, m.width-browserModalMargin)
	innerWidth := panelWidth - helpModalValue6
	row := func(leftKey, leftLabel, rightKey, rightLabel string) string {
		left := paint(leftKey, browserBranch, true) + " " + paint(leftLabel, browserMuted, false)
		right := paint(rightKey, browserBranch, true) + " " + paint(rightLabel, browserMuted, false)
		columnWidth := (innerWidth - helpModalColumnGap) / helpModalValue2
		left = ansi.Truncate(left, columnWidth, "…")
		left += strings.Repeat(" ", max(1, columnWidth-ansi.StringWidth(left)))
		return left + "   " + ansi.Truncate(right, columnWidth, "…")
	}
	content := []string{
		paint("⌘  QUICK HELP", browserAccent, true),
		paint(strings.Repeat("─", innerWidth), browserBorder, false),
	}
	groups := m.helpHintGroups(m.keyHints(true))
	for groupIndex, group := range groups {
		if len(group.hints) == 0 {
			continue
		}
		if groupIndex > 0 {
			content = append(content, "")
		}
		content = append(content, paint(strings.ToUpper(group.title), browserKey, true))
		for i := 0; i < len(group.hints); i += 2 {
			left := group.hints[i]
			if i+1 == len(group.hints) {
				content = append(content, row(left.key, left.action, "", ""))
				continue
			}
			right := group.hints[i+1]
			content = append(content, row(left.key, left.action, right.key, right.action))
		}
	}
	panel := tenantPanelStyle().Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/helpModalValue2)
	startX := max(0, (m.width-panelWidth)/helpModalValue2)
	for i, line := range panelLines {
		y := startY + i
		if y >= len(background) {
			break
		}
		if width := ansi.StringWidth(line); width < panelWidth {
			line += strings.Repeat(" ", panelWidth-width)
		}
		base := ansi.Truncate(background[y], m.width, "")
		base += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(base)))
		background[y] = ansi.Cut(base, 0, startX) + line + ansi.Cut(base, startX+panelWidth, m.width)
	}
	return background[:m.height]
}

type helpHintGroup struct {
	title string
	hints []keyHint
}

func (m *browser) helpHintGroups(hints []keyHint) []helpHintGroup {
	groups := make([]helpHintGroup, 0, helpHintGroupCapacity)
	groups = append(groups, helpHintGroup{title: "Navigation"}, helpHintGroup{title: "App views"})
	viewTitle := "Current view"
	switch m.activeAppView {
	case appViewSummary:
		viewTitle = "Summary view"
	case appViewUsage:
		viewTitle = "Resources view"
	case appViewScaling:
	case appViewLogs:
		viewTitle = "Logs view"
	case appViewBuilds:
		viewTitle = "CI/CD view"
	case appViewEnv:
		viewTitle = "Environment view"
	case appViewDomains:
		viewTitle = "Domains view"
	case appViewPorts:
		viewTitle = "Ports view"
	}
	groups = append(groups, helpHintGroup{title: viewTitle}, helpHintGroup{title: "Actions"})
	for _, hint := range hints {
		switch {
		case hint.action == "previous / next app view":
			groups[1].hints = append(groups[1].hints, hint)
		case m.currentViewHelpHint(hint):
			groups[2].hints = append(groups[2].hints, hint)
		case hint.action == hintQuit || hint.action == hintCloseHelp || hint.action == hintHelp:
			groups[3].hints = append(groups[3].hints, hint)
		default:
			groups[0].hints = append(groups[0].hints, hint)
		}
	}
	return groups
}

func (m *browser) currentViewHelpHint(hint keyHint) bool {
	if !m.detail || !m.selectedAppActive() {
		return false
	}
	switch m.activeAppView {
	case appViewLogs:
		return isLogsHelpHint(hint)
	case appViewBuilds:
		return isBuildsHelpHint(hint)
	case appViewEnv:
		return isEnvHelpHint(hint)
	case appViewDomains:
		return isDomainsHelpHint(hint)
	case appViewPorts:
		return isPortsHelpHint(hint)
	case appViewUsage:
		return isResourcesHelpHint(hint)
	default:
		return hint.action == hintScrollDetails
	}
}

func isResourcesHelpHint(h keyHint) bool {
	switch h.key {
	case hintVerticalSelect, "e", "s", "x", "w", "m", "p", hintPageScroll, hintTextType, "Enter/y", hintEscOrNo, hintCtrlU, hintEnter:
		return true
	}
	return h.action == hintScrollDiff || h.action == hintEditToggle || h.action == hintReviewSave || h.action == hintSelectSetting
}

func isPortsHelpHint(hint keyHint) bool {
	switch hint.key {
	case hintVerticalSelect, hintHorizontalMovement, "c", "y", "r", "m", "e", "+", "d", "s", "x", hintTextType:
		return true
	}
	return hint.action == hintScrollDiff
}

func isBuildsHelpHint(hint keyHint) bool {
	switch hint.key {
	case "↑↓", "↵", "/", hintNextPrevious, "c", hintEscape, "r", "a", "v", "y":
		return true
	}
	return hint.action == "builds" || hint.action == "output" || hint.action == hintSearch || hint.action == "match" ||
		hint.action == hintCopy || hint.action == hintList || hint.action == hintRefresh || hint.action == hintBuildActions
}

func isLogsHelpHint(hint keyHint) bool {
	switch hint.key {
	case "t", "v/V", "f", "p", "z", "m", "n/N", "w", "End/G":
		return true
	default:
		return hint.action == hintSearch || hint.action == "scroll logs" || hint.action == hintRefresh || hint.action == "target"
	}
}

func isEnvHelpHint(hint keyHint) bool {
	switch hint.key {
	case hintVerticalMovement, "e", hintAddEnv, "d", "s", "x", "y", "v", "r", "m", hintPageScroll:
		return true
	default:
		return hint.action == "scroll details / move by page"
	}
}

func isDomainsHelpHint(hint keyHint) bool {
	switch hint.key {
	case "↑↓", "↑/↓", "+", "d", "e", "s", "x", "r", "m", "n", "y":
		return true
	}
	return hint.action == "select domain or setting" || hint.action == "add domain" || hint.action == "remove domain" ||
		hint.action == "edit selected setting" || hint.action == hintReviewSave || hint.action == hintMaximize
}

func (m *browser) namespaceChoiceRow(choice namespaceChoice, index, width int) string {
	marker := " "
	if index == m.namespaceCursor {
		marker = "›"
	}
	name := "All namespaces"
	meta := fmt.Sprintf("%d apps", len(m.apps))
	if choice.scoped {
		name = choice.key.name
		for _, namespace := range m.namespaces {
			if namespace.ID == choice.key.id && namespace.Name == choice.key.name {
				name = namespace.Name
				if namespace.Cluster.Name != "" {
					name += " · " + namespace.Cluster.Name
				}
				name += fmt.Sprintf(" · ID %d", namespace.ID)
				break
			}
		}
		count := m.namespaceAppCount(choice.key)
		meta = fmt.Sprintf("%d apps", count)
		if count == 0 {
			meta += " · empty"
		}
	}
	check := " "
	if choice.scoped == m.namespaceScoped && (!choice.scoped || choice.key == m.namespaceScope) {
		check = paint("✓", browserSuccess, true)
	}
	metaWidth := min(namespaceChoiceRowValue20, max(namespaceChoiceMinimumMetaWidth, width/namespaceChoiceMetaWidthDivisor))
	nameWidth := max(namespaceChoiceRowValue8, width-metaWidth-namespaceChoiceNameRightPadding)
	name = ansi.Truncate(safeText(name), nameWidth, "…")
	meta = ansi.Truncate(meta, metaWidth, "…")
	plain := fmt.Sprintf(" %s %s%s%s%s", marker, name, strings.Repeat(" ", max(1,
		width-ansi.StringWidth(name)-ansi.StringWidth(meta)-namespaceChoiceRowPadding)), meta, check)
	plain = ansi.Truncate(plain, width, "")
	plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
	if index == m.namespaceCursor {
		return selectedRowStyle().Width(width).Render(plain)
	}
	return " " + paint(marker, browserBranch, true) + " " + paint(name, browserKey, false) + strings.Repeat(" ", max(1,
		width-ansi.StringWidth(name)-ansi.StringWidth(meta)-namespaceChoiceRowPadding)) + paint(meta, browserMuted, false) + "  " + check
}
