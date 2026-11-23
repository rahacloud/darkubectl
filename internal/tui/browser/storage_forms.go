package browser

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) clearStorageInputs() {
	m.storageDraft = storageActionDraft{}
}

//nolint:nestif // The visibility confirmation includes a public-access warning.
func (m *browser) storageDialogLines() []string {
	if m.storageDialog == storageDialogKeyForm {
		return m.storageKeyFormLines()
	}
	if m.storageDialog == storageDialogWriteConfirm {
		return m.storageKeyReviewLines()
	}
	title, label, warning := "CONFIRM ACTION", "Confirmation", ""
	switch m.storageDialog {
	case storageDialogBucketDeleteConfirm:
		title, label, warning = "DELETE BUCKET", "Type the bucket name", "Every object will be deleted. This cannot be undone."
	case storageDialogKeyDeleteConfirm:
		title, label, warning = "REVOKE KEY", "Type the key name", "Consumers using this key will stop working."
	case storageDialogKeyRotateConfirm:
		title, label, warning = "ROTATE KEY", "Type the key name", "The old pair stops working immediately."
	case storageDialogOTP:
		title, label = "CONFIRM CHANGE", "Authenticator code"
	}
	lines := []string{paint(storageIconKey+"  "+title, browserAccent, true), "", summaryField("Tenant", safeText(m.tenant))}
	if storage := m.selectedStorage(); storage != nil {
		lines = append(lines, summaryField("Datacenter", safeText(storage.Name)))
	}
	if bucket := m.selectedBucket(); bucket != nil {
		lines = append(lines, summaryField("Bucket", safeText(bucket.Name)), summaryField("Size", storageSize(bucket.Size)))
		if m.storagePendingAction == storageOpVisibility {
			target := storageVisibilityPublic
			if bucket.IsPublic {
				target = "private"
			}
			lines = append(lines, summaryField("Change to", target))
			if target == storageVisibilityPublic {
				warning = "Anyone with a URL can read objects in this bucket."
			}
		}
	}
	if key := m.selectedKey(); key != nil {
		lines = append(lines, summaryField("Key", safeText(key.Name)))
	}
	if warning != "" {
		lines = append(lines, "", paint(warning, browserNumber, false))
	}
	value := safeText(m.storageDraft.text)
	if m.storageDialog == storageDialogOTP {
		value = strings.Repeat("•", len(m.storageDraft.otp))
	}
	return append(lines, "", label, paint(value+"▏", browserBranch, true))
}

func (m *browser) storageIsModal() bool {
	switch m.storageDialog {
	case storageDialogBucketName, storageDialogVisibilityConfirm, storageDialogSavePath, storageDialogSaveReview,
		storageDialogBucketDeleteConfirm, storageDialogKeyDeleteConfirm, storageDialogKeyRotateConfirm:
		return true
	case storageDialogOTP:
		return m.storagePendingAction != storageOpKeyCreate && m.storagePendingAction != storageOpKeyGrant
	}
	return false
}

func (m *browser) storageActionModal(background []string) []string {
	width := min(storageModalWidth, m.width-dialogOuterMargin)
	var lines []string
	switch m.storageDialog {
	case storageDialogSavePath, storageDialogSaveReview:
		lines = m.storageSaveModalLines()
	case storageDialogBucketName:
		lines = []string{paint(storageIconBucket+"  CREATE BUCKET", browserAccent, true), ""}
		if s := m.selectedStorage(); s != nil {
			lines = append(lines, summaryField("Datacenter", safeText(s.Name)))
		}
		lines = append(lines, "", "Bucket name", paint(m.storageDraft.text+"▏", browserBranch, true), "",
			paint("New buckets are private.", browserMuted, false))
	case storageDialogVisibilityConfirm:
		b := m.selectedBucket()
		if b == nil {
			return background
		}
		target := storageVisibilityPublic
		detail := "Anyone with a URL can read objects in this bucket."
		if b.IsPublic {
			target = "private"
			detail = "Public access to this bucket will be removed."
		}
		lines = []string{paint(storageIconBucket+"  CHANGE VISIBILITY", browserAccent, true), "",
			summaryField("Bucket", safeText(b.Name)), summaryField("Change to", target), "", paint(detail, browserMuted, false),
			"", "Authenticator code", paint(strings.Repeat("•", len(m.storageDraft.otp))+"▏", browserBranch, true)}
	default:
		lines = m.storageDialogLines()
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, width-dialogInnerPadding), "…")
	}
	panel := tenantPanelStyle().Width(width).Render(strings.Join(lines, "\n"))
	return m.actionOverlay(background, panel, width)
}

