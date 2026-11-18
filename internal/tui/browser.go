package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
)

type appsResult struct {
	meta requestMeta
	apps []client.App
	err  error
}

type profileResult struct {
	meta    requestMeta
	profile *client.Profile
	err     error
}

type appDetailResult struct {
	meta requestMeta
	app  *client.App
	err  error
}

type appUsageResult struct {
	meta  requestMeta
	usage *AppUsage
	err   error
}

type usageHistoryResult struct {
	meta    requestMeta
	history *UsageHistory
	err     error
}

type appAction uint8

const (
	appActionNone appAction = iota
	appActionRestart
	appActionDelete
)

type appActionResult struct {
	tenant string
	id     string
	name   string
	action appAction
	mock   bool
	err    error
}

type namespacesResult struct {
	meta       requestMeta
	namespaces []client.Namespace
	err        error
}

// AppUsage is an optional latest resource snapshot, in CPU cores and bytes.
type AppUsage struct {
	CPUCores  *float64
	RAMBytes  *float64
	DiskBytes *float64
	Failed    UsageMetricFailures
}

// UsageMetricFailures records per-resource query failures so partial metric
// responses remain distinct from a successful query with no samples.
type UsageMetricFailures struct {
	CPU            bool
	CPUThrottle    bool
	RAM            bool
	Disk           bool
	NetworkReceive bool
	NetworkSend    bool
}

type appView uint8

const (
	appViewSummary appView = iota
	appViewUsage
)

type appSort int

const (
	sortName appSort = iota
	sortNamespace
	sortState
	sortUpdated
)

var appSortNames = [...]string{"Name", "Namespace", "State", "Last updated"}

type spinnerMsg struct{}

type tenantOption struct {
	name   string
	source string
	status string
}

type keyHint struct {
	key    string
	action string
}

// BrowserOptions contains the live browser's reads and opt-in app lifecycle actions.
type BrowserOptions struct {
	Tenant       string
	LocalTenants []string
	AllowWrites  bool
	Profile      func(context.Context) (*client.Profile, error)
	ListApps     func(context.Context, string) ([]client.App, error)
	GetApp       func(context.Context, string, string) (*client.App, error)
	Usage        func(context.Context, string, string) (*AppUsage, error)
	UsageHistory func(context.Context, string, string, time.Duration, time.Duration) (*UsageHistory, error)
	Namespaces   func(context.Context, string) ([]client.Namespace, error)
	RestartApp   func(context.Context, string, string) error
	DeleteApp    func(context.Context, string, string) error
}

type requestKind uint8

const (
	requestProfile requestKind = iota
	requestApps
	requestNamespaces
	requestSummary
	requestUsage
	requestUsageHistory
)

type requestKey struct {
	kind  requestKind
	appID string
}

type inFlightRequest struct {
	meta   requestMeta
	cancel context.CancelFunc
}

type namespaceKey struct {
	id   int
	name string
}

type namespaceChoice struct {
	key    namespaceKey
	scoped bool
}

type treeNode struct {
	app     int
	key     namespaceKey
	heading string
	last    bool
}

type browser struct {
	tenant              string
	mode                string
	listApps            func(context.Context, string) ([]client.App, error)
	getApp              func(context.Context, string, string) (*client.App, error)
	usage               func(context.Context, string, string) (*AppUsage, error)
	usageHistory        func(context.Context, string, string, time.Duration, time.Duration) (*UsageHistory, error)
	listNamespaces      func(context.Context, string) ([]client.Namespace, error)
	profile             func(context.Context) (*client.Profile, error)
	ctx                 context.Context
	now                 func() time.Time
	nextRequestID       uint64
	tenantGeneration    uint64
	selectionGeneration uint64
	inFlight            map[requestKey]inFlightRequest
	localTenants        []string
	tenants             []tenantOption
	tenantCursor        int
	selectingTenant     bool
	contextFocused      bool
	contextRow          int
	allowWrites         bool
	actionMenu          bool
	actionCursor        int
	actionMenuMessage   string
	confirmAction       appAction
	confirmButton       int
	confirmName         string
	confirmError        string
	actionBusy          bool
	actionNotice        string
	actionTargetTenant  string
	actionTargetID      string
	actionTargetName    string
	restartApp          func(string, string) error
	deleteApp           func(string, string) error
	mockDeleteApp       func(string, string)
	profileState        requestState
	tenantStatus        string
	apps                []client.App
	appsState           requestState
	selected            int
	namespaceSelected   bool
	namespaceScope      namespaceKey
	namespaceScoped     bool
	namespaces          []client.Namespace
	namespaceFallback   bool
	namespaceState      requestState
	selectingNamespace  bool
	namespaceCursor     int
	namespaceQuery      string
	filterQuery         string
	filterBeforeEdit    string
	filterEditing       bool
	filterSelection     string
	collapsed           map[namespaceKey]bool
	detail              bool
	activeAppView       appView
	detailState         requestState
	detailApp           *client.App
	detailUsage         *AppUsage
	usageAppID          string
	usageState          requestState
	historyState        requestState
	history             *UsageHistory
	historyRange        int
	historyMetric       int
	historyPod          string
	historyPodChosen    bool
	usageScroll         int
	sorting             bool
	sortChoice          int
	sortBy              appSort
	sortDescending      bool
	help                bool
	spinnerFrame        int
	width               int
	height              int
}

// RunBrowser opens the app browser using the caller's scoped clients.
func RunBrowser(ctx context.Context, options BrowserOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &browser{tenant: options.Tenant, mode: "LIVE", profile: options.Profile, ctx: ctx,
		allowWrites: options.AllowWrites, localTenants: append([]string(nil), options.LocalTenants...),
		listApps: options.ListApps}
	if options.GetApp != nil {
		m.getApp = options.GetApp
	}
	if options.Usage != nil {
		m.usage = options.Usage
	}
	m.usageHistory = options.UsageHistory
	if options.Namespaces != nil {
		m.listNamespaces = options.Namespaces
	}
	if options.RestartApp != nil {
		m.restartApp = func(tenant, id string) error { return options.RestartApp(ctx, tenant, id) }
	}
	if options.DeleteApp != nil {
		m.deleteApp = func(tenant, id string) error { return options.DeleteApp(ctx, tenant, id) }
	}
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

// RunMockBrowser opens a browser backed only by the supplied local fixture.
func RunMockBrowser(ctx context.Context, profile MockProfile) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(newMockBrowser(profile), tea.WithContext(ctx)).Run()
	return err
}

func newMockBrowser(profile MockProfile) *browser {
	tenantProfiles := append([]MockTenant(nil), profile.Tenants...)
	if len(tenantProfiles) == 0 {
		tenantProfiles = []MockTenant{{Name: profile.Tenant, Apps: profile.Apps, Namespaces: profile.Namespaces}}
	}
	appsByTenant := make(map[string][]client.App, len(tenantProfiles))
	namespacesByTenant := make(map[string][]client.Namespace, len(tenantProfiles))
	options := make([]tenantOption, 0, len(tenantProfiles))
	for _, tenant := range tenantProfiles {
		appsByTenant[tenant.Name] = append([]client.App(nil), tenant.Apps...)
		namespacesByTenant[tenant.Name] = append([]client.Namespace(nil), tenant.Namespaces...)
		options = append(options, tenantOption{name: tenant.Name, source: "Mock profile"})
	}
	if len(options) > 0 {
		options[0].status = "current"
	}
	deletedApps := make(map[string]map[string]bool)
	m := &browser{
		tenant:  profile.Tenant,
		mode:    "MOCK PROFILE · OFFLINE",
		tenants: options,
		listApps: func(_ context.Context, tenant string) ([]client.App, error) {
			apps, ok := appsByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			remaining := make([]client.App, 0, len(apps))
			for _, app := range apps {
				if !deletedApps[tenant][app.ID] {
					remaining = append(remaining, app)
				}
			}
			return remaining, nil
		},
		getApp: func(_ context.Context, tenant, id string) (*client.App, error) {
			for _, app := range appsByTenant[tenant] {
				if app.ID == id && !deletedApps[tenant][id] {
					copy := app
					return &copy, nil
				}
			}
			return nil, fmt.Errorf("app %q is not present in mock tenant %q", id, tenant)
		},
		listNamespaces: func(_ context.Context, tenant string) ([]client.Namespace, error) {
			namespaces, ok := namespacesByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.Namespace(nil), namespaces...), nil
		},
		mockDeleteApp: func(tenant, id string) {
			if deletedApps[tenant] == nil {
				deletedApps[tenant] = make(map[string]bool)
			}
			deletedApps[tenant][id] = true
		},
	}
	if m.tenant == "" {
		m.contextFocused = true
	}
	return m
}

func (m *browser) requestTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *browser) startRequest(kind requestKind, tenant, appID string, scopeGeneration uint64) (requestMeta, context.Context) {
	if m.inFlight == nil {
		m.inFlight = make(map[requestKey]inFlightRequest)
	}
	key := requestKey{kind: kind, appID: appID}
	if active, ok := m.inFlight[key]; ok {
		active.cancel()
	}
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	requestCtx, cancel := context.WithCancel(parent)
	m.nextRequestID++
	meta := requestMeta{tenant: tenant, appID: appID, scopeGeneration: scopeGeneration, requestID: m.nextRequestID}
	m.inFlight[key] = inFlightRequest{meta: meta, cancel: cancel}
	return meta, requestCtx
}

func (m *browser) finishRequest(kind requestKind, meta requestMeta) {
	key := requestKey{kind: kind, appID: meta.appID}
	if active, ok := m.inFlight[key]; ok && active.meta.requestID == meta.requestID {
		active.cancel()
		delete(m.inFlight, key)
	}
}

func (m *browser) cancelRequests(keepProfile bool) {
	for key, active := range m.inFlight {
		if keepProfile && key.kind == requestProfile {
			continue
		}
		active.cancel()
		delete(m.inFlight, key)
	}
}

func (m *browser) cancelRequest(key requestKey) {
	for activeKey, active := range m.inFlight {
		if activeKey.kind == key.kind && (key.appID == "" || activeKey.appID == key.appID) {
			active.cancel()
			delete(m.inFlight, activeKey)
		}
	}
}

