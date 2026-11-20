package browser

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestActionMenuHintsMatchVimShortcuts(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24, mode: "MOCK PROFILE · OFFLINE", actionMenu: true}
	applyAppsResult(m, []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
	}}, nil)
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.actionCursor != 1 {
		t.Fatal("j should move from Terminal to Restart in the action menu")
	}
	m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.actionCursor != 0 {
		t.Fatal("k should move from Restart to Terminal in the action menu")
	}
	footer := ansi.Strip(m.footerLine())
	if !strings.Contains(footer, "Esc close") {
		t.Fatalf("action footer = %q, want a close hint", footer)
	}
	if !strings.Contains(ansi.Strip(m.actionMenuPanel()), "j/k choose") {
		t.Fatal("action menu modal should advertise j/k navigation")
	}
}

func TestLiveTerminalRequiresWriteOptIn(t *testing.T) {
	t.Parallel()
	calls := 0
	m := &browser{
		mode: connectionLabelLive,
		terminal: func(context.Context, string, string, string, string, string) tea.ExecCommand {
			calls++
			return &mockTerminalCommand{}
		},
	}
	if m.actionAvailable(appActionTerminal) {
		t.Fatal("terminal action is available without --allow-writes")
	}
	if items := m.actionMenuItems(); len(items) != 0 {
		t.Fatalf("read-only live action menu = %v, want no write actions", items)
	}
	if cmd := m.connectTerminal(); cmd != nil {
		t.Fatal("terminal command started without --allow-writes")
	}
	if calls != 0 {
		t.Fatalf("terminal callback called %d times without --allow-writes", calls)
	}
	if m.actionNotice != errWriteActionsAreDisabledRelaunchWithAllow.Error() {
		t.Fatalf("terminal notice = %q", m.actionNotice)
	}

	m.allowWrites = true
	if !m.actionAvailable(appActionTerminal) {
		t.Fatal("terminal action is unavailable with --allow-writes")
	}
}

func TestActionModalsFitNarrowAndWidePanels(t *testing.T) {
	t.Parallel()
	for _, width := range []int{60, 72, 80, 120} {
		m := &browser{width: width, height: 24, mode: "MOCK PROFILE · OFFLINE", actionMenu: true}
		applyAppsResult(m, []client.App{{ID: "app-1", Name: "api"}}, nil)
		panelWidth := min(72, width-4)
		for line := range strings.SplitSeq(m.actionMenuPanel(), "\n") {
			if got := ansi.StringWidth(line); got > panelWidth {
				t.Errorf("action menu line width %d exceeds panel width %d at terminal %d", got, panelWidth, width)
			}
		}
		m.actionMenu = false
		m.confirmAction = appActionDelete
		content := m.actionConfirmationContent().content
		innerWidth := panelWidth - 6
		for _, line := range content {
			if got := ansi.StringWidth(line); got > innerWidth {
				t.Errorf("delete confirmation line width %d exceeds inner width %d at terminal %d", got, innerWidth, width)
			}
		}
	}
}
