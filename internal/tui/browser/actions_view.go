package browser

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
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
	items := m.actionMenuItems()
	content := []string{
		paint("⌘  APP ACTIONS", browserAccent, true),
		paint(strings.Repeat("─", innerWidth), browserBorder, false),
		"App  " + paint(ansi.Truncate(safeText(m.actionTargetName), innerWidth-4, "…"), browserKey, true),
	}
	for i, action := range items {
		label := actionMenuGlyph(action) + " " + actionMenuTitle(action)
		if i == m.actionCursor {
			content = append(content, selectedRowStyle().Width(innerWidth).Render(ansi.Truncate(label, innerWidth, "…")))
		} else if action == appActionDelete {
			content = append(content, paint(ansi.Truncate(label, innerWidth, "…"), browserNumber, true))
		} else {
			content = append(content, paint(ansi.Truncate(label, innerWidth, "…"), browserKey, false))
		}
	}
	if m.actionMenuMessage != "" {
		content = append(content, paint(ansi.Truncate(safeText(m.actionMenuMessage), innerWidth, "…"), browserNumber, false))
	}
	content = append(content, "", paint(m.actionMenuHelp(), browserMuted, false))
	for i := range content {
		content[i] = ansi.Truncate(content[i], innerWidth, "…")
	}
	return tenantPanelStyle().Width(width).Render(strings.Join(content, "\n"))
}

func (m *browser) actionMenuHelp() string {
	controls := []string{"↑/↓ or j/k choose"}
	if m.actionAvailable(appActionRestart) {
		controls = append(controls, "r restart")
	}
	if m.actionAvailable(appActionDelete) {
		controls = append(controls, "d delete")
	}
	if m.actionAvailable(appActionTerminal) {
		controls = append(controls, "t terminal")
	}
	controls = append(controls, "↵ select", "q/Esc close")
	return ansi.Truncate(strings.Join(controls, " · "), max(1, m.width-10), "…")
}

func actionMenuGlyph(action appAction) string {
	switch action {
	case appActionRestart:
		return "↻"
	case appActionDelete:
		return "✖"
	case appActionTerminal:
		return ">_"
	default:
		return " "
	}
}

func actionMenuTitle(action appAction) string {
	switch action {
	case appActionRestart:
		return "Restart"
	case appActionDelete:
		return "Delete"
	case appActionTerminal:
		return "Terminal"
	default:
		return "Unknown"
	}
}

