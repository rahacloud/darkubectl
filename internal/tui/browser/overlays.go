package browser

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/output"
	"strings"
)

func (m *browser) namespaceModal(background []string) []string {
	panelWidth := min(72, m.width-4)
	innerWidth := panelWidth - 6
	choices := m.namespaceChoices()
	content := []string{
		paint("⎈  NAMESPACE SCOPE", output.ColorAccent, true),
		"Filter by namespace, cluster, or ID: " + paint(m.namespaceQuery+"▏", output.ColorBranch, true),
		paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
	}
	if m.namespaceState.active() {
		content = append(content, m.loadingMessage("Loading namespace inventory…"))
	}
	if m.namespaceState.err != nil && m.namespaceState.status != requestStale {
		content = append(content, paint("Namespace inventory failed: "+safeText(m.namespaceState.err.Error()), output.ColorNumber, false))
	} else if m.namespaceState.status == requestStale {
		content = append(content, paint("Cached inventory · refresh failed", output.ColorNumber, false), staleRequestTime("Namespaces", m.namespaceState))
	}
	if m.namespaceFallback {
		message := "Inventory unavailable · showing namespaces with apps"
		if strings.HasPrefix(m.mode, "MOCK") {
			message = "Using mock app namespaces · empty projects need explicit fixture inventory"
		}
		content = append(content, paint(message, output.ColorNumber, false))
	}
	capacity := max(1, min(10, m.height-12))
	first := max(0, min(m.namespaceCursor-capacity+1, len(choices)-capacity))
	last := min(len(choices), first+capacity)
	for i := first; i < last; i++ {
		content = append(content, m.namespaceChoiceRow(choices[i], i, innerWidth))
	}
	if len(choices) == 0 {
		content = append(content, paint("No namespaces match this filter.", output.ColorMuted, false))
	}
	if first > 0 || last < len(choices) {
		content = append(content, paint(fmt.Sprintf("Showing %d–%d of %d", first+1, last, len(choices)), output.ColorMuted, false))
	}
	content = append(content, "", "↑/↓ move  ·  ↵ select", "Type to filter  ·  Esc close")
	panel := tenantPanelStyle().Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/2)
	startX := max(0, (m.width-panelWidth)/2)
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
	innerWidth := panelWidth - 6
	row := func(leftKey, leftLabel, rightKey, rightLabel string) string {
		left := paint(leftKey, output.ColorBranch, true) + " " + paint(leftLabel, output.ColorMuted, false)
		right := paint(rightKey, output.ColorBranch, true) + " " + paint(rightLabel, output.ColorMuted, false)
		columnWidth := (innerWidth - 3) / 2
		left = ansi.Truncate(left, columnWidth, "…")
		left += strings.Repeat(" ", max(1, columnWidth-ansi.StringWidth(left)))
		return left + "   " + ansi.Truncate(right, columnWidth, "…")
	}
	content := []string{
		paint("⌘  QUICK HELP", output.ColorAccent, true),
		paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
	}
	groups := m.helpHintGroups(m.keyHints(true))
	for groupIndex, group := range groups {
		if len(group.hints) == 0 {
			continue
		}
		if groupIndex > 0 {
			content = append(content, "")
		}
		content = append(content, paint(strings.ToUpper(group.title), output.ColorKey, true))
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
	content = append(content, "", paint("Select text: Shift+drag · Copy: terminal copy shortcut", output.ColorMuted, false))
	panel := tenantPanelStyle().Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/2)
	startX := max(0, (m.width-panelWidth)/2)
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
	groups := []helpHintGroup{{title: "Navigation"}, {title: "App views"}}
	viewTitle := "Current view"
	switch m.activeAppView {
	case appViewSummary:
		viewTitle = "Summary view"
	case appViewUsage:
		viewTitle = "Usage view"
	case appViewScaling:
		viewTitle = "Scaling view"
	case appViewLogs:
		viewTitle = "Logs view"
	}
	groups = append(groups, helpHintGroup{title: viewTitle}, helpHintGroup{title: "Actions"})
	for _, hint := range hints {
		switch {
		case hint.action == "previous / next app view":
			groups[1].hints = append(groups[1].hints, hint)
		case m.detail && m.selectedAppActive() && (m.activeAppView == appViewLogs && isLogsHelpHint(hint) || m.activeAppView != appViewLogs && hint.action == "scroll details / move by page"):
			groups[2].hints = append(groups[2].hints, hint)
		case hint.action == "quit" || hint.action == "close help" || hint.action == "help":
			groups[3].hints = append(groups[3].hints, hint)
		default:
			groups[0].hints = append(groups[0].hints, hint)
		}
	}
	return groups
}

func isLogsHelpHint(hint keyHint) bool {
	switch hint.key {
	case "t", "v/V", "f", "p", "z", "m", "n/N", "w", "End/G":
		return true
	default:
		return hint.action == "search" || hint.action == "scroll logs" || hint.action == "refresh" || hint.action == "target"
	}
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
		check = paint("✓", output.ColorSuccess, true)
	}
	metaWidth := min(20, max(8, width/3))
	nameWidth := max(8, width-metaWidth-8)
	name = ansi.Truncate(safeText(name), nameWidth, "…")
	meta = ansi.Truncate(meta, metaWidth, "…")
	plain := fmt.Sprintf(" %s %s%s%s%s", marker, name, strings.Repeat(" ", max(1, width-ansi.StringWidth(name)-ansi.StringWidth(meta)-6)), meta, check)
	plain = ansi.Truncate(plain, width, "")
	plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
	if index == m.namespaceCursor {
		return selectedRowStyle().Width(width).Render(plain)
	}
	return " " + paint(marker, output.ColorBranch, true) + " " + paint(name, output.ColorKey, false) + strings.Repeat(" ", max(1, width-ansi.StringWidth(name)-ansi.StringWidth(meta)-6)) + paint(meta, output.ColorMuted, false) + "  " + check
}
