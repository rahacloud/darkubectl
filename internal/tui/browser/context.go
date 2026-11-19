package browser

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/output"
	"strings"
)

func (m *browser) tenantPaneLines(height int) []string {
	_, _, width := m.paneWidths()
	lines := []string{}
	if len(m.tenants) == 0 {
		if m.profileState.active() || m.profileState.status == requestIdle {
			lines = append(lines, m.loadingMessage("Loading…"))
		} else if m.profileState.err != nil || m.profileState.status == requestUnavailable {
			lines = append(lines, paint("No choices", output.ColorMuted, false), paint("Use --org or login", output.ColorMuted, false))
		} else {
			lines = append(lines, paint("No memberships", output.ColorMuted, false))
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
	lines := []string{m.contextRowLine("● Apps", width, m.contextFocused && m.contextRow == 1)}
	for _, page := range []string{"Activity", "Tickets", "Resources", "Registry", "Billing", "Account"} {
		lines = append(lines, m.contextRowLine("· "+page, width, false))
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
	if strings.HasPrefix(value, "● Apps") {
		return paint(line, output.ColorSuccess, true)
	}
	if strings.HasPrefix(value, "· ") {
		return paint(line, output.ColorMuted, false)
	}
	return line
}
