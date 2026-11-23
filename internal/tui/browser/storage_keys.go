package browser

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

func (m *browser) storageHints(forHelp bool) []keyHint {
	if m.storageDialog != "" {
		return m.storageDialogHints()
	}
	if m.storageSecret != nil {
		return []keyHint{{"v", "reveal"}, {"c", "copy"}, {"s", hintSave}, {hintEnter, "done"}}
	}
	hints := []keyHint{{"b/t", "buckets / keys"}}
	if m.detail {
		hints = []keyHint{{hintEscape, hintList}}
	}
	hints = append(hints, m.storageActionHints()...)
	if forHelp {
		hints = append(hints, keyHint{hintVerticalSelect, "tree selection"}, keyHint{"←→/h/l", "collapse / expand"},
			keyHint{"r", hintRefresh}, keyHint{hintTab, "cycle panes"})
	}
	return hints
}

func (m *browser) storageDialogHints() []keyHint {
	switch m.storageDialog {
	case storageDialogSavePath:
		return []keyHint{{hintTextType, "path"}, {hintEnter, "review"}, {hintEscape, hintCancel}}
	case storageDialogSaveReview:
		return []keyHint{{hintEnter, hintSave}, {hintEscape, "edit path"}}
	case storageDialogBucketName:
		return []keyHint{{hintTextType, "bucket name"}, {hintEnter, "create"}, {hintEscape, hintCancel}}
	case storageDialogKeyForm:
		return []keyHint{{"Tab/↑↓", "choose field"}, {"Space", hintSelect}, {hintEnter, "review"}, {hintEscape, hintCancel}}
	default:
		if m.storageUsesOTP() {
			return []keyHint{{hintTextType, "TOTP code"}, {hintEnter, "submit"}, {hintEscape, hintCancel}}
		}
		return []keyHint{{hintTextType, "input"}, {hintEnter, hintContinue}, {hintEscape, hintCancel}}
	}
}

func (m *browser) storageActionHints() []keyHint {
	if !m.storageWritesReady() || m.selectedStorage() == nil {
		return nil
	}
	if m.storageKind == storageViewBuckets {
		hints := []keyHint{{"+", "create bucket"}}
		if m.selectedBucket() != nil {
			hints = append(hints, keyHint{"p", "visibility"}, keyHint{"d", "delete"})
		}
		return hints
	}
	hints := []keyHint{{"K", "create key"}}
	if m.selectedKey() != nil {
		hints = append(hints, keyHint{"g", "grant"}, keyHint{"R", "rotate"}, keyHint{"d", "revoke"})
	}
	return hints
}

func (m *browser) storageFooterLine(left string) string {
	return m.rightFooterLine(left, m.fitFooterHints(left, m.storageHints(false)))
}

