package browser

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	handleAppUsageResultValue3     = 3
	handleUsageHistoryResultValue5 = 5
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
	selectedID := m.selectedAppID()
	m.appsState.succeed(len(msg.apps) == 0, m.requestTime())
	m.reconcileApps(msg.apps, selectedID)
	if oldTarget != m.selectedDetailTarget() {
		m.invalidateAppReads()
	}
	if m.detail && m.selectedDetailTarget() != "" {
		return m, m.loadSelectedSummary()
	}
	var selectedPodsCmd tea.Cmd
	if oldTarget != m.selectedDetailTarget() && m.page == pageApps {
		selectedPodsCmd = m.loadSelectedPods()
	}
	m.syncNamespacesAfterApps()
	return m, selectedPodsCmd
}

func (m *browser) selectedAppID() string {
	if m.selected < 0 || m.selected >= len(m.apps) {
		return ""
	}
	return m.apps[m.selected].ID
}

func (m *browser) reconcileApps(apps []client.App, selectedID string) {
	m.apps = apps
	m.sortApps()
	m.selected = min(m.selected, max(0, len(m.apps)-1))
	for i := range m.apps {
		if selectedID == "" || m.apps[i].ID != selectedID {
			continue
		}
		m.selected = i
		break
	}
	m.ensureFilterSelection()
	if len(m.apps) == 0 {
		m.detailApp = nil
		return
	}
	if m.detailApp == nil || m.detailApp.ID != m.apps[m.selected].ID {
		selected := m.apps[m.selected]
		m.detailApp = &selected
	}
}

func (m *browser) syncNamespacesAfterApps() {
	if m.namespaceState.status != requestReady && m.namespaceState.status != requestEmpty && m.namespaceState.status != requestPartial {
		return
	}
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

func (m *browser) handleProfileResult(msg profileResult) tea.Model {
	m.finishRequest(requestProfile, msg.meta)
	if !m.profileState.matches(msg.meta) {
		return m
	}
	if msg.err != nil {
		m.handleProfileError(msg.err)
	} else {
		m.handleProfileSuccess(msg.profile)
	}
	m.syncProfileTenantCursor()
	if m.tenant == "" {
		m.contextFocused = true
		m.contextRow = 0
	}
	return m
}

func (m *browser) handleProfileError(err error) {
	m.profileState.fail(err)
	if m.profileState.status == requestStale {
		return
	}
	m.tenants = nil
	for _, name := range m.localTenants {
		m.tenants = appendTenant(m.tenants, tenantOption{name: name, source: "Local config"})
	}
	if m.tenant != "" {
		m.tenants = appendTenant(m.tenants, tenantOption{name: m.tenant, source: "Local config", status: labelCurrent})
		m.tenantStatus = "local config"
	}
}

func (m *browser) handleProfileSuccess(profile *client.Profile) {
	m.profileState.succeed(profile == nil || len(profile.Organizations) == 0, m.requestTime())
	m.userEmail = ""
	m.tenantStatus = ""
	m.tenants = nil
	if profile != nil {
		m.userEmail = safeText(profile.Email)
		for _, org := range profile.Organizations {
			status := ""
			if org.Name == m.tenant {
				status = "current"
			}
			m.tenants = append(m.tenants, tenantOption{name: org.Name, source: "Profile", status: status, roles: append([]string(nil),
				org.Roles...)})
		}
	}
	if m.tenant != "" && !containsTenant(m.tenants, m.tenant) {
		m.tenantStatus = "unverified"
		m.tenants = appendTenant(m.tenants, tenantOption{name: m.tenant, source: "Configured org", status: "unverified"})
	}
}

func (m *browser) syncProfileTenantCursor() {
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
}

func (m *browser) handleAppDetailResult(msg appDetailResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestSummary, msg.meta)
	if !m.detailState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		m.selectedDetailTarget() != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil || msg.app == nil || msg.app.ID != msg.meta.appID {
		if msg.err == nil {
			msg.err = errDetailResponseDidNotMatchTheSelected
		}
		m.detailState.fail(msg.err)
		return m, nil
	}
	m.detailState.succeed(false, m.requestTime())
	m.detailApp = msg.app
	m.syncResourceDraftFromDetail()
	if m.detail && m.activeAppView == appViewBuilds && msg.app.BuildMethod == client.BuildMethodWebhook {
		return m, m.loadSelectedBuilds()
	}
	return m, nil
}

