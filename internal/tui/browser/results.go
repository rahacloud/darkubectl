package browser

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"sort"
	"strings"
)

func (m *browser) handleAppsResult(msg appsResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestApps, msg.meta)
	if !m.appsState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.appsState.fail(msg.err)
		return m, nil
	}
	oldTarget := m.selectedDetailTarget()
	selectedID := ""
	if len(m.apps) > 0 && m.selected >= 0 && m.selected < len(m.apps) {
		selectedID = m.apps[m.selected].ID
	}
	m.appsState.succeed(len(msg.apps) == 0, m.requestTime())
	m.apps = msg.apps
	m.sortApps()
	m.selected = min(m.selected, max(0, len(m.apps)-1))
	for i := range m.apps {
		if m.apps[i].ID == selectedID && selectedID != "" {
			m.selected = i
			break
		}
	}
	m.ensureFilterSelection()
	if len(m.apps) == 0 {
		m.detailApp = nil
	} else if m.detailApp == nil || m.detailApp.ID != m.apps[m.selected].ID {
		selected := m.apps[m.selected]
		m.detailApp = &selected
	}
	if oldTarget != m.selectedDetailTarget() {
		m.invalidateAppReads()
	}
	if m.detail && m.selectedDetailTarget() != "" {
		return m, m.loadSelectedSummary()
	}
	if m.namespaceState.status == requestReady || m.namespaceState.status == requestEmpty || m.namespaceState.status == requestPartial {
		if m.namespaceFallback {
			m.useAppNamespaceFallback(m.namespaceState.err)
		} else {
			m.namespaces = mergeNamespaces(m.namespaces, namespacesFromApps(m.apps))
			m.sortNamespaces()
		}
		if m.selectingNamespace {
			m.syncNamespaceCursor()
		}
	}
	return m, nil
}

func (m *browser) handleProfileResult(msg profileResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestProfile, msg.meta)
	if !m.profileState.matches(msg.meta) {
		return m, nil
	}
	if msg.err != nil {
		m.profileState.fail(msg.err)
		if m.profileState.status != requestStale {
			m.tenants = nil
			for _, name := range m.localTenants {
				m.tenants = appendTenant(m.tenants, tenantOption{name: name, source: "Local config"})
			}
			if m.tenant != "" {
				m.tenants = appendTenant(m.tenants, tenantOption{name: m.tenant, source: "Local config", status: "current"})
				m.tenantStatus = "local config"
			}
		}
	} else {
		m.profileState.succeed(msg.profile == nil || len(msg.profile.Organizations) == 0, m.requestTime())
		m.tenantStatus = ""
		m.tenants = nil
		if msg.profile != nil {
			for _, org := range msg.profile.Organizations {
				status := ""
				if org.Name == m.tenant {
					status = "current"
				}
				m.tenants = append(m.tenants, tenantOption{name: org.Name, source: "Profile", status: status})
			}
		}
		if m.tenant != "" && !containsTenant(m.tenants, m.tenant) {
			m.tenantStatus = "unverified"
			m.tenants = appendTenant(m.tenants, tenantOption{name: m.tenant, source: "Configured org", status: "unverified"})
		}
	}
	if m.tenant != "" {
		for i, option := range m.tenants {
			if option.name == m.tenant {
				m.tenantCursor = i
				break
			}
		}
	} else if len(m.tenants) > 0 {
		m.tenantCursor = min(m.tenantCursor, len(m.tenants)-1)
	}
	if m.tenant == "" {
		m.contextFocused = true
		m.contextRow = 0
	}
	return m, nil
}

func (m *browser) handleAppDetailResult(msg appDetailResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestSummary, msg.meta)
	if !m.detailState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration || m.selectedDetailTarget() != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil || msg.app == nil || msg.app.ID != msg.meta.appID {
		if msg.err == nil {
			msg.err = fmt.Errorf("detail response did not match the selected app")
		}
		m.detailState.fail(msg.err)
		return m, nil
	}
	m.detailState.succeed(false, m.requestTime())
	m.detailApp = msg.app
	return m, nil
}