func (m *browser) markCurrentUsageStale(appID string) {
	if m.detailUsage != nil && m.usageAppID == appID {
		m.detailUsage.Failed = UsageMetricFailures{CPU: true, RAM: true, Disk: true}
	}
}

func (m *browser) invalidateAppReads() {
	m.selectionGeneration++
	m.activeAppView = appViewSummary
	m.cancelRequest(requestKey{kind: requestSummary})
	m.cancelRequest(requestKey{kind: requestUsage})
	m.cancelRequest(requestKey{kind: requestUsageHistory})
	m.detailState.reset()
	m.usageState.reset()
	m.historyState.reset()
	m.history = nil
	m.historyPod = ""
	m.historyPodChosen = false
	m.usageScroll = 0
	m.detailUsage = nil
	m.usageAppID = ""
}

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
	return func() tea.Msg {
		apps, err := m.listApps(requestCtx, tenant)
		return appsResult{meta: meta, apps: apps, err: err}
	}
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

func (m *browser) sortNamespaces() {
	sort.Slice(m.namespaces, func(i, j int) bool {
		if !strings.EqualFold(m.namespaces[i].Name, m.namespaces[j].Name) {
			return strings.ToLower(m.namespaces[i].Name) < strings.ToLower(m.namespaces[j].Name)
		}
		return m.namespaces[i].ID < m.namespaces[j].ID
	})
}

func (m *browser) openNamespacePicker() tea.Cmd {
	m.selectingNamespace = true
	m.namespaceQuery = ""
	m.namespaceCursor = 0
	choices := m.namespaceChoices()
	for i, choice := range choices {
		if choice.scoped == m.namespaceScoped && (!choice.scoped || choice.key == m.namespaceScope) {
			m.namespaceCursor = i
			break
		}
	}
	return m.loadNamespaces()
}

func (m *browser) openTenantSelector() {
	m.selectingTenant = true
	m.contextFocused = true
	m.contextRow = 0
	m.detail = false
	m.tenantCursor = 0
	for i, tenant := range m.tenants {
		if tenant.name == m.tenant {
			m.tenantCursor = i
			return
		}
	}
}

func (m *browser) cycleFocus(reverse bool) tea.Cmd {
	m.selectingTenant = false
	if reverse {
		switch {
		case m.contextFocused:
			if m.contextRow == 1 {
				m.contextRow = 0
				return nil
			}
			m.contextFocused = false
			m.detail = true
			return m.loadSelectedSummary()
		case m.detail:
			m.detail = false
		default:
			m.contextFocused = true
			m.contextRow = 1
		}
		return nil
	}
	switch {
	case m.contextFocused:
		if m.contextRow == 0 {
			m.contextRow = 1
			return nil
		}
		m.contextFocused = false
		m.detail = false
	case m.detail:
		m.detail = false
		m.contextFocused = true
		m.contextRow = 0
	default:
		m.detail = true
		return m.loadSelectedSummary()
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
		m.contextRow = 1
		m.detail = false
	case 2:
		m.contextFocused = false
		m.detail = false
	case 3:
		m.contextFocused = false
		m.detail = true
		return m.loadSelectedSummary()
	}
	return nil
}

func (m *browser) handleNamespaceKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	choices := m.namespaceChoices()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingNamespace = false
		m.namespaceQuery = ""
	case "enter":
		if len(choices) > 0 {
			m.chooseNamespace(choices[m.namespaceCursor])
		}
	case "up":
		m.namespaceCursor = max(0, m.namespaceCursor-1)
	case "down":
		m.namespaceCursor = min(max(0, len(choices)-1), m.namespaceCursor+1)
	case "ctrl+u":
		m.namespaceQuery = ""
		m.namespaceCursor = 0
	case "backspace", "ctrl+h":
		query := []rune(m.namespaceQuery)
		if len(query) > 0 {
			m.namespaceQuery = string(query[:len(query)-1])
			m.namespaceCursor = 0
		}
	default:
		if msg.Text != "" {
			m.namespaceQuery += safeText(msg.Text)
			m.namespaceCursor = 0
		}
	}
	return m, nil
}

func (m *browser) namespaceChoices() []namespaceChoice {
	choices := []namespaceChoice{{}}
	for _, namespace := range m.namespaces {
		key := namespaceKey{id: namespace.ID, name: namespace.Name}
		query := strings.TrimSpace(m.namespaceQuery)
		if query != "" && !fuzzyMatch(namespace.Name, query) && !fuzzyMatch(namespace.Cluster.Name, query) && !fuzzyMatch(fmt.Sprint(namespace.ID), query) {
			continue
		}
		choices = append(choices, namespaceChoice{key: key, scoped: true})
	}
	if strings.TrimSpace(m.namespaceQuery) != "" && !fuzzyMatch("All namespaces", m.namespaceQuery) {
		choices = choices[1:]
	}
	return choices
}

func (m *browser) namespaceAppCount(key namespaceKey) int {
	count := 0
	for _, app := range m.apps {
		if app.Namespace.ID == key.id && app.Namespace.Name == key.name {
			count++
		}
	}
	return count
}

func (m *browser) chooseNamespace(choice namespaceChoice) {
	m.namespaceScoped = choice.scoped
	m.namespaceScope = choice.key
	m.selectingNamespace = false
	m.namespaceQuery = ""
	m.filterQuery = ""
	m.filterEditing = false
	m.namespaceSelected = false
	m.detail = false
	m.ensureFilterSelection()
}

func (m *browser) syncNamespaceCursor() {
	choices := m.namespaceChoices()
	for i, choice := range choices {
		if choice.scoped == m.namespaceScoped && (!choice.scoped || choice.key == m.namespaceScope) {
			m.namespaceCursor = i
			return
		}
	}
	m.namespaceCursor = 0
}

func uniqueNamespaces(input []client.Namespace) []client.Namespace {
	out := make([]client.Namespace, 0, len(input))
	seen := make(map[namespaceKey]struct{}, len(input))
	for _, namespace := range input {
		if strings.TrimSpace(namespace.Name) == "" {
			continue
		}
		key := namespaceKey{id: namespace.ID, name: namespace.Name}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, namespace)
	}
	return out
}

func mergeNamespaces(inventory, appNamespaces []client.Namespace) []client.Namespace {
	merged := append([]client.Namespace(nil), inventory...)
	seen := make(map[namespaceKey]struct{}, len(inventory)+len(appNamespaces))
	for _, namespace := range inventory {
		seen[namespaceKey{id: namespace.ID, name: namespace.Name}] = struct{}{}
	}
	for _, namespace := range appNamespaces {
		key := namespaceKey{id: namespace.ID, name: namespace.Name}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, namespace)
	}
	return uniqueNamespaces(merged)
}

func (m *browser) Init() tea.Cmd {
	profileCmd := m.loadProfile()
	if m.mode != "LIVE" {
		return m.load()
	}
	if m.tenant == "" && m.mode == "LIVE" {
		m.contextFocused = true
		return tea.Batch(profileCmd, animationCmd())
	}
	if m.mode == "LIVE" {
		return tea.Batch(profileCmd, m.load(), animationCmd())
	}
	return m.load()
}

func animationCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerMsg{} })
}

