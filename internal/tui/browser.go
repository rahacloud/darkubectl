package tui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
)

type appsResult struct {
	apps []client.App
	err  error
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
	listApps          func() ([]client.App, error)
	apps              []client.App
	err               error
	loading           bool
	selected          int
	namespaceSelected bool
	collapsed         map[namespaceKey]bool
	detail            bool
	help              bool
	width             int
	height            int
}

// RunBrowser opens the read-only app browser using the caller's scoped client.
func RunBrowser(ctx context.Context, tenant string, listApps func(context.Context) ([]client.App, error)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &browser{tenant: tenant, listApps: func() ([]client.App, error) { return listApps(ctx) }}
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

func (m *browser) load() tea.Cmd {
	m.loading = true
	return func() tea.Msg {
		apps, err := m.listApps()
		return appsResult{apps: apps, err: err}
	}
}

func (m *browser) Init() tea.Cmd { return m.load() }

//nolint:cyclop,gocognit,gocyclo // Message routing keeps the model's transitions together.
func (m *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case appsResult:
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
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.help = !m.help
		case keyEscape:
			if m.help {
				m.help = false
			} else {
				m.detail = false
			}
		case "up", "k":
			if !m.help && !m.detail {
				m.moveTree(-1)
			}
		case "down", "j":
			if !m.help && !m.detail {
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
			if !m.help && len(m.apps) > 0 {
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
			if !m.help && !m.loading {
				return m, m.load()
			}
		}
	}
	return m, nil
}

func (m *browser) View() tea.View {
	var lines []string
	if m.width < 60 || m.height < 16 {
		lines = []string{"Resize terminal to at least 60 x 16", "q quit"}
	} else {
		lines = append(lines, paint("◆ Darkubectl / Apps", output.ColorAccent, true)+"  │  Tenant: "+safeText(m.tenant)+"  │  Read only", "")
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
				lines = append(lines, "Refresh failed; displayed apps may be stale: "+safeText(m.err.Error()))
			}
			if !m.loading && m.err == nil && len(m.apps) == 0 {
				lines = append(lines, "No apps in this tenant.")
			}
			lines = append(lines, m.appTree(max(0, m.height-len(lines)-footerHeight-1))...)
		}
		lines = append(lines, "", "j/k move  Enter open/toggle  h/l collapse/expand  r refresh  ? help  q quit")
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, m.width), "")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
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