func (m *browser) storageKeyReviewLines() []string {
	title, note := "REVIEW ACCESS KEY", "Credentials are shown once after creation."
	if m.storagePendingAction == storageOpKeyGrant {
		title, note = "REVIEW KEY GRANTS", "Existing grants are retained; only additions are applied."
	}
	lines := []string{paint(storageIconKey+"  "+title, browserAccent, true), ""}
	if s := m.selectedStorage(); s != nil {
		lines = append(lines, summaryField("Datacenter", safeText(s.Name)))
	}
	lines = append(lines, summaryField("Name", safeText(m.storageDraft.name)),
		summaryField("Permissions", storageInputPermissions(m.storageDraft.permissions)), "",
		paint("TWO-FACTOR AUTHENTICATION", browserAccent, true), "Authenticator code",
		paint(strings.Repeat("•", len([]rune(m.storageDraft.otp)))+"▏", browserBranch, true),
		paint(note, browserMuted, false), "", paint("BUCKETS", browserAccent, true))
	for _, bucket := range splitStorageNames(m.storageDraft.buckets) {
		lines = append(lines, "  • "+safeText(bucket))
	}
	return lines
}

// The key form keeps the name and all grants visible together.
func (m *browser) storageKeyFormLines() []string {
	title := "CREATE ACCESS KEY"
	if m.storagePendingAction == storageOpKeyGrant {
		title = "ADD KEY GRANTS"
	}
	lines := []string{paint(storageIconKey+"  "+title, browserAccent, true), ""}
	if m.storagePendingAction == storageOpKeyGrant {
		lines[1] = paint("Existing grants are retained", browserMuted, false)
	}
	if s := m.selectedStorage(); s != nil {
		lines = append(lines, summaryField("Datacenter", safeText(s.Name)))
	}
	name := safeText(m.storageDraft.text)
	if m.storageNameEditable() {
		name += "▏"
	}
	lines = append(lines, "", "  Name  "+name, "", paint("  PERMISSIONS", browserAccent, true))
	for i, item := range []struct{ code, label string }{{"R", "Read"}, {"W", "Write"}, {"L", "List"}} {
		label := fmt.Sprintf("%-5s  (%s)", item.label, item.code)
		if m.storagePendingAction == storageOpKeyGrant && slices.Contains(m.storagePendingActions, item.label) {
			label += "  existing"
		}
		lines = append(lines, storageCheckbox(m.storageDraft.field == i+1, strings.Contains(m.storageDraft.permissions, item.code), label))
	}
	lines = append(lines, "", paint("  BUCKETS", browserAccent, true))
	if s := m.selectedStorage(); s != nil {
		if len(s.Buckets) == 0 {
			lines = append(lines, paint("  No buckets available", browserMuted, false))
		}
		selected := splitStorageNames(m.storageDraft.buckets)
		for i, bucket := range s.Buckets {
			label := safeText(bucket.Name)
			if m.storagePendingAction == storageOpKeyGrant && slices.Contains(m.storagePendingBuckets, bucket.Name) {
				label += "  existing"
			}
			lines = append(lines, storageCheckbox(m.storageDraft.field == i+4, containsStorageValue(selected, bucket.Name), label))
		}
	}
	return lines
}

func storageCheckbox(focused, checked bool, label string) string {
	box := "󰄱"
	if checked {
		box = "󰄲"
	}
	line := "  " + box + "  " + label
	if focused {
		return selectedRowStyle().Render(line)
	}
	return paint(line, browserBranch, false)
}