func (m *browser) handleBuildsResult(msg buildsResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestBuilds, msg.meta)
	if !m.buildsState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		m.selectedDetailTarget() != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil {
		m.handleBuildsError(msg.err)
		return m, nil
	}
	return m, m.acceptBuildsResult(msg.builds)
}

func (m *browser) handleBuildsError(err error) {
	m.buildsState.fail(err)
	if m.buildActionAwaiting != buildActionNone {
		m.buildActionNotice = "Submission accepted · follow-up observation failed; outcome unknown"
		m.buildActionAwaiting = buildActionNone
	}
}

func (m *browser) acceptBuildsResult(builds []client.Build) tea.Cmd {
	selected := m.buildSelectedID
	m.builds = builds
	m.buildsState.succeed(len(builds) == 0, m.requestTime())
	m.buildListCursor = 0
	m.buildListCursor = findBuildIndex(m.builds, selected, 0)
	var cmd tea.Cmd
	if m.buildActionAwaiting == buildActionRetry && m.buildActionRetryID != "" {
		if index := findBuildIndex(m.builds, m.buildActionRetryID, -1); index >= 0 {
			m.buildListCursor = index
			m.buildSelectedID = m.builds[index].ID
			if m.buildDetail != nil {
				cmd = m.openSelectedBuild()
			}
		}
	}
	if len(m.builds) > 0 {
		m.buildSelectedID = m.builds[m.buildListCursor].ID
	} else {
		m.buildSelectedID = ""
	}
	m.finishBuildActionObservation()
	return cmd
}

func findBuildIndex(builds []client.Build, id client.BuildID, fallback int) int {
	if id == "" {
		return fallback
	}
	for i, build := range builds {
		if build.ID == id {
			return i
		}
	}
	return fallback
}

func (m *browser) finishBuildActionObservation() {
	switch m.buildActionAwaiting {
	case buildActionStop:
		m.buildActionNotice = m.stopBuildObservation()
	case buildActionRetry:
		m.buildActionNotice = m.retryBuildObservation()
	case buildActionLatest:
		m.buildActionNotice = "Build and deploy accepted · refreshed list; new build attribution is unknown"
	default:
		return
	}
	if m.actionIsMock() {
		m.buildActionNotice = "MOCK · simulated · " + m.buildActionNotice
	}
	m.buildActionAwaiting = buildActionNone
	m.buildActionRetryID = ""
}

func (m *browser) stopBuildObservation() string {
	index := findBuildIndex(m.builds, m.buildActionAwaitingID, -1)
	if index < 0 {
		return "Stop accepted · build not present in refreshed list; outcome unknown"
	}
	status := m.builds[index].Status
	if status == client.BuildCanceled {
		return "Stop accepted · canceled state observed"
	}
	return "Stop accepted · observed status: " + safeText(status)
}

func (m *browser) retryBuildObservation() string {
	if m.buildActionRetryID == "" {
		return "Retry accepted · refreshed list; new build attribution is unknown"
	}
	if findBuildIndex(m.builds, m.buildActionRetryID, -1) >= 0 {
		return "Retry accepted · returned build ID found in refreshed list"
	}
	return "Retry accepted · returned ID not found in refreshed list; outcome unknown"
}

func (m *browser) handleBuildResult(msg buildResult) tea.Model {
	m.finishRequest(requestBuild, msg.meta)
	if !m.buildDetailState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		m.selectedDetailTarget() != msg.meta.appID || m.buildSelectedID != msg.id {
		return m
	}
	if msg.err != nil {
		m.buildDetailState.fail(msg.err)
		return m
	}
	if msg.build == nil || msg.build.ID != msg.id {
		m.buildDetailState.fail(errBuildResponseDidNotMatchSelectedID)
		return m
	}
	if len(msg.build.Log) > buildOutputLimit {
		msg.build.Log = msg.build.Log[:buildOutputLimit]
	}
	m.buildDetail = msg.build
	m.buildDetailState.succeed(false, m.requestTime())
	m.buildScroll = 0
	return m
}

