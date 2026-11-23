package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	activatePageRowValue2  = 2
	activatePageRowValue4  = 4
	activatePageRowValue5  = 5
	activatePageRowStorage = 6
	focusPaneValue2        = 2
	focusPaneValue3        = 3
	pageTicketsContextRow  = 3
)

func (m *browser) cycleFocus(reverse bool) tea.Cmd {
	m.selectingTenant = false
	if reverse {
		switch {
		case m.contextFocused:
			if m.contextRow > 1 {
				m.contextRow--
				return nil
			}
			if m.contextRow == 1 {
				m.contextRow = 0
				return nil
			}
			m.contextFocused = false
			m.detail = true
			return m.loadFocusedSummary()
		case m.detail:
			m.detail = false
		default:
			switch m.page {
			case pageActivity:
				m.contextFocused = true
				m.contextRow = 2
			case pageTickets:
				m.contextFocused = true
				m.contextRow = 3
			case pageDB:
				m.contextFocused = true
				m.contextRow = 4
			case pageRegistry:
				m.contextFocused = true
				m.contextRow = 5
			case pageStorage:
				m.contextFocused = true
				m.contextRow = 6
			default:
				m.contextFocused = true
				m.contextRow = 1
			}
		}
		return nil
	}
	switch {
	case m.contextFocused:
		if m.contextRow == 0 {
			m.contextRow = 1
			return nil
		}
		if m.contextRow > 1 {
			m.contextFocused = false
			m.detail = false
			return m.activatePageRow(m.contextRow)
		}
		m.contextFocused = false
		m.detail = false
	case m.detail:
		m.detail = false
		m.contextFocused = true
		m.contextRow = 0
	default:
		m.detail = true
		return m.loadFocusedSummary()
	}
	return nil
}

func (m *browser) focusPane(index int) tea.Cmd {
	m.selectingTenant = false
	switch index {
	case 0:
		m.contextFocused = true
		m.contextRow = 0
		m.selectingTenant = true
		m.detail = false
	case 1:
		m.contextFocused = true
		switch m.page {
		case pageActivity:
			m.contextRow = 2
		case pageTickets:
			m.contextRow = 3
		case pageDB:
			m.contextRow = 4
		case pageRegistry:
			m.contextRow = 5
		case pageStorage:
			m.contextRow = 6
		default:
			m.contextRow = 1
		}
		m.detail = false
	case focusPaneValue2:
		m.contextFocused = false
		m.detail = false
	case focusPaneValue3:
		m.contextFocused = false
		m.detail = true
		return m.loadFocusedSummary()
	}
	return nil
}

func (m *browser) loadFocusedSummary() tea.Cmd {
	if m.page == pageActivity || m.page == pageTickets || m.page == pageDB || m.page == pageRegistry || m.page == pageStorage {
		return nil
	}
	return m.loadSelectedSummary()
}

func (m *browser) activatePageRow(row int) tea.Cmd {
	switch row {
	case 1:
		if m.page == pageDB {
			m.resetDatabaseDetail()
		}
		m.page = pageApps
		m.contextFocused = true
		m.detail = false
		return nil
	case activatePageRowValue2:
		if m.page == pageDB {
			m.resetDatabaseDetail()
		}
		if m.page != pageActivity {
			m.page = pageActivity
			m.activityTab = activityAlerts
			m.detail = false
			m.activityScroll = 0
		}
		return m.loadActivityFeed(false)
	case pageTicketsContextRow:
		m.page = pageTickets
		m.contextFocused = true
		m.contextRow = pageTicketsContextRow
		m.selectingTenant = false
		m.detail = false
		m.ticketStatus = client.TicketStatusAll
		m.ticketMine = true
		return m.loadTickets(false)
	case activatePageRowValue4:
		m.page = pageDB
		m.contextFocused = true
		m.contextRow = activatePageRowValue4
		m.selectingTenant = false
		m.detail = false
		return tea.Batch(m.loadDatabases(false), m.loadSelectedDatabaseDetail(false))
	case activatePageRowValue5:
		if m.page == pageDB {
			m.resetDatabaseDetail()
		}
		m.page = pageRegistry
		m.contextFocused = true
		m.contextRow = activatePageRowValue5
		m.selectingTenant = false
		m.detail = false
		m.registryNotice = ""
		return m.loadRegistry(false)
	case activatePageRowStorage:
		m.page = pageStorage
		m.contextFocused = true
		m.contextRow = activatePageRowStorage
		m.selectingTenant = false
		m.detail = false
		m.storageKind = "buckets"
		return m.loadStorages(false)
	default:
		return nil
	}
}

