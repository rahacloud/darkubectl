package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestSummaryShowsReadinessRestartsAndPreviousContainerState(t *testing.T) {
	t.Parallel()
	app := client.App{ID: "app-1", Name: "api"}
	m := &browser{
		apps:          []client.App{app},
		detail:        true,
		activeAppView: appViewSummary,
		podsState:     requestState{status: requestReady, lastSuccessAt: time.Now()},
		podList: []appstate.Pod{{
			Name: "api-abc", State: "CrashLoopBackOff", Ready: false,
			Containers: []appstate.Container{{Name: "api", State: "Waiting", RestartCount: 4, LastState: "Error"}},
		}},
	}
	got := ansi.Strip(strings.Join(m.summaryPaneLines(), "\n"))
	for _, want := range []string{"api-abc", "Ready 0/1", "CrashLoopBackOff", "Restarts 4", "previous Error"} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary missing pod detail %q: %s", want, got)
		}
	}
}
