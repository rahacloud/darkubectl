package browser

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) selectedDetailTarget() string {
	if m.namespaceSelected || !m.selectedAppVisible() {
		return ""
	}
	return m.apps[m.selected].ID
}

func (m *browser) syncDetailTarget(previous string) tea.Cmd {
	current := m.selectedDetailTarget()
	if previous == current {
		return nil
	}
	m.invalidateAppReads()
	if current == "" {
		return nil
	}
	selected := m.apps[m.selected]
	m.detailApp = &selected
	if m.detail {
		return m.loadSelectedSummary()
	}
	return nil
}

func (m *browser) load() tea.Cmd {
	tenant := m.tenant
	meta, requestCtx := m.startRequest(requestApps, tenant, "", m.tenantGeneration)
	m.appsState.begin(meta, m.requestTime())
	if m.listApps == nil {
		m.finishRequest(requestApps, meta)
		m.appsState.unavailable(fmt.Errorf("app listing is unavailable"))
		return nil
	}
	appsCmd := func() tea.Msg {
		apps, err := m.listApps(requestCtx, tenant)
		return appsResult{meta: meta, apps: apps, err: err}
	}
	return tea.Batch(appsCmd, m.loadEgressRoutes(false))
}

func (m *browser) loadProfile() tea.Cmd {
	if m.profile == nil {
		m.profileState.unavailable(nil)
		return nil
	}
	meta, requestCtx := m.startRequest(requestProfile, "", "", 0)
	m.profileState.begin(meta, m.requestTime())
	return func() tea.Msg {
		profile, err := m.profile(requestCtx)
		return profileResult{meta: meta, profile: profile, err: err}
	}
}

func (m *browser) loadNamespaces() tea.Cmd {
	if m.namespaceState.status == requestReady || m.namespaceState.status == requestEmpty || m.namespaceState.status == requestUnavailable || m.namespaceState.matches(m.namespaceState.meta) {
		return nil
	}
	if m.listNamespaces == nil {
		m.useAppNamespaceFallback(nil)
		m.namespaceState.unavailable(nil)
		m.syncNamespaceCursor()
		return nil
	}
	tenant := m.tenant
	meta, requestCtx := m.startRequest(requestNamespaces, tenant, "", m.tenantGeneration)
	m.namespaceState.begin(meta, m.requestTime())
	return func() tea.Msg {
		namespaces, err := m.listNamespaces(requestCtx, tenant)
		return namespacesResult{meta: meta, namespaces: namespaces, err: err}
	}
}

func (m *browser) useAppNamespaceFallback(cause error) {
	byKey := make(map[namespaceKey]client.Namespace)
	for _, app := range m.apps {
		ns := app.Namespace
		byKey[namespaceKey{id: ns.ID, name: ns.Name}] = ns
	}
	m.namespaces = m.namespaces[:0]
	for _, namespace := range byKey {
		m.namespaces = append(m.namespaces, namespace)
	}
	m.sortNamespaces()
	m.namespaceFallback = true
	if cause != nil {
		m.namespaceState.err = cause
	}
}

func (m *browser) loadSelectedSummary() tea.Cmd {
	id := m.selectedDetailTarget()
	if id == "" {
		return nil
	}
	selected := m.apps[m.selected]
	if m.detailApp == nil || m.detailApp.ID != selected.ID {
		m.detailApp = &selected
	}
	tenant := m.tenant
	var cmds []tea.Cmd
	if !m.detailState.active() {
		meta, requestCtx := m.startRequest(requestSummary, tenant, id, m.selectionGeneration)
		m.detailState.begin(meta, m.requestTime())
		if m.getApp == nil {
			m.finishRequest(requestSummary, meta)
			m.detailState.unavailable(fmt.Errorf("app detail is unavailable"))
		} else {
			getApp := m.getApp
			cmds = append(cmds, func() tea.Msg {
				app, err := getApp(requestCtx, tenant, id)
				return appDetailResult{meta: meta, app: app, err: err}
			})
		}
	}
	if podsCmd := m.loadSelectedPods(); podsCmd != nil {
		cmds = append(cmds, podsCmd)
	}
	if egressCmd := m.loadEgressRoutes(false); egressCmd != nil {
		cmds = append(cmds, egressCmd)
	}
	return tea.Batch(cmds...)
}

func (m *browser) loadSelectedUsage() tea.Cmd {
	id := m.selectedDetailTarget()
	if id == "" || m.usageState.active() {
		return nil
	}
	tenant := m.tenant
	meta, requestCtx := m.startRequest(requestUsage, tenant, id, m.selectionGeneration)
	m.usageState.begin(meta, m.requestTime())
	if m.usage == nil {
		m.finishRequest(requestUsage, meta)
		m.usageState.unavailable(nil)
		return nil
	}
	getUsage := m.usage
	return func() tea.Msg {
		usage, err := getUsage(requestCtx, tenant, id)
		return appUsageResult{meta: meta, usage: usage, err: err}
	}
}

func (m *browser) loadSelectedPods() tea.Cmd {
	id := m.selectedDetailTarget()
	if id == "" || m.podsState.active() {
		return nil
	}
	meta, requestCtx := m.startRequest(requestPods, m.tenant, id, m.selectionGeneration)
	m.podsState.begin(meta, m.requestTime())
	if m.fetchPods == nil {
		m.finishRequest(requestPods, meta)
		m.podsState.unavailable(fmt.Errorf("pod data is unavailable"))
		return nil
	}
	getPods, tenant := m.fetchPods, m.tenant
	return func() tea.Msg {
		pods, err := getPods(requestCtx, tenant, id)
		return podsResult{meta: meta, pods: pods, err: err}
	}
}

func (m *browser) loadEgressRoutes(force bool) tea.Cmd {
	if m.egressState.active() || !force && (m.egressState.status == requestReady || m.egressState.status == requestEmpty) {
		return nil
	}
	meta, requestCtx := m.startRequest(requestEgress, m.tenant, "", m.tenantGeneration)
	m.egressState.begin(meta, m.requestTime())
	if m.egressRoutes == nil {
		m.finishRequest(requestEgress, meta)
		m.egressState.unavailable(fmt.Errorf("outbound IP data is unavailable"))
		return nil
	}
	getRoutes := m.egressRoutes
	tenant := m.tenant
	return func() tea.Msg {
		routes, err := getRoutes(requestCtx, tenant)
		return egressRoutesResult{meta: meta, routes: routes, err: err}
	}
}
