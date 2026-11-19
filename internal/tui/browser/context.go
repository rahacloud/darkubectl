package browser

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (m *browser) tenantPaneLines(height int) []string {
	_, _, width := m.paneWidths()
	lines := []string{}
	if len(m.tenants) == 0 {
		if m.profileState.active() || m.profileState.status == requestIdle {
			lines = append(lines, m.loadingMessage("Loading…"))
		} else if m.profileState.err != nil || m.profileState.status == requestUnavailable {
			lines = append(lines, paint("No choices", browserMuted, false), paint("Use --org or login", browserMuted, false))
		} else {
			lines = append(lines, paint("No memberships", browserMuted, false))
		}
	} else {
		first := max(0, min(m.tenantCursor-height+1, len(m.tenants)-height))
		last := min(len(m.tenants), first+height)
		for i := first; i < last; i++ {
			marker := " "
			if i == m.tenantCursor {
				marker = "›"
			}
			label := marker + " " + safeText(m.tenants[i].name)
			lines = append(lines, m.contextRowLine(label, width, i == m.tenantCursor))
		}
	}
	return fitPaneLines(lines, height)
}

func (m *browser) pagesPaneLines(height int) []string {
	_, _, width := m.paneWidths()
	lines := []string{m.contextRowLine("▦ Apps", width, m.contextFocused && m.contextRow == 1)}
	pages := []struct{ icon, name string }{
		{"∿", "Activity"},
		{"▱", "Tickets"},
		{"▤", "DB"},
		{"◇", "Registry"},
		{"¤", "Billing"},
	}
	for _, page := range pages {
		selected := m.contextFocused && m.contextRow == len(lines)+1
		lines = append(lines, m.contextRowLine(page.icon+" "+page.name, width, selected))
	}
	return fitPaneLines(lines, height)
}

func fitPaneLines(lines []string, height int) []string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (m *browser) contextRowLine(value string, width int, selected bool) string {
	line := ansi.Truncate(safeText(value), width, "…")
	line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	if selected {
		return selectedRowStyle().Width(width).Render(line)
	}
	if strings.HasPrefix(value, "▦ Apps") && m.page == pageApps {
		return paint(line, browserSuccess, true)
	}
	if strings.HasPrefix(value, "∿ Activity") && m.page == pageActivity {
		return paint(line, browserSuccess, true)
	}
	if strings.HasPrefix(value, "◇ Registry") && m.page == pageRegistry {
		return paint(line, browserSuccess, true)
	}
	if strings.HasPrefix(value, "▤ DB") && m.page == pageDB {
		return paint(line, browserSuccess, true)
	}
	if strings.HasPrefix(value, "∿ ") || strings.HasPrefix(value, "▱ ") || strings.HasPrefix(value, "▤ ") ||
		strings.HasPrefix(value, "◇ ") || strings.HasPrefix(value, "¤ ") {
		return paint(line, browserMuted, false)
	}
	return line
}