//nolint:cyclop,gocognit,gocyclo // Message routing keeps the model's transitions together.
func (m *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerMsg:
		if m.mode == "LIVE" {
			m.spinnerFrame++
			return m, animationCmd()
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case appsResult:
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
	case profileResult:
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
	case appDetailResult:
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
	case appUsageResult:
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
	case usageHistoryResult:
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
	case namespacesResult:
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
	case appActionResult:
		return m, m.applyActionResult(msg)
	case tea.MouseClickMsg:
		return m, m.handleActionMouse(msg)
	case tea.KeyPressMsg:
		if m.actionBusy {
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.actionMenu {
			return m, m.handleActionMenuKey(msg)
		}
		if m.confirmAction != appActionNone {
			return m, m.handleActionConfirmationKey(msg)
		}
		if m.selectingNamespace {
			return m.handleNamespaceKey(msg)
		}
		if m.filterEditing {
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
		if m.sorting {
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
		if m.help {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "esc", "?":
				m.help = false
			}
			return m, nil
		}
		switch msg.String() {
		case "1":
			if !m.help && !m.sorting {
				return m, m.focusPane(0)
			}
		case "2":
			if !m.help && !m.sorting {
				return m, m.focusPane(1)
			}
		case "3":
			if !m.help && !m.sorting {
				return m, m.focusPane(2)
			}
		case "4":
			if !m.help && !m.sorting {
				return m, m.focusPane(3)
			}
		case "tab":
			return m, m.cycleFocus(false)
		case "shift+tab", "backtab":
			return m, m.cycleFocus(true)
		case "ctrl+c", "q":
			return m, tea.Quit
		case "t":
			if !m.help {
				m.openTenantSelector()
			}
		case "a":
			m.openActionMenu()
		case "[":
			return m, m.switchAppView(-1)
		case "]":
			return m, m.switchAppView(1)
		case "w":
			if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
				m.historyRange = (m.historyRange + 1) % len(usageRanges)
				m.cancelRequest(requestKey{kind: requestUsageHistory})
				m.historyState.reset()
				m.history = nil
				return m, m.loadUsageHistory()
			}
		case "p":
			if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
				m.nextHistoryPod()
			}
		case "m":
			if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
				app := m.apps[m.selected]
				if m.detailApp != nil && m.detailApp.ID == app.ID {
					app = *m.detailApp
				}
				metrics := availableHistoryMetrics(app)
				if len(metrics) > 0 {
					m.historyMetric = (m.historyMetric + 1) % len(metrics)
				}
			}
		case "n", ":":
			if !m.help && !m.selectingTenant {
				return m, m.openNamespacePicker()
			}
		case "?":
			m.help = !m.help
		case "/":
			if !m.help && !m.selectingTenant {
				previousTarget := m.selectedDetailTarget()
				m.filterEditing = true
				m.contextFocused = false
				m.detail = false
				m.filterBeforeEdit = m.filterQuery
				m.namespaceSelected = false
				m.filterSelection = ""
				if m.selected >= 0 && m.selected < len(m.apps) {
					m.filterSelection = m.apps[m.selected].ID
				}
				m.syncDetailTarget(previousTarget)
			}
		case "s":
			if !m.help && !m.selectingTenant {
				m.sorting = true
				m.sortChoice = int(m.sortBy)
			}
		case "left", "h":
			if m.sorting {
				m.sortDescending = false
			} else if !m.help && m.contextFocused {
				m.selectingTenant = false
			} else if !m.help && !m.selectingTenant && !m.detail && m.selectedAppVisible() {
				m.setCollapsed(true)
			} else if !m.help && !m.selectingTenant && m.detail {
				m.detail = false
			}
		case "right", "l":
			if m.sorting {
				m.sortDescending = true
			} else if !m.help && m.contextFocused {
				m.selectingTenant = false
				m.contextFocused = false
				m.detail = false
			} else if !m.help && !m.selectingTenant && !m.detail && m.namespaceSelected && m.selectedAppVisible() {
				m.setCollapsed(false)
			} else if !m.help && !m.selectingTenant && !m.detail && !m.namespaceSelected && len(m.apps) > 0 {
				m.detail = true
				return m, m.loadSelectedSummary()
			}
		case keyEscape:
			if m.sorting {
				m.sorting = false
			} else if m.selectingTenant {
				m.selectingTenant = false
			} else if m.help {
				m.help = false
			} else {
				if m.detail {
					m.detail = false
				} else if m.filterQuery != "" {
					m.filterQuery = ""
					m.ensureFilterSelection()
				}
			}
		case "up", "k":
			if m.sorting {
				m.sortChoice = (m.sortChoice + len(appSortNames) - 1) % len(appSortNames)
			} else if m.selectingTenant {
				m.tenantCursor = max(0, m.tenantCursor-1)
			} else if m.contextFocused && m.contextRow == 0 {
				m.tenantCursor = max(0, m.tenantCursor-1)
			} else if m.contextFocused {
				m.contextRow = max(0, m.contextRow-1)
			} else if !m.help && !m.detail {
				return m, m.moveTree(-1)
			} else if !m.help && m.detail {
				return m, m.moveTree(-1)
			}
		case "down", "j":
			if m.sorting {
				m.sortChoice = (m.sortChoice + 1) % len(appSortNames)
			} else if m.selectingTenant {
				m.tenantCursor = min(max(0, len(m.tenants)-1), m.tenantCursor+1)
			} else if m.contextFocused && m.contextRow == 0 {
				m.tenantCursor = min(max(0, len(m.tenants)-1), m.tenantCursor+1)
			} else if m.contextFocused {
				m.contextRow = min(1, m.contextRow+1)
			} else if !m.help && !m.detail {
				return m, m.moveTree(1)
			} else if !m.help && m.detail {
				return m, m.moveTree(1)
			}
		case "home", "g":
			if !m.help && !m.contextFocused && !m.selectingTenant {
				return m, m.selectTree(0)
			}
		case "end", "G":
			if !m.help && !m.contextFocused && !m.selectingTenant {
				return m, m.selectTree(len(m.treeNodes()) - 1)
			}
		case "pgup":
			if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
				m.usageScroll = max(0, m.usageScroll-max(1, m.height-8))
				return m, nil
			}
			if !m.help && !m.contextFocused && !m.selectingTenant {
				return m, m.moveTree(-max(1, m.height-8))
			}
		case "pgdown":
			if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
				m.usageScroll += max(1, m.height-8)
				return m, nil
			}
			if !m.help && !m.contextFocused && !m.selectingTenant {
				return m, m.moveTree(max(1, m.height-8))
			}
		case "enter":
			if m.sorting {
				m.sortBy = appSort(m.sortChoice)
				m.sorting = false
				m.sortApps()
			} else if m.selectingTenant {
				if len(m.tenants) > 0 {
					return m, m.chooseTenant(m.tenants[m.tenantCursor].name)
				}
			} else if m.contextFocused && m.contextRow == 0 {
				if len(m.tenants) > 0 {
					return m, m.chooseTenant(m.tenants[m.tenantCursor].name)
				}
			} else if m.contextFocused {
				// Apps is the only implemented view; other entries are disabled.
			} else if !m.help && len(m.visibleAppIndices()) > 0 {
				if m.namespaceSelected {
					if m.filterQuery == "" {
						m.toggleNamespace()
					} else {
						m.namespaceSelected = false
						m.detail = true
						return m, m.loadSelectedSummary()
					}
				} else {
					m.detail = true
					return m, m.loadSelectedSummary()
				}
			}
		case "r":
			if m.selectingTenant {
				return m, m.loadProfile()
			}
			if m.detail {
				if m.activeAppView == appViewUsage {
					return m, tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory())
				}
				return m, m.loadSelectedSummary()
			}
			if !m.help && !m.appsState.active() {
				return m, m.load()
			}
		}
	}
	return m, nil
}

