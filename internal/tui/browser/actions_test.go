package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
	"strings"
	"testing"
)

func TestActionMenuHintsMatchVimShortcuts(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24, mode: "MOCK PROFILE · OFFLINE", actionMenu: true}
	applyAppsResult(m, []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
	}}, nil)
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.actionCursor != 1 {
		t.Fatal("j should move to Delete in the action menu")
	}
	m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.actionCursor != 0 {
		t.Fatal("k should move to Restart in the action menu")
	}
	footer := ansi.Strip(m.footerLine())
	if footer != "q quit · ? help" {
		t.Fatalf("action footer = %q, want only quit and help", footer)
	}
	if !strings.Contains(ansi.Strip(m.actionMenuPanel()), "j/k choose") {
		t.Fatal("action menu modal should advertise j/k navigation")
	}
}

func TestActionModalsFitNarrowAndWidePanels(t *testing.T) {
	t.Parallel()
	for _, width := range []int{60, 72, 80, 120} {
		m := &browser{width: width, height: 24, mode: "MOCK PROFILE · OFFLINE", actionMenu: true}
		applyAppsResult(m, []client.App{{ID: "app-1", Name: "api"}}, nil)
		panelWidth := min(72, width-4)
		for _, line := range strings.Split(m.actionMenuPanel(), "\n") {
			if got := ansi.StringWidth(line); got > panelWidth {
				t.Errorf("action menu line width %d exceeds panel width %d at terminal %d", got, panelWidth, width)
			}
		}
		m.actionMenu = false
		m.confirmAction = appActionDelete
		content, _, _, _ := m.actionConfirmationContent()
		innerWidth := panelWidth - 6
		for _, line := range content {
			if got := ansi.StringWidth(line); got > innerWidth {
				t.Errorf("delete confirmation line width %d exceeds inner width %d at terminal %d", got, innerWidth, width)
			}
		}
	}
}
