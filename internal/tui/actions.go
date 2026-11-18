package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
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

func (m *browser) openActionMenu() {
	if m.help || m.sorting || m.selectingNamespace || m.selectingTenant || m.contextFocused || m.actionBusy {
		return
	}
	if !m.selectedAppVisible() {
		m.actionNotice = "Select an app to view its actions."
		return
	}
	if m.width < 60 || m.height < 20 {
		return
	}
	app := m.apps[m.selected]
	m.actionTargetTenant = m.tenant
	m.actionTargetID = app.ID
	m.actionTargetName = app.Name
	m.actionCursor = 0
	m.actionMenuMessage = ""
	m.actionNotice = ""
	m.actionMenu = true
}

func (m *browser) actionIsMock() bool {
	return strings.HasPrefix(m.mode, "MOCK")
}

func (m *browser) actionAvailable(action appAction) bool {
	if m.actionIsMock() {
		return true
	}
	if !m.allowWrites {
		return false
	}
	switch action {
	case appActionRestart:
		return m.restartApp != nil
	case appActionDelete:
		return m.deleteApp != nil
	default:
		return false
	}
}

func actionTitle(action appAction) string {
	switch action {
	case appActionRestart:
		return "Restart app"
	case appActionDelete:
		return "Delete app"
	default:
		return "Unknown action"
	}
}

func (m *browser) handleActionMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	items := []appAction{appActionRestart, appActionDelete}
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc", "q":
		m.actionMenu = false
		m.actionMenuMessage = ""
	case "up", "k":
		m.actionCursor = (m.actionCursor + len(items) - 1) % len(items)
	case "down", "j":
		m.actionCursor = (m.actionCursor + 1) % len(items)
	case "enter":
		m.beginActionConfirmation(items[m.actionCursor])
	case "r":
		m.beginActionConfirmation(appActionRestart)
	case "d":
		m.beginActionConfirmation(appActionDelete)
	}
	return nil
}

func (m *browser) beginActionConfirmation(action appAction) {
	if !m.actionAvailable(action) {
		if !m.actionIsMock() && !m.allowWrites {
			m.actionMenuMessage = "Writes are disabled. Relaunch with --allow-writes."
		} else {
			m.actionMenuMessage = actionTitle(action) + " is unavailable."
		}
		return
	}
	m.actionMenu = false
	m.actionMenuMessage = ""
	m.confirmAction = action
	m.confirmButton = 0 // No/Cancel is focused first.
	m.confirmName = ""
	m.confirmError = ""
}

func (m *browser) handleActionConfirmationKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if msg.String() == "esc" {
		m.cancelActionConfirmation()
		return nil
	}
	if m.confirmAction == appActionDelete {
		switch msg.String() {
		case "tab", "right":
			m.confirmButton = 1
			return nil
		case "shift+tab", "backtab", "left":
			m.confirmButton = 0
			return nil
		case "backspace", "ctrl+h":
			runes := []rune(m.confirmName)
			if len(runes) > 0 {
				m.confirmName = string(runes[:len(runes)-1])
			}
			m.confirmError = ""
			return nil
		case "ctrl+u":
			m.confirmName = ""
			m.confirmError = ""
			return nil
		case "enter":
			if m.confirmButton == 0 {
				m.cancelActionConfirmation()
				return nil
			}
			return m.submitConfirmedAction()
		}
		if msg.Text != "" {
			m.confirmName += safeText(msg.Text)
			m.confirmError = ""
		}
		return nil
	}
	switch msg.String() {
	case "y":
		return m.submitConfirmedAction()
	case "n":
		m.cancelActionConfirmation()
	case "tab", "right":
		m.confirmButton = 1
	case "shift+tab", "backtab", "left":
		m.confirmButton = 0
	case "enter":
		if m.confirmButton == 1 {
			return m.submitConfirmedAction()
		}
		m.cancelActionConfirmation()
	}
	return nil
}

func (m *browser) cancelActionConfirmation() {
	m.confirmAction = appActionNone
	m.confirmName = ""
	m.confirmError = ""
}

