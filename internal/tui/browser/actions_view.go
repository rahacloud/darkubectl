package browser

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/output"
)

type actionButtonRect struct {
	left   int
	right  int
	top    int
	bottom int
	index  int
}

func (m *browser) actionMenuPanel() string {
	width := min(72, m.width-4)
	innerWidth := width - 6
	items := []appAction{appActionRestart, appActionDelete}
	content := []string{
		paint("⌘  APP ACTIONS", output.ColorAccent, true),
		paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
		"App  " + paint(ansi.Truncate(safeText(m.actionTargetName), innerWidth-4, "…"), output.ColorKey, true),
	}
	for i, action := range items {
		label := "  " + actionTitle(action)
		if action == appActionDelete {
			label += " · type name to confirm"
		}
		if !m.actionAvailable(action) {
			label += " · disabled"
			content = append(content, paint(label, output.ColorMuted, false))
		} else if i == m.actionCursor {
			content = append(content, selectedRowStyle().Width(innerWidth).Render(ansi.Truncate(label, innerWidth, "…")))
		} else if action == appActionDelete {
			content = append(content, paint(ansi.Truncate(label, innerWidth, "…"), output.ColorNumber, true))
		} else {
			content = append(content, paint(ansi.Truncate(label, innerWidth, "…"), output.ColorKey, false))
		}
	}
	if !m.actionIsMock() && !m.allowWrites {
		content = append(content, paint("Writes disabled · relaunch with --allow-writes", output.ColorMuted, false))
	} else if m.actionMenuMessage != "" {
		content = append(content, paint(ansi.Truncate(safeText(m.actionMenuMessage), innerWidth, "…"), output.ColorNumber, false))
	}
	if innerWidth < 60 {
		content = append(content,
			"",
			paint("↑/↓ or j/k choose · r restart · d delete", output.ColorMuted, false),
			paint("↵ select · q/Esc close", output.ColorMuted, false),
		)
	} else {
		content = append(content, "", paint("↑/↓ or j/k choose · r restart · d delete · ↵ select · q/Esc close", output.ColorMuted, false))
	}
	for i := range content {
		content[i] = ansi.Truncate(content[i], innerWidth, "…")
	}
	return tenantPanelStyle().Width(width).Render(strings.Join(content, "\n"))
}

func (m *browser) actionConfirmationContent() ([]string, int, string, string) {
	width := min(72, m.width-4)
	innerWidth := width - 6
	var content []string
	leftButton, rightButton := "[ Cancel ]", "[ Confirm ]"
	if m.confirmAction == appActionRestart {
		content = []string{
			paint("↻ Restart", output.ColorAccent, true),
			paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
			"Restart pods for " + safeText(m.actionTargetName) + "?",
		}
		leftButton, rightButton = "[ Cancel ]", "[ ↻ Restart ]"
	} else {
		content = []string{
			paint("✖ Delete", output.ColorNumber, true),
			paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
			"Delete " + safeText(m.actionTargetName) + " permanently?",
			paint("Type the exact app name:", output.ColorMuted, false),
			paint(ansi.Truncate("› "+safeText(m.confirmName)+"█", innerWidth, "…"), output.ColorKey, true),
			paint("Cannot be undone.", output.ColorMuted, false),
		}
		if m.confirmError != "" {
			content = append(content, paint(ansi.Truncate(safeText(m.confirmError), innerWidth, "…"), output.ColorNumber, true))
		}
		leftButton, rightButton = "[ Cancel ]", "[ ✖ Delete ]"
	}
	buttonRow := len(content)
	left := paint(leftButton, output.ColorKey, m.confirmButton == 0)
	if m.confirmButton == 0 {
		left = selectedRowStyle().Render(leftButton)
	}
	right := paint(rightButton, output.ColorNumber, true)
	if m.confirmButton == 1 {
		right = selectedRowStyle().Render(rightButton)
	}
	content = append(content, left+"   "+right)
	if m.confirmAction == appActionDelete {
		if innerWidth < 60 {
			content = append(content,
				paint("Type name · click ✖ Delete or Tab then ↵", output.ColorMuted, false),
				paint("Esc cancel", output.ColorMuted, false),
			)
		} else {
			content = append(content, paint("Type name · click ✖ Delete or Tab then ↵ · Esc cancel", output.ColorMuted, false))
		}
	} else {
		content = append(content, paint("←/→ choose · ↵ confirm · Esc cancel", output.ColorMuted, false))
	}
	for i := range content {
		content[i] = ansi.Truncate(content[i], innerWidth, "…")
	}
	return content, buttonRow, leftButton, rightButton
}

func (m *browser) actionConfirmButtonRects() (actionButtonRect, actionButtonRect) {
	content, buttonRow, leftButton, rightButton := m.actionConfirmationContent()
	width := min(72, m.width-4)
	panel := tenantPanelStyle().Width(width).Render(strings.Join(content, "\n"))
	panelHeight := len(strings.Split(panel, "\n"))
	startX := max(0, (m.width-width)/2)
	startY := max(0, (m.height-panelHeight)/2)
	buttonY := startY + 2 + buttonRow // border plus vertical padding
	leftStart := startX + 3           // border plus horizontal padding
	leftEnd := leftStart + ansi.StringWidth(leftButton) - 1
	rightStart := leftEnd + 4
	rightEnd := rightStart + ansi.StringWidth(rightButton) - 1
	return actionButtonRect{left: leftStart, right: leftEnd, top: buttonY, bottom: buttonY, index: 0},
		actionButtonRect{left: rightStart, right: rightEnd, top: buttonY, bottom: buttonY, index: 1}
}

func (m *browser) actionOverlay(background []string, panel string, width int) []string {
	panelLines := strings.Split(panel, "\n")
	if len(panelLines) > 0 {
		width = ansi.StringWidth(panelLines[0])
	}
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/2)
	startX := max(0, (m.width-width)/2)
	for i, line := range panelLines {
		y := startY + i
		if y >= len(background) {
			break
		}
		if lineWidth := ansi.StringWidth(line); lineWidth < width {
			line += strings.Repeat(" ", width-lineWidth)
		}
		base := ansi.Truncate(background[y], m.width, "")
		base += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(base)))
		background[y] = ansi.Cut(base, 0, startX) + line + ansi.Cut(base, startX+width, m.width)
	}
	return background[:m.height]
}

func (m *browser) renderActionMenu(background []string) []string {
	width := min(72, m.width-4)
	return m.actionOverlay(background, m.actionMenuPanel(), width)
}

func (m *browser) renderActionConfirmation(background []string) []string {
	width := min(72, m.width-4)
	content, _, _, _ := m.actionConfirmationContent()
	panel := tenantPanelStyle().Width(width).Render(strings.Join(content, "\n"))
	return m.actionOverlay(background, panel, width)
}