func (m *browser) moveTenantSelection(delta int) tea.Cmd {
	if len(m.tenants) == 0 {
		return nil
	}
	previous := m.tenantCursor
	m.tenantCursor = min(max(0, m.tenantCursor+delta), len(m.tenants)-1)
	if previous == m.tenantCursor || m.tenants[m.tenantCursor].name == m.tenant {
		return nil
	}
	cmd := m.chooseTenant(m.tenants[m.tenantCursor].name)
	// Keep focus in the tenant list after the immediate switch so the user can
	// continue moving through tenants without reopening a selector.
	m.contextFocused = true
	m.contextRow = 0
	m.selectingTenant = true
	return cmd
}

func appendTenant(tenants []tenantOption, option tenantOption) []tenantOption {
	for i := range tenants {
		if tenants[i].name == option.name {
			if option.status != "" {
				tenants[i].status = option.status
			}
			return tenants
		}
	}
	return append(tenants, option)
}

func containsTenant(tenants []tenantOption, name string) bool {
	for _, option := range tenants {
		if option.name == name {
			return true
		}
	}
	return false
}

func (m *browser) chooseTenant(name string) tea.Cmd {
	for i := range m.tenants {
		m.tenants[i].status = ""
		if m.tenants[i].name == name {
			m.tenants[i].status = labelCurrent
		}
	}
	if name == m.tenant {
		m.selectingTenant = false
		m.contextFocused = false
		return nil
	}
	m.tenant = name
	m.resetTenantContext()
	cmd := m.load()
	return m.loadTenantPage(cmd)
}

func (m *browser) resetTenantContext() {
	m.cancelRequests(true)
	m.registryState.reset()
	m.resetStorageContext()
	m.registries = nil
	m.registryRepos = nil
	m.registryDigests = nil
	m.registryDigestErrors = nil
	m.registryRows = nil
	m.registryCursor = 0
	m.cancelRequest(requestKey{kind: requestDatabases})
	m.cancelRequest(requestKey{kind: requestTickets})
	m.cancelRequest(requestKey{kind: requestTicketThread})
	m.ticketState.reset()
	m.ticketDetailState.reset()
	m.ticketRows = nil
	m.ticketComments = nil
	m.ticketDetail = nil
	m.ticketSelection = ""
	m.databaseState.reset()
	m.databases = nil
	m.databaseCursor = 0
	m.databaseSelected = ""
	m.databaseScroll = 0
	m.resetDatabaseDetail()
	m.cancelRequest(requestKey{kind: requestAlerts})
	m.activityAlertsState.reset()
	m.activityAlertsData = nil
	m.activityAlertCursor = 0
	m.activityAlertSelected = ""
	m.tenantGeneration++
	m.egressState.reset()
	m.egressData = nil
	m.invalidateAppReads()
	m.tenantStatus = ""
	m.selectingTenant = false
	m.apps = nil
	m.appsState.reset()
	m.selected = 0
	m.namespaceSelected = false
	m.filterQuery = ""
	m.filterBeforeEdit = ""
	m.filterEditing = false
	m.collapsed = nil
	m.detail = false
	m.contextFocused = false
	m.detailApp = nil
	m.detailState.reset()
	m.usageState.reset()
	m.detailUsage = nil
	m.usageAppID = ""
	m.namespaceScoped = false
	m.namespaceScope = namespaceKey{}
	m.namespaces = nil
	m.namespaceState.reset()
	m.namespaceFallback = false
	m.selectingNamespace = false
}

func (m *browser) loadTenantPage(cmd tea.Cmd) tea.Cmd {
	if m.page == pageActivity && m.activityTab == activityAlerts {
		return tea.Batch(cmd, m.loadActivityFeed(true))
	}
	if m.page == pageRegistry {
		return tea.Batch(cmd, m.loadRegistry(true))
	}
	if m.page == pageStorage {
		return tea.Batch(cmd, m.loadStorages(true))
	}
	if m.page == pageDB {
		return tea.Batch(cmd, m.loadDatabases(true))
	}
	if m.page == pageTickets {
		return m.loadTickets(true)
	}
	return cmd
}
