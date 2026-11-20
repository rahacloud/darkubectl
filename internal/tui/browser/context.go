package browser

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *browser) tenantPaneLines(height int) []string {
	_, _, width := m.paneWidths()
	lines := []string{}
	if len(m.tenants) == 0 {
		lines = m.emptyTenantPaneLines()
	} else {
		first := max(0, min(m.tenantCursor-height+1, len(m.tenants)-height))
		last := min(len(m.tenants), first+height)
		for i := first; i < last; i++ {
			label := safeText(m.tenants[i].name)
			if roles := tenantRoleBadges(m.tenants[i].roles); roles != "" {
				label += "  " + roles
			}
			selected := m.contextFocused && m.contextRow == 0 && i == m.tenantCursor
			line := m.contextRowLine(label, width, selected)
			if !selected && m.tenants[i].name == m.tenant {
				line = paint(line, browserSuccess, true)
			}
			lines = append(lines, line)
		}
	}
	return fitPaneLines(lines, height)
}

func (m *browser) emptyTenantPaneLines() []string {
	if m.profileState.active() || m.profileState.status == requestIdle {
		return []string{m.loadingMessage("Loading…")}
	}
	if m.profileState.err != nil || m.profileState.status == requestUnavailable {
		return []string{paint("No choices", browserMuted, false), paint("Use --org or login", browserMuted, false)}
	}
	return []string{paint("No memberships", browserMuted, false)}
}

func tenantRoleBadges(roles []string) string {
	var badges []string
	for _, known := range []struct{ role, glyph string }{
		{"owner", "◆"}, {"admin", "✦"}, {"developer", "⌘"}, {"accountant", "$"},
	} {
		for _, role := range roles {
			if strings.EqualFold(role, known.role) {
				badges = append(badges, known.glyph)
				break
			}
		}
	}
	for _, role := range roles {
		if !isKnownTenantRole(role) {
			badges = append(badges, role)
		}
	}
	return strings.Join(badges, " ")
}

func isKnownTenantRole(role string) bool {
	switch strings.ToLower(role) {
	case "owner", "admin", "developer", "accountant":
		return true
	default:
		return false
	}
}

func (m *browser) pagesPaneLines(height int) []string {
	_, _, width := m.paneWidths()
	pages := []struct{ icon, name string }{
		{"∿", "Activity"},
		{"▱", "Tickets"},
		{"▤", "DB"},
		{"◇", "Registry"},
	}
	lines := make([]string, 0, 1+len(pages))
	lines = append(lines, m.contextRowLine("▦ Apps", width, m.contextFocused && m.contextRow == 1))
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
	if strings.HasPrefix(value, "▱ Tickets") && m.page == pageTickets {
		return paint(line, browserSuccess, true)
	}
	if strings.HasPrefix(value, "▤ DB") && m.page == pageDB {
		return paint(line, browserSuccess, true)
	}
	if strings.HasPrefix(value, "∿ ") || strings.HasPrefix(value, "▱ ") || strings.HasPrefix(value, "▤ ") ||
		strings.HasPrefix(value, "◇ ") {
		return paint(line, browserMuted, false)
	}
	return line
}