//nolint:gocognit,gocyclo,cyclop,nestif,funlen,maintidx // One map routes actions through visible safety gates.
func (m *browser) handleStorageKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == inputKeyCtrlC {
		return tea.Quit
	}
	defer func() {
		if m.storageDialog != "" && !m.storageIsModal() {
			m.detail = true
		}
	}()
	if m.storageBusy {
		if key == inputKeyCtrlC || key == "q" {
			return tea.Quit
		}
		return nil
	}
	if m.storageDialog != "" {
		return m.handleStorageDialogKey(msg)
	}
	if m.storageSecret != nil {
		switch key {
		case "v":
			if m.allowSecrets {
				m.storageSecretVisible = !m.storageSecretVisible
			} else {
				m.storageNotice = "Relaunch with --allow-secrets to reveal credentials"
			}
		case "c":
			if m.allowSecrets {
				payload := "AWS_ACCESS_KEY_ID=" + storageEnvQuote(m.storageSecret.AccessKey) +
					"\nAWS_SECRET_ACCESS_KEY=" + storageEnvQuote(m.storageSecret.SecretKey) + "\n"
				if m.storageSecretEndpoint != "" {
					payload += "AWS_ENDPOINT_URL=" + storageEnvQuote(m.storageSecretEndpoint) + "\n"
				}
				if err := clipboard.WriteAll(payload); err != nil {
					m.storageNotice = messageClipboardUnavailable
				} else {
					return m.storageFeedbackMessage("Credential pair copied")
				}
			}
		case "s":
			if m.allowSecrets {
				m.storageDialog = storageDialogSavePath
				m.storageFeedback = ""
				m.storageNotice = ""
				m.storageDraft.text = ""
				m.storageDetailScroll = 0
			} else {
				m.storageNotice = "Relaunch with --allow-secrets to save credentials"
			}
		case keyEnter:
			m.storageSecret = nil
			m.storageSecretEndpoint = ""
			m.storageSecretVisible = false
			m.storageFeedback = ""
			m.clearStorageInputs()
			m.storageNotice = "One-time credential view closed"
		case browserKeyEscape:
			return nil
		case keyUp, "k":
			m.storageDetailScroll = max(0, m.storageDetailScroll-1)
		case keyDown, "j":
			m.storageDetailScroll++
		}
		return nil
	}
	switch key {
	case "1", "2", "3", "4":
		return m.focusPane(int(key[0] - '1'))
	case inputKeyTab:
		return m.cycleFocus(false)
	case inputKeyShiftTab, inputKeyBacktab:
		return m.cycleFocus(true)
	case inputKeyCtrlC, "q":
		return tea.Quit
	case "?":
		m.help = true
	case keyUp, "k":
		if m.detail {
			m.storageDetailScroll = max(0, m.storageDetailScroll-1)
			return nil
		}
		m.moveStorageSelection(-1)
		return m.loadStorageTraffic(false)
	case keyDown, "j":
		if m.detail {
			m.storageDetailScroll++
			return nil
		}
		m.moveStorageSelection(1)
		return m.loadStorageTraffic(false)
	case "b":
		childSelected := m.storageSelectedChild != ""
		m.storageKind = storageViewBuckets
		m.storageDetailScroll = 0
		m.storageScroll = 0
		if childSelected && m.storageExpanded {
			m.syncSelectedStorageChild()
		}
	case "K":
		if m.storageWritesReady() && m.storageKind == storageViewKeys && m.selectedStorage() != nil {
			m.storagePendingAction = storageOpKeyCreate
			m.storageDialog = storageDialogKeyForm
			m.clearStorageInputs()
			m.captureStorageTarget()
		}
	case "t":
		childSelected := m.storageSelectedChild != ""
		m.storageKind = storageViewKeys
		m.storageDetailScroll = 0
		m.storageScroll = 0
		if childSelected && m.storageExpanded {
			m.syncSelectedStorageChild()
		}
	case keyLeft, "h":
		if !m.detail {
			if m.storageSelectedChild == "" {
				m.storageExpanded = false
			}
			m.storageScroll = 0
			m.storageSelectedChild = ""
			m.storageDetailScroll = 0
		}
	case keyRight, "l":
		if !m.detail {
			if m.storageExpanded && m.storageSelectedChild == "" && m.storageChildCount(m.selectedStorage()) > 0 {
				m.moveStorageSelection(1)
			} else {
				m.storageExpanded = true
			}
		}
	case "+":
		if m.storageWritesReady() && m.storageKind == storageViewBuckets && m.selectedStorage() != nil {
			m.storagePendingAction = storageOpBucketCreate
			m.storageDialog = storageDialogBucketName
			m.clearStorageInputs()
			m.captureStorageTarget()
		}
	case "p":
		if m.storageWritesReady() {
			if b := m.selectedBucket(); b != nil {
				m.storagePendingAction = storageOpVisibility
				m.storageDialog = storageDialogVisibilityConfirm
				m.clearStorageInputs()
				m.captureStorageTarget()
			}
		}
	case "d":
		if m.storageWritesReady() {
			if b := m.selectedBucket(); b != nil {
				m.storagePendingAction = storageOpBucketDelete
				m.storageDialog = storageDialogBucketDeleteConfirm
				m.storageDraft.text = ""
				m.captureStorageTarget()
			} else if k := m.selectedKey(); k != nil {
				m.storagePendingAction = storageOpKeyDelete
				m.storageDialog = storageDialogKeyDeleteConfirm
				m.storageDraft.text = ""
				m.captureStorageTarget()
			}
		}
	case "g":
		if m.storageWritesReady() {
			if k := m.selectedKey(); k != nil {
				m.storagePendingAction = storageOpKeyGrant
				m.storageDialog = storageDialogKeyForm
				m.clearStorageInputs()
				m.captureStorageTarget()
				m.storageDraft.text = k.Name
				m.storageDraft.permissions = storagePermissions(k.Actions)
				m.storageDraft.buckets = strings.Join(k.Buckets, ",")
				m.storageDraft.field = 1
			}
		}
	case "R":
		if m.storageWritesReady() {
			if k := m.selectedKey(); k != nil {
				m.storagePendingAction = storageOpKeyRotate
				m.storageDialog = storageDialogKeyRotateConfirm
				m.storageDraft.text = ""
				m.captureStorageTarget()
			}
		}
	case "r":
		return m.loadStorages(true)
	case " ":
		if m.detail {
			m.detail = false
		} else {
			m.detail = true
		}
	case keyEnter:
		switch {
		case m.storageSelectedID == "" && len(m.storageRows) > 0 && !m.contextFocused:
			m.storageSelectedID = m.storageRows[m.storageCursor].ID
			m.storageSelectionLost = false
			m.storageExpanded = true
			m.syncSelectedStorageChild()
		case m.detail:
			m.detail = false
		default:
			m.detail = true
		}
	case browserKeyEscape:
		if m.detail {
			m.detail = false
		}
	}
	return nil
}