func (m *browser) sortApps() {
	selectedID := ""
	if m.selected >= 0 && m.selected < len(m.apps) {
		selectedID = m.apps[m.selected].ID
	}
	sort.SliceStable(m.apps, func(i, j int) bool {
		a, b := m.apps[i], m.apps[j]
		var less bool
		switch m.sortBy {
		case sortNamespace:
			less = strings.ToLower(a.Namespace.Name) < strings.ToLower(b.Namespace.Name)
		case sortState:
			less = strings.ToLower(appState(a)) < strings.ToLower(appState(b))
		case sortUpdated:
			ta, ea := time.Parse(time.RFC3339, a.UpdatedAt)
			tb, eb := time.Parse(time.RFC3339, b.UpdatedAt)
			if ea != nil || eb != nil {
				if ea != eb {
					return ea == nil
				}
				less = false
			} else {
				less = ta.Before(tb)
			}
		default:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if m.sortDescending {
			less = !less
		}
		if a.ID == b.ID {
			return false
		}
		if valueEqualForSort(a, b, m.sortBy) {
			return strings.ToLower(a.ID) < strings.ToLower(b.ID)
		}
		return less
	})
	m.selected = min(m.selected, max(0, len(m.apps)-1))
	for i := range m.apps {
		if selectedID != "" && m.apps[i].ID == selectedID {
			m.selected = i
			break
		}
	}
	m.ensureFilterSelection()
}

func valueEqualForSort(a, b client.App, by appSort) bool {
	switch by {
	case sortNamespace:
		return strings.EqualFold(a.Namespace.Name, b.Namespace.Name)
	case sortState:
		return strings.EqualFold(appState(a), appState(b))
	case sortUpdated:
		return a.UpdatedAt == b.UpdatedAt
	default:
		return strings.EqualFold(a.Name, b.Name)
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
	if usageCmd := m.loadSelectedUsage(); usageCmd != nil {
		cmds = append(cmds, usageCmd)
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

func (m *browser) switchAppView(delta int) tea.Cmd {
	if !m.detail || !m.selectedAppActive() {
		return nil
	}
	m.activeAppView = appView((int(m.activeAppView) + delta + 2) % 2)
	if m.activeAppView == appViewUsage && m.usageState.status == requestIdle {
		return tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory())
	}
	if m.activeAppView == appViewUsage && m.historyState.status == requestIdle {
		return m.loadUsageHistory()
	}
	return nil
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
			m.tenants[i].status = "current"
		}
	}
	if name == m.tenant {
		m.selectingTenant = false
		m.contextFocused = false
		return nil
	}
	m.tenant = name
	m.cancelRequests(true)
	m.tenantGeneration++
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
	return m.load()
}

func (m *browser) View() tea.View {
	var lines []string
	footer := m.footerLine()
	if m.width < 60 || m.height < 20 {
		lines = []string{"Resize terminal to at least 60 x 20"}
	} else {
		header := m.breadcrumb()
		if m.filterQuery != "" || m.filterEditing {
			header += "   " + m.filterLine()
		}
		rightIndicators := m.sortLine()
		if namespaceIndicator := m.namespaceTopLine(); namespaceIndicator != "" {
			rightIndicators = namespaceIndicator + "   " + rightIndicators
		}
		leftWidth := max(0, m.width-ansi.StringWidth(rightIndicators)-1)
		header = ansi.Truncate(header, leftWidth, "…")
		padding := max(1, m.width-ansi.StringWidth(header)-ansi.StringWidth(rightIndicators))
		header += strings.Repeat(" ", padding) + rightIndicators
		lines = append(lines, header, "")
		if m.actionNotice != "" {
			lines = append(lines, paint(safeText(m.actionNotice), output.ColorNumber, false))
		}
		if m.profileState.status == requestFailed || m.profileState.status == requestStale {
			lines = append(lines, "Account profile unavailable; tenant apps remain available. Press t then r to retry profile.")
			if m.profileState.status == requestStale {
				lines = append(lines, staleRequestTime("Profile", m.profileState))
			}
		}
		bodyHeight := max(1, m.height-len(lines)-footerHeight-1)
		left := m.appPaneLines(bodyHeight)
		right := m.summaryPaneLines()
		if m.activeAppView == appViewUsage {
			right = m.usageViewport(right, bodyHeight)
		}
		context := m.contextPaneLines(bodyHeight)
		lines = append(lines, m.splitPanes(context, left, right, bodyHeight)...)
	}
	if len(lines) > m.height-footerHeight {
		lines = lines[:max(0, m.height-footerHeight)]
	}
	for len(lines) < max(0, m.height-footerHeight) {
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	if m.actionMenu && m.width >= 60 && m.height >= 20 {
		lines = m.renderActionMenu(lines)
	} else if m.confirmAction != appActionNone && m.width >= 60 && m.height >= 20 {
		lines = m.renderActionConfirmation(lines)
	} else if m.selectingNamespace && m.width >= 60 && m.height >= 20 {
		lines = m.namespaceModal(lines)
	} else if m.help && m.width >= 60 && m.height >= 20 {
		lines = m.helpModal(lines)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *browser) appPaneLines(height int) []string {
	heading := "  [3] APPS"
	if !m.detail {
		heading = "▸ [3] APPS"
	}
	if m.appsState.active() {
		heading += " " + paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	lines := []string{paint(heading, output.ColorAccent, true)}
	if m.appsState.err != nil {
		if len(m.apps) == 0 {
			lines = append(lines, "Could not load apps: "+safeText(m.appsState.err.Error()))
		} else {
			lines = append(lines, paint("Stale · refresh failed: "+safeText(m.appsState.err.Error()), output.ColorNumber, false))
			lines = append(lines, staleRequestTime("Apps", m.appsState))
		}
	}
	if m.appsState.status == requestUnavailable && len(m.apps) == 0 {
		lines = append(lines, paint("App listing is unavailable.", output.ColorMuted, false))
	}
	if !m.appsState.active() && m.appsState.err == nil && len(m.apps) == 0 {
		label := "No apps in this tenant."
		if strings.HasPrefix(m.mode, "MOCK") {
			label = "No apps in this mock profile."
		}
		if m.namespaceScoped {
			label = fmt.Sprintf("No apps in namespace %q.", safeText(m.namespaceScope.name))
		}
		lines = append(lines, label)
	}
	if !m.appsState.active() && m.appsState.err == nil && len(m.apps) > 0 && len(m.visibleAppIndices()) == 0 {
		if m.namespaceScoped && m.filterQuery == "" {
			lines = append(lines, fmt.Sprintf("No apps in namespace %q.", safeText(m.namespaceScope.name)))
		} else {
			lines = append(lines, "No apps match this filter.")
		}
	}
	room := max(0, height-len(lines))
	appWidth, _, _ := m.paneWidths()
	return append(lines, m.appTreeForWidth(room, appWidth)...)
}

func (m *browser) summaryPaneLines() []string {
	if m.activeAppView == appViewUsage {
		return m.usagePaneLines()
	}
	heading := m.appViewTabs()
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{heading, paint("No app selected. Move to an app to preview its summary.", output.ColorMuted, false)}
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	stateGlyph, stateColor := stateStyle(appState(a))
	lines := []string{heading, paint("· "+safeText(a.Name), output.ColorKey, true)}
	if m.detailState.err != nil {
		lines = append(lines, paint("Detail refresh failed: "+safeText(m.detailState.err.Error()), output.ColorNumber, false))
		if m.detailState.status == requestStale {
			lines = append(lines, staleRequestTime("Summary", m.detailState))
		}
	} else if m.detailState.status == requestUnavailable {
		lines = append(lines, paint("App detail is unavailable; showing the list snapshot.", output.ColorMuted, false))
	}
	if m.usageState.err != nil {
		lines = append(lines, paint("Resource usage unavailable: "+safeText(m.usageState.err.Error()), output.ColorMuted, false))
		if m.usageState.status == requestStale {
			lines = append(lines, staleRequestTime("Usage", m.usageState))
		}
	} else if m.usageState.status == requestPartial {
		lines = append(lines, paint("Some resource metrics are unavailable or have no current sample.", output.ColorMuted, false))
	} else if m.usageState.status == requestUnavailable {
		lines = append(lines, paint("Usage unavailable · showing configured totals", output.ColorMuted, false))
	}
	replicas := "-"
	if a.ReplicasPresent {
		replicas = fmt.Sprintf("%d", a.Replicas)
	}
	lines = append(lines, "",
		summaryField("Tenant", displayValue(m.tenant)),
		summaryField("Namespace", displayValue(a.Namespace.Name)),
		summaryField("Cluster", displayValue(a.Namespace.Cluster.Name)),
		summaryField("State", paint(stateGlyph+" "+safeText(appState(a)), stateColor, true)),
		summaryField("Description", displayValue(a.State.Description)),
		summaryField("Replicas", replicas))
	plan := planName(a.Plan)
	if plan == "-" {
		lines = append(lines, summaryLabel("Plan"))
	} else {
		lines = append(lines, summaryField("Plan", plan))
	}
	usage := m.detailUsage
	if m.usageAppID != a.ID {
		usage = nil
	}
	cpuTotal := "-"
	ramTotal := "-"
	if a.Plan != nil {
		if a.Plan.Detail.CPURequest > 0 {
			cpuTotal = fmt.Sprintf("%dm", a.Plan.Detail.CPURequest)
		}
		if a.Plan.Detail.RAMLimit > 0 {
			ramTotal = fmt.Sprintf("%dM", a.Plan.Detail.RAMLimit)
		}
	}
	if cpuTotal == "-" {
		cpuTotal = displayValue(a.CPURequest)
	}
	if ramTotal == "-" {
		ramTotal = displayValue(a.RAMLimit)
	}
	lines = append(lines,
		planResourceField("", "CPU", resourceUsageText(usageValue(usage, func(u *AppUsage) *float64 { return u.CPUCores }), cpuTotal, formatCores, usage != nil && usage.Failed.CPU)),
		planResourceField("", "RAM", resourceUsageText(usageValue(usage, func(u *AppUsage) *float64 { return u.RAMBytes }), ramTotal, formatBytes, usage != nil && usage.Failed.RAM)))
	if (a.Disk != nil && a.Disk.SizeInGi > 0) || (usage != nil && (usage.DiskBytes != nil || usage.Failed.Disk)) {
		diskTotal := "-"
		if a.Disk != nil && a.Disk.SizeInGi > 0 {
			diskTotal = fmt.Sprintf("%d GiB", a.Disk.SizeInGi)
		}
		lines = append(lines, planResourceField("", "Disk", resourceUsageText(usageValue(usage, func(u *AppUsage) *float64 { return u.DiskBytes }), diskTotal, formatBytes, usage != nil && usage.Failed.Disk)))
	}
	if a.Service != nil {
		if value := displayValue(a.Service.Type); value != "-" {
			lines = append(lines, summaryField("Service", value))
		}
		if port, ok := primaryServicePort(a.Service.Ports); ok {
			servicePort := port.ServicePort
			if servicePort == 0 {
				servicePort = port.ContainerPort
			}
			if servicePort > 0 {
				lines = append(lines, summaryField("Port Service", fmt.Sprintf("%d", servicePort)))
			}
		}
		if value := strings.TrimSpace(a.Service.InternalAddress); value != "" {
			lines = append(lines, summaryField("Internal Address", internalAddressDisplay(value, a.Service.Ports)))
		}
		lines = append(lines, summaryField("External IP", displayValue(a.Service.ExternalIP)))
	} else {
		lines = append(lines, summaryField("External IP", "-"))
	}
	if probe := readinessProbeDisplay(a); probe != "" {
		lines = append(lines, summaryField("Readiness Probe", probe))
	}
	if command := appCommandDisplay(a); command != "" {
		lines = append(lines, summaryField("Command", command))
	}
	if len(a.Inputs) > 0 {
		lines = append(lines, summaryField("Inputs", strings.Join(a.Inputs, ", ")))
	}
	lines = append(lines, summaryField("Created via", creationMethodLabel(a.CreationMethod)), summaryField("Image", displayValue(a.Image())))
	domains := appDomains(a)
	if len(domains) == 0 {
		lines = append(lines, summaryField("Domains", "-"))
	} else {
		for i, host := range domains {
			label := ""
			if i == 0 {
				label = "Domains"
			}
			lines = append(lines, summaryLabel(label)+domainLink(host))
		}
	}
	lines = append(lines, summaryField("Updated", formatLocalTime(a.UpdatedAt)), summaryField("ID", displayValue(a.ID)))
	return lines
}

func (m *browser) appViewTabs() string {
	focus := "  "
	if m.detail {
		focus = "▸ "
	}
	if !m.selectedAppActive() {
		return paint(focus+"[4] SUMMARY", output.ColorAccent, true)
	}
	summary, usage := "Summary", "Usage"
	if m.activeAppView == appViewSummary {
		summary = "[Summary]"
	} else {
		usage = "[Usage]"
	}
	heading := paint(focus+"[4] ", output.ColorAccent, true) + paint(summary, output.ColorKey, m.activeAppView == appViewSummary) + paint("  ", output.ColorBorder, false) + paint(usage, output.ColorKey, m.activeAppView == appViewUsage)
	if m.detailState.active() || m.usageState.active() {
		heading += " " + paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	return heading
}

func (m *browser) usagePaneLines() []string {
	lines := []string{m.appViewTabs()}
	if m.namespaceSelected || !m.selectedAppVisible() {
		return append(lines, paint("No app selected. Move to an app to view its resource usage.", output.ColorMuted, false))
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	lines = append(lines, paint("· "+safeText(a.Name), output.ColorKey, true), "")
	switch m.usageState.status {
	case requestLoading:
		lines = append(lines, paint("Loading current resource usage…", output.ColorMuted, false))
	case requestRefreshing:
		lines = append(lines, paint("Refreshing current resource usage…", output.ColorMuted, false))
	case requestStale:
		lines = append(lines, paint("Refresh failed · showing the last successful samples", output.ColorNumber, false), staleRequestTime("Usage", m.usageState))
	case requestFailed:
		lines = append(lines, paint("Resource usage is unavailable. Press r to retry.", output.ColorNumber, false))
	case requestUnavailable:
		lines = append(lines, paint("Resource usage is unavailable for this connection.", output.ColorMuted, false))
	case requestEmpty:
		lines = append(lines, paint("No resource samples in the last 5 minutes.", output.ColorMuted, false))
	case requestPartial:
		lines = append(lines, paint("Partial snapshot · rows show current samples and missing data.", output.ColorNumber, false))
	default:
		if m.usageState.lastSuccessAt.IsZero() {
			lines = append(lines, paint("No usage snapshot yet. Press r to load it.", output.ColorMuted, false))
		}
	}
	usage := m.detailUsage
	if m.usageAppID != a.ID {
		usage = nil
	}
	lines = append(lines,
		usageField(" CPU", usageValue(usage, func(u *AppUsage) *float64 { return u.CPUCores }), resourceLimit(a, "cpu"), formatCores, usage != nil && usage.Failed.CPU),
		usageField(" RAM", usageValue(usage, func(u *AppUsage) *float64 { return u.RAMBytes }), resourceLimit(a, "ram"), formatBytes, usage != nil && usage.Failed.RAM))
	if a.Disk != nil && a.Disk.SizeInGi > 0 {
		lines = append(lines, usageField(" Disk", usageValue(usage, func(u *AppUsage) *float64 { return u.DiskBytes }), resourceLimit(a, "disk"), formatBytes, usage != nil && usage.Failed.Disk))
	}
	lines = append(lines, m.historyLines()...)
	if !m.usageState.lastSuccessAt.IsZero() {
		when := m.usageState.lastSuccessAt.Local().Format("Jan 2, 3:04 PM MST")
		lines = append(lines, "", paint("Last usage response · "+when, output.ColorMuted, false))
	}
	lines = append(lines, paint("Window · latest samples from the last 5 minutes", output.ColorMuted, false))
	return lines
}

func resourceLimit(app client.App, resource string) string {
	switch resource {
	case "cpu":
		if app.Plan != nil && app.Plan.Detail.CPURequest > 0 {
			return fmt.Sprintf("%dm", app.Plan.Detail.CPURequest)
		}
		return displayValue(app.CPURequest)
	case "ram":
		if app.Plan != nil && app.Plan.Detail.RAMLimit > 0 {
			return fmt.Sprintf("%dM", app.Plan.Detail.RAMLimit)
		}
		return displayValue(app.RAMLimit)
	case "disk":
		if app.Disk != nil && app.Disk.SizeInGi > 0 {
			return fmt.Sprintf("%d GiB", app.Disk.SizeInGi)
		}
	}
	return "-"
}

func usageField(label string, value *float64, limit string, format func(float64) string, failed bool) string {
	icon := map[string]string{"CPU": "", "RAM": "", "Disk": ""}[label]
	field := func(shown string) string {
		return paint(icon+" "+label+" ", output.ColorMuted, true) + paint(shown, output.ColorBranch, false)
	}
	if value == nil && !failed {
		shown := "No current sample"
		if limit != "-" {
			shown += " / " + limit
		}
		return field(shown)
	}
	return field(resourceUsageText(value, limit, format, failed))
}

func resourceUsageText(value *float64, limit string, format func(float64) string, failed bool) string {
	shown := ""
	switch {
	case failed && value == nil:
		shown = "Unavailable"
	case value == nil:
		shown = displayValue(limit)
	default:
		shown = format(*value)
		if failed {
			shown += " · stale"
		}
	}
	if value != nil && limit != "-" {
		shown += " / " + limit
	} else if failed && limit != "-" {
		shown += " / " + limit
	}
	return shown
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func appDomains(app client.App) []string {
	domains := make([]string, 0, len(app.ExternalHosts)+3)
	seen := make(map[string]struct{}, len(app.ExternalHosts)+3)
	hosts := append([]string{app.CustomDomainAddress}, app.ExternalHosts...)
	if app.Service != nil {
		hosts = append(hosts, app.Service.ExternalAddress)
	}
	for _, domain := range hosts {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		key := strings.ToLower(domain)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		domains = append(domains, domain)
	}
	return domains
}

func internalAddressDisplay(address string, ports map[string]client.Port) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return safeText(address)
	}
	port, ok := primaryServicePort(ports)
	if ok {
		servicePort := port.ServicePort
		if servicePort == 0 {
			servicePort = port.ContainerPort
		}
		if servicePort > 0 {
			return safeText(net.JoinHostPort(address, fmt.Sprint(servicePort)))
		}
	}
	return safeText(address)
}

func primaryServicePort(ports map[string]client.Port) (client.Port, bool) {
	if port, ok := ports["main"]; ok {
		return port, true
	}
	if len(ports) == 1 {
		for _, port := range ports {
			return port, true
		}
	}
	return client.Port{}, false
}

func appCommandDisplay(app client.App) string {
	command := strings.TrimSpace(app.Command)
	args := strings.TrimSpace(app.Args)
	switch {
	case command == "":
		return args
	case args == "":
		return command
	default:
		return command + " " + args
	}
}

func readinessProbeDisplay(app client.App) string {
	if app.CustomConfig != nil && app.CustomConfig.Container != nil && app.CustomConfig.Container.ReadinessProbe != nil {
		probe := app.CustomConfig.Container.ReadinessProbe
		if probe.Exec != nil && len(probe.Exec.Command) > 0 {
			return "exec: " + strings.Join(probe.Exec.Command, " ")
		}
		if probe.HTTPGet != nil {
			value := "HTTP GET " + displayValue(probe.HTTPGet.Path)
			if port := jsonScalar(probe.HTTPGet.Port); port != "" {
				value += ":" + port
			}
			return value
		}
		if probe.TCPSocket != nil {
			return "TCP " + jsonScalar(probe.TCPSocket.Port)
		}
	}
	if path := strings.TrimSpace(app.ReadinessProbePath); path != "" {
		return "HTTP GET " + safeText(path)
	}
	return ""
}

func jsonScalar(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return safeText(value)
	}
	if string(raw) == "null" {
		return ""
	}
	return safeText(string(raw))
}

func summaryLabel(label string) string {
	return paint(fmt.Sprintf("%-16s ", label), output.ColorMuted, true)
}

func staleRequestTime(label string, state requestState) string {
	if state.lastSuccessAt.IsZero() {
		return paint(label+" has no successful snapshot yet.", output.ColorMuted, false)
	}
	when := state.lastSuccessAt.Local().Format("Jan 2, 3:04 PM MST")
	return paint(label+" last succeeded "+when+" · press r to retry", output.ColorMuted, false)
}

func summaryField(label, value string) string {
	return summaryLabel(label) + paint(value, output.ColorBranch, false)
}

func planResourceField(icon, label, value string) string {
	return summaryLabel("") + paint(icon+" "+label+" ", output.ColorMuted, true) + paint(value, output.ColorBranch, false)
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return safeText(value)
}

func planName(plan *client.Plan) string {
	if plan == nil {
		return "-"
	}
	if plan.Name != "" {
		return safeText(plan.Name)
	}
	if plan.CodeName != "" {
		return safeText(plan.CodeName)
	}
	return "-"
}

func usageValue(usage *AppUsage, selectValue func(*AppUsage) *float64) *float64 {
	if usage == nil {
		return nil
	}
	return selectValue(usage)
}

func usageRatio(used *float64, total string, format func(float64) string) string {
	if used == nil {
		return displayValue(total)
	}
	usedText := format(*used)
	if strings.TrimSpace(total) == "" || total == "-" {
		return usedText
	}
	return usedText + " / " + displayValue(total)
}

func formatCores(cores float64) string {
	return fmt.Sprintf("%dm", int(math.Round(cores*1000)))
}

func formatBytes(bytes float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := bytes
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func domainLink(host string) string {
	host = strings.TrimSpace(safeText(host))
	if host == "" {
		return "-"
	}
	target := host
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return paint(host, output.ColorKey, false)
	}
	style := lipgloss.NewStyle().Underline(true).Hyperlink(target)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.Foreground(output.ColorBranch)
	}
	return style.Render(host)
}

func creationMethodLabel(method string) string {
	method = strings.TrimSpace(method)
	switch strings.ToLower(method) {
	case "git_repo_url":
		return " Git"
	case "docker_image":
		return " Docker"
	case "":
		return "◇ Unknown"
	}
	short := strings.ReplaceAll(method, "_", " ")
	short = strings.ReplaceAll(short, "-", " ")
	short = strings.TrimSuffix(short, "new")
	short = strings.TrimSpace(short)
	if short == "" {
		short = method
	}
	words := strings.Fields(short)
	for i, word := range words {
		if strings.EqualFold(word, "postgresql") {
			words[i] = "PostgreSQL"
		} else {
			runes := []rune(strings.ToLower(word))
			if len(runes) > 0 {
				runes[0] = unicode.ToUpper(runes[0])
			}
			words[i] = string(runes)
		}
	}
	return " " + safeText(strings.Join(words, " "))
}

func formatLocalTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return fallbackText(value)
	}
	return parsed.Local().Format("Jan 2, 2006 · 3:04 PM MST")
}

func (m *browser) paneWidths() (int, int, int) {
	contextWidth := min(28, max(18, m.width/5))
	available := max(3, m.width-contextWidth-6)
	appsWidth := available / 2
	return appsWidth, available - appsWidth, contextWidth
}

func (m *browser) splitPanes(context, apps, summary []string, height int) []string {
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		appLine, summaryLine, contextLine := "", "", ""
		if i < len(apps) {
			appLine = apps[i]
		}
		if i < len(summary) {
			summaryLine = summary[i]
		}
		if i < len(context) {
			contextLine = context[i]
		}
		contextLine = ansi.Truncate(contextLine, contextWidth, "…")
		appLine = ansi.Truncate(appLine, appsWidth, "…")
		summaryLine = ansi.Truncate(summaryLine, summaryWidth, "…")
		contextLine += strings.Repeat(" ", max(0, contextWidth-ansi.StringWidth(contextLine)))
		appLine += strings.Repeat(" ", max(0, appsWidth-ansi.StringWidth(appLine)))
		summaryLine += strings.Repeat(" ", max(0, summaryWidth-ansi.StringWidth(summaryLine)))
		dividerOne := paint("│", output.ColorBorder, false)
		if m.contextFocused {
			dividerOne = paint("┃", output.ColorBranch, true)
		}
		dividerTwo := paint("│", output.ColorBorder, false)
		if m.detail {
			dividerTwo = paint("┃", output.ColorBranch, true)
		}
		lines[i] = contextLine + " " + dividerOne + " " + appLine + " " + dividerTwo + " " + summaryLine
	}
	return lines
}

