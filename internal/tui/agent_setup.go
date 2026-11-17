package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/rahacloud/darkubectl/internal/agentsetup"
	"github.com/rahacloud/darkubectl/internal/output"
	"golang.org/x/term"
)

const (
	setupClientStage = iota
	setupScopeStage
	setupItemsStage
	setupPermissionsStage
	setupReviewStage
)

type agentSetupModel struct {
	config   agentsetup.Config
	selected map[agentsetup.Client]bool
	stage    int
	cursor   int
	warning  string
	plan     *agentsetup.Plan
	cwd      string
	home     string
	review   viewport.Model
	width    int
	height   int
	done     bool
	approved bool
}

var setupClients = []agentsetup.Client{
	agentsetup.ClientCodex,
	agentsetup.ClientClaude,
	agentsetup.ClientOpenCode,
	agentsetup.ClientHermes,
}

var (
	setupTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(output.ColorAccent)
	setupCurrentStyle  = lipgloss.NewStyle().Bold(true).Foreground(output.ColorBranch)
	setupSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(output.ColorSuccess)
	setupMutedStyle    = lipgloss.NewStyle().Foreground(output.ColorMuted)
	setupDisabledStyle = lipgloss.NewStyle().Foreground(output.ColorMuted).Faint(true)
	setupAddedStyle    = lipgloss.NewStyle().Foreground(output.ColorSuccess)
	setupRemovedStyle  = lipgloss.NewStyle().Foreground(output.ColorDanger)
	setupCautionStyle  = lipgloss.NewStyle().Bold(true).Foreground(output.ColorNumber)
	setupWarningStyle  = lipgloss.NewStyle().Bold(true).Foreground(output.ColorDanger)
	setupKeyStyle      = lipgloss.NewStyle().Bold(true).Foreground(output.ColorBranch)
	setupFileStyle     = lipgloss.NewStyle().Bold(true)
)

func RunAgentSetup(cwd, home string) (*agentsetup.Plan, bool, error) {
	m := &agentSetupModel{
		config: agentsetup.Config{
			Scope: agentsetup.ScopeProject,
			MCP:   true,
		},
		selected: make(map[agentsetup.Client]bool),
		cwd:      cwd,
		home:     home,
	}
	final, err := tea.NewProgram(m, agentSetupProgramOptions()...).Run()
	if err != nil {
		return nil, false, err
	}
	result, ok := final.(*agentSetupModel)
	if !ok {
		return nil, false, errors.New("agent setup wizard ended unexpectedly")
	}
	return result.plan, result.approved, nil
}

func agentSetupProgramOptions() []tea.ProgramOption {
	noColor, _ := strconv.ParseBool(os.Getenv("NO_COLOR"))
	if noColor || os.Getenv("CLICOLOR") == "0" || !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil
	}

	profile := colorprofile.Detect(os.Stdout, os.Environ())
	if profile == colorprofile.NoTTY {
		if os.Getenv("TERM") == "dumb" && os.Getenv("CLICOLOR_FORCE") != "1" {
			return nil
		}
		profile = colorprofile.ANSI
		if strings.EqualFold(os.Getenv("COLORTERM"), "truecolor") {
			profile = colorprofile.TrueColor
		}
	}
	return []tea.ProgramOption{tea.WithColorProfile(profile)}
}

func (m *agentSetupModel) Init() tea.Cmd { return nil }

