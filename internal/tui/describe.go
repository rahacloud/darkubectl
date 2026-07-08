// Package tui provides the interactive Bubble Tea viewers for darkubectl.
package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rahacloud/darkubectl/internal/output"
)

const (
	headerHeight = 2 // title line + blank line
	footerHeight = 1 // hints or search input

	scrollContextDivisor = 3   // place a jumped-to match ~1/3 down the viewport
	percentScale         = 100 // ScrollPercent (0..1) to a whole percentage
)

// model is the Bubble Tea state for the interactive `describe` viewer.
type model struct {
	title       string
	rows        []output.Row
	styledLines []string // colorized, one per row
	plainLines  []string // lowercased plain text, for search
	styles      output.DescribeStyles

	vp        viewport.Model
	search    textinput.Model
	searching bool
	query     string
	matches   []int
	matchPos  int
	ready     bool

	titleStyle   lipgloss.Style
	hintStyle    lipgloss.Style
	matchStyle   lipgloss.Style
	currentStyle lipgloss.Style
}

// RunDescribe launches the interactive viewer for a described object.
func RunDescribe(title string, rows []output.Row) error {
	styles := output.NewStyles()

	styled := make([]string, len(rows))
	plain := make([]string, len(rows))
	for i, row := range rows {
		styled[i] = output.RenderRow(styles, row)
		plain[i] = strings.ToLower(output.PlainRow(row))
	}

	search := textinput.New()
	search.Placeholder = "search"
	search.Prompt = "/"

	m := &model{
		title:        title,
		rows:         rows,
		styledLines:  styled,
		plainLines:   plain,
		styles:       styles,
		search:       search,
		titleStyle:   lipgloss.NewStyle().Bold(true).Foreground(output.ColorAccent),
		hintStyle:    lipgloss.NewStyle().Foreground(output.ColorMuted).Faint(true),
		matchStyle:   lipgloss.NewStyle().Background(output.ColorMatchBG).Foreground(output.ColorMatchFG),
		currentStyle: lipgloss.NewStyle().Background(output.ColorCurrentBG).Foreground(output.ColorCurrentFG),
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	return nil
}

func (m *model) Init() tea.Cmd { return nil }

// Update routes messages to the resize/search/normal handlers.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg)
		return m, nil
	case tea.KeyPressMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		if cmd, handled := m.handleKey(msg); handled {
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *model) resize(msg tea.WindowSizeMsg) {
	if !m.ready {
		m.vp = viewport.New(viewport.WithWidth(msg.Width), viewport.WithHeight(msg.Height-headerHeight-footerHeight))
		m.ready = true
	} else {
		m.vp.SetWidth(msg.Width)
		m.vp.SetHeight(msg.Height - headerHeight - footerHeight)
	}
	m.vp.SetContent(m.content())
}

// handleKey processes navigation keys; the bool reports whether it consumed the key.
func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return tea.Quit, true
	case "/":
		m.searching = true
		m.search.SetValue("")
		return m.search.Focus(), true
	case "n":
		m.jump(1)
		return nil, true
	case "N":
		m.jump(-1)
		return nil, true
	case "g":
		m.vp.GotoTop()
		return nil, true
	case "G":
		m.vp.GotoBottom()
		return nil, true
	default:
		return nil, false
	}
}

func (m *model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.query = ""
		m.matches = nil
		m.search.Blur()
		m.vp.SetContent(m.content())
		return m, nil
	case "enter":
		m.searching = false
		m.search.Blur()
		if len(m.matches) > 0 {
			m.scrollTo(m.matches[m.matchPos])
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		m.query = strings.ToLower(m.search.Value())
		m.recomputeMatches()
		m.vp.SetContent(m.content())
		return m, cmd
	}
}

func (m *model) recomputeMatches() {
	m.matches = m.matches[:0]
	if m.query == "" {
		return
	}
	for i, line := range m.plainLines {
		if strings.Contains(line, m.query) {
			m.matches = append(m.matches, i)
		}
	}
	m.matchPos = 0
}

func (m *model) jump(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.matchPos = (m.matchPos + delta + len(m.matches)) % len(m.matches)
	m.scrollTo(m.matches[m.matchPos])
	m.vp.SetContent(m.content())
}

func (m *model) scrollTo(line int) {
	// Position the target roughly a third down from the top for context.
	m.vp.SetYOffset(max(line-m.vp.Height()/scrollContextDivisor, 0))
}

func (m *model) content() string {
	if m.query == "" {
		return strings.Join(m.styledLines, "\n")
	}
	lines := make([]string, len(m.styledLines))
	for i := range m.styledLines {
		switch {
		case !strings.Contains(m.plainLines[i], m.query):
			lines[i] = m.styledLines[i]
		case len(m.matches) > 0 && m.matches[m.matchPos] == i:
			lines[i] = m.currentStyle.Render(output.PlainRow(m.rows[i]))
		default:
			lines[i] = m.matchStyle.Render(output.PlainRow(m.rows[i]))
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) View() tea.View {
	if !m.ready {
		return tea.NewView("\n  loading…")
	}
	v := tea.NewView(m.header() + "\n" + m.vp.View() + "\n" + m.footer())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *model) header() string {
	title := m.titleStyle.Render(m.title)
	if len(m.matches) > 0 {
		title += m.hintStyle.Render("  " + strconv.Itoa(m.matchPos+1) + "/" + strconv.Itoa(len(m.matches)) + " matches")
	} else if m.query != "" {
		title += m.hintStyle.Render("  no matches")
	}
	return title + "\n"
}

func (m *model) footer() string {
	if m.searching {
		return m.search.View()
	}
	pct := strconv.Itoa(int(m.vp.ScrollPercent()*percentScale)) + "%"
	hints := "↑/↓ scroll · / search · n/N next/prev · g/G top/bottom · q quit"
	return m.hintStyle.Render(hints + "  " + pct)
}