func (m *browser) contextPaneLines(height int) []string {
	_, _, width := m.paneWidths()
	tenantHeight := height / 2
	pageHeight := height - tenantHeight
	tenantContentHeight := max(1, tenantHeight-1) // reserve one row for the pane divider
	tenantLines := []string{}
	tenantHeading := paint("[1] TENANTS", output.ColorKey, true)
	if m.contextFocused && m.contextRow == 0 {
		tenantHeading = paint("[1] TENANTS", output.ColorAccent, true)
	}
	tenantLines = append(tenantLines, tenantHeading)
	if len(m.tenants) == 0 {
		if m.profileState.active() || m.profileState.status == requestIdle {
			tenantLines = append(tenantLines, m.loadingMessage("Loading…"))
		} else if m.profileState.err != nil || m.profileState.status == requestUnavailable {
			tenantLines = append(tenantLines, paint("No choices", output.ColorMuted, false), paint("Use --org or login", output.ColorMuted, false))
		} else {
			tenantLines = append(tenantLines, paint("No memberships", output.ColorMuted, false))
		}
	} else {
		capacity := max(1, tenantContentHeight-len(tenantLines))
		first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
		last := min(len(m.tenants), first+capacity)
		for i := first; i < last; i++ {
			option := m.tenants[i]
			marker := " "
			if i == m.tenantCursor {
				marker = "›"
			}
			label := marker + " " + safeText(option.name)
			if option.name == m.tenant {
				label += " ✓"
			}
			tenantLines = append(tenantLines, m.contextRowLine(label, width, m.contextFocused && m.contextRow == 0 && i == m.tenantCursor))
		}
	}
	if len(tenantLines) > tenantContentHeight {
		tenantLines = tenantLines[:tenantContentHeight]
	}
	for len(tenantLines) < tenantContentHeight {
		tenantLines = append(tenantLines, "")
	}
	lines := append(tenantLines, paint(strings.Repeat("─", width), output.ColorBorder, false))

	pageHeading := paint("[2] PAGES", output.ColorMuted, true)
	if m.contextFocused && m.contextRow == 1 {
		pageHeading = paint("[2] PAGES", output.ColorAccent, true)
	}
	pageLines := []string{pageHeading}
	pageLines = append(pageLines, m.contextRowLine("● Apps", width, m.contextFocused && m.contextRow == 1))
	for _, page := range []string{"Activity", "Tickets", "Resources", "Registry", "Billing", "Account"} {
		pageLines = append(pageLines, m.contextRowLine("· "+page, width, false))
	}
	if len(pageLines) > pageHeight {
		pageLines = pageLines[:pageHeight]
	}
	for len(pageLines) < pageHeight {
		pageLines = append(pageLines, "")
	}
	lines = append(lines, pageLines...)
	return lines
}

