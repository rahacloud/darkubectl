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
	var cmd tea.Cmd
	options := appSortNames[:]
	descending := m.sortDescending
	if m.page == pageActivity {
		var sortBy activitySort
		options, sortBy, descending = m.activitySortOptions()
		if !m.sorting {
			m.sortChoice = int(sortBy)
		}
	} else if m.page == pageDB {
		options = dbSortNames[:]
		descending = m.databaseSortDescending
		if !m.sorting {
			m.sortChoice = int(m.databaseSortBy)
		}
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.sorting = false
	case "up", "k":
		m.sortChoice = (m.sortChoice + len(options) - 1) % len(options)
	case "down", "j":
		m.sortChoice = (m.sortChoice + 1) % len(options)
	case "left", "h":
		descending = false
	case "right", "l":
		descending = true
	case "enter":
		if m.page == pageActivity {
			m.applyActivitySort(m.sortChoice, descending)
		} else if m.page == pageDB {
			m.databaseSortBy = dbSort(m.sortChoice)
			m.databaseSortDescending = descending
			m.sortDatabases()
			m.databaseCursor = 0
			cmd = m.updateDatabaseSelection()
		} else {
			m.sortBy = appSort(m.sortChoice)
			m.sortDescending = descending
			m.sortApps()
		}
		m.sorting = false
	}
	if m.page == pageActivity && m.sorting {
		m.setActivitySortDirection(descending)
	}
	if m.page != pageActivity {
		m.sortDescending = descending
	}
	return m, cmd
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