func (m *browser) submitConfirmedAction() tea.Cmd {
	if m.confirmAction == appActionDelete && m.confirmName != m.actionTargetName {
		m.confirmError = "Type the app name exactly before deleting."
		return nil
	}
	action := m.confirmAction
	tenant, id, name := m.actionTargetTenant, m.actionTargetID, m.actionTargetName
	m.cancelActionConfirmation()
	m.actionBusy = true
	m.actionNotice = ""
	return func() tea.Msg {
		result := appActionResult{tenant: tenant, id: id, name: name, action: action, mock: m.actionIsMock()}
		if result.mock {
			return result
		}
		if !m.allowWrites {
			result.err = fmt.Errorf("write actions are disabled; relaunch with --allow-writes")
			return result
		}
		if m.getApp == nil {
			result.err = fmt.Errorf("cannot verify the selected app before the action")
			return result
		}
		current, err := m.getApp(tenant, id)
		if err != nil {
			result.err = fmt.Errorf("verify app before %s: %w", actionTitle(action), err)
			return result
		}
		if current == nil || current.ID != id || current.Name != name {
			result.err = fmt.Errorf("the selected app changed; refresh the app list and try again")
			return result
		}
		switch action {
		case appActionRestart:
			if m.restartApp == nil {
				result.err = fmt.Errorf("app restart is unavailable")
			} else {
				result.err = m.restartApp(tenant, id)
			}
		case appActionDelete:
			if m.deleteApp == nil {
				result.err = fmt.Errorf("app deletion is unavailable")
			} else {
				result.err = m.deleteApp(tenant, id)
			}
		default:
			result.err = fmt.Errorf("unknown app action")
		}
		return result
	}
}

func (m *browser) applyActionResult(result appActionResult) tea.Cmd {
	m.actionBusy = false
	if result.tenant != m.tenant {
		return nil
	}
	if result.err != nil {
		m.actionNotice = actionTitle(result.action) + " failed for " + safeText(result.name) + ": " + safeText(result.err.Error())
		return nil
	}
	if result.mock {
		if result.action == appActionRestart {
			m.actionNotice = "MOCK · restart simulated for " + safeText(result.name) + " · no API call made"
			return nil
		}
		if result.action == appActionDelete && m.mockDeleteApp != nil {
			m.mockDeleteApp(result.tenant, result.id)
		}
	}
	if result.action == appActionRestart {
		if result.mock {
			return nil
		}
		m.actionNotice = "Restart requested for " + safeText(result.name) + "; rollout may still be in progress."
		selectedID := ""
		if m.selectedAppVisible() {
			selectedID = m.apps[m.selected].ID
		}
		if selectedID == result.id {
			return tea.Batch(m.load(), m.loadSelectedSummary())
		}
		return m.load()
	}
	if result.action == appActionDelete {
		if m.selectedAppVisible() && m.apps[m.selected].ID == result.id {
			m.detail = false
			m.detailApp = nil
			m.detailUsage = nil
			m.usageAppID = ""
			m.selected = 0
		}
		if result.mock {
			m.actionNotice = "MOCK · app removed from this session; fixture file is unchanged"
		} else {
			m.actionNotice = "Delete request accepted for " + safeText(result.name)
		}
		return m.load()
	}
	return nil
}

func (m *browser) handleActionMouse(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if m.confirmAction == appActionNone {
		return nil
	}
	left, right := m.actionConfirmButtonRects()
	for _, button := range []actionButtonRect{left, right} {
		if msg.X < button.left || msg.X > button.right || msg.Y < button.top || msg.Y > button.bottom {
			continue
		}
		if button.index == 0 {
			m.cancelActionConfirmation()
			return nil
		}
		if m.confirmAction == appActionDelete && m.confirmName != m.actionTargetName {
			m.confirmError = "Type the app name exactly before deleting."
			return nil
		}
		return m.submitConfirmedAction()
	}
	return nil
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
			content = append(content, tenantSelectedStyle().Width(innerWidth).Render(ansi.Truncate(label, innerWidth, "…")))
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
	content = append(content, "", paint("↑/↓ choose · Enter select · Esc close", output.ColorMuted, false))
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
		left = tenantSelectedStyle().Render(leftButton)
	}
	right := paint(rightButton, output.ColorNumber, true)
	if m.confirmButton == 1 {
		right = tenantSelectedStyle().Render(rightButton)
	}
	content = append(content, left+"   "+right)
	if m.confirmAction == appActionDelete {
		content = append(content, paint("Type name · click ✖ Delete or Tab then Enter · Esc cancel", output.ColorMuted, false))
	} else {
		content = append(content, paint("←/→ choose · Enter confirm · Esc cancel", output.ColorMuted, false))
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
