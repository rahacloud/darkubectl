package browser

import tea "charm.land/bubbletea/v2"

func (m *browser) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	previousTarget := m.selectedDetailTarget()
	switch msg.String() {
	case inputKeyCtrlC:
		return m, tea.Quit
	case keyEnter:
		m.filterEditing = false
		m.filterBeforeEdit = m.filterQuery
		if len(m.visibleAppIndices()) > 0 {
			m.namespaceSelected = false
			m.detail = true
			return m, m.loadSelectedSummary()
		}
		return m, nil
	case "up", keyDown:
		m.filterEditing = false
		m.filterBeforeEdit = m.filterQuery
		if msg.String() == "up" {
			return m, m.moveTree(-1)
		}
		return m, m.moveTree(1)
	case browserKeyEscape:
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
	case keyBackspace, keyCtrlH:
		runes := []rune(m.filterQuery)
		if len(runes) > 0 {
			m.filterQuery = string(runes[:len(runes)-1])
			m.ensureFilterSelection()
		}
		return m, m.syncDetailTarget(previousTarget)
	case keyCtrlU:
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
	switch m.page {
	case pageActivity:
		var sortBy activitySort
		options, sortBy, descending = m.activitySortOptions()
		if !m.sorting {
			m.sortChoice = int(sortBy)
		}
	case pageDB:
		options = dbSortNames[:]
		descending = m.databaseSortDescending
		if !m.sorting {
			m.sortChoice = int(m.databaseSortBy)
		}
	case pageApps, pageRegistry, pageTickets, pageStorage:
		break
	}
	switch msg.String() {
	case inputKeyCtrlC:
		return m, tea.Quit
	case browserKeyEscape:
		m.sorting = false
	case "up", "k":
		m.sortChoice = (m.sortChoice + len(options) - 1) % len(options)
	case keyDown, "j":
		m.sortChoice = (m.sortChoice + 1) % len(options)
	case keyLeft, "h":
		descending = false
	case keyRight, "l":
		descending = true
	case keyEnter:
		switch m.page {
		case pageActivity:
			m.applyActivitySort(m.sortChoice, descending)
		case pageDB:
			m.databaseSortBy = dbSort(m.sortChoice)
			m.databaseSortDescending = descending
			m.sortDatabases()
			m.databaseCursor = 0
			cmd = m.updateDatabaseSelection()
		default:
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
	case inputKeyCtrlC, "q":
		return m, tea.Quit
	case browserKeyEscape, "?":
		m.help = false
	}
	return m, nil
}
