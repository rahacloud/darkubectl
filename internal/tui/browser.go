package tui

import (
	"context"
	"fmt"
	"image/color"
	"math"
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
	tenant string
	apps   []client.App
	err    error
}

type profileResult struct {
	profile *client.Profile
	err     error
}

type appDetailResult struct {
	tenant string
	id     string
	app    *client.App
	usage  *AppUsage
	err    error
}

// AppUsage is an optional latest resource snapshot, in CPU cores and bytes.
type AppUsage struct {
	CPUCores  *float64
	RAMBytes  *float64
	DiskBytes *float64
}

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

// BrowserOptions contains the account and tenant-scoped reads used by the live browser.
type BrowserOptions struct {
	Tenant       string
	LocalTenants []string
	Profile      func(context.Context) (*client.Profile, error)
	ListApps     func(context.Context, string) ([]client.App, error)
	GetApp       func(context.Context, string, string) (*client.App, error)
	Usage        func(context.Context, string, string) (*AppUsage, error)
}

type namespaceKey struct {
	id   int
	name string
}

type treeNode struct {
	app     int
	key     namespaceKey
	heading string
	last    bool
}

type browser struct {
	tenant            string
	mode              string
	listApps          func(string) ([]client.App, error)
	getApp            func(string, string) (*client.App, error)
	usage             func(string, string) (*AppUsage, error)
	profile           func(context.Context) (*client.Profile, error)
	ctx               context.Context
	localTenants      []string
	tenants           []tenantOption
	tenantCursor      int
	selectingTenant   bool
	profileLoaded     bool
	profileErr        error
	profileEmpty      bool
	tenantStatus      string
	apps              []client.App
	err               error
	loading           bool
	selected          int
	namespaceSelected bool
	filterQuery       string
	filterBeforeEdit  string
	filterEditing     bool
	filterSelection   string
	collapsed         map[namespaceKey]bool
	detail            bool
	detailLoading     bool
	detailErr         error
	detailApp         *client.App
	detailUsage       *AppUsage
	usageAppID        string
	sorting           bool
	sortChoice        int
	sortBy            appSort
	sortDescending    bool
	help              bool
	spinnerFrame      int
	width             int
	height            int
}

// RunBrowser opens the read-only app browser using the caller's scoped client.
func RunBrowser(ctx context.Context, options BrowserOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &browser{tenant: options.Tenant, mode: "LIVE", profile: options.Profile, ctx: ctx,
		localTenants: append([]string(nil), options.LocalTenants...),
		listApps:     func(tenant string) ([]client.App, error) { return options.ListApps(ctx, tenant) }}
	if options.GetApp != nil {
		m.getApp = func(tenant, id string) (*client.App, error) { return options.GetApp(ctx, tenant, id) }
	}
	if options.Usage != nil {
		m.usage = func(tenant, id string) (*AppUsage, error) { return options.Usage(ctx, tenant, id) }
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
		tenantProfiles = []MockTenant{{Name: profile.Tenant, Apps: profile.Apps}}
	}
	appsByTenant := make(map[string][]client.App, len(tenantProfiles))
	options := make([]tenantOption, 0, len(tenantProfiles))
	for _, tenant := range tenantProfiles {
		appsByTenant[tenant.Name] = append([]client.App(nil), tenant.Apps...)
		options = append(options, tenantOption{name: tenant.Name, source: "Mock profile"})
	}
	if len(options) > 0 {
		options[0].status = "current"
	}
	m := &browser{
		tenant:  profile.Tenant,
		mode:    "MOCK PROFILE · OFFLINE",
		tenants: options,
		listApps: func(tenant string) ([]client.App, error) {
			apps, ok := appsByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.App(nil), apps...), nil
		},
	}
	m.selectingTenant = len(options) > 1
	return m
}

func (m *browser) load() tea.Cmd {
	m.loading = true
	tenant := m.tenant
	loadCmd := func() tea.Msg {
		apps, err := m.listApps(tenant)
		return appsResult{tenant: tenant, apps: apps, err: err}
	}
	return loadCmd
}

func (m *browser) loadProfile() tea.Cmd {
	if m.profile == nil {
		return nil
	}
	return func() tea.Msg {
		profile, err := m.profile(m.ctx)
		return profileResult{profile: profile, err: err}
	}
}