func (m *browser) handleStorageKeyForm(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == browserKeyEscape {
		m.storageDialog = ""
		m.storagePendingAction = ""
		m.clearStorageInputs()
		return nil
	}

	switch k {
	case inputKeyTab, keyDown:
		m.moveStorageFormField(1)
	case inputKeyShiftTab, inputKeyBacktab, keyUp:
		m.moveStorageFormField(-1)
	case keyEnter:
		if strings.TrimSpace(m.storageDraft.text) == "" || m.storageDraft.permissions == "" || m.storageDraft.buckets == "" {
			m.storageNotice = "Enter a name and select at least one permission and bucket"
			return nil
		}
		m.storageDraft.name = strings.TrimSpace(m.storageDraft.text)
		m.storageDraft.text = ""
		m.storageNotice = ""
		m.storageDialog = storageDialogWriteConfirm
		m.storageDetailScroll = 0
	case " ", "space":
		m.toggleStorageFormField()
	case keyCtrlU:
		if m.storageNameEditable() {
			m.storageDraft.text = ""
		}
	case keyBackspace, keyCtrlH:
		if m.storageNameEditable() {
			r := []rune(m.storageDraft.text)
			if len(r) > 0 {
				m.storageDraft.text = string(r[:len(r)-1])
			}
		}
	default:
		if m.storageNameEditable() && msg.Text != "" {
			m.handleStorageText(msg.Text)
		}
	}

	return nil
}

func (m *browser) moveStorageFormField(step int) {
	const fixedFields = 4
	count := fixedFields
	if storage := m.selectedStorage(); storage != nil {
		count += len(storage.Buckets)
	}
	m.storageDraft.field = (m.storageDraft.field + step + count) % count
	if m.storagePendingAction == storageOpKeyGrant && m.storageDraft.field == 0 {
		m.storageDraft.field = 1
		if step < 0 {
			m.storageDraft.field = count - 1
		}
	}
}

func (m *browser) storageNameEditable() bool {
	return m.storageDraft.field == 0 && m.storagePendingAction != storageOpKeyGrant
}

//nolint:mnd // Field positions are name, three permissions, then bucket rows.
func (m *browser) toggleStorageFormField() {
	if m.storageDraft.field == 0 {
		if m.storageNameEditable() {
			m.storageDraft.text += " "
		}
		return
	}
	if m.storageDraft.field <= 3 {
		m.toggleStoragePermission()
		return
	}
	storage := m.selectedStorage()
	index := m.storageDraft.field - 4
	if storage == nil || index >= len(storage.Buckets) {
		return
	}
	name := storage.Buckets[index].Name
	if m.storagePendingAction == storageOpKeyGrant && slices.Contains(m.storagePendingBuckets, name) {
		return
	}
	names := splitStorageNames(m.storageDraft.buckets)
	if slices.Contains(names, name) {
		names = slices.DeleteFunc(names, func(value string) bool { return value == name })
	} else {
		names = append(names, name)
	}
	m.storageDraft.buckets = strings.Join(names, ",")
}

func (m *browser) toggleStoragePermission() {
	index := m.storageDraft.field - 1
	action := []string{client.StorageRead, client.StorageWrite, client.StorageList}[index]
	if m.storagePendingAction == storageOpKeyGrant && slices.Contains(m.storagePendingActions, action) {
		return
	}
	code := []string{"R", "W", "L"}[index]
	if strings.Contains(m.storageDraft.permissions, code) {
		m.storageDraft.permissions = strings.ReplaceAll(m.storageDraft.permissions, code, "")
	} else {
		m.storageDraft.permissions += code
	}
}

func (m *browser) storageUsesOTP() bool {
	return m.storageDialog == storageDialogOTP || m.storageDialog == storageDialogVisibilityConfirm ||
		(m.storageDialog == storageDialogWriteConfirm &&
			(m.storagePendingAction == storageOpKeyCreate || m.storagePendingAction == storageOpKeyGrant))
}

