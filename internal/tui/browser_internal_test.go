package tui

import (
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestBrowserSelectionAndRefresh(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24}
	m.Update(appsResult{apps: []client.App{{ID: "b", Name: "beta"}, {ID: "a", Name: "alpha"}}})
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.apps[m.selected].ID != "b" {
		t.Fatal("j should select the second app")
	}
	m.Update(appsResult{apps: []client.App{{ID: "b", Name: "aardvark"}, {ID: "a", Name: "alpha"}}})
	if m.apps[m.selected].ID != "b" {
		t.Fatal("refresh should preserve the selected app ID")
	}
	m.Update(appsResult{err: io.EOF})
	if !strings.Contains(m.View().Content, "stale") || len(m.apps) != 2 {
		t.Fatal("failed refresh should keep and label the old rows")
	}
}

func TestAppTreeGroupsAndScrolls(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 16}
	m.Update(appsResult{apps: []client.App{
		{ID: "c", Name: "api", Namespace: client.Namespace{ID: 2, Name: "prod"}},
		{ID: "b", Name: "worker", Namespace: client.Namespace{ID: 1, Name: "prod"}},
		{ID: "a", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"}},
	}})
	got := ansi.Strip(strings.Join(m.appTree(6), "\n"))
	if !strings.Contains(got, "▾ prod (ID 1)  ·  2 apps\n  ├── api") || !strings.Contains(got, "▾ prod (ID 2)  ·  1 app\n  └── api") {
		t.Fatalf("unexpected tree: %s", got)
	}
	m.selected = 1
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
	m.Update(appsResult{apps: []client.App{
		{ID: "a", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"}, State: client.State{Text: "healthy"}},
		{ID: "b", Name: "\u6570\u636e\u5e93", Namespace: client.Namespace{ID: 1, Name: "prod"}, State: client.State{Text: "healthy"}},
	}})
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