func (m *browser) Init() tea.Cmd {
	profileCmd := m.loadProfile()
	if m.mode != "LIVE" {
		return m.load()
	}
	if m.tenant == "" && m.mode == "LIVE" {
		m.selectingTenant = true
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
		if msg.tenant != m.tenant {
			return m, nil
		}
		m.loading = false
		m.err = msg.err
		if msg.err == nil { //nolint:nestif // Refresh preserves the selected app and its detail state.
			selectedID := ""
			if len(m.apps) > 0 {
				selectedID = m.apps[m.selected].ID
			}
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
			} else {
				m.detailApp = &m.apps[m.selected]
				m.detailLoading = false
				m.detailErr = nil
			}
		}
	case profileResult:
		m.profileLoaded = true
		m.profileErr = msg.err
		m.tenantStatus = ""
		m.tenants = nil
		if msg.err == nil {
			m.profileEmpty = msg.profile == nil || len(msg.profile.Organizations) == 0
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
			}
		} else {
			m.profileEmpty = false
			for _, name := range m.localTenants {
				m.tenants = appendTenant(m.tenants, tenantOption{name: name, source: "Local config"})
			}
			if m.tenant != "" {
				m.tenants = appendTenant(m.tenants, tenantOption{name: m.tenant, source: "Local config", status: "current"})
				m.tenantStatus = "local config"
			}
		}
		if m.tenant == "" {
			m.selectingTenant = true
			m.tenantCursor = min(m.tenantCursor, max(0, len(m.tenants)-1))
		}
	case appDetailResult:
		if msg.tenant != m.tenant || len(m.apps) == 0 || m.apps[m.selected].ID != msg.id {
			return m, nil
		}
		m.detailLoading = false
		m.detailErr = msg.err
		if msg.err == nil && msg.app != nil {
			m.detailApp = msg.app
		}
		if msg.err == nil && msg.usage != nil {
			m.detailUsage = msg.usage
			m.usageAppID = msg.id
		}
	case tea.KeyPressMsg:
		if m.filterEditing {
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
				return m, nil
			case "backspace", "ctrl+h":
				runes := []rune(m.filterQuery)
				if len(runes) > 0 {
					m.filterQuery = string(runes[:len(runes)-1])
					m.ensureFilterSelection()
				}
				return m, nil
			case "ctrl+u":
				m.filterQuery = ""
				m.ensureFilterSelection()
				return m, nil
			}
			if msg.Text != "" {
				m.filterQuery += safeText(msg.Text)
				m.ensureFilterSelection()
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "t":
			if !m.help {
				m.selectingTenant = !m.selectingTenant
				m.tenantCursor = 0
			}
		case "?":
			m.help = !m.help
		case "/":
			if !m.help && !m.selectingTenant {
				m.filterEditing = true
				m.filterBeforeEdit = m.filterQuery
				m.namespaceSelected = false
				m.filterSelection = ""
				if m.selected >= 0 && m.selected < len(m.apps) {
					m.filterSelection = m.apps[m.selected].ID
				}
			}
		case "s":
			if !m.help && !m.selectingTenant {
				m.sorting = true
				m.sortChoice = int(m.sortBy)
			}
		case "left", "h":
			if m.sorting {
				m.sortDescending = false
			} else if !m.help && !m.selectingTenant && !m.detail && len(m.apps) > 0 {
				m.setCollapsed(true)
			} else if !m.help && !m.selectingTenant && m.detail {
				m.detail = false
			}
		case "right", "l":
			if m.sorting {
				m.sortDescending = true
			} else if !m.help && !m.selectingTenant && !m.detail && m.namespaceSelected && len(m.apps) > 0 {
				m.setCollapsed(false)
			} else if !m.help && !m.selectingTenant && !m.detail && !m.namespaceSelected && len(m.apps) > 0 {
				m.detail = true
				return m, m.loadSelectedSummary()
			}
		case keyEscape:
			if m.sorting {
				m.sorting = false
			} else if m.selectingTenant && m.tenant != "" {
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
			} else if !m.help && !m.detail {
				return m, m.moveTree(1)
			} else if !m.help && m.detail {
				return m, m.moveTree(1)
			}
		case "home", "g":
			if !m.help {
				return m, m.selectTree(0)
			}
		case "end", "G":
			if !m.help {
				return m, m.selectTree(len(m.treeNodes()) - 1)
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
				m.profileLoaded = false
				m.profileEmpty = false
				m.profileErr = nil
				return m, m.loadProfile()
			}
			if m.detail && !m.detailLoading {
				return m, m.loadSelectedSummary()
			}
			if !m.help && !m.loading {
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
	if len(m.apps) == 0 || m.selected < 0 || m.selected >= len(m.apps) {
		return nil
	}
	selected := m.apps[m.selected]
	if m.detailApp == nil || m.detailApp.ID != selected.ID {
		m.detailApp = &selected
	}
	m.detailErr = nil
	m.detailUsage = nil
	m.usageAppID = ""
	if m.getApp == nil {
		m.detailLoading = false
		return nil
	}
	m.detailLoading = true
	tenant, id := m.tenant, m.apps[m.selected].ID
	return func() tea.Msg {
		app, err := m.getApp(tenant, id)
		var usage *AppUsage
		if err == nil && m.usage != nil {
			usage, _ = m.usage(tenant, id)
		}
		return appDetailResult{tenant: tenant, id: id, app: app, usage: usage, err: err}
	}
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
		return nil
	}
	m.tenant = name
	m.tenantStatus = ""
	m.selectingTenant = false
	m.apps = nil
	m.err = nil
	m.selected = 0
	m.namespaceSelected = false
	m.filterQuery = ""
	m.filterBeforeEdit = ""
	m.filterEditing = false
	m.collapsed = nil
	m.detail = false
	m.detailApp = nil
	m.detailErr = nil
	m.detailLoading = false
	return m.load()
}

