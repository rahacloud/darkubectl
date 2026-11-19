package browser

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *browser) openActionMenu() {
	if m.help || m.sorting || m.selectingNamespace || m.selectingTenant || m.contextFocused || m.actionBusy {
		return
	}
	if !m.selectedAppActive() {
		m.actionNotice = "Select an app to view its actions."
		return
	}
	if m.width < 60 || m.height < 20 {
		return
	}
	app := m.apps[m.selected]
	items := m.actionMenuItems()
	if len(items) == 0 {
		m.actionNotice = "No actions are available for this app."
		return
	}
	m.actionTargetTenant = m.tenant
	m.actionTargetID = app.ID
	m.actionTargetName = app.Name
	m.actionCursor = 0
	m.actionMenuMessage = ""
	m.actionNotice = ""
	m.actionMenu = true
}

func (m *browser) selectedAppActive() bool {
	return !m.namespaceSelected && m.selectedAppVisible()
}

func (m *browser) actionIsMock() bool {
	return strings.HasPrefix(m.mode, "MOCK")
}

func (m *browser) actionAvailable(action appAction) bool {
	if m.actionIsMock() {
		return true
	}
	if action == appActionTerminal {
		return m.terminal != nil
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

func (m *browser) actionMenuItems() []appAction {
	all := []appAction{appActionTerminal, appActionRestart, appActionDelete}
	items := make([]appAction, 0, len(all))
	for _, action := range all {
		if m.actionAvailable(action) {
			items = append(items, action)
		}
	}
	return items
}

func actionTitle(action appAction) string {
	switch action {
	case appActionRestart:
		return "Restart app"
	case appActionDelete:
		return "Delete app"
	case appActionTerminal:
		return "Open terminal"
	default:
		return "Unknown action"
	}
}

func (m *browser) handleActionMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	items := m.actionMenuItems()
	if len(items) == 0 {
		m.actionMenu = false
		return nil
	}
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc", "q":
		m.actionMenu = false
		m.actionMenuMessage = ""
	case "up", "k":
		m.moveActionCursor(-1, items)
	case "down", "j":
		m.moveActionCursor(1, items)
	case "enter":
		return m.beginActionConfirmation(items[m.actionCursor])
	case "r":
		if m.actionAvailable(appActionRestart) {
			return m.beginActionConfirmation(appActionRestart)
		}
	case "d":
		if m.actionAvailable(appActionDelete) {
			return m.beginActionConfirmation(appActionDelete)
		}
	case "t":
		if m.actionAvailable(appActionTerminal) {
			return m.beginActionConfirmation(appActionTerminal)
		}
	}
	return nil
}

func (m *browser) moveActionCursor(delta int, items []appAction) {
	m.actionCursor = (m.actionCursor + delta + len(items)) % len(items)
}

func (m *browser) beginActionConfirmation(action appAction) tea.Cmd {
	if !m.actionAvailable(action) {
		if !m.actionIsMock() && !m.allowWrites && (action == appActionRestart || action == appActionDelete) {
			return nil
		}
		if !m.actionIsMock() && !m.allowWrites {
			m.actionMenuMessage = "Writes are disabled. Relaunch with --allow-writes."
		} else {
			m.actionMenuMessage = actionTitle(action) + " is unavailable."
		}
		return nil
	}
	if action == appActionTerminal {
		m.actionMenu = false
		return m.beginTerminalPicker()
	}
	m.actionMenu = false
	m.actionMenuMessage = ""
	m.confirmAction = action
	m.confirmButton = 0 // No/Cancel is focused first.
	m.confirmName = ""
	m.confirmError = ""
	return nil
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
	if m.actionBusy || m.confirmAction == appActionNone {
		return nil
	}
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
		current, err := m.getApp(m.ctx, tenant, id)
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
	if msg.Button != tea.MouseLeft || m.actionBusy {
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
