package browser

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/output"
	"strings"
)

func (m *browser) contextPaneLines(height int) []string {
	_, _, width := m.paneWidths()
	tenantHeight := height / 2
	pageHeight := height - tenantHeight
	tenantContentHeight := max(1, tenantHeight-1) // reserve one row for the pane divider
	tenantLines := []string{}
	tenantHeading := paint("[1] TENANTS", output.ColorKey, true)
	if m.contextFocused && m.contextRow == 0 {
		tenantHeading = paint("[1] TENANTS", output.ColorAccent, true)
	}
	tenantLines = append(tenantLines, tenantHeading)
	if len(m.tenants) == 0 {
		if m.profileState.active() || m.profileState.status == requestIdle {
			tenantLines = append(tenantLines, m.loadingMessage("Loading…"))
		} else if m.profileState.err != nil || m.profileState.status == requestUnavailable {
			tenantLines = append(tenantLines, paint("No choices", output.ColorMuted, false), paint("Use --org or login", output.ColorMuted, false))
		} else {
			tenantLines = append(tenantLines, paint("No memberships", output.ColorMuted, false))
		}
	} else {
		capacity := max(1, tenantContentHeight-len(tenantLines))
		first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
		last := min(len(m.tenants), first+capacity)
		for i := first; i < last; i++ {
			option := m.tenants[i]
			marker := " "
			if i == m.tenantCursor {
				marker = "›"
			}
			label := marker + " " + safeText(option.name)
			if option.name == m.tenant {
				label += " ✓"
			}
			tenantLines = append(tenantLines, m.contextRowLine(label, width, m.contextFocused && m.contextRow == 0 && i == m.tenantCursor))
		}
	}
	if len(tenantLines) > tenantContentHeight {
		tenantLines = tenantLines[:tenantContentHeight]
	}
	for len(tenantLines) < tenantContentHeight {
		tenantLines = append(tenantLines, "")
	}
	lines := append(tenantLines, paint(strings.Repeat("─", width), output.ColorBorder, false))

	pageHeading := paint("[2] PAGES", output.ColorMuted, true)
	if m.contextFocused && m.contextRow == 1 {
		pageHeading = paint("[2] PAGES", output.ColorAccent, true)
	}
	pageLines := []string{pageHeading}
	pageLines = append(pageLines, m.contextRowLine("● Apps", width, m.contextFocused && m.contextRow == 1))
	for _, page := range []string{"Activity", "Tickets", "Resources", "Registry", "Billing", "Account"} {
		pageLines = append(pageLines, m.contextRowLine("· "+page, width, false))
	}
	if len(pageLines) > pageHeight {
		pageLines = pageLines[:pageHeight]
	}
	for len(pageLines) < pageHeight {
		pageLines = append(pageLines, "")
	}
	lines = append(lines, pageLines...)
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