func (m *browser) handleAppUsageResult(msg appUsageResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestUsage, msg.meta)
	if !m.usageState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration || m.selectedDetailTarget() != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil {
		m.markCurrentUsageStale(msg.meta.appID)
		m.usageState.fail(fmt.Errorf("resource usage request failed"))
		return m, nil
	}
	if msg.usage == nil {
		m.markCurrentUsageStale(msg.meta.appID)
		m.usageState.fail(fmt.Errorf("resource usage response was empty"))
		return m, nil
	}
	previous := m.detailUsage
	if m.usageAppID != msg.meta.appID {
		previous = nil
	}
	usage := *msg.usage
	if previous != nil {
		if usage.Failed.CPU {
			usage.CPUCores = previous.CPUCores
		}
		if usage.Failed.RAM {
			usage.RAMBytes = previous.RAMBytes
		}
		if usage.Failed.Disk {
			usage.DiskBytes = previous.DiskBytes
		}
	}
	m.detailUsage = &usage
	m.usageAppID = msg.meta.appID
	failedCount := boolCount(usage.Failed.CPU) + boolCount(usage.Failed.RAM) + boolCount(usage.Failed.Disk)
	sampleCount := boolCount(usage.CPUCores != nil) + boolCount(usage.RAMBytes != nil) + boolCount(usage.DiskBytes != nil)
	switch {
	case failedCount == 3:
		m.usageState.fail(fmt.Errorf("all resource metrics are unavailable"))
	case failedCount > 0 || sampleCount > 0 && sampleCount < 3:
		m.usageState.partial(m.requestTime())
	case sampleCount == 0:
		m.usageState.succeed(true, m.requestTime())
	default:
		m.usageState.succeed(false, m.requestTime())
	}
	return m, nil
}

func (m *browser) handleUsageHistoryResult(msg usageHistoryResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestUsageHistory, msg.meta)
	if !m.historyState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration || m.selectedDetailTarget() != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil || msg.history == nil {
		m.historyState.fail(fmt.Errorf("history request failed"))
		return m, nil
	}
	failed := boolCount(msg.history.Failed.CPU) + boolCount(msg.history.Failed.CPUThrottle) + boolCount(msg.history.Failed.RAM) + boolCount(msg.history.Failed.Disk) + boolCount(msg.history.Failed.NetworkReceive) + boolCount(msg.history.Failed.NetworkSend)
	samples := len(msg.history.CPU) + len(msg.history.CPUThrottle) + len(msg.history.RAM) + len(msg.history.Disk) + len(msg.history.NetworkReceive) + len(msg.history.NetworkSend)
	switch {
	case failed >= 5:
		m.historyState.fail(fmt.Errorf("history metrics unavailable"))
	case failed > 0:
		m.history = msg.history
		m.syncHistoryPod()
		m.historyState.partial(m.requestTime())
	default:
		m.history = msg.history
		m.syncHistoryPod()
		m.historyState.succeed(samples == 0, m.requestTime())
	}
	return m, nil
}

func (m *browser) handlePodsResult(msg podsResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestPods, msg.meta)
	if !m.podsState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration || m.selectedDetailTarget() != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil {
		if strings.Contains(msg.err.Error(), "requires a Console JWT") {
			m.podsState.unavailable(msg.err)
			return m, nil
		}
		if strings.Contains(strings.ToLower(msg.err.Error()), "401") {
			m.podsState.fail(fmt.Errorf("Console rejected the session; run `darkubectl login` and retry"))
			return m, nil
		}
		m.podsState.fail(fmt.Errorf("pod request failed"))
		return m, nil
	}
	sort.Slice(msg.pods, func(i, j int) bool { return msg.pods[i].Name < msg.pods[j].Name })
	m.podList = msg.pods
	m.podsState.succeed(len(m.podList) == 0, m.requestTime())
	if m.activeAppView == appViewLogs && m.logPod == "" {
		return m, m.prepareLogTarget()
	}
	return m, nil
}

func (m *browser) handleEgressRoutesResult(msg egressRoutesResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestEgress, msg.meta)
	if !m.egressState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.egressState.fail(fmt.Errorf("outbound IP request failed"))
		return m, nil
	}
	m.egressData = msg.routes
	m.egressState.succeed(len(msg.routes) == 0, m.requestTime())
	return m, nil
}

func (m *browser) handleNamespacesResult(msg namespacesResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestNamespaces, msg.meta)
	if !m.namespaceState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.namespaceState.fail(msg.err)
		if m.namespaceState.status != requestStale {
			m.useAppNamespaceFallback(msg.err)
		}
	} else if len(msg.namespaces) == 0 && len(m.apps) > 0 {
		m.useAppNamespaceFallback(nil)
		m.namespaceState.partial(m.requestTime())
	} else {
		m.namespaces = mergeNamespaces(msg.namespaces, namespacesFromApps(m.apps))
		m.sortNamespaces()
		m.namespaceFallback = false
		m.namespaceState.succeed(len(m.namespaces) == 0, m.requestTime())
	}
	m.syncNamespaceCursor()
	return m, nil
}
