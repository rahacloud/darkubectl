package browser

import tea "charm.land/bubbletea/v2"

func (m *browser) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	previousTarget := m.selectedDetailTarget()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		m.filterEditing = false
		m.filterBeforeEdit = m.filterQuery
		if len(m.visibleAppIndices()) > 0 {
			m.namespaceSelected = false
			m.detail = true
			return m, m.loadSelectedSummary()
		}
		return m, nil
	case "up", "down":
		m.filterEditing = false
		m.filterBeforeEdit = m.filterQuery
		if msg.String() == "up" {
			return m, m.moveTree(-1)
		} else {
			return m, m.moveTree(1)
		}
		return m, nil
	case "esc":
		m.filterEditing = false
		m.filterQuery = m.filterBeforeEdit
		for i := range m.apps {
			if m.apps[i].ID == m.filterSelection && m.filterSelection != "" {
				m.selected = i
				break
			}
		}
		m.ensureFilterSelection()
		return m, m.syncDetailTarget(previousTarget)
	case "backspace", "ctrl+h":
		runes := []rune(m.filterQuery)
		if len(runes) > 0 {
			m.filterQuery = string(runes[:len(runes)-1])
			m.ensureFilterSelection()
		}
		return m, m.syncDetailTarget(previousTarget)
	case "ctrl+u":
		m.filterQuery = ""
		m.ensureFilterSelection()
		return m, m.syncDetailTarget(previousTarget)
	}
	if msg.Text != "" {
		m.filterQuery += safeText(msg.Text)
		m.ensureFilterSelection()
	}
	return m, m.syncDetailTarget(previousTarget)
}

func (m *browser) handleSortKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.sorting = false
	case "up", "k":
		m.sortChoice = (m.sortChoice + len(appSortNames) - 1) % len(appSortNames)
	case "down", "j":
		m.sortChoice = (m.sortChoice + 1) % len(appSortNames)
	case "left", "h":
		m.sortDescending = false
	case "right", "l":
		m.sortDescending = true
	case "enter":
		m.sortBy = appSort(m.sortChoice)
		m.sorting = false
		m.sortApps()
	}
	return m, nil
}

func (m *browser) handleHelpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "?":
		m.help = false
	}
	return m, nil
}
