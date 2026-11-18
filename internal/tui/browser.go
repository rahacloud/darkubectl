package tui

import (
	"context"
	"fmt"
	"image/color"
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

type blinkMsg struct{}

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
	collapsed         map[namespaceKey]bool
	detail            bool
	help              bool
	blinkOn           bool
	width             int
	height            int
}

// RunBrowser opens the read-only app browser using the caller's scoped client.
func RunBrowser(ctx context.Context, options BrowserOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &browser{tenant: options.Tenant, mode: "LIVE", profile: options.Profile, ctx: ctx, blinkOn: true,
		localTenants: append([]string(nil), options.LocalTenants...),
		listApps:     func(tenant string) ([]client.App, error) { return options.ListApps(ctx, tenant) }}
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
	return func() tea.Msg {
		apps, err := m.listApps(tenant)
		return appsResult{tenant: tenant, apps: apps, err: err}
	}
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
		return tea.Batch(profileCmd, blinkCmd())
	}
	return tea.Batch(profileCmd, m.load(), blinkCmd())
}

func blinkCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return blinkMsg{} })
}

//nolint:cyclop,gocognit,gocyclo // Message routing keeps the model's transitions together.
func (m *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case blinkMsg:
		if m.mode == "LIVE" {
			m.blinkOn = !m.blinkOn
			return m, blinkCmd()
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
			sort.Slice(m.apps, func(i, j int) bool {
				if m.apps[i].Namespace.Name != m.apps[j].Namespace.Name {
					return m.apps[i].Namespace.Name < m.apps[j].Namespace.Name
				}
				if m.apps[i].Namespace.ID != m.apps[j].Namespace.ID {
					return m.apps[i].Namespace.ID < m.apps[j].Namespace.ID
				}
				if m.apps[i].Name != m.apps[j].Name {
					return m.apps[i].Name < m.apps[j].Name
				}
				return m.apps[i].ID < m.apps[j].ID
			})
			m.selected = min(m.selected, max(0, len(m.apps)-1))
			for i := range m.apps {
				if m.apps[i].ID == selectedID && selectedID != "" {
					m.selected = i
					break
				}
			}
			if m.detail && (len(m.apps) == 0 || m.apps[m.selected].ID != selectedID) {
				m.detail = false
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
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "t":
			if !m.help && !m.detail {
				m.selectingTenant = !m.selectingTenant
				m.tenantCursor = 0
			}
		case "?":
			m.help = !m.help
		case keyEscape:
			if m.selectingTenant && m.tenant != "" {
				m.selectingTenant = false
			} else if m.help {
				m.help = false
			} else {
				m.detail = false
			}
		case "up", "k":
			if m.selectingTenant {
				m.tenantCursor = max(0, m.tenantCursor-1)
			} else if !m.help && !m.detail {
				m.moveTree(-1)
			}
		case "down", "j":
			if m.selectingTenant {
				m.tenantCursor = min(max(0, len(m.tenants)-1), m.tenantCursor+1)
			} else if !m.help && !m.detail {
				m.moveTree(1)
			}
		case "home", "g":
			if !m.help && !m.detail {
				m.selectTree(0)
			}
		case "end", "G":
			if !m.help && !m.detail {
				m.selectTree(len(m.treeNodes()) - 1)
			}
		case "enter":
			if m.selectingTenant {
				if len(m.tenants) > 0 {
					return m, m.chooseTenant(m.tenants[m.tenantCursor].name)
				}
			} else if !m.help && len(m.apps) > 0 {
				if m.namespaceSelected {
					m.toggleNamespace()
				} else {
					m.detail = true
				}
			}
		case "left", "h":
			if !m.help && !m.detail && len(m.apps) > 0 {
				m.setCollapsed(true)
			}
		case "right", "l":
			if !m.help && !m.detail && len(m.apps) > 0 {
				m.setCollapsed(false)
			}
		case "r":
			if m.selectingTenant {
				m.profileLoaded = false
				m.profileEmpty = false
				m.profileErr = nil
				return m, m.loadProfile()
			}
			if !m.help && !m.loading {
				return m, m.load()
			}
		}
	}
	return m, nil
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
	m.collapsed = nil
	m.detail = false
	return m.load()
}

