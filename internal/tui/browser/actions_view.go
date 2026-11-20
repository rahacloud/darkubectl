package browser

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	actionDialogWidth             = 72
	buildActionDialogWidth        = 76
	buildConfirmationDialogWidth  = 80
	dialogOuterMargin             = 4
	dialogInnerPadding            = 6
	dialogAppLabelWidth           = 5
	dialogCenterDivisor           = 2
	dialogButtonTopInset          = 2
	dialogButtonLeftInset         = 3
	dialogButtonGap               = 4
	actionControlsInset           = 10
	buildConfirmationLineCapacity = 6
)

type actionButtonRect struct {
	left   int
	right  int
	top    int
	bottom int
	index  int
}

func (m *browser) actionMenuPanel() string {
	width := min(actionDialogWidth, m.width-dialogOuterMargin)
	innerWidth := width - dialogInnerPadding
	items := m.actionMenuItems()
	content := []string{
		paint("⌘  APP ACTIONS", browserAccent, true),
		paint(strings.Repeat("─", innerWidth), browserBorder, false),
		"App  " + paint(ansi.Truncate(safeText(m.actionTargetName), innerWidth-dialogOuterMargin, "…"), browserKey, true),
	}
	for i, action := range items {
		label := actionMenuGlyph(action) + " " + actionMenuTitle(action)
		switch {
		case i == m.actionCursor:
			content = append(content, selectedRowStyle().Width(innerWidth).Render(ansi.Truncate(label, innerWidth, "…")))
		case action == appActionDelete:
			content = append(content, paint(ansi.Truncate(label, innerWidth, "…"), browserNumber, true))
		default:
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
	return ansi.Truncate(strings.Join(controls, " · "), max(1, m.width-actionControlsInset), "…")
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
		return logUnknownState
	}
}

type actionConfirmation struct {
	content                 []string
	buttonRow               int
	leftButton, rightButton string
}

func (m *browser) actionConfirmationContent() actionConfirmation {
	width := min(actionDialogWidth, m.width-dialogOuterMargin)
	innerWidth := width - dialogInnerPadding
	content := make([]string, 0, buildConfirmationLineCapacity)
	leftButton := labelCancelButton
	var rightButton string
	if m.confirmAction == appActionRestart {
		content = append(content,
			paint("↻ Restart", browserAccent, true),
			paint(strings.Repeat("─", innerWidth), browserBorder, false),
			"Restart pods for "+safeText(m.actionTargetName)+"?",
		)
		rightButton = "[ ↻ Restart ]"
	} else {
		content = append(content,
			paint("✖ Delete", browserNumber, true),
			paint(strings.Repeat("─", innerWidth), browserBorder, false),
			"Delete "+safeText(m.actionTargetName)+" permanently?",
			paint(ansi.Truncate("App name  "+safeText(m.confirmName)+"█", innerWidth, "…"), browserKey, true),
			paint("Cannot be undone.", browserMuted, false),
		)
		if m.confirmError != "" {
			content = append(content, paint(ansi.Truncate(safeText(m.confirmError), innerWidth, "…"), browserNumber, true))
		}
		rightButton = "[ ✖ Delete ]"
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
	return actionConfirmation{content: content, buttonRow: buttonRow, leftButton: leftButton, rightButton: rightButton}
}

func (m *browser) actionConfirmButtonRects() (actionButtonRect, actionButtonRect) {
	confirmation := m.actionConfirmationContent()
	width := min(actionDialogWidth, m.width-dialogOuterMargin)
	panel := tenantPanelStyle().Width(width).Render(strings.Join(confirmation.content, "\n"))
	panelHeight := len(strings.Split(panel, "\n"))
	startX := max(0, (m.width-width)/dialogCenterDivisor)
	startY := max(0, (m.height-panelHeight)/dialogCenterDivisor)
	buttonY := startY + dialogButtonTopInset + confirmation.buttonRow // border plus vertical padding
	leftStart := startX + dialogButtonLeftInset                       // border plus horizontal padding
	leftEnd := leftStart + ansi.StringWidth(confirmation.leftButton) - 1
	rightStart := leftEnd + dialogButtonGap
	rightEnd := rightStart + ansi.StringWidth(confirmation.rightButton) - 1
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
	startY := max(0, (m.height-len(panelLines))/dialogCenterDivisor)
	startX := max(0, (m.width-width)/dialogCenterDivisor)
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
	width := min(actionDialogWidth, m.width-dialogOuterMargin)
	return m.actionOverlay(background, m.actionMenuPanel(), width)
}

func (m *browser) renderActionConfirmation(background []string) []string {
	width := min(actionDialogWidth, m.width-dialogOuterMargin)
	confirmation := m.actionConfirmationContent()
	panel := tenantPanelStyle().Width(width).Render(strings.Join(confirmation.content, "\n"))
	return m.actionOverlay(background, panel, width)
}

func (m *browser) buildActionPanel() string {
	width := min(buildActionDialogWidth, m.width-dialogOuterMargin)
	inner := width - dialogInnerPadding
	items := m.buildActionItems()
	content := []string{paint("⚒  BUILD ACTIONS", browserAccent, true), paint(strings.Repeat("─", inner), browserBorder, false),
		"App  " + ansi.Truncate(safeText(m.buildConfirmAppName), inner-dialogAppLabelWidth, "…")}
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
	width := min(buildConfirmationDialogWidth, m.width-dialogOuterMargin)
	inner := width - dialogInnerPadding
	title := buildActionTitle(m.buildConfirm)
	content := []string{paint("⚒  "+title, browserAccent, true), paint(strings.Repeat("─", inner), browserBorder, false),
		"Tenant     " + safeText(m.buildConfirmTenant), "App        " + safeText(m.buildConfirmAppName),
		"Namespace  " + safeText(m.selectedApp().Namespace.Name)}
	switch m.buildConfirm {
	case buildActionLatest:
		content = append(content, "Target     Current branch head (resolved by Darkube)", "Result     Builds and deploys the resulting image")
	case buildActionRetry:
		content = append(content, "Build      "+safeText(string(m.buildConfirmBuildID)),
			"Branch     "+safeText(buildText(m.buildDetailBranch(), "—")), "Commit     "+safeText(buildText(m.buildDetailHash(), "—")),
			"Result     Retry submitted; deployment is not assumed")
	case buildActionStop:
		content = append(content, "Build      "+safeText(string(m.buildConfirmBuildID)), "Result     Stop this build")
	case buildActionNone:
		break
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
	width := min(buildActionDialogWidth, m.width-dialogOuterMargin)
	return m.actionOverlay(background, m.buildActionPanel(), width)
}
func (m *browser) renderBuildActionConfirmation(background []string) []string {
	width := min(buildConfirmationDialogWidth, m.width-dialogOuterMargin)
	return m.actionOverlay(background, m.buildActionConfirmationPanel(), width)
}
