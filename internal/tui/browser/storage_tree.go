package browser

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) storageVisibleItems() []storageTreeItem {
	var items []storageTreeItem
	for i, s := range m.storageRows {
		items = append(items, storageTreeItem{storage: i, child: -1})
		if s.ID != m.storageSelectedID || !m.storageExpanded {
			continue
		}
		count := len(s.Buckets)
		if m.storageKind == storageViewKeys {
			count = len(s.Keys)
		}
		for j := range count {
			items = append(items, storageTreeItem{storage: i, child: j})
		}
		if count == 0 {
			items = append(items, storageTreeItem{storage: i, child: -2})
		}
	}
	return items
}

func (m *browser) storageChildCount(storage *client.Storage) int {
	if storage == nil {
		return 0
	}
	if m.storageKind == storageViewKeys {
		return len(storage.Keys)
	}
	return len(storage.Buckets)
}

//nolint:mnd // Maps form fields to their rendered rows.
func storageFormFieldRow(field int) int {
	if field == 0 {
		return 4
	}
	if field <= 3 {
		return field + 6
	}
	return field + 8
}

//nolint:mnd // Three columns account for the pane border and separator.
func (m *browser) handleStorageFormMouse(msg tea.MouseClickMsg) tea.Cmd {
	listWidth, _, contextWidth := m.paneWidths()
	left := contextWidth + 3
	if m.width >= viewValue100 {
		left += listWidth + 3
	}
	if msg.X < left || msg.X >= m.width-1 {
		return nil
	}
	row := msg.Y - m.browserBodyY() - 1 + m.storageDetailScroll
	count := 4
	if storage := m.selectedStorage(); storage != nil {
		count += len(storage.Buckets)
	}
	for field := range count {
		if storageFormFieldRow(field) != row {
			continue
		}
		m.storageDraft.field = field
		if field > 0 {
			return m.handleStorageKeyForm(tea.KeyPressMsg{Code: ' ', Text: " "})
		}
		return nil
	}
	return nil
}

func (m *browser) handleStorageMouse(msg tea.MouseClickMsg) tea.Cmd {
	if m.storageDialog == storageDialogKeyForm {
		return m.handleStorageFormMouse(msg)
	}
	if m.storageBusy || m.storageDialog != "" || m.storageSecret != nil {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	appsWidth, _, contextWidth := m.paneWidths()
	if msg.X < contextWidth+2 {
		return m.handleContextPaneMouse(msg, bodyY, false)
	}
	if m.width < viewValue100 {
		if m.detail {
			return nil
		}
		appsWidth = m.compactContentWidth()
	}
	left := contextWidth + handleRegistryMouseValue3
	if msg.X >= left+appsWidth+3 {
		m.detail = true
		return nil
	}
	if msg.X < left || msg.X >= left+appsWidth+2 {
		return nil
	}
	items := m.storageVisibleItems()
	paneHeight := max(1, m.height-bodyY-browserFooterHeight-handleRegistryMouseValue3)
	index := msg.Y - bodyY - 1 + m.storageViewportStart(items, paneHeight)
	if index < 0 || index >= len(items) {
		return nil
	}
	item := items[index]
	if item.child == -2 {
		return nil
	}
	m.storageCursor = item.storage
	wasSelected := m.storageSelectedID == m.storageRows[item.storage].ID
	m.storageSelectedID = m.storageRows[item.storage].ID
	m.storageSelectionLost = false
	if !wasSelected {
		m.storageExpanded = true
	} else if item.child == -1 && m.storageSelectedChild == "" {
		m.storageExpanded = !m.storageExpanded
	}
	m.contextFocused = false
	m.detail = false
	m.storageDetailScroll = 0
	m.storageScroll = max(0, item.child)
	if item.child >= 0 {
		m.syncSelectedStorageChild()
	} else {
		m.storageSelectedChild = ""
	}
	return m.loadStorageTraffic(false)
}

func (m *browser) selectedStorage() *client.Storage {
	for i := range m.storageRows {
		if m.storageRows[i].ID == m.storageSelectedID {
			return &m.storageRows[i]
		}
	}
	return nil
}

func (m *browser) storagePaneLines(height int) []string {
	width, _, _ := m.paneWidths()
	if m.width < viewValue100 {
		width = m.compactContentWidth()
	}
	switch {
	case m.storageState.active() && len(m.storageRows) == 0:
		return fitPaneLines([]string{m.loadingMessage("Loading storages…")}, height)
	case m.storageState.err != nil && len(m.storageRows) == 0:
		return fitPaneLines([]string{"Storage list failed", "Press r to retry", safeText(m.storageState.err.Error())}, height)
	case len(m.storageRows) == 0:
		return fitPaneLines([]string{"No object storages in this tenant"}, height)
	}
	items := m.storageVisibleItems()
	start := m.storageViewportStart(items, height)
	lines := make([]string, 0, height)
	for _, item := range items[start:min(len(items), start+height)] {
		lines = append(lines, m.storageTreeLine(item, width))
	}
	return fitPaneLines(lines, height)
}

func (m *browser) storageViewportStart(items []storageTreeItem, height int) int {
	return registryViewportStart(len(items), height, m.storageSelectedRow(items))
}

func (m *browser) storageSelectedRow(items []storageTreeItem) int {
	for i, item := range items {
		if item.storage == m.storageCursor && ((item.child == -1 && m.storageSelectedChild == "") ||
			(item.child >= 0 && m.storageSelectedChild != "" && item.child == m.storageScroll)) {
			return i
		}
	}
	return 0
}

func (m *browser) moveStorageWheel(delta int) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for range 3 {
		m.moveStorageSelection(step)
	}
	m.contextFocused = false
	m.detail = false
}

