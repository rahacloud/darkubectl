package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/agentsetup"
)

func newTestWizard(t *testing.T) *agentSetupModel {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	cwd, home := filepath.Join(base, "project"), filepath.Join(base, "home")
	for _, dir := range []string{filepath.Join(cwd, ".git"), home} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	m := &agentSetupModel{
		config:   agentsetup.Config{Scope: agentsetup.ScopeProject, MCP: true},
		selected: make(map[agentsetup.Client]bool),
		cwd:      cwd,
		home:     home,
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return m
}

// The wizard tests stay in project scope, where no environment variable moves a
// file, so they need no environment of their own.

// press sends keys to the wizard and reports whether the last one ended it.
func press(m *agentSetupModel, keys ...string) bool {
	quit := false
	for _, key := range keys {
		msg := tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
		switch key {
		case keyEnter:
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case keySpace:
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case keyDown:
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case keyEsc:
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		}
		_, cmd := m.Update(msg)
		quit = cmd != nil
	}
	return quit
}

func TestWizardBuildsAReadOnlyPlanByDefault(t *testing.T) {
	t.Parallel()

	m := newTestWizard(t)

	// Enter with no client selected stays put and says why.
	press(m, keyEnter)
	if m.stage != setupClientStage || m.warning == "" {
		t.Fatalf("stage %d, warning %q: want to stay on the client stage with a warning", m.stage, m.warning)
	}

	// Claude Code (second row), project scope, MCP only, default access.
	press(m, keyDown, keySpace, keyEnter, keyEnter, keyEnter)
	if m.stage != setupPermissionsStage {
		t.Fatalf("stage %d, want the MCP access stage", m.stage)
	}
	if quit := press(m, keyEnter); quit || m.stage != setupReviewStage {
		t.Fatalf("stage %d, quit %v: want the review stage", m.stage, quit)
	}
	if m.plan == nil || len(m.plan.Changes) != 1 || filepath.Base(m.plan.Changes[0].Path) != ".mcp.json" {
		t.Fatalf("unexpected plan: %+v", m.plan)
	}
	if after := m.plan.Changes[0].After; strings.Contains(after, "--allow") {
		t.Fatalf("the default plan grants more than reads:\n%s", after)
	}
	view := m.View().Content
	if !strings.Contains(view, "CREATE") || !strings.Contains(view, ".mcp.json") {
		t.Errorf("review does not show the file to create:\n%s", view)
	}

	if quit := press(m, keyEnter); !quit || !m.approved {
		t.Fatalf("quit %v, approved %v: Enter on review must approve", quit, m.approved)
	}
	if _, err := os.Stat(m.plan.Changes[0].Path); !os.IsNotExist(err) {
		t.Fatalf("the wizard wrote a file itself: %v", err)
	}
}

func TestWizardWritesImplySecretsAndBackDropsThePlan(t *testing.T) {
	t.Parallel()

	m := newTestWizard(t)
	press(m, keySpace, keyEnter, keyEnter, keyEnter) // codex, project, MCP
	press(m, keySpace)                               // allow writes
	if !m.config.AllowWrites {
		t.Fatal("space on the first row did not enable writes")
	}
	press(m, keyDown, keySpace) // secrets cannot be toggled while writes are on
	if m.config.AllowSecrets || m.warning == "" {
		t.Fatalf("secrets %v, warning %q: want a refusal", m.config.AllowSecrets, m.warning)
	}
	press(m, keyEnter)
	if m.plan == nil || !strings.Contains(m.plan.Changes[0].After, `"--allow-writes"`) {
		t.Fatalf("the plan lacks write access: %+v", m.plan)
	}

	// Esc leaves the review to edit the choices, discarding the reviewed plan.
	if quit := press(m, keyEsc); quit || m.stage != setupItemsStage || m.plan != nil {
		t.Fatalf("quit %v, stage %d, plan %v: want the items stage with no plan", quit, m.stage, m.plan)
	}
	// Turning MCP off also withdraws the access that only made sense with it.
	press(m, keySpace)
	if m.config.MCP || m.config.AllowWrites {
		t.Fatalf("config after turning MCP off: %+v", m.config)
	}
	if press(m, keyEnter); m.stage != setupItemsStage || m.warning == "" {
		t.Fatalf("stage %d: an empty selection was accepted", m.stage)
	}
	if quit := press(m, "q"); !quit || m.approved {
		t.Fatalf("quit %v, approved %v: q must cancel", quit, m.approved)
	}
}

func TestWizardReportsAnUnsupportedCombination(t *testing.T) {
	t.Parallel()

	m := newTestWizard(t)
	// Hermes (fourth row) keeps MCP servers in its global config only.
	press(m, keyDown, keyDown, keyDown, keySpace, keyEnter, keyEnter, keyEnter, keyEnter)
	if m.stage != setupClientStage || !strings.Contains(m.warning, "Hermes") {
		t.Fatalf("stage %d, warning %q: want to be sent back with the reason", m.stage, m.warning)
	}
}