func (m *browser) contextRowLine(value string, width int, selected bool) string {
	line := ansi.Truncate(safeText(value), width, "…")
	line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	if selected {
		return selectedRowStyle().Width(width).Render(line)
	}
	if strings.HasPrefix(value, "● Apps") {
		return paint(line, output.ColorSuccess, true)
	}
	if strings.HasPrefix(value, "· ") {
		return paint(line, output.ColorMuted, false)
	}
	return line
}

func (m *browser) footerLine() string {
	items := m.keyHints(false)
	items = fitKeyHints(items, m.width)
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, paint(item.key, output.ColorBranch, true)+" "+paint(item.action, output.ColorMuted, false))
	}
	return strings.Join(parts, paint(" · ", output.ColorBorder, false))
}

func fitKeyHints(hints []keyHint, width int) []keyHint {
	hints = append([]keyHint(nil), hints...)
	for len(hints) > 1 {
		parts := make([]string, len(hints))
		for i, hint := range hints {
			parts[i] = hint.key + " " + hint.action
		}
		if ansi.StringWidth(strings.Join(parts, " · ")) <= width {
			break
		}
		remove := -1
		for i := len(hints) - 1; i >= 0; i-- {
			switch hints[i].key {
			case "q", "?", "Esc", "Esc/?", "q/Esc", "↵", "type", "Click", "Ctrl+U", "y", "n/Esc",
				"↑↓", "↑↓/jk", "←→", "←→/hl", "r", "d", "[/]", "w", "p", "PgUp/Dn":
				continue
			default:
				remove = i
			}
			break
		}
		if remove < 0 {
			remove = len(hints) - 2
		}
		hints = append(hints[:remove], hints[remove+1:]...)
	}
	return hints
}

// keyHints is the presentation registry used by the footer and help modal.
// Key dispatch remains in Update and the focused modal handlers.
func (m *browser) keyHints(forHelp bool) []keyHint {
	var hints []keyHint
	switch {
	case m.actionBusy:
		hints = []keyHint{{"…", "action in progress"}, {"q", "quit"}}
	case m.actionMenu:
		hints = []keyHint{{"↑↓/jk", "choose"}, {"r", "restart"}, {"d", "delete"}, {"↵", "select"}, {"q/Esc", "close"}}
	case m.confirmAction == appActionRestart:
		hints = []keyHint{{"←→", "choose"}, {"y", "yes"}, {"n/Esc", "no"}, {"↵", "confirm"}}
	case m.confirmAction == appActionDelete:
		hints = []keyHint{{"type", "app name"}, {"Tab", "Delete button"}, {"Click", "delete"}, {"Esc", "cancel"}}
	case m.help && !forHelp:
		hints = []keyHint{{"Esc/?", "close help"}, {"q", "quit"}}
	case m.filterEditing:
		hints = []keyHint{{"type", "filter"}, {"↑↓", "move"}, {"↵", "open"}, {"Esc", "cancel"}, {"Ctrl+U", "clear"}}
	case m.selectingNamespace:
		hints = []keyHint{{"type", "filter"}, {"↑↓", "move"}, {"↵", "scope"}, {"Esc", "close"}, {"Ctrl+U", "clear"}}
	case m.selectingTenant:
		hints = []keyHint{{"↑↓/jk", "choose"}, {"↵", "switch"}, {"Esc", "close"}, {"r", "retry profile"}, {"Tab/ShiftTab", "cycle"}, {"1-4", "panes"}, {"?", "help"}, {"q", "quit"}}
	case m.contextFocused:
		hints = []keyHint{{"1-4", "panes"}, {"↑↓/jk", "control"}, {"↵", "select"}, {"Tab/ShiftTab", "cycle"}, {"→/l", "open Apps"}, {"n", "scope"}, {"/", "filter"}, {"s", "sort"}, {"r", "refresh"}, {"t", "tenant"}, {"?", "help"}, {"q", "quit"}}
	}
	if len(hints) == 0 {
		if forHelp {
			hints = []keyHint{
				{"↑↓/jk", "move"}, {"←→/hl", "tree / summary"},
				{"1-4", "focus pane"}, {"Tab / Shift+Tab", "cycle panes"}, {"↵", "open / toggle"},
				{"Home/End", "first / last"}, {"g/G", "first / last"}, {"PgUp/Dn", "move by page"},
				{"Esc", "back / clear"}, {"n", "namespace scope"}, {"/", "fuzzy filter"}, {"s", "sort"},
				{"r", "refresh"}, {"t", "tenant selector"},
			}
			if m.selectedAppActive() {
				hints = append(hints, keyHint{"a", "app actions"})
			}
		} else if m.width < 72 {
			if m.detail && m.selectedAppActive() {
				hints = []keyHint{{"[/]", "views"}, {"r", "refresh"}, {"Esc", "Apps"}, {"?", "help"}, {"q", "quit"}}
			} else {
				hints = []keyHint{{"↑↓/jk", "move"}, {"n", "scope"}, {"/", "filter"}, {"?", "help"}, {"q", "quit"}}
			}
		} else if m.width < 100 {
			if m.detail && m.selectedAppActive() {
				hints = []keyHint{{"↑↓/jk", "apps"}, {"[/]", "views"}, {"r", "refresh"}, {"Esc", "back"}, {"?", "help"}, {"q", "quit"}}
			} else {
				hints = []keyHint{
					{"↑↓/jk", "move"}, {"1-4", "panes"}, {"←→/hl", "tree"},
					{"n", "scope"}, {"/", "filter"}, {"s", "sort"}, {"?", "help"}, {"q", "quit"},
				}
			}
		} else {
			hints = []keyHint{
				{"↑↓/jk", "move"}, {"1-4", "panes"}, {"←→/hl", "tree"},
				{"Tab", "cycle"}, {"↵", "open"}, {"n", "scope"}, {"/", "filter"},
				{"s", "sort"}, {"r", "refresh"}, {"t", "tenant"}, {"?", "help"}, {"q", "quit"},
			}
			if m.selectedAppActive() {
				hints = append(hints[:5], append([]keyHint{{"a", "actions"}}, hints[5:]...)...)
			}
		}
	}
	if forHelp {
		hints = appendUniqueKeyHints(hints, keyHint{"Esc", "close help"}, keyHint{"?", "close help"}, keyHint{"q", "quit"})
	}
	if m.detail && m.selectedAppActive() && (forHelp || !m.help && !m.actionBusy && !m.actionMenu && m.confirmAction == appActionNone && !m.filterEditing && !m.selectingNamespace && !m.selectingTenant) {
		hints = appendUniqueKeyHints(hints, keyHint{"[/]", "previous / next app view"})
		if m.activeAppView == appViewUsage {
			usageHints := []keyHint{{"m", "metric"}, {"w", "range"}, {"PgUp/Dn", "scroll"}}
			if len(historyPods(m.history)) > 0 {
				usageHints = append([]keyHint{{"p", "pod"}}, usageHints...)
			}
			hints = append(hints, usageHints...)
		}
	}
	return hints
}

func appendUniqueKeyHints(hints []keyHint, extras ...keyHint) []keyHint {
	for _, extra := range extras {
		found := false
		for i, hint := range hints {
			if hint.key == extra.key {
				hints[i].action = extra.action
				found = true
				break
			}
		}
		if !found {
			hints = append(hints, extra)
		}
	}
	return hints
}

func (m *browser) sortLine() string {
	choice := int(m.sortBy)
	if m.sorting && m.sortChoice >= 0 && m.sortChoice < len(appSortNames) {
		choice = m.sortChoice
	}
	direction := "↑"
	if m.sortDescending {
		direction = "↓"
	}
	return paint("⇅", output.ColorMuted, false) + " " + paint(appSortNames[choice], output.ColorBranch, true) + " " + paint(direction, output.ColorNumber, true)
}

