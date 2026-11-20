package browser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestAppTreeGroupsAndScrolls(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 16}
	applyAppsResult(m, []client.App{
		{ID: "c", Name: "api", Namespace: client.Namespace{ID: 2, Name: "prod"}},
		{ID: "b", Name: "worker", Namespace: client.Namespace{ID: 1, Name: "prod"}},
		{ID: "a", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"}},
	}, nil)
	got := ansi.Strip(strings.Join(m.appTree(6), "\n"))
	if !strings.Contains(got, "▾ prod (ID 1)  ·  2 apps\n  ├── api") || !strings.Contains(got, "▾ prod (ID 2)  ·  1 app\n  └── api") {
		t.Fatalf("unexpected tree: %s", got)
	}
	for i := range m.apps {
		if m.apps[i].ID == "b" {
			m.selected = i
			break
		}
	}
	got = ansi.Strip(strings.Join(m.appTree(2), "\n"))
	if !strings.Contains(got, "prod (ID 1)  ·  2 apps\n  └── worker") {
		t.Fatalf("selected app lost its namespace while scrolling: %s", got)
	}
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	got = ansi.Strip(strings.Join(m.appTree(6), "\n"))
	if !strings.Contains(got, "  ▸ prod (ID 1)") || strings.Contains(got, "worker") {
		t.Fatalf("namespace did not collapse: %s", got)
	}
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got = ansi.Strip(strings.Join(m.appTree(6), "\n"))
	if !strings.Contains(got, "  ▾ prod (ID 1)") || !strings.Contains(got, "└── worker") {
		t.Fatalf("namespace did not expand: %s", got)
	}
}

func TestAppTreeAlignsUnicodeNames(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24}
	applyAppsResult(m, []client.App{
		{ID: "a", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"}, State: client.State{Text: "healthy"}},
		{ID: "b", Name: "\u6570\u636e\u5e93", Namespace: client.Namespace{ID: 1, Name: "prod"}, State: client.State{Text: "healthy"}},
	}, nil)
	rows := m.appTree(5)
	first, second := ansi.Strip(rows[1]), ansi.Strip(rows[2])
	firstIndex, secondIndex := strings.Index(first, "●"), strings.Index(second, "●")
	if firstIndex < 0 || secondIndex < 0 {
		t.Fatal("state glyph missing")
	}
	if ansi.StringWidth(first[:firstIndex]) != ansi.StringWidth(second[:secondIndex]) {
		t.Fatalf("states not aligned: %q and %q", first, second)
	}
}

func TestAppTreeAndContextUseTheSameSelectedRowStyle(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24}
	applyAppsResult(m, []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
	}}, nil)

	tree := m.appTreeForWidth(4, 80)
	if len(tree) < 2 {
		t.Fatalf("expected namespace and selected app rows, got %v", tree)
	}
	wantTree := selectedRowStyle().Width(80).Render(ansi.Strip(tree[1]))
	if tree[1] != wantTree {
		t.Fatal("selected app row does not use the shared selected-row style")
	}

	contextRow := m.contextRowLine("selected tenant", 80, true)
	wantContext := selectedRowStyle().Width(80).Render(ansi.Strip(contextRow))
	if contextRow != wantContext {
		t.Fatal("selected context row does not use the shared selected-row style")
	}
}