func (m *browser) View() tea.View {
	var lines []string
	footer := m.footerLine()
	if m.width < 60 || m.height < 16 {
		lines = []string{"Resize terminal to at least 60 x 16"}
	} else {
		header := m.breadcrumb()
		if m.filterQuery != "" || m.filterEditing {
			header += "   " + m.filterLine()
		}
		sortIndicator := m.sortLine()
		padding := max(1, m.width-ansi.StringWidth(header)-ansi.StringWidth(sortIndicator))
		header += strings.Repeat(" ", padding) + sortIndicator
		lines = append(lines, header, "")
		if m.profileLoaded && m.profileErr != nil && !m.selectingTenant {
			lines = append(lines, "Account profile unavailable; tenant apps remain available. Press t to choose a local-config tenant.")
		}
		bodyHeight := max(1, m.height-len(lines)-footerHeight-1)
		if m.help {
			lines = append(lines, "Navigation", "↑/↓ or j/k  move through the app tree", "←/h  collapse namespace / return from Summary",
				"→/l  expand namespace / open selected app Summary", "Enter  toggle namespace or focus app Summary",
				"/  filter    s  sort    t  tenant    r  refresh", "?  close help    q  quit")
		} else {
			left := m.appPaneLines(bodyHeight)
			right := m.summaryPaneLines()
			lines = append(lines, m.splitPanes(left, right, bodyHeight)...)
		}
	}
	if len(lines) > m.height-footerHeight {
		lines = lines[:max(0, m.height-footerHeight)]
	}
	for len(lines) < max(0, m.height-footerHeight) {
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	if m.selectingTenant && m.width >= 60 && m.height >= 16 {
		lines = m.tenantModal(lines)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

func (m *browser) appPaneLines(height int) []string {
	heading := "  APPS"
	if !m.detail {
		heading = "▸ APPS"
	}
	if m.loading {
		heading += " " + paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	lines := []string{paint(heading, output.ColorAccent, true)}
	if m.err != nil {
		if len(m.apps) == 0 {
			lines = append(lines, "Could not load apps: "+safeText(m.err.Error()))
		} else {
			lines = append(lines, paint("Refresh failed; apps may be stale: "+safeText(m.err.Error()), output.ColorNumber, false))
		}
	}
	if !m.loading && m.err == nil && len(m.apps) == 0 {
		label := "No apps in this tenant."
		if strings.HasPrefix(m.mode, "MOCK") {
			label = "No apps in this mock profile."
		}
		lines = append(lines, label)
	}
	if !m.loading && m.err == nil && len(m.apps) > 0 && len(m.visibleAppIndices()) == 0 {
		lines = append(lines, "No apps match this filter.")
	}
	room := max(0, height-len(lines))
	return append(lines, m.appTreeForWidth(room, max(1, (m.width-3)/2))...)
}

func (m *browser) summaryPaneLines() []string {
	heading := "  SUMMARY"
	if m.detail {
		heading = "▸ SUMMARY"
	}
	if m.detailLoading {
		heading += " " + paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	if m.namespaceSelected || len(m.apps) == 0 || m.selected < 0 || m.selected >= len(m.apps) {
		return []string{paint(heading, output.ColorAccent, true), paint("No app selected. Move to an app to preview its summary.", output.ColorMuted, false)}
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	stateGlyph, stateColor := stateStyle(appState(a))
	headingLine := paint(heading, output.ColorAccent, true) + paint(" · ", output.ColorBorder, false) + paint(safeText(a.Name), output.ColorKey, true)
	lines := []string{headingLine}
	if m.detailErr != nil {
		lines = append(lines, paint("Detail refresh failed: "+safeText(m.detailErr.Error()), output.ColorNumber, false))
	}
	lines = append(lines, "",
		summaryField("Tenant", displayValue(m.tenant)),
		summaryField("Namespace", displayValue(a.Namespace.Name)),
		summaryField("Cluster", displayValue(a.Namespace.Cluster.Name)),
		summaryField("State", paint(stateGlyph+" "+safeText(appState(a)), stateColor, true)),
		summaryField("Description", displayValue(a.State.Description)),
		summaryField("Replicas", fmt.Sprintf("%d", a.Replicas)))
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
		planResourceField("", "CPU", usageRatio(usageValue(usage, func(u *AppUsage) *float64 { return u.CPUCores }), cpuTotal, formatCores)),
		planResourceField("", "RAM", usageRatio(usageValue(usage, func(u *AppUsage) *float64 { return u.RAMBytes }), ramTotal, formatBytes)))
	if (a.Disk != nil && a.Disk.SizeInGi > 0) || (usage != nil && usage.DiskBytes != nil) {
		diskTotal := "-"
		if a.Disk != nil && a.Disk.SizeInGi > 0 {
			diskTotal = fmt.Sprintf("%d GiB", a.Disk.SizeInGi)
		}
		lines = append(lines, planResourceField("", "Disk", usageRatio(usageValue(usage, func(u *AppUsage) *float64 { return u.DiskBytes }), diskTotal, formatBytes)))
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

func appDomains(app client.App) []string {
	domains := make([]string, 0, len(app.ExternalHosts)+1)
	seen := make(map[string]struct{}, len(app.ExternalHosts)+1)
	for _, domain := range append([]string{app.CustomDomainAddress}, app.ExternalHosts...) {
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

func summaryLabel(label string) string {
	return paint(fmt.Sprintf("%-12s", label), output.ColorMuted, true)
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
	return "◆ " + safeText(strings.Join(words, " "))
}

func formatLocalTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return fallbackText(value)
	}
	return parsed.Local().Format("Jan 2, 2006 · 3:04 PM MST")
}

func (m *browser) splitPanes(left, right []string, height int) []string {
	leftWidth := max(1, (m.width-3)/2)
	rightWidth := max(1, m.width-3-leftWidth)
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		l = ansi.Truncate(l, leftWidth, "…")
		r = ansi.Truncate(r, rightWidth, "…")
		l += strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(l)))
		divider := paint("│", output.ColorBorder, false)
		if m.detail {
			divider = paint("┃", output.ColorBranch, true)
		}
		lines[i] = l + " " + divider + " " + r
	}
	return lines
}

func (m *browser) footerLine() string {
	items := [][2]string{{"↑↓", "move"}, {"←→/h/l", "panes"}, {"/", "filter"}, {"s", "sort"}, {"?", "help"}, {"q", "quit"}}
	if m.width >= 100 {
		items = [][2]string{{"↑↓", "move"}, {"←→/h/l", "panes"}, {"↵", "open/toggle"}, {"/", "filter"}, {"s", "sort"}, {"r", "refresh"}, {"?", "help"}, {"q", "quit"}}
		items = append(items[:5], append([][2]string{{"t", "tenant"}}, items[5:]...)...)
	}
	if m.help {
		items = [][2]string{{"Esc", "close help"}, {"q", "quit"}}
	}
	if m.filterEditing {
		items = [][2]string{{"type", "filter"}, {"↑↓", "move"}, {"Enter", "open"}, {"Esc", "cancel"}, {"Ctrl+U", "clear"}}
	}
	if m.sorting {
		items = [][2]string{{"↑↓", "field"}, {"←→", "order"}, {"Enter", "apply"}, {"Esc", "close"}}
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, paint(item[0], output.ColorBranch, true)+" "+paint(item[1], output.ColorMuted, false))
	}
	return strings.Join(parts, paint(" · ", output.ColorBorder, false))
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
	if !m.namespaceSelected && len(m.apps) > 0 && m.selected >= 0 && m.selected < len(m.apps) {
		app := m.apps[m.selected]
		parts = append(parts, safeText(app.Namespace.Name), safeText(app.Name))
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
	return paint("◈", output.ColorNumber, true) + " " + paint("MOCK · OFFLINE", output.ColorMuted, false) + "  " + path
}

func fallbackText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return safeText(value)
}

func (m *browser) busy() bool {
	return m.mode == "LIVE" && (!m.profileLoaded || m.loading)
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

func (m *browser) tenantModal(background []string) []string {
	panelWidth := min(68, m.width-4)
	innerWidth := panelWidth - 6 // border and horizontal padding
	messages := []string{}
	if !m.profileLoaded {
		messages = append(messages, m.loadingMessage("Loading account profile…"))
	}
	if m.profileErr != nil {
		messages = append(messages, "Profile unavailable · using local config")
	}
	if m.profileEmpty {
		messages = append(messages, "Profile returned no tenant memberships")
	}
	noChoices := len(m.tenants) == 0 && m.profileLoaded
	if noChoices {
		messages = append(messages, "No tenant choices are available.", "Use darkubectl whoami, --org, or darkubectl login.")
	}
	maxInnerHeight := max(6, min(15, m.height-8))
	listCapacity := max(1, maxInnerHeight-5-len(messages))
	first := max(0, min(m.tenantCursor-listCapacity+1, len(m.tenants)-listCapacity))
	last := min(len(m.tenants), first+listCapacity)
	content := []string{
		paint("◈  SWITCH TENANT", output.ColorAccent, true),
		"Choose a tenant for this TUI session",
		paint(strings.Repeat("─", innerWidth), output.ColorBorder, false),
	}
	content = append(content, messages...)
	for i := first; i < last; i++ {
		option := m.tenants[i]
		label := m.tenantRow(option, i, innerWidth)
		content = append(content, label)
	}
	if first > 0 || last < len(m.tenants) {
		content = append(content, paint(fmt.Sprintf("Showing %d–%d of %d", first+1, last, len(m.tenants)), output.ColorMuted, false))
	}
	content = append(content, "", "↑/↓ or k/j move  ·  Enter select", "Esc cancel  ·  r retry profile  ·  session only")
	style := tenantPanelStyle()
	panel := style.Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	if len(background) < m.height {
		background = append(background, make([]string, m.height-len(background))...)
	}
	startY := max(0, (m.height-len(panelLines))/2)
	startX := max(0, (m.width-panelWidth)/2)
	for i, panelLine := range panelLines {
		y := startY + i
		if y >= len(background) {
			break
		}
		width := ansi.StringWidth(panelLine)
		if width < panelWidth {
			panelLine += strings.Repeat(" ", panelWidth-width)
		}
		base := ansi.Truncate(background[y], m.width, "")
		base = base + strings.Repeat(" ", max(0, m.width-ansi.StringWidth(base)))
		background[y] = ansi.Cut(base, 0, startX) + panelLine + ansi.Cut(base, startX+panelWidth, m.width)
	}
	return background[:m.height]
}

func (m *browser) tenantRow(option tenantOption, index, width int) string {
	const sourceWidth = 12
	nameWidth := max(8, width-sourceWidth-8)
	name := ansi.Truncate(safeText(option.name), nameWidth, "…")
	marker := " "
	if index == m.tenantCursor {
		marker = "›"
	}
	plain := " " + marker + " " + name
	plain += strings.Repeat(" ", max(1, width-sourceWidth-3-ansi.StringWidth(plain)))
	source := ansi.Truncate(option.source, sourceWidth, "…")
	plain += source + "  "
	check := " "
	if option.status == "current" {
		check = "✓"
	}
	plain += check
	plain = ansi.Truncate(plain, width, "")
	plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
	if index == m.tenantCursor {
		return tenantSelectedStyle().Width(width).Render(plain)
	}
	row := " " + paint(marker, output.ColorBranch, true) + " " + paint(name, output.ColorKey, false)
	row += strings.Repeat(" ", max(1, width-sourceWidth-3-ansi.StringWidth(" "+marker+" "+name)))
	row += paint(source, output.ColorMuted, false) + "  "
	if check == "✓" {
		row += paint(check, output.ColorSuccess, true)
	} else {
		row += check
	}
	return row + strings.Repeat(" ", max(0, width-ansi.StringWidth(row)))
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

func tenantSelectedStyle() lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.Foreground(lipgloss.Color("#FFFFFF")).Background(output.ColorBranch)
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
	rows := m.treeNodes()
	if len(rows) == 0 {
		return nil
	}
	node := rows[max(0, min(index, len(rows)-1))]
	m.namespaceSelected = node.app < 0
	if node.app >= 0 {
		m.selected = node.app
		m.detailApp = &m.apps[m.selected]
		m.detailLoading = false
		m.detailErr = nil
		return nil
	}
	for _, i := range m.visibleAppIndices() {
		app := m.apps[i]
		if (namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name}) == node.key {
			m.selected = i
			m.detailApp = &m.apps[m.selected]
			m.detailLoading = false
			m.detailErr = nil
			return nil
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
			line = lipgloss.NewStyle().Reverse(true).Render(plain)
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