func (m *browser) View() tea.View {
	var lines []string
	if m.width < 60 || m.height < 16 {
		lines = []string{"Resize terminal to at least 60 x 16", "q quit"}
	} else {
		lines = append(lines, m.breadcrumb(), "")
		if m.profileLoaded && m.profileErr != nil && !m.selectingTenant {
			lines = append(lines, "Account profile unavailable; tenant apps remain available. Press t to choose a local-config tenant.")
		}
		switch {
		case m.help:
			lines = append(lines, "Navigation", "Up/Down or k/j: select    Enter: toggle namespace or open app",
				"Left/Right or h/l: collapse/expand    Esc: back", "Home/End or g/G: first/last    r: refresh    q: quit",
				"", "Press ? or Esc to close help")
		case m.detail && len(m.apps) > 0:
			a := m.apps[m.selected]
			lines = append(lines, safeText(a.Name), "Namespace: "+safeText(a.Namespace.Name), "ID: "+safeText(a.ID),
				"State: "+safeText(appState(a)), fmt.Sprintf("Desired replicas: %d", a.Replicas),
				"Type: "+safeText(a.CreationMethod), "Image: "+safeText(a.Image()), "Domain: "+safeText(a.CustomDomainAddress))
		default:
			if m.loading {
				lines = append(lines, "Loading apps...")
			}
			if m.err != nil {
				if len(m.apps) == 0 {
					lines = append(lines, "Could not load apps: "+safeText(m.err.Error())+"  (r to retry)")
				} else {
					lines = append(lines, "Refresh failed; displayed apps may be stale: "+safeText(m.err.Error()))
				}
			}
			if !m.loading && m.err == nil && len(m.apps) == 0 {
				emptyLabel := "No apps in this tenant."
				if strings.HasPrefix(m.mode, "MOCK") {
					emptyLabel = "No apps in this mock profile."
				}
				lines = append(lines, emptyLabel)
			}
			lines = append(lines, m.appTree(max(0, m.height-len(lines)-footerHeight-1))...)
		}
		lines = append(lines, "", "j/k move  Enter open/toggle  h/l collapse/expand  t tenants  r refresh  ? help  q quit")
	}
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
	if m.detail && len(m.apps) > 0 && m.selected >= 0 && m.selected < len(m.apps) {
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
		glyph, glyphColor := "●", output.ColorSuccess
		if !m.blinkOn {
			glyph, glyphColor = "○", output.ColorMuted
		}
		return paint(glyph, glyphColor, true) + " " + path
	}
	return paint("◈", output.ColorNumber, true) + " " + paint("MOCK · OFFLINE", output.ColorMuted, false) + "  " + path
}

func (m *browser) tenantModal(background []string) []string {
	panelWidth := min(68, m.width-4)
	innerWidth := panelWidth - 6 // border and horizontal padding
	messages := []string{}
	if !m.profileLoaded {
		messages = append(messages, "Loading account profile…")
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
	var rows []treeNode
	namespaceIDs := make(map[string]map[int]bool)
	for _, app := range m.apps {
		if namespaceIDs[app.Namespace.Name] == nil {
			namespaceIDs[app.Namespace.Name] = make(map[int]bool)
		}
		namespaceIDs[app.Namespace.Name][app.Namespace.ID] = true
	}
	for i, app := range m.apps {
		key := namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name}
		if i == 0 || app.Namespace.ID != m.apps[i-1].Namespace.ID || app.Namespace.Name != m.apps[i-1].Namespace.Name {
			name := app.Namespace.Name
			if name == "" {
				name = "Unknown namespace"
			}
			if len(namespaceIDs[app.Namespace.Name]) > 1 {
				name += fmt.Sprintf(" (ID %d)", app.Namespace.ID)
			}
			rows = append(rows, treeNode{app: -1, key: key, heading: name})
		}
		if !m.collapsed[key] {
			last := i+1 == len(m.apps) || app.Namespace.ID != m.apps[i+1].Namespace.ID || app.Namespace.Name != m.apps[i+1].Namespace.Name
			rows = append(rows, treeNode{app: i, key: key, heading: rows[len(rows)-1].heading, last: last})
		}
	}
	return rows
}

func (m *browser) treeSelection(rows []treeNode) int {
	if len(m.apps) == 0 {
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

func (m *browser) selectTree(index int) {
	rows := m.treeNodes()
	if len(rows) == 0 {
		return
	}
	node := rows[max(0, min(index, len(rows)-1))]
	m.namespaceSelected = node.app < 0
	if node.app >= 0 {
		m.selected = node.app
		return
	}
	for i, app := range m.apps {
		if (namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name}) == node.key {
			m.selected = i
			return
		}
	}
}

func (m *browser) moveTree(delta int) {
	rows := m.treeNodes()
	if len(rows) > 0 {
		m.selectTree(m.treeSelection(rows) + delta)
	}
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
	}
}

func (m *browser) toggleNamespace() {
	a := m.apps[m.selected]
	key := namespaceKey{id: a.Namespace.ID, name: a.Namespace.Name}
	m.setCollapsed(!m.collapsed[key])
}

// appTree keeps namespace headings visible even when a group spans the viewport.
func (m *browser) appTree(height int) []string {
	rows := m.treeNodes()
	if len(rows) == 0 || height < 2 {
		return nil
	}
	selectedRow := m.treeSelection(rows)
	nameWidth := 0
	for _, app := range m.apps {
		nameWidth = max(nameWidth, ansi.StringWidth(safeText(app.Name)))
	}
	const minNameWidth = 8
	const reservedWidth = 30
	nameWidth = min(nameWidth, max(minNameWidth, m.width-reservedWidth))
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
			plain := ansi.Truncate(ansi.Strip(line), m.width, "")
			plain += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(plain)))
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