func (m *browser) moveStorageSelection(step int) {
	items := m.storageVisibleItems()
	if len(items) == 0 {
		return
	}
	if m.storageSelectedID == "" {
		m.storageCursor = min(max(0, m.storageCursor), len(m.storageRows)-1)
		m.storageSelectedID = m.storageRows[m.storageCursor].ID
		m.storageSelectionLost = false
		m.storageExpanded = false
		m.storageSelectedChild = ""
		m.storageDetailScroll = 0
		return
	}
	index := m.storageSelectedRow(items) + step
	for index >= 0 && index < len(items) && items[index].child == -2 {
		index += step
	}
	if index < 0 || index >= len(items) {
		return
	}
	item := items[index]
	changedStorage := m.storageSelectedID != m.storageRows[item.storage].ID
	m.storageCursor = item.storage
	m.storageSelectedID = m.storageRows[item.storage].ID
	m.storageSelectionLost = false
	if changedStorage {
		m.storageExpanded = false
	}
	m.storageScroll = max(0, item.child)
	if item.child < 0 {
		m.storageSelectedChild = ""
	} else {
		m.syncSelectedStorageChild()
	}
	m.storageDetailScroll = 0
}

func (m *browser) storageTreeLine(item storageTreeItem, width int) string {
	s := m.storageRows[item.storage]
	selected := item.storage == m.storageCursor && s.ID == m.storageSelectedID && !m.contextFocused && !m.detail
	if item.child == -1 {
		marker := "▸"
		if s.ID == m.storageSelectedID && m.storageExpanded {
			marker = "▾"
		}
		return m.storageRootLine(marker, s, width, selected && m.storageSelectedChild == "")
	}
	if item.child == -2 {
		return m.storageFit("  └── (no "+m.storageKind+")", width, false)
	}
	branch := "├── "
	if item.child == m.storageChildCount(&s)-1 {
		branch = "└── "
	}
	var label, value string
	if m.storageKind == storageViewKeys {
		key := s.Keys[item.child]
		label, value = storageIconKey+" "+key.Name, storagePermissions(key.Actions)
	} else {
		bucket := s.Buckets[item.child]
		label, value = storageIconBucket+" "+bucket.Name, storageSize(bucket.Size)
	}
	return m.storageTreeValue("  "+branch, label, value, width,
		selected && m.storageSelectedChild != "" && item.child == m.storageScroll)
}

