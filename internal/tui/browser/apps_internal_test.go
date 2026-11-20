package browser

import (
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestBrowserSelectionAndRefresh(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24}
	applyAppsResult(m, []client.App{{ID: "b", Name: "beta"}, {ID: "a", Name: "alpha"}}, nil)
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.apps[m.selected].ID != "b" {
		t.Fatal("j should select the second app")
	}
	applyAppsResult(m, []client.App{{ID: "b", Name: "aardvark"}, {ID: "a", Name: "alpha"}}, nil)
	if m.apps[m.selected].ID != "b" {
		t.Fatal("refresh should preserve the selected app ID")
	}
	applyAppsResult(m, nil, io.EOF)
	if !strings.Contains(strings.ToLower(m.View().Content), "stale") || len(m.apps) != 2 {
		t.Fatalf("failed refresh should keep and label the old rows; apps=%d view=%q", len(m.apps), m.View().Content)
	}
}