func (m *agentSetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.stage == setupReviewStage {
			m.resizeReview()
		}
	case tea.KeyPressMsg:
		if m.handleKey(msg) {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *agentSetupModel) handleKey(msg tea.KeyPressMsg) bool {
	key := msg.String()
	if key == "ctrl+c" || key == "q" {
		m.done = true
		return true
	}
	if key == "esc" || key == "left" || key == "backspace" {
		if m.stage == setupClientStage {
			m.done = true
			return true
		}
		if m.stage == setupReviewStage {
			m.plan = nil
			m.warning = ""
			m.stage = setupItemsStage
			m.cursor = 0
			return false
		}
		m.stage--
		m.cursor = 0
		if m.stage == setupScopeStage {
			m.cursor = scopeCursor(m.config.Scope)
		}
		return false
	}

	switch m.stage {
	case setupClientStage:
		switch key {
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(len(setupClients)-1, m.cursor+1)
		case "space":
			client := setupClients[m.cursor]
			m.selected[client] = !m.selected[client]
		case "enter", "right":
			if len(m.selectedClients()) == 0 {
				m.warning = "Select at least one client."
				return false
			}
			m.config.Clients = m.selectedClients()
			m.stage = setupScopeStage
			m.cursor = scopeCursor(m.config.Scope)
			m.warning = ""
		}
	case setupScopeStage:
		switch key {
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
			m.config.Scope = scopeAt(m.cursor)
		case "down", "j":
			m.cursor = min(1, m.cursor+1)
			m.config.Scope = scopeAt(m.cursor)
		case "space":
			m.config.Scope = scopeAt(m.cursor)
		case "enter", "right":
			m.config.Scope = scopeAt(m.cursor)
			m.stage = setupItemsStage
			m.cursor = 0
		}
	case setupItemsStage:
		items := []bool{m.config.MCP, m.config.Skill, m.config.Instructions}
		switch key {
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(len(items)-1, m.cursor+1)
		case "space":
			switch m.cursor {
			case 0:
				m.config.MCP = !m.config.MCP
				if !m.config.MCP {
					m.config.AllowWrites = false
					m.config.AllowSecrets = false
				}
			case 1:
				m.config.Skill = !m.config.Skill
			case 2:
				m.config.Instructions = !m.config.Instructions
			}
		case "enter", "right":
			if !m.config.MCP && !m.config.Skill && !m.config.Instructions {
				m.warning = "Select at least one setup item."
				return false
			}
			m.warning = ""
			if m.config.MCP {
				m.stage = setupPermissionsStage
				m.cursor = 0
			} else {
				m.prepareReview()
			}
		}
	case setupPermissionsStage:
		switch key {
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(1, m.cursor+1)
		case "space":
			if m.cursor == 0 {
				m.config.AllowWrites = !m.config.AllowWrites
				m.warning = ""
			} else if m.config.AllowWrites {
				m.warning = "Secret reads are required while write access is enabled. Turn off writes to change this option."
			} else {
				m.config.AllowSecrets = !m.config.AllowSecrets
				m.warning = ""
			}
		case "enter", "right":
			m.prepareReview()
		}
	case setupReviewStage:
		switch key {
		case "up", "k", "down", "j", "pgup", "pgdown":
			var vpCmd tea.Cmd
			m.review, vpCmd = m.review.Update(msg)
			_ = vpCmd
		case "enter":
			m.approved = true
			m.done = true
			return true
		}
	}
	return false
}

func (m *agentSetupModel) prepareReview() {
	cfg := m.config
	cfg.Clients = m.selectedClients()
	plan, err := agentsetup.Build(cfg, m.cwd, m.home)
	if err != nil {
		m.warning = err.Error()
		m.stage = setupClientStage
		m.cursor = 0
		return
	}
	m.plan = plan
	m.stage = setupReviewStage
	m.warning = ""
	m.review = viewport.New(viewport.WithWidth(max(1, m.width)), viewport.WithHeight(max(1, m.height-10)))
	m.review.SetContent(m.reviewContent())
}

func (m *agentSetupModel) resizeReview() {
	if m.stage != setupReviewStage {
		return
	}
	m.review.SetWidth(max(1, m.width))
	m.review.SetHeight(max(1, m.height-10))
	m.review.SetContent(m.reviewContent())
}

func (m *agentSetupModel) selectedClients() []agentsetup.Client {
	var clients []agentsetup.Client
	for _, client := range setupClients {
		if m.selected[client] {
			clients = append(clients, client)
		}
	}
	return clients
}

func scopeAt(cursor int) agentsetup.Scope {
	if cursor == 1 {
		return agentsetup.ScopeGlobal
	}
	return agentsetup.ScopeProject
}

func scopeCursor(scope agentsetup.Scope) int {
	if scope == agentsetup.ScopeGlobal {
		return 1
	}
	return 0
}

func (m *agentSetupModel) View() tea.View {
	var b strings.Builder
	b.WriteString(setupTitleStyle.Render("◆ darkubectl"))
	b.WriteString(" " + setupMutedStyle.Render("/ agent setup") + "\n")
	b.WriteString(setupMutedStyle.Render("Configure client access to Darkube tools.") + "\n\n")
	b.WriteString(m.progress() + "\n\n")
	switch m.stage {
	case setupClientStage:
		b.WriteString(setupTitleStyle.Render("Choose clients") + "\n")
		b.WriteString(setupMutedStyle.Render("Select every client you want to configure.") + "\n\n")
		for i, client := range setupClients {
			cursor := " "
			if i == m.cursor {
				cursor = setupCurrentStyle.Render("›")
			}
			mark := setupMutedStyle.Render("○")
			label := clientLabel(client)
			if m.selected[client] {
				mark = setupSelectedStyle.Render("✓")
				if i == m.cursor {
					label = setupCurrentStyle.Render(label)
				} else {
					label = setupSelectedStyle.Render(label)
				}
			} else if i == m.cursor {
				label = lipgloss.NewStyle().Bold(true).Render(label)
			}
			b.WriteString(fmt.Sprintf("%s %s  %s\n", cursor, mark, label))
		}
	case setupScopeStage:
		b.WriteString(setupTitleStyle.Render("Choose a scope") + "\n")
		b.WriteString(setupMutedStyle.Render("The same scope applies to every selected client.") + "\n\n")
		for i, scope := range []agentsetup.Scope{agentsetup.ScopeProject, agentsetup.ScopeGlobal} {
			cursor := " "
			if i == m.cursor {
				cursor = setupCurrentStyle.Render("›")
			}
			mark := setupMutedStyle.Render("○")
			label := string(scope)
			if m.config.Scope == scope {
				mark = setupSelectedStyle.Render("●")
				if i == m.cursor {
					label = setupCurrentStyle.Render(label)
				} else {
					label = setupSelectedStyle.Render(label)
				}
			} else if i == m.cursor {
				label = lipgloss.NewStyle().Bold(true).Render(label)
			}
			b.WriteString(fmt.Sprintf("%s %s  %s\n", cursor, mark, label))
		}
	case setupItemsStage:
		b.WriteString(setupTitleStyle.Render("Choose setup items") + "\n")
		b.WriteString(setupMutedStyle.Render("MCP is read-only by default; you can add a skill or instructions too.") + "\n\n")
		items := []struct {
			label string
			on    bool
		}{
			{label: "Connect the darkubectl MCP server", on: m.config.MCP},
			{label: "Install or update the darkubectl skill", on: m.config.Skill},
			{label: "Add or update agent instructions", on: m.config.Instructions},
		}
		for i, item := range items {
			cursor := " "
			if i == m.cursor {
				cursor = setupCurrentStyle.Render("›")
			}
			mark := setupMutedStyle.Render("○")
			label := item.label
			if item.on {
				mark = setupSelectedStyle.Render("✓")
				if i == m.cursor {
					label = setupCurrentStyle.Render(label)
				} else {
					label = setupSelectedStyle.Render(label)
				}
			} else if i == m.cursor {
				label = lipgloss.NewStyle().Bold(true).Render(label)
			}
			b.WriteString(fmt.Sprintf("%s %s  %s\n", cursor, mark, label))
		}
	case setupPermissionsStage:
		b.WriteString(setupTitleStyle.Render("MCP access") + "\n")
		b.WriteString(setupMutedStyle.Render("Secret reads and writes are separate opt-ins.") + "\n\n")
		writeMark := setupMutedStyle.Render("○")
		writeLabel := "Allow write commands"
		if m.config.AllowWrites {
			writeMark = setupWarningStyle.Render("✓")
			writeLabel = setupWarningStyle.Render(writeLabel)
		}
		secretMark := setupMutedStyle.Render("○")
		secretLabel := "Allow reading secrets"
		if m.config.AllowSecrets || m.config.AllowWrites {
			secretMark = setupCautionStyle.Render("✓")
		}
		if m.config.AllowWrites {
			secretLabel += setupDisabledStyle.Render("  (included with writes)")
		} else if m.config.AllowSecrets {
			secretLabel = setupCautionStyle.Render(secretLabel)
		}
		cursor := []string{" ", " "}
		cursor[m.cursor] = setupCurrentStyle.Render("›")
		if m.cursor == 0 && !m.config.AllowWrites {
			writeLabel = lipgloss.NewStyle().Bold(true).Render(writeLabel)
		}
		if m.cursor == 1 && !m.config.AllowWrites && !m.config.AllowSecrets {
			secretLabel = lipgloss.NewStyle().Bold(true).Render(secretLabel)
		}
		b.WriteString(fmt.Sprintf("%s %s  %s\n", cursor[0], writeMark, writeLabel))
		b.WriteString(fmt.Sprintf("%s %s  %s\n", cursor[1], secretMark, secretLabel))
		if m.config.AllowWrites {
			b.WriteString("\n" + setupCautionStyle.Render("!"))
			b.WriteString(" " + setupMutedStyle.Render("Write access includes secret reads."))
		} else {
			b.WriteString("\n" + setupMutedStyle.Render("Read-only is the default."))
		}
	case setupReviewStage:
		b.WriteString(setupTitleStyle.Render("Review changes") + "\n")
		b.WriteString(setupMutedStyle.Render("Nothing is written until you press Enter.") + "\n\n")
		b.WriteString(m.footer() + "\n\n")
		b.WriteString(m.review.View())
	}
	if m.warning != "" {
		b.WriteString("\n\n" + setupWarningStyle.Render("! "+m.warning))
	}
	if m.stage != setupReviewStage {
		b.WriteString("\n\n" + m.footer())
	}
	return tea.NewView(b.String())
}

func (m *agentSetupModel) progress() string {
	labels := []string{"Clients", "Scope", "Setup", "MCP access", "Review"}
	current := m.stage + 1
	steps := make([]string, len(labels))
	for i, label := range labels {
		marker := "○"
		style := setupMutedStyle
		switch {
		case i+1 < current:
			marker = "✓"
			style = setupSelectedStyle
		case i+1 == current:
			marker = "●"
			style = setupTitleStyle
		}
		if i == 3 && !m.config.MCP {
			marker = "–"
			style = setupDisabledStyle
			label = "MCP off"
		}
		steps[i] = style.Render(marker + " " + label)
	}
	return strings.Join(steps, setupMutedStyle.Render("   ━   "))
}

func (m *agentSetupModel) footer() string {
	var hints []string
	add := func(key, description string) {
		hints = append(hints, setupKeyStyle.Render(key)+" "+setupMutedStyle.Render(description))
	}
	switch m.stage {
	case setupClientStage:
		add("↑/↓", "move")
		add("SPACE", "toggle")
		add("ENTER", "continue")
		add("ESC", "quit")
	case setupScopeStage:
		add("↑/↓", "change")
		add("ENTER", "continue")
		add("ESC", "back")
	case setupItemsStage:
		add("↑/↓", "move")
		add("SPACE", "toggle")
		add("ENTER", "continue")
		add("ESC", "back")
	case setupPermissionsStage:
		add("↑/↓", "move")
		add("SPACE", "toggle")
		add("ENTER", "review")
		add("ESC", "back")
	case setupReviewStage:
		add("↑/↓", "scroll")
		add("ENTER", "accept and apply")
		add("ESC", "edit choices")
		add("Q", "cancel setup")
	}
	return strings.Join(hints, "   ")
}

func (m *agentSetupModel) reviewContent() string {
	if m.plan == nil || len(m.plan.Changes) == 0 {
		return setupSelectedStyle.Render("✓ No changes are needed.")
	}
	review := m.plan.ReviewLines()
	fileLabel := "files"
	if len(m.plan.Changes) == 1 {
		fileLabel = "file"
	}
	lines := []string{
		setupMutedStyle.Render(fmt.Sprintf("%d %s will be created or updated", len(m.plan.Changes), fileLabel)),
		"",
	}
	index := 0
	for _, change := range m.plan.Changes {
		index++
		action := "UPDATE"
		actionStyle := setupCurrentStyle
		icon := "✎"
		if !change.Exists {
			action = "CREATE"
			actionStyle = setupSelectedStyle
			icon = "✚"
		}
		lines = append(lines, actionStyle.Render(icon+" "+action)+" "+setupFileStyle.Render(m.displayPath(change.Path)))
		for index < len(review) &&
			!strings.HasPrefix(review[index], "create ") &&
			!strings.HasPrefix(review[index], "update ") {
			line := review[index]
			index++
			switch {
			case strings.HasPrefix(line, "+ "):
				lines = append(lines, setupAddedStyle.Render("  + "+strings.TrimPrefix(line, "+ ")))
			case strings.HasPrefix(line, "- "):
				lines = append(lines, setupRemovedStyle.Render("  - "+strings.TrimPrefix(line, "- ")))
			default:
				lines = append(lines, setupMutedStyle.Render("  "+line))
			}
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *agentSetupModel) displayPath(path string) string {
	if relative, ok := relativeTo(m.cwd, path); ok {
		if relative == "." {
			return "./"
		}
		return filepath.Join(".", relative)
	}
	if relative, ok := relativeTo(m.home, path); ok {
		if relative == "." {
			return "~"
		}
		return filepath.Join("~", relative)
	}
	return path
}

func relativeTo(base, path string) (string, bool) {
	if base == "" {
		return "", false
	}
	relative, err := filepath.Rel(base, path)
	if err != nil || filepath.IsAbs(relative) ||
		(relative != ".." && strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return "", false
	}
	return relative, true
}

func clientLabel(client agentsetup.Client) string {
	if client == agentsetup.ClientClaude {
		return "Claude Code"
	}
	return string(client)
}