func (m *browser) namespaceTopLine() string {
	if m.selectingNamespace && strings.TrimSpace(m.namespaceQuery) != "" {
		return paint("⎈ NS", output.ColorMuted, true) + " " + paint("/ "+safeText(m.namespaceQuery), output.ColorBranch, true)
	}
	if m.namespaceScoped {
		name := ansi.Truncate(safeText(m.namespaceBreadcrumb(m.namespaceScope)), 28, "…")
		return paint("⎈ NS", output.ColorMuted, true) + " " + paint(name, output.ColorBranch, true)
	}
	return paint("⎈ NS", output.ColorMuted, true) + " " + paint("All", output.ColorBranch, true)
}

func (m *browser) filterLine() string {
	query := safeText(m.filterQuery)
	line := paint("⌕", output.ColorBranch, true) + "  " + paint("FILTER", output.ColorMuted, true) + "  " + paint("›", output.ColorBorder, false) + " "
	if query == "" && m.filterEditing {
		line += paint("type app or namespace/app", output.ColorMuted, false)
	} else if query != "" {
		line += paint(query, output.ColorBranch, true)
	}
	if m.filterEditing {
		line += paint("▏", output.ColorNumber, true)
	}
	if !m.filterEditing && m.filterQuery != "" {
		matches := len(m.visibleAppIndices())
		matchColor := output.ColorMuted
		if matches == 0 {
			matchColor = output.ColorDanger
		} else {
			matchColor = output.ColorNumber
		}
		line += paint(fmt.Sprintf("   ·   %d matches", matches), matchColor, false)
	}
	return line
}

func (m *browser) ensureFilterSelection() {
	visible := m.visibleAppIndices()
	if len(visible) == 0 {
		m.namespaceSelected = false
		return
	}
	for _, index := range visible {
		if index == m.selected {
			return
		}
	}
	m.selected = visible[0]
	m.namespaceSelected = false
}

func (m *browser) visibleAppIndices() []int {
	query := strings.TrimSpace(m.filterQuery)
	namespaceQuery, appQuery, scoped := strings.Cut(query, "/")
	namespaceQuery = strings.TrimSpace(namespaceQuery)
	appQuery = strings.TrimSpace(appQuery)
	indices := make([]int, 0, len(m.apps))
	for i, app := range m.apps {
		if m.namespaceScoped && (app.Namespace.ID != m.namespaceScope.id || app.Namespace.Name != m.namespaceScope.name) {
			continue
		}
		if scoped {
			if fuzzyMatch(app.Namespace.Name, namespaceQuery) && fuzzyMatch(app.Name, appQuery) {
				indices = append(indices, i)
			}
		} else if fuzzyMatch(app.Name, query) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *browser) selectedAppVisible() bool {
	if m.selected < 0 || m.selected >= len(m.apps) {
		return false
	}
	for _, index := range m.visibleAppIndices() {
		if index == m.selected {
			return true
		}
	}
	return false
}

func fuzzyMatch(value, query string) bool {
	queryRunes := []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(queryRunes) == 0 {
		return true
	}
	valueRunes := []rune(strings.ToLower(value))
	queryIndex := 0
	for _, valueRune := range valueRunes {
		if valueRune == queryRunes[queryIndex] {
			queryIndex++
			if queryIndex == len(queryRunes) {
				return true
			}
		}
	}
	return false
}

func (m *browser) breadcrumb() string {
	parts := []string{"Darkubectl"}
	if m.tenant != "" {
		tenant := safeText(m.tenant)
		if m.tenantStatus != "" {
			tenant += " (" + m.tenantStatus + ")"
		}
		parts = append(parts, tenant)
	}
	parts = append(parts, "Apps")
	if m.namespaceScoped {
		parts = append(parts, safeText(m.namespaceBreadcrumb(m.namespaceScope)))
	}
	if !m.namespaceSelected && m.selectedAppVisible() {
		app := m.apps[m.selected]
		if !m.namespaceScoped {
			parts = append(parts, safeText(m.namespaceBreadcrumb(namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name})))
		}
		parts = append(parts, safeText(app.Name))
	}
	separator := paint(" / ", output.ColorMuted, false)
	crumbs := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == 0 {
			crumbs = append(crumbs, paint(part, output.ColorAccent, true))
		} else {
			crumbs = append(crumbs, paint(part, output.ColorBranch, i == len(parts)-1))
		}
	}
	path := strings.Join(crumbs, separator)
	if m.mode == "LIVE" {
		return m.liveGlyph() + " " + path
	}
	return paint("◈", output.ColorNumber, true) + " " + paint("MOCK PROFILE · OFFLINE", output.ColorMuted, false) + "  " + path
}

func (m *browser) namespaceBreadcrumb(key namespaceKey) string {
	name := key.name
	duplicates := make([]client.Namespace, 0, 2)
	for _, namespace := range m.namespaces {
		if namespace.Name == key.name {
			duplicates = append(duplicates, namespace)
		}
	}
	if len(duplicates) < 2 {
		return name
	}
	for _, namespace := range duplicates {
		if namespace.ID == key.id {
			if namespace.Cluster.Name != "" {
				return fmt.Sprintf("%s · %s · ID %d", name, namespace.Cluster.Name, namespace.ID)
			}
			return fmt.Sprintf("%s · ID %d", name, namespace.ID)
		}
	}
	return name
}

func fallbackText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return safeText(value)
}

func (m *browser) busy() bool {
	return m.mode == "LIVE" && (m.profileState.active() || m.appsState.active())
}

func (m *browser) liveGlyph() string {
	if m.busy() {
		return paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	pulse := m.spinnerFrame % 8
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		if pulse < 2 || pulse >= 6 {
			return "○"
		}
		return "●"
	}
	style := lipgloss.NewStyle().Foreground(output.ColorBranch)
	if pulse < 2 || pulse >= 6 {
		style = style.Faint(true)
	}
	return style.Render("●")
}

func (m *browser) spinnerGlyph() string {
	frames := [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[m.spinnerFrame%len(frames)]
}

func (m *browser) loadingMessage(label string) string {
	return paint(m.spinnerGlyph(), output.ColorBranch, true) + " " + paint(label, output.ColorKey, false)
}

func (m *browser) namespaceModal(background []string) []string {
	panelWidth := min(72, m.width-4)
	innerWidth := panelWidth - 6
	choices := m.namespaceChoices()
	content := []string{
		paint("⎈  NAMESPACE SCOPE", output.ColorAccent, true),
		"Filter by namespace, cluster, or ID: " + paint(m.namespaceQuery+"▏", output.ColorBranch, true),
		paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
	}
	if m.namespaceState.active() {
		content = append(content, m.loadingMessage("Loading namespace inventory…"))
	}
	if m.namespaceState.err != nil && m.namespaceState.status != requestStale {
		content = append(content, paint("Namespace inventory failed: "+safeText(m.namespaceState.err.Error()), output.ColorNumber, false))
	} else if m.namespaceState.status == requestStale {
		content = append(content, paint("Cached inventory · refresh failed", output.ColorNumber, false), staleRequestTime("Namespaces", m.namespaceState))
	}
	if m.namespaceFallback {
		message := "Inventory unavailable · showing namespaces with apps"
		if strings.HasPrefix(m.mode, "MOCK") {
			message = "Using mock app namespaces · empty projects need explicit fixture inventory"
		}
		content = append(content, paint(message, output.ColorNumber, false))
	}
	capacity := max(1, min(10, m.height-12))
	first := max(0, min(m.namespaceCursor-capacity+1, len(choices)-capacity))
	last := min(len(choices), first+capacity)
	for i := first; i < last; i++ {
		content = append(content, m.namespaceChoiceRow(choices[i], i, innerWidth))
	}
	if len(choices) == 0 {
		content = append(content, paint("No namespaces match this filter.", output.ColorMuted, false))
	}
	if first > 0 || last < len(choices) {
		content = append(content, paint(fmt.Sprintf("Showing %d–%d of %d", first+1, last, len(choices)), output.ColorMuted, false))
	}
	content = append(content, "", "↑/↓ move  ·  ↵ select", "Type to filter  ·  Esc close")
	panel := tenantPanelStyle().Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/2)
	startX := max(0, (m.width-panelWidth)/2)
	for i, line := range panelLines {
		y := startY + i
		if y >= len(background) {
			break
		}
		if width := ansi.StringWidth(line); width < panelWidth {
			line += strings.Repeat(" ", panelWidth-width)
		}
		base := ansi.Truncate(background[y], m.width, "")
		base += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(base)))
		background[y] = ansi.Cut(base, 0, startX) + line + ansi.Cut(base, startX+panelWidth, m.width)
	}
	return background[:m.height]
}

func (m *browser) helpModal(background []string) []string {
	panelWidth := min(72, m.width-4)
	innerWidth := panelWidth - 6
	row := func(leftKey, leftLabel, rightKey, rightLabel string) string {
		left := paint(leftKey, output.ColorBranch, true) + " " + paint(leftLabel, output.ColorMuted, false)
		right := paint(rightKey, output.ColorBranch, true) + " " + paint(rightLabel, output.ColorMuted, false)
		columnWidth := (innerWidth - 3) / 2
		left = ansi.Truncate(left, columnWidth, "…")
		left += strings.Repeat(" ", max(1, columnWidth-ansi.StringWidth(left)))
		return left + "   " + ansi.Truncate(right, columnWidth, "…")
	}
	content := []string{
		paint("⌘  QUICK HELP", output.ColorAccent, true),
		paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
		paint("AVAILABLE CONTROLS", output.ColorKey, true),
	}
	hints := m.keyHints(true)
	for i := 0; i < len(hints); i += 2 {
		left := hints[i]
		if i+1 == len(hints) {
			content = append(content, row(left.key, left.action, "", ""))
			continue
		}
		right := hints[i+1]
		content = append(content, row(left.key, left.action, right.key, right.action))
	}
	panel := tenantPanelStyle().Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/2)
	startX := max(0, (m.width-panelWidth)/2)
	for i, line := range panelLines {
		y := startY + i
		if y >= len(background) {
			break
		}
		if width := ansi.StringWidth(line); width < panelWidth {
			line += strings.Repeat(" ", panelWidth-width)
		}
		base := ansi.Truncate(background[y], m.width, "")
		base += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(base)))
		background[y] = ansi.Cut(base, 0, startX) + line + ansi.Cut(base, startX+panelWidth, m.width)
	}
	return background[:m.height]
}