func (m *browser) validateStorageOTP() bool {
	if len(m.storageDraft.otp) != 6 || !resourceDigits(m.storageDraft.otp) {
		m.storageNotice = "Enter a 6-digit authenticator code"
		return false
	}
	return true
}

func (m *browser) handleStorageText(text string) {
	if m.storageDialog == "" || m.storageBusy || m.storageDialog == storageDialogSaveReview {
		return
	}
	if m.storageUsesOTP() {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if !resourceDigits(text) || len(m.storageDraft.otp)+len(text) > 6 {
			m.storageNotice = "Authenticator codes must contain six digits"
			return
		}
		m.storageDraft.otp += text
	} else {
		if m.storageDialog == storageDialogKeyForm && (m.storageDraft.field != 0 || m.storagePendingAction == storageOpKeyGrant) {
			return
		}
		m.storageDraft.text += safeText(text)
	}
	m.storageNotice = ""
}

func (m *browser) advanceStorageDialog() tea.Cmd {
	switch m.storageDialog {
	case storageDialogBucketName:
		if strings.TrimSpace(m.storageDraft.text) != "" {
			m.storageDraft.name = strings.TrimSpace(m.storageDraft.text)
			m.storageDraft.text = ""
			return m.submitStorageWrite()
		}
	case storageDialogWriteConfirm:
		if !m.validateStorageOTP() {
			return nil
		}
		return m.submitStorageWrite()
	case storageDialogBucketDeleteConfirm, storageDialogKeyDeleteConfirm, storageDialogKeyRotateConfirm:
		target := ""
		if b := m.selectedBucket(); b != nil {
			target = b.Name
		}
		if k := m.selectedKey(); k != nil {
			target = k.Name
		}
		if target != "" && m.storageDraft.text == target {
			m.storageDialog = storageDialogOTP
			m.storageDraft.text = ""
		}
	case storageDialogVisibilityConfirm:
		if !m.validateStorageOTP() {
			return nil
		}
		return m.submitStorageWrite()
	case storageDialogOTP:
		if !m.storageWritesReady() {
			m.storageNotice = "Wait for a fresh storage read before retrying the code"
		} else if m.validateStorageOTP() {
			return m.submitStorageWrite()
		}
	case storageDialogSavePath, storageDialogSaveReview:
		return m.advanceStorageSaveDialog()
	}
	return nil
}

//nolint:nestif // Input and confirmation stay staged in the storage dialog state machine.
func (m *browser) handleStorageDialogKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if m.storageDialog == storageDialogKeyForm {
		return m.handleStorageKeyForm(msg)
	}
	if k == keyUp {
		m.storageDetailScroll = max(0, m.storageDetailScroll-1)
		return nil
	}
	if k == keyDown {
		m.storageDetailScroll++
		return nil
	}
	if k == browserKeyEscape {
		if m.storageDialog == storageDialogSaveReview {
			m.storageDialog = storageDialogSavePath
			return nil
		}
		m.storageDialog = ""
		m.storageDraft.otp = ""
		m.storagePendingAction = ""
		m.clearStorageInputs()
		return nil
	}
	if m.storageDialog == storageDialogSaveReview {
		if k == keyEnter {
			return m.advanceStorageDialog()
		}
		return nil
	}
	if k == inputKeyCtrlC {
		return tea.Quit
	}
	if k == keyBackspace || k == keyCtrlH {
		m.storageNotice = ""
		if m.storageUsesOTP() {
			r := []rune(m.storageDraft.otp)
			if len(r) > 0 {
				m.storageDraft.otp = string(r[:len(r)-1])
			}
		} else {
			r := []rune(m.storageDraft.text)
			if len(r) > 0 {
				m.storageDraft.text = string(r[:len(r)-1])
			}
		}
		return nil
	}
	if k == keyCtrlU {
		m.storageNotice = ""
		m.storageDraft.text = ""
		m.storageDraft.otp = ""
		return nil
	}
	if k == keyEnter {
		return m.advanceStorageDialog()
	}
	if msg.Text != "" {
		m.handleStorageText(msg.Text)
	}
	return nil
}
