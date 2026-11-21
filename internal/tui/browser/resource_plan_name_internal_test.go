package browser

import (
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestResourcePlanName(t *testing.T) {
	t.Parallel()
	m := browser{resourcePlans: []client.Plan{{ID: "one", Name: "Standard"}, {ID: "two", CodeName: "dynamic"}}}
	app := client.App{Plan: &client.Plan{ID: "one", Name: "Current"}}
	for _, tc := range []struct{ id, want string }{
		{"one", "Current"}, {"two", "dynamic"}, {"missing", "Unknown plan"},
	} {
		if got := m.resourcePlanName(tc.id, app); got != tc.want {
			t.Errorf("resourcePlanName(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
	app.Plan.Name = ""
	if got := m.resourcePlanName("one", app); got != "Standard" {
		t.Errorf("catalogue lookup = %q, want Standard", got)
	}
}

func TestResourceSettingsHidePlanIdentifier(t *testing.T) {
	t.Parallel()
	for _, writes := range []bool{false, true} {
		m := browser{allowWrites: writes, resourceDraft: ResourceConfig{Plan: "plan-uuid"}}
		lines := strings.Join(m.appendResourceSettings(nil, client.App{}, make([]bool, len(resourceFields))), "\n")
		if strings.Contains(lines, "plan-uuid") || strings.Contains(lines, "Unknown plan") {
			t.Fatalf("writes=%v: plan identifier shown in settings: %q", writes, lines)
		}
		if strings.Contains(lines, "Change plan") != writes {
			t.Fatalf("writes=%v: unexpected plan action: %q", writes, lines)
		}
	}
}

func TestResourcePlansIgnoreOtherTenant(t *testing.T) {
	t.Parallel()
	m := browser{tenant: "current", resourcePlans: []client.Plan{{ID: "current"}}}
	m.handleResourcePlans(resourcePlansResult{tenant: "old", plans: []client.Plan{{ID: "old", PlanType: "app"}}})
	if len(m.resourcePlans) != 1 || m.resourcePlans[0].ID != "current" {
		t.Fatalf("stale plans replaced current plans: %+v", m.resourcePlans)
	}
}