func (m *browser) storageRootLine(marker string, storage client.Storage, width int, selected bool) string {
	const columnSpacing = 4
	const minNameWidth = 4
	nameWidth, usageWidth, countWidth := 0, 0, 0
	for _, row := range m.storageRows {
		nameWidth = max(nameWidth, ansi.StringWidth(safeText(row.Name)))
		usageWidth = max(usageWidth, ansi.StringWidth(storageTreeUsage(row)))
		countWidth = max(countWidth, ansi.StringWidth(storageBucketCount(row)))
	}
	prefix := marker + " " + storageIconDatacenter + " "
	available := width - ansi.StringWidth(prefix) - usageWidth - countWidth - columnSpacing
	if available < minNameWidth {
		return m.storageTreeValue(prefix, storage.Name, storageTreeSummary(storage), width, selected)
	}
	nameWidth = min(nameWidth, available)
	name := ansi.Truncate(safeText(storage.Name), nameWidth, "…")
	usage := storageTreeUsage(storage)
	line := prefix + name + strings.Repeat(" ", max(0, nameWidth-ansi.StringWidth(name))) + "  " + usage +
		strings.Repeat(" ", max(0, usageWidth-ansi.StringWidth(usage))) + "  " + storageBucketCount(storage)
	return m.storageFit(line, width, selected)
}

func (m *browser) storageTreeValue(prefix, label, value string, width int, selected bool) string {
	prefix, label, value = safeText(prefix), safeText(label), safeText(value)
	suffix := "  " + value
	available := width - ansi.StringWidth(prefix) - ansi.StringWidth(suffix)
	if available < 1 {
		return m.storageFit(prefix+label+suffix, width, selected)
	}
	return m.storageFit(prefix+ansi.Truncate(label, available, "…")+suffix, width, selected)
}

func (m *browser) storageFit(line string, width int, selected bool) string {
	line = ansi.Truncate(safeText(line), width, "…")
	line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	if selected {
		return selectedRowStyle().Width(width).Render(line)
	}
	return paint(line, browserValue, false)
}

func (m *browser) syncSelectedStorageChild() {
	m.syncStorageChildSelection(m.selectedStorage(), "")
}

func (m *browser) syncStorageChildSelection(storage *client.Storage, preferred string) {
	if storage == nil {
		m.storageSelectedChild = ""
		return
	}
	if m.storageKind == storageViewKeys {
		for i := range storage.Keys {
			if (preferred != "" && storage.Keys[i].Name == preferred) || (preferred == "" && i == m.storageScroll) {
				m.storageScroll = i
				m.storageSelectedChild = storage.Keys[i].Name
				return
			}
		}
	} else {
		for i := range storage.Buckets {
			if (preferred != "" && storage.Buckets[i].ID == preferred) || (preferred == "" && i == m.storageScroll) {
				m.storageScroll = i
				m.storageSelectedChild = storage.Buckets[i].ID
				return
			}
		}
	}
	m.storageScroll = 0
	m.storageSelectedChild = ""
}

func (m *browser) selectedBucket() *client.Bucket {
	s := m.selectedStorage()
	if s == nil || m.storageKind != storageViewBuckets || m.storageScroll < 0 || m.storageScroll >= len(s.Buckets) {
		return nil
	}
	if m.storageSelectedChild == "" || s.Buckets[m.storageScroll].ID != m.storageSelectedChild {
		return nil
	}
	return &s.Buckets[m.storageScroll]
}

func (m *browser) selectedKey() *client.StorageKey {
	s := m.selectedStorage()
	if s == nil || m.storageKind != storageViewKeys || m.storageScroll < 0 || m.storageScroll >= len(s.Keys) {
		return nil
	}
	if m.storageSelectedChild == "" || s.Keys[m.storageScroll].Name != m.storageSelectedChild {
		return nil
	}
	return &s.Keys[m.storageScroll]
}