func (m *browser) namespaceChoiceRow(choice namespaceChoice, index, width int) string {
	marker := " "
	if index == m.namespaceCursor {
		marker = "›"
	}
	name := "All namespaces"
	meta := fmt.Sprintf("%d apps", len(m.apps))
	if choice.scoped {
		name = choice.key.name
		for _, namespace := range m.namespaces {
			if namespace.ID == choice.key.id && namespace.Name == choice.key.name {
				name = namespace.Name
				if namespace.Cluster.Name != "" {
					name += " · " + namespace.Cluster.Name
				}
				name += fmt.Sprintf(" · ID %d", namespace.ID)
				break
			}
		}
		count := m.namespaceAppCount(choice.key)
		meta = fmt.Sprintf("%d apps", count)
		if count == 0 {
			meta += " · empty"
		}
	}
	check := " "
	if choice.scoped == m.namespaceScoped && (!choice.scoped || choice.key == m.namespaceScope) {
		check = paint("✓", output.ColorSuccess, true)
	}
	metaWidth := min(20, max(8, width/3))
	nameWidth := max(8, width-metaWidth-8)
	name = ansi.Truncate(safeText(name), nameWidth, "…")
	meta = ansi.Truncate(meta, metaWidth, "…")
	plain := fmt.Sprintf(" %s %s%s%s%s", marker, name, strings.Repeat(" ", max(1, width-ansi.StringWidth(name)-ansi.StringWidth(meta)-6)), meta, check)
	plain = ansi.Truncate(plain, width, "")
	plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
	if index == m.namespaceCursor {
		return selectedRowStyle().Width(width).Render(plain)
	}
	return " " + paint(marker, output.ColorBranch, true) + " " + paint(name, output.ColorKey, false) + strings.Repeat(" ", max(1, width-ansi.StringWidth(name)-ansi.StringWidth(meta)-6)) + paint(meta, output.ColorMuted, false) + "  " + check
}

func tenantPanelStyle() lipgloss.Style {
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.BorderForeground(output.ColorBorder)
	} else {
		style = style.Border(lipgloss.NormalBorder())
	}
	return style
}

func selectedRowStyle() lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.Foreground(output.ColorSelectionFG).Background(output.ColorSelectionBG)
	} else {
		style = style.Reverse(true)
	}
	return style
}

func (m *browser) treeNodes() []treeNode {
	visible := m.visibleAppIndices()
	namespaceIDs := make(map[string]map[int]bool)
	type namespaceGroup struct {
		key  namespaceKey
		apps []int
	}
	groupsByKey := make(map[namespaceKey]int)
	groups := make([]namespaceGroup, 0)
	for _, appIndex := range visible {
		app := m.apps[appIndex]
		key := namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name}
		groupIndex, exists := groupsByKey[key]
		if !exists {
			groupIndex = len(groups)
			groupsByKey[key] = groupIndex
			groups = append(groups, namespaceGroup{key: key})
		}
		groups[groupIndex].apps = append(groups[groupIndex].apps, appIndex)
		if namespaceIDs[app.Namespace.Name] == nil {
			namespaceIDs[app.Namespace.Name] = make(map[int]bool)
		}
		namespaceIDs[app.Namespace.Name][app.Namespace.ID] = true
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].key.name != groups[j].key.name {
			return strings.ToLower(groups[i].key.name) < strings.ToLower(groups[j].key.name)
		}
		return groups[i].key.id < groups[j].key.id
	})
	var rows []treeNode
	for _, group := range groups {
		app := m.apps[group.apps[0]]
		heading := namespaceHeading(app, namespaceIDs, len(group.apps))
		rows = append(rows, treeNode{app: -1, key: group.key, heading: heading})
		if m.filterQuery == "" && m.collapsed[group.key] {
			continue
		}
		for i, appIndex := range group.apps {
			rows = append(rows, treeNode{app: appIndex, key: group.key, heading: heading, last: i == len(group.apps)-1})
		}
	}
	return rows
}

func namespaceHeading(app client.App, namespaceIDs map[string]map[int]bool, appCount int) string {
	name := app.Namespace.Name
	if name == "" {
		name = "Unknown namespace"
	}
	if len(namespaceIDs[app.Namespace.Name]) > 1 {
		name += fmt.Sprintf(" (ID %d)", app.Namespace.ID)
	}
	appLabel := "apps"
	if appCount == 1 {
		appLabel = "app"
	}
	return fmt.Sprintf("%s  ·  %d %s", name, appCount, appLabel)
}

func (m *browser) treeSelection(rows []treeNode) int {
	if len(m.visibleAppIndices()) == 0 {
		return 0
	}
	a := m.apps[m.selected]
	key := namespaceKey{id: a.Namespace.ID, name: a.Namespace.Name}
	for i, row := range rows {
		if row.key == key && ((m.namespaceSelected && row.app < 0) || (!m.namespaceSelected && row.app == m.selected)) {
			return i
		}
	}
	return 0
}

func (m *browser) selectTree(index int) tea.Cmd {
	previousTarget := m.selectedDetailTarget()
	rows := m.treeNodes()
	if len(rows) == 0 {
		return nil
	}
	node := rows[max(0, min(index, len(rows)-1))]
	m.namespaceSelected = node.app < 0
	if node.app >= 0 {
		m.selected = node.app
		m.detailApp = &m.apps[m.selected]
		return m.syncDetailTarget(previousTarget)
	}
	for _, i := range m.visibleAppIndices() {
		app := m.apps[i]
		if (namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name}) == node.key {
			m.selected = i
			m.detailApp = &m.apps[m.selected]
			return m.syncDetailTarget(previousTarget)
		}
	}
	return nil
}

func (m *browser) moveTree(delta int) tea.Cmd {
	rows := m.treeNodes()
	if len(rows) > 0 {
		return m.selectTree(m.treeSelection(rows) + delta)
	}
	return nil
}

func (m *browser) setCollapsed(collapsed bool) {
	a := m.apps[m.selected]
	key := namespaceKey{id: a.Namespace.ID, name: a.Namespace.Name}
	if m.collapsed == nil {
		m.collapsed = make(map[namespaceKey]bool)
	}
	m.collapsed[key] = collapsed
	if collapsed {
		m.namespaceSelected = true
	} else {
		m.namespaceSelected = false
	}
}

func (m *browser) toggleNamespace() {
	a := m.apps[m.selected]
	key := namespaceKey{id: a.Namespace.ID, name: a.Namespace.Name}
	m.setCollapsed(!m.collapsed[key])
}

// appTree keeps namespace headings visible even when a group spans the viewport.
func (m *browser) appTree(height int) []string {
	return m.appTreeForWidth(height, m.width)
}

func (m *browser) appTreeForWidth(height, width int) []string {
	rows := m.treeNodes()
	if len(rows) == 0 || height < 2 {
		return nil
	}
	selectedRow := m.treeSelection(rows)
	nameWidth := 0
	for _, appIndex := range m.visibleAppIndices() {
		app := m.apps[appIndex]
		nameWidth = max(nameWidth, ansi.StringWidth(safeText(app.Name)))
	}
	const minNameWidth = 8
	const reservedWidth = 30
	nameWidth = min(nameWidth, max(minNameWidth, width-reservedWidth))
	start := max(0, selectedRow-height+1)
	var result []string
	if rows[start].app >= 0 {
		result = append(result, "  "+paint("▾ "+safeText(rows[start].heading), output.ColorBranch, true))
		if selectedRow-start >= height-1 {
			start++
		}
	}
	for i := start; i < len(rows) && len(result) < height; i++ {
		node := rows[i]
		var line string
		if node.app < 0 {
			indicator := "▾"
			if m.collapsed[node.key] {
				indicator = "▸"
			}
			line = "  " + paint(indicator+" "+safeText(node.heading), output.ColorBranch, true)
		} else {
			branch := "├── "
			if node.last {
				branch = "└── "
			}
			app := m.apps[node.app]
			name := ansi.Truncate(safeText(app.Name), nameWidth, "…")
			padding := strings.Repeat(" ", max(0, nameWidth-ansi.StringWidth(name)))
			state := safeText(appState(app))
			glyph, stateColor := stateStyle(state)
			line = "  " + paint(branch, output.ColorBorder, false) + name + padding + "  " + paint(glyph+" "+state, stateColor, false)
		}
		if i == selectedRow {
			plain := ansi.Truncate(ansi.Strip(line), width, "")
			plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
			line = selectedRowStyle().Width(width).Render(plain)
		}
		result = append(result, line)
	}
	return result
}

func safeText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(text))
}

func paint(text string, c color.Color, bold bool) string {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return text
	}
	style := lipgloss.NewStyle().Bold(bold)
	if c != nil {
		style = style.Foreground(c)
	}
	return style.Render(text)
}

func stateStyle(state string) (string, color.Color) {
	s := strings.ToLower(strings.TrimSpace(state))
	switch {
	case strings.Contains(s, "unhealthy"), strings.Contains(s, "not ready"), strings.Contains(s, "error"),
		strings.Contains(s, "fail"), strings.Contains(s, "crash"):
		return "●", output.ColorDanger
	case strings.Contains(s, "healthy"), strings.Contains(s, "running"), strings.Contains(s, "ready"):
		return "●", output.ColorSuccess
	case strings.Contains(s, "degraded"), strings.Contains(s, "warning"):
		return "▲", output.ColorNumber
	default:
		return "○", output.ColorMuted
	}
}

func appState(a client.App) string {
	if a.State.Text != "" {
		return a.State.Text
	}
	if a.State.StateType != "" {
		return a.State.StateType
	}
	return "Unknown"
}