func (m *browser) handleAppUsageResult(msg appUsageResult) tea.Model {
	m.finishRequest(requestUsage, msg.meta)
	if !m.usageState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		m.selectedDetailTarget() != msg.meta.appID {
		return m
	}
	if msg.err != nil {
		m.markCurrentUsageStale(msg.meta.appID)
		m.usageState.fail(errResourceUsageRequestFailed)
		return m
	}
	if msg.usage == nil {
		m.markCurrentUsageStale(msg.meta.appID)
		m.usageState.fail(errResourceUsageResponseWasEmpty)
		return m
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
	case failedCount == handleAppUsageResultValue3:
		m.usageState.fail(errAllResourceMetricsAreUnavailable)
	case failedCount > 0 || sampleCount > 0 && sampleCount < 3:
		m.usageState.partial(m.requestTime())
	case sampleCount == 0:
		m.usageState.succeed(true, m.requestTime())
	default:
		m.usageState.succeed(false, m.requestTime())
	}
	return m
}

func (m *browser) handleUsageHistoryResult(msg usageHistoryResult) tea.Model {
	m.finishRequest(requestUsageHistory, msg.meta)
	if !m.historyState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		m.selectedDetailTarget() != msg.meta.appID {
		return m
	}
	if msg.err != nil || msg.history == nil {
		m.historyState.fail(errHistoryRequestFailed)
		return m
	}
	failed := boolCount(msg.history.Failed.CPU) + boolCount(msg.history.Failed.CPUThrottle) +
		boolCount(msg.history.Failed.RAM) + boolCount(msg.history.Failed.Disk) +
		boolCount(msg.history.Failed.NetworkReceive) + boolCount(msg.history.Failed.NetworkSend)
	samples := len(msg.history.CPU) + len(msg.history.CPUThrottle) + len(msg.history.RAM) +
		len(msg.history.Disk) + len(msg.history.NetworkReceive) + len(msg.history.NetworkSend)
	switch {
	case failed >= handleUsageHistoryResultValue5:
		m.historyState.fail(errHistoryMetricsUnavailable)
	case failed > 0:
		m.history = msg.history
		m.syncHistoryPod()
		m.historyState.partial(m.requestTime())
	default:
		m.history = msg.history
		m.syncHistoryPod()
		m.historyState.succeed(samples == 0, m.requestTime())
	}
	return m
}

func (m *browser) handlePodsResult(msg podsResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestPods, msg.meta)
	if !m.podsState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		m.selectedDetailTarget() != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil {
		if m.terminalRequestPending {
			m.terminalRequestPending = false
			m.actionNotice = "Could not load pods for terminal access"
		}
		if strings.Contains(msg.err.Error(), "requires a Console JWT") {
			m.podsState.unavailable(msg.err)
			return m, nil
		}
		if strings.Contains(strings.ToLower(msg.err.Error()), "401") {
			m.podsState.fail(errConsoleRejectedSession)
			return m, nil
		}
		m.podsState.fail(fmt.Errorf("pod request failed: %w", msg.err))
		return m, nil
	}
	sort.Slice(msg.pods, func(i, j int) bool { return msg.pods[i].Name < msg.pods[j].Name })
	m.podList = msg.pods
	m.podsState.succeed(len(m.podList) == 0, m.requestTime())
	if m.terminalRequestPending {
		m.terminalRequestPending = false
		if len(m.podList) == 0 {
			m.actionNotice = "No pods are available for terminal access"
			return m, nil
		}
		return m, m.beginTerminalPicker()
	}
	if m.activeAppView == appViewLogs && m.logPod == "" {
		return m, m.prepareLogTarget()
	}
	return m, nil
}

func (m *browser) handleEgressRoutesResult(msg egressRoutesResult) tea.Model {
	m.finishRequest(requestEgress, msg.meta)
	if !m.egressState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m
	}
	if msg.err != nil {
		m.egressState.fail(errOutboundIPRequestFailed)
		return m
	}
	m.egressData = msg.routes
	m.egressState.succeed(len(msg.routes) == 0, m.requestTime())
	return m
}

func (m *browser) handleNamespacesResult(msg namespacesResult) tea.Model {
	m.finishRequest(requestNamespaces, msg.meta)
	if !m.namespaceState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m
	}
	switch {
	case msg.err != nil:
		m.namespaceState.fail(msg.err)
		if m.namespaceState.status != requestStale {
			m.useAppNamespaceFallback(msg.err)
		}
	case len(msg.namespaces) == 0 && len(m.apps) > 0:
		m.useAppNamespaceFallback(nil)
		m.namespaceState.partial(m.requestTime())
	default:
		m.namespaces = mergeNamespaces(msg.namespaces, namespacesFromApps(m.apps))
		m.sortNamespaces()
		m.namespaceFallback = false
		m.namespaceState.succeed(len(m.namespaces) == 0, m.requestTime())
	}
	m.syncNamespaceCursor()
	return m
}
