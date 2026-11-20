package browser

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestAppListDisplaysLoadedAppsAndFilters(t *testing.T) {
	t.Parallel()
	m := &browser{width: 100, height: 24}
	applyAppsResult(m, []client.App{
		{ID: "api", Name: "api", Namespace: client.Namespace{ID: 1, Name: "production"}},
		{ID: "worker", Name: "worker", Namespace: client.Namespace{ID: 2, Name: "staging"}},
	}, nil)
	for _, tc := range []struct {
		query string
		want  []string
		hide  []string
	}{
		{want: []string{"api", "worker"}},
		{query: "wrk", want: []string{"worker"}, hide: []string{"api"}},
		{query: "prod/api", want: []string{"api"}, hide: []string{"worker"}},
	} {
		m.filterQuery = tc.query
		view := ansi.Strip(strings.Join(m.appPaneLines(16), "\n"))
		for _, name := range tc.want {
			if !strings.Contains(view, name) {
				t.Errorf("filter %q: missing %q in %q", tc.query, name, view)
			}
		}
		for _, name := range tc.hide {
			if strings.Contains(view, name) {
				t.Errorf("filter %q: unexpected %q in %q", tc.query, name, view)
			}
		}
	}
}