func (m *browser) actionConfirmationContent() ([]string, int, string, string) {
	width := min(72, m.width-4)
	innerWidth := width - 6
	var content []string
	leftButton, rightButton := "[ Cancel ]", "[ Confirm ]"
	if m.confirmAction == appActionRestart {
		content = []string{
			paint("↻ Restart", browserAccent, true),
			paint(strings.Repeat("─", innerWidth), browserBorder, false),
			"Restart pods for " + safeText(m.actionTargetName) + "?",
		}
		leftButton, rightButton = "[ Cancel ]", "[ ↻ Restart ]"
	} else {
		content = []string{
			paint("✖ Delete", browserNumber, true),
			paint(strings.Repeat("─", innerWidth), browserBorder, false),
			"Delete " + safeText(m.actionTargetName) + " permanently?",
			paint(ansi.Truncate("App name: "+safeText(m.confirmName)+"█", innerWidth, "…"), browserKey, true),
			paint("Cannot be undone.", browserMuted, false),
		}
		if m.confirmError != "" {
			content = append(content, paint(ansi.Truncate(safeText(m.confirmError), innerWidth, "…"), browserNumber, true))
		}
		leftButton, rightButton = "[ Cancel ]", "[ ✖ Delete ]"
	}
	buttonRow := len(content)
	left := paint(leftButton, browserKey, m.confirmButton == 0)
	if m.confirmButton == 0 {
		left = selectedRowStyle().Render(leftButton)
	}
	right := paint(rightButton, browserNumber, true)
	if m.confirmButton == 1 {
		right = selectedRowStyle().Render(rightButton)
	}
	content = append(content, left+"   "+right)
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

func (m *browser) buildActionPanel() string {
	width := min(76, m.width-4)
	inner := width - 6
	items := m.buildActionItems()
	content := []string{paint("⚒  BUILD ACTIONS", browserAccent, true), paint(strings.Repeat("─", inner), browserBorder, false),
		"App  " + ansi.Truncate(safeText(m.buildConfirmAppName), inner-5, "…")}
	for i, act := range items {
		label := buildActionTitle(act)
		if i == m.buildActionCursor {
			label = selectedRowStyle().Width(inner).Render(ansi.Truncate("› "+label, inner, "…"))
		} else {
			label = paint(ansi.Truncate("  "+label, inner, "…"), browserKey, false)
		}
		content = append(content, label)
	}
	if m.buildActionNotice != "" {
		content = append(content, paint(ansi.Truncate(safeText(m.buildActionNotice), inner, "…"), browserNumber, false))
	}
	content = append(content, "", paint("↑/↓ choose · Enter select · Esc close", browserMuted, false))
	return tenantPanelStyle().Width(width).Render(strings.Join(content, "\n"))
}

func (m *browser) buildActionConfirmationPanel() string {
	width := min(80, m.width-4)
	inner := width - 6
	title := buildActionTitle(m.buildConfirm)
	content := []string{paint("⚒  "+title, browserAccent, true), paint(strings.Repeat("─", inner), browserBorder, false),
		"Tenant     " + safeText(m.buildConfirmTenant), "App        " + safeText(m.buildConfirmAppName), "Namespace  " + safeText(m.selectedApp().Namespace.Name)}
	switch m.buildConfirm {
	case buildActionLatest:
		content = append(content, "Target     Current branch head (resolved by Darkube)", "Result     Builds and deploys the resulting image")
	case buildActionRetry:
		content = append(content, "Build      "+safeText(string(m.buildConfirmBuildID)), "Branch     "+safeText(buildText(m.buildDetailBranch(), "—")), "Commit     "+safeText(buildText(m.buildDetailHash(), "—")), "Result     Retry submitted; deployment is not assumed")
	case buildActionStop:
		content = append(content, "Build      "+safeText(string(m.buildConfirmBuildID)), "Result     Stop this build")
	}
	cancel, confirm := "[ Cancel ]", "[ Confirm ]"
	if m.buildConfirmButton == 0 {
		cancel = selectedRowStyle().Render(cancel)
	} else {
		confirm = selectedRowStyle().Render(confirm)
	}
	content = append(content, "", cancel+"   "+confirm, "Target is checked again before submission.")
	for i := range content {
		content[i] = ansi.Truncate(content[i], inner, "…")
	}
	return tenantPanelStyle().Width(width).Render(strings.Join(content, "\n"))
}

func (m *browser) buildDetailBranch() string {
	if m.buildDetail != nil {
		return m.buildDetail.GitBranch
	}
	if m.buildListCursor >= 0 && m.buildListCursor < len(m.builds) {
		return m.builds[m.buildListCursor].GitBranch
	}
	return ""
}
func (m *browser) buildDetailHash() string {
	if m.buildDetail != nil {
		return m.buildDetail.ShortCommitHash
	}
	if m.buildListCursor >= 0 && m.buildListCursor < len(m.builds) {
		return m.builds[m.buildListCursor].ShortCommitHash
	}
	return ""
}
func buildActionTitle(action buildAction) string {
	switch action {
	case buildActionLatest:
		return "Build and deploy latest commit"
	case buildActionRetry:
		return "Retry selected build"
	case buildActionStop:
		return "Stop selected build"
	default:
		return "Build action"
	}
}

func (m *browser) renderBuildActionMenu(background []string) []string {
	width := min(76, m.width-4)
	return m.actionOverlay(background, m.buildActionPanel(), width)
}
func (m *browser) renderBuildActionConfirmation(background []string) []string {
	width := min(80, m.width-4)
	return m.actionOverlay(background, m.buildActionConfirmationPanel(), width)
}
