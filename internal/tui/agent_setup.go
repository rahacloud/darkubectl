package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// The rows of the setup-items stage, in display order.
const (
	setupItemMCP = iota
	setupItemSkill
	setupItemInstructions
	setupItemCount
)

const (
	setupPermissionCount = 2
	// setupReviewChrome is the number of lines the review stage draws around
	// its scrolling diff.
	setupReviewChrome = 10
	// setupAccessStep is the index of the MCP access step in the progress bar.
	setupAccessStep = 3

	keyEsc   = "esc"
	keyEnter = "enter"
	keySpace = "space"
	keyUp    = "up"
	keyDown  = "down"
	keyRight = "right"
)

var errWizardEnded = errors.New("agent setup wizard ended unexpectedly")

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

var setupScopes = []agentsetup.Scope{agentsetup.ScopeProject, agentsetup.ScopeGlobal}

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
	setupBoldStyle     = lipgloss.NewStyle().Bold(true)
)

// RunAgentSetup runs the interactive wizard and returns the plan the user built
// and whether they approved it. Nothing is written here; the caller applies
// the plan.
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
		return nil, false, errWizardEnded
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
	switch key {
	case "ctrl+c", "q":
		m.done = true
		return true
	case keyEsc, "left", "backspace":
		return m.back()
	}
	switch m.stage {
	case setupClientStage:
		m.clientKey(key)
	case setupScopeStage:
		m.scopeKey(key)
	case setupItemsStage:
		m.itemsKey(key)
	case setupPermissionsStage:
		m.permissionsKey(key)
	case setupReviewStage:
		return m.reviewKey(msg)
	}
	return false
}

// back steps to the previous stage; on the first one it leaves the wizard.
func (m *agentSetupModel) back() bool {
	switch m.stage {
	case setupClientStage:
		m.done = true
		return true
	case setupReviewStage:
		m.plan = nil
		m.warning = ""
		m.stage = setupItemsStage
	default:
		m.stage--
	}
	m.cursor = 0
	if m.stage == setupScopeStage {
		m.cursor = scopeCursor(m.config.Scope)
	}
	return false
}

// move shifts the cursor by one within a list of count entries.
func (m *agentSetupModel) move(key string, count int) bool {
	switch key {
	case keyUp, "k":
		m.cursor = max(0, m.cursor-1)
	case keyDown, "j":
		m.cursor = min(count-1, m.cursor+1)
	default:
		return false
	}
	return true
}

func (m *agentSetupModel) clientKey(key string) {
	if m.move(key, len(setupClients)) {
		return
	}
	switch key {
	case keySpace:
		client := setupClients[m.cursor]
		m.selected[client] = !m.selected[client]
	case keyEnter, keyRight:
		if len(m.selectedClients()) == 0 {
			m.warning = "Select at least one client."
			return
		}
		m.config.Clients = m.selectedClients()
		m.stage = setupScopeStage
		m.cursor = scopeCursor(m.config.Scope)
		m.warning = ""
	}
}

func (m *agentSetupModel) scopeKey(key string) {
	if m.move(key, len(setupScopes)) || key == keySpace {
		m.config.Scope = setupScopes[m.cursor]
		return
	}
	if key == keyEnter || key == keyRight {
		m.config.Scope = setupScopes[m.cursor]
		m.stage = setupItemsStage
		m.cursor = 0
	}
}

func (m *agentSetupModel) itemsKey(key string) {
	if m.move(key, setupItemCount) {
		return
	}
	switch key {
	case keySpace:
		switch m.cursor {
		case setupItemMCP:
			m.config.MCP = !m.config.MCP
			if !m.config.MCP {
				m.config.AllowWrites = false
				m.config.AllowSecrets = false
			}
		case setupItemSkill:
			m.config.Skill = !m.config.Skill
		case setupItemInstructions:
			m.config.Instructions = !m.config.Instructions
		}
	case keyEnter, keyRight:
		if !m.config.MCP && !m.config.Skill && !m.config.Instructions {
			m.warning = "Select at least one setup item."
			return
		}
		m.warning = ""
		if m.config.MCP {
			m.stage = setupPermissionsStage
			m.cursor = 0
		} else {
			m.prepareReview()
		}
	}
}

func (m *agentSetupModel) permissionsKey(key string) {
	if m.move(key, setupPermissionCount) {
		return
	}
	switch key {
	case keySpace:
		switch {
		case m.cursor == 0:
			m.config.AllowWrites = !m.config.AllowWrites
			m.warning = ""
		case m.config.AllowWrites:
			m.warning = "Secret reads are required while write access is enabled. Turn off writes to change this option."
		default:
			m.config.AllowSecrets = !m.config.AllowSecrets
			m.warning = ""
		}
	case keyEnter, keyRight:
		m.prepareReview()
	}
}

func (m *agentSetupModel) reviewKey(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case keyUp, "k", keyDown, "j", "pgup", "pgdown":
		m.review, _ = m.review.Update(msg)
	case keyEnter:
		m.approved = true
		m.done = true
		return true
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
	m.review = viewport.New(viewport.WithWidth(max(1, m.width)), viewport.WithHeight(max(1, m.height-setupReviewChrome)))
	m.review.SetContent(m.reviewContent())
}

func (m *agentSetupModel) resizeReview() {
	if m.stage != setupReviewStage {
		return
	}
	m.review.SetWidth(max(1, m.width))
	m.review.SetHeight(max(1, m.height-setupReviewChrome))
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

func scopeCursor(scope agentsetup.Scope) int {
	return max(0, slices.Index(setupScopes, scope))
}

func (m *agentSetupModel) View() tea.View {
	var b strings.Builder
	b.WriteString(setupTitleStyle.Render("◆ darkubectl"))
	b.WriteString(" " + setupMutedStyle.Render("/ agent setup") + "\n")
	b.WriteString(setupMutedStyle.Render("Configure client access to Darkube tools.") + "\n\n")
	b.WriteString(m.progress() + "\n\n")
	switch m.stage {
	case setupClientStage:
		b.WriteString(m.clientView())
	case setupScopeStage:
		b.WriteString(m.scopeView())
	case setupItemsStage:
		b.WriteString(m.itemsView())
	case setupPermissionsStage:
		b.WriteString(m.permissionsView())
	case setupReviewStage:
		b.WriteString(setupHeading("Review changes", "Nothing is written until you press Enter."))
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

func setupHeading(title, hint string) string {
	return setupTitleStyle.Render(title) + "\n" + setupMutedStyle.Render(hint) + "\n\n"
}

// setupOption renders one line of a list: a cursor when it is the current
// line, then glyph when the option is on or an empty circle when it is off.
func setupOption(current, on bool, glyph, label string) string {
	cursor := " "
	if current {
		cursor = setupCurrentStyle.Render("›")
	}
	mark := setupMutedStyle.Render("○")
	switch {
	case on && current:
		mark = setupSelectedStyle.Render(glyph)
		label = setupCurrentStyle.Render(label)
	case on:
		mark = setupSelectedStyle.Render(glyph)
		label = setupSelectedStyle.Render(label)
	case current:
		label = setupBoldStyle.Render(label)
	}
	return cursor + " " + mark + "  " + label + "\n"
}

func (m *agentSetupModel) clientView() string {
	var b strings.Builder
	b.WriteString(setupHeading("Choose clients", "Select every client you want to configure."))
	for i, client := range setupClients {
		b.WriteString(setupOption(i == m.cursor, m.selected[client], "✓", clientLabel(client)))
	}
	return b.String()
}

func (m *agentSetupModel) scopeView() string {
	var b strings.Builder
	b.WriteString(setupHeading("Choose a scope", "The same scope applies to every selected client."))
	for i, scope := range setupScopes {
		b.WriteString(setupOption(i == m.cursor, m.config.Scope == scope, "●", string(scope)))
	}
	return b.String()
}

func (m *agentSetupModel) itemsView() string {
	var b strings.Builder
	b.WriteString(setupHeading("Choose setup items", "MCP is read-only by default; you can add a skill or instructions too."))
	items := [setupItemCount]struct {
		label string
		on    bool
	}{
		setupItemMCP:          {label: "Connect the darkubectl MCP server", on: m.config.MCP},
		setupItemSkill:        {label: "Install or update the darkubectl skill", on: m.config.Skill},
		setupItemInstructions: {label: "Add or update agent instructions", on: m.config.Instructions},
	}
	for i, item := range items {
		b.WriteString(setupOption(i == m.cursor, item.on, "✓", item.label))
	}
	return b.String()
}

func (m *agentSetupModel) permissionsView() string {
	var b strings.Builder
	b.WriteString(setupHeading("MCP access", "Secret reads and writes are separate opt-ins."))
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
	cursor := [setupPermissionCount]string{" ", " "}
	cursor[m.cursor] = setupCurrentStyle.Render("›")
	if m.cursor == 0 && !m.config.AllowWrites {
		writeLabel = setupBoldStyle.Render(writeLabel)
	}
	if m.cursor == 1 && !m.config.AllowWrites && !m.config.AllowSecrets {
		secretLabel = setupBoldStyle.Render(secretLabel)
	}
	fmt.Fprintf(&b, "%s %s  %s\n", cursor[0], writeMark, writeLabel)
	fmt.Fprintf(&b, "%s %s  %s\n", cursor[1], secretMark, secretLabel)
	if m.config.AllowWrites {
		b.WriteString("\n" + setupCautionStyle.Render("!"))
		b.WriteString(" " + setupMutedStyle.Render("Write access includes secret reads."))
	} else {
		b.WriteString("\n" + setupMutedStyle.Render("Read-only is the default."))
	}
	return b.String()
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
		if i == setupAccessStep && !m.config.MCP {
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
	fileLabel := "files"
	if len(m.plan.Changes) == 1 {
		fileLabel = "file"
	}
	lines := []string{
		setupMutedStyle.Render(fmt.Sprintf("%d %s will be created or updated", len(m.plan.Changes), fileLabel)),
		"",
	}
	for _, change := range m.plan.Changes {
		action := "UPDATE"
		actionStyle := setupCurrentStyle
		icon := "✎"
		if !change.Exists {
			action = "CREATE"
			actionStyle = setupSelectedStyle
			icon = "✚"
		}
		lines = append(lines, actionStyle.Render(icon+" "+action)+" "+setupFileStyle.Render(m.displayPath(change.Path)))
		for _, line := range change.DiffLines() {
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
