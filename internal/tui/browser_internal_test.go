package tui

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func applyAppsResult(m *browser, apps []client.App, err error) {
	meta := requestMeta{tenant: m.tenant, scopeGeneration: m.tenantGeneration, requestID: m.nextRequestID + 1}
	m.nextRequestID = meta.requestID
	m.appsState.begin(meta, m.requestTime())
	m.Update(appsResult{meta: meta, apps: apps, err: err})
}

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

func TestTenantPaneSwitchesOnCursorMovementWithoutEnter(t *testing.T) {
	t.Parallel()
	profile := MockProfile{Tenant: "alpha", Tenants: []MockTenant{
		{Name: "alpha", Apps: []client.App{{ID: "a", Name: "api"}}},
		{Name: "beta", Apps: []client.App{{ID: "b", Name: "worker"}}},
	}}
	m := newMockBrowser(profile)
	if cmd := m.Init(); cmd != nil {
		m.Update(cmd().(appsResult))
	}
	m.focusPane(0)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.tenant != "beta" || m.tenantCursor != 1 || !m.contextFocused || !m.selectingTenant || cmd == nil {
		t.Fatalf("moving to next tenant should switch immediately and retain focus: tenant=%q cursor=%d focus=%v selecting=%v cmd=%v", m.tenant, m.tenantCursor, m.contextFocused, m.selectingTenant, cmd != nil)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.tenant != "alpha" || m.tenantCursor != 0 || cmd == nil {
		t.Fatalf("moving back should immediately switch tenant: tenant=%q cursor=%d cmd=%v", m.tenant, m.tenantCursor, cmd != nil)
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tenant != "alpha" {
		t.Fatalf("Enter should not be needed to apply the selected tenant; tenant=%q", m.tenant)
	}
}

func TestTenantShortcutRemovedFromDispatchAndHelp(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 30}
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.selectingTenant || m.contextFocused {
		t.Fatal("t should not open or focus tenant selection")
	}
	for _, hints := range [][]keyHint{m.keyHints(false), m.keyHints(true)} {
		for _, hint := range hints {
			if hint.key == "t" || strings.Contains(hint.action, "tenant selector") {
				t.Fatalf("obsolete tenant shortcut appears in hints: %+v", hint)
			}
		}
	}
	foundImmediateTenantHint := false
	for _, hint := range m.keyHints(true) {
		if hint.key == "1" && strings.Contains(hint.action, "switch now") {
			foundImmediateTenantHint = true
		}
	}
	if !foundImmediateTenantHint {
		t.Fatal("help should explain immediate tenant switching")
	}
}

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

func TestPageKeysMoveThroughTheAppsTree(t *testing.T) {
	t.Parallel()
	apps := make([]client.App, 24)
	for i := range apps {
		apps[i] = client.App{
			ID: fmt.Sprintf("app-%02d", i), Name: fmt.Sprintf("app-%02d", i),
			Namespace: client.Namespace{ID: 1, Name: "prod"},
		}
	}
	m := &browser{width: 80, height: 20}
	applyAppsResult(m, apps, nil)

	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.selected == 0 {
		t.Fatal("PageDown should advance the selected app")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.selected != 0 {
		t.Fatalf("PageUp should return to the first app, selected index is %d", m.selected)
	}
}

func TestFooterShowsOnlyQuitAndHelpWhileHelpModalListsShortcuts(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 24}
	applyAppsResult(m, []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
	}}, nil)

	footer := ansi.Strip(m.footerLine())
	if footer != "q quit · ? help" {
		t.Fatalf("footer = %q, want only quit and help", footer)
	}
	m.help = true
	background := make([]string, m.height)
	help := ansi.Strip(strings.Join(m.helpModal(background), "\n"))
	for _, hint := range m.keyHints(true) {
		if !strings.Contains(help, hint.key) || !strings.Contains(help, hint.action) {
			t.Errorf("help is missing hint %q %q", hint.key, hint.action)
		}
	}
	for _, key := range []string{"q", "?"} {
		if !strings.Contains(help, key) {
			t.Errorf("help is missing global footer key %q", key)
		}
	}
	for _, unavailable := range []string{"Pods", "Terminal", "Domains"} {
		if strings.Contains(help, unavailable) {
			t.Errorf("help advertises unimplemented app view %q", unavailable)
		}
	}
}

func TestSummaryDistinguishesUnknownReplicasFromExplicitZero(t *testing.T) {
	t.Parallel()
	app := client.App{ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"}}
	m := &browser{tenant: "demo", detail: true, width: 120, height: 30, apps: []client.App{app}}
	if !summaryHasValue(m, "Replicas", "-") {
		t.Fatalf("unknown replicas should render as '-': %s", ansi.Strip(strings.Join(m.summaryPaneLines(), "\n")))
	}
	m.apps[0].ReplicasPresent = true
	m.apps[0].Replicas = 0
	if !summaryHasValue(m, "Replicas", "0") {
		t.Fatalf("explicit zero replicas should render as 0: %s", ansi.Strip(strings.Join(m.summaryPaneLines(), "\n")))
	}
}

func summaryHasValue(m *browser, label, value string) bool {
	prefix := ansi.Strip(summaryLabel(label))
	for _, line := range m.summaryPaneLines() {
		line = ansi.Strip(line)
		if strings.HasPrefix(line, prefix) && strings.TrimSpace(strings.TrimPrefix(line, prefix)) == value {
			return true
		}
	}
	return false
}

func TestSummaryUsesConfiguredResourceTotalsWithoutFakeUsage(t *testing.T) {
	t.Parallel()
	app := client.App{
		ID: "app-1", Name: "api", CPURequest: "500m", RAMLimit: "1000M",
		Namespace: client.Namespace{ID: 1, Name: "prod"},
		Plan:      &client.Plan{ID: "plan-1", Name: "Standard", Detail: client.PlanDetail{CPURequest: 500, RAMLimit: 1000}},
	}
	m := &browser{tenant: "demo", detail: true, width: 120, height: 30, apps: []client.App{app}}
	got := ansi.Strip(strings.Join(m.summaryPaneLines(), "\n"))
	if !strings.Contains(got, "CPU 500m") || !strings.Contains(got, "RAM 1000M") || strings.Contains(got, "0m / 500m") {
		t.Fatalf("summary fabricated usage or lost configured totals: %s", got)
	}
	used := 0.125
	m.detailUsage = &AppUsage{CPUCores: &used}
	m.usageAppID = app.ID
	got = ansi.Strip(strings.Join(m.summaryPaneLines(), "\n"))
	if !strings.Contains(got, "125m / 500m") || !strings.Contains(got, "RAM 1000M") {
		t.Fatalf("partial metrics should affect only sampled resources: %s", got)
	}
}

func TestSummaryRejectsDetailForDifferentIDWithSameName(t *testing.T) {
	t.Parallel()
	selected := client.App{ID: "app-a", Name: "api"}
	m := &browser{tenant: "demo", detail: true, apps: []client.App{selected}, detailApp: &selected}
	meta := requestMeta{tenant: "demo", appID: "app-a", scopeGeneration: m.selectionGeneration, requestID: 1}
	m.detailState.begin(meta, time.Now())
	m.Update(appDetailResult{meta: meta, app: &client.App{ID: "app-b", Name: "api"}})
	if m.detailState.status != requestFailed || m.detailApp == nil || m.detailApp.ID != "app-a" {
		t.Fatalf("mismatched detail replaced the selected app: state=%+v detail=%+v", m.detailState, m.detailApp)
	}
}

func TestAppDomainsCombinesAPIHostSourcesAndDeduplicates(t *testing.T) {
	t.Parallel()
	app := client.App{
		CustomDomainAddress: "API.Example.Test",
		ExternalHosts:       []string{"api.example.test", "public.example.test"},
		Service: &client.AppService{
			InternalAddress: "api.prod.svc",
			ExternalAddress: "app-123.hsvc.example.test",
			ExternalIP:      "192.0.2.10",
		},
	}
	got := appDomains(app)
	want := []string{"API.Example.Test", "public.example.test", "app-123.hsvc.example.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appDomains() = %v, want %v", got, want)
	}
}

func TestSummaryShowsOptionalServiceAndRuntimeFields(t *testing.T) {
	t.Parallel()
	app := client.App{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
		Service: &client.AppService{
			Type: "LoadBalancer", InternalAddress: "api.prod.svc", ExternalAddress: "app.hsvc.example.test", ExternalIP: "192.0.2.42",
			Ports: map[string]client.Port{
				"main":    {ContainerPort: 8080, ServicePort: 80, NodePort: 30421, Protocol: "TCP"},
				"metrics": {ContainerPort: 9090, ServicePort: 9091, Protocol: "TCP"},
			},
		},
		ReadinessProbePath: "/healthz", Command: "/app/server", Args: "--port 8080", Inputs: []string{"PUBLIC_BASE_URL"},
	}
	m := &browser{tenant: "demo", detail: true, width: 160, height: 40, apps: []client.App{app}}
	got := ansi.Strip(strings.Join(m.summaryPaneLines(), "\n"))
	for _, want := range []string{
		"Service", "LoadBalancer", "Port Service", "Internal Address", "api.prod.svc:80", "External IP", "192.0.2.42", "Readiness Probe", "HTTP GET /healthz",
		"Command", "/app/server --port 8080", "Inputs", "PUBLIC_BASE_URL",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %s", want, got)
		}
	}
	if !summaryHasValue(m, "Port Service", "80") || strings.Contains(got, "9090") || strings.Contains(got, "30421") {
		t.Fatalf("summary should show only the primary service port: %s", got)
	}
	if got, want := ansi.StringWidth(ansi.Strip(summaryLabel("Tenant"))), ansi.StringWidth(ansi.Strip(summaryLabel("Readiness Probe"))); got != want {
		t.Fatalf("summary label columns are not aligned: Tenant=%d Readiness Probe=%d", got, want)
	}
}

func TestSummaryShowsMissingExternalIPAsDash(t *testing.T) {
	t.Parallel()
	app := client.App{
		ID: "internal-only", Name: "worker", Namespace: client.Namespace{ID: 1, Name: "prod"},
		Service: &client.AppService{Type: "ClusterIP", InternalAddress: "worker.prod.svc", Ports: map[string]client.Port{"main": {ServicePort: 8080}}},
	}
	m := &browser{tenant: "demo", detail: true, width: 120, height: 30, apps: []client.App{app}}
	if !summaryHasValue(m, "External IP", "-") {
		t.Fatalf("service without external IP should show '-': %s", ansi.Strip(strings.Join(m.summaryPaneLines(), "\n")))
	}
	m.apps[0].Service = nil
	if !summaryHasValue(m, "External IP", "-") {
		t.Fatalf("app without a service object should show '-': %s", ansi.Strip(strings.Join(m.summaryPaneLines(), "\n")))
	}
}

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

func TestNamespaceSelectionDoesNotOfferAppActions(t *testing.T) {
	t.Parallel()
	m := &browser{
		width: 80, height: 24, namespaceSelected: true,
		apps: []client.App{{ID: "app-1", Name: "api"}},
	}
	for _, hint := range m.keyHints(true) {
		if hint.key == "a" {
			t.Fatal("app actions should not be advertised while a namespace row is selected")
		}
	}
	m.openActionMenu()
	if m.actionMenu {
		t.Fatal("app actions should not open while a namespace row is selected")
	}
}

func TestFooterFitsSupportedTerminalWidths(t *testing.T) {
	t.Parallel()
	for _, width := range []int{60, 72, 80, 99, 100, 120} {
		states := []struct {
			name string
			set  func(*browser)
		}{
			{name: "default"},
			{name: "action", set: func(m *browser) { m.actionMenu = true }},
			{name: "restart", set: func(m *browser) { m.confirmAction = appActionRestart }},
			{name: "delete", set: func(m *browser) { m.confirmAction = appActionDelete }},
			{name: "filter", set: func(m *browser) { m.filterEditing = true }},
			{name: "sort", set: func(m *browser) { m.sorting = true }},
			{name: "namespace", set: func(m *browser) { m.selectingNamespace = true }},
			{name: "tenant", set: func(m *browser) { m.selectingTenant = true }},
			{name: "context", set: func(m *browser) { m.contextFocused = true }},
			{name: "help", set: func(m *browser) { m.help = true }},
		}
		for _, state := range states {
			m := &browser{width: width, height: 24}
			if state.set != nil {
				state.set(m)
			}
			if got := ansi.StringWidth(ansi.Strip(m.footerLine())); got > width {
				t.Errorf("%s footer width %d exceeds terminal width %d", state.name, got, width)
			}
		}
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

func TestResizeBelowMinimumPreservesCurrentOverlayState(t *testing.T) {
	t.Parallel()
	m := &browser{width: 100, height: 30, actionMenu: true, confirmAction: appActionDelete, confirmName: "api"}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if !m.actionMenu || m.confirmAction != appActionDelete || m.confirmName != "api" {
		t.Fatalf("resize discarded overlay state: menu=%v action=%v input=%q", m.actionMenu, m.confirmAction, m.confirmName)
	}
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "Resize terminal") {
		t.Fatalf("small terminal should show resize guidance, got %q", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !m.actionMenu || m.confirmAction != appActionDelete || m.confirmName != "api" {
		t.Fatal("resizing back should restore the same overlay state")
	}
}

func TestResizePreservesFilterAndSelection(t *testing.T) {
	t.Parallel()
	m := &browser{width: 100, height: 30, tenant: "acme", filterQuery: "prod/api", filterEditing: true, selected: 3}
	m.Update(tea.WindowSizeMsg{Width: 45, Height: 14})
	if m.tenant != "acme" || m.filterQuery != "prod/api" || !m.filterEditing || m.selected != 3 {
		t.Fatalf("resize changed browsing state: tenant=%q filter=%q editing=%v selected=%d", m.tenant, m.filterQuery, m.filterEditing, m.selected)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.filterQuery != "prod/api" || !m.filterEditing || m.selected != 3 {
		t.Fatal("resizing back should resume the same filter and selection")
	}
}

func TestPrintableFilterInputDoesNotTriggerGlobalKeys(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24, filterEditing: true, filterBeforeEdit: "a", filterQuery: "a"}
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !m.filterEditing || m.filterQuery != "aq" {
		t.Fatalf("q should remain literal filter input, editing=%v query=%q", m.filterEditing, m.filterQuery)
	}
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if !m.filterEditing || m.filterQuery != "aq1" {
		t.Fatalf("pane shortcuts should remain literal filter input, editing=%v query=%q", m.filterEditing, m.filterQuery)
	}
}

func TestSortOverlayConsumesKeysUntilClosed(t *testing.T) {
	t.Parallel()
	m := &browser{
		width: 80, height: 24, sorting: true, sortChoice: int(sortName),
		apps: []client.App{{ID: "a", Name: "api"}},
	}
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.sortChoice != int(sortNamespace) {
		t.Fatal("j should move through the sort fields")
	}
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.actionMenu || !m.sorting {
		t.Fatal("global action shortcuts should not escape the sort overlay")
	}
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !m.sorting {
		t.Fatal("q should not quit or dismiss the sort overlay")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sorting || m.sortBy != sortNamespace {
		t.Fatal("Enter should apply the selected sort field")
	}
}

func TestHelpOverlayConsumesNavigationUntilClosed(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24, help: true, contextFocused: true, contextRow: 0}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.help || !m.contextFocused || m.contextRow != 0 {
		t.Fatal("pane navigation should not change focus behind the help overlay")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.help {
		t.Fatal("Escape should close the help overlay")
	}
}

func TestSummaryAndUsageViewNavigation(t *testing.T) {
	t.Parallel()
	m := &browser{width: 80, height: 24}
	applyAppsResult(m, []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"},
	}}, nil)
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if !m.detail {
		t.Fatal("l should open the selected app summary")
	}
	if m.activeAppView != appViewSummary {
		t.Fatal("opening app details should start on Summary")
	}
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.activeAppView != appViewUsage || !m.detail {
		t.Fatal("] should switch to Usage without changing app focus")
	}
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.activeAppView != appViewSummary {
		t.Fatal("] should wrap to Summary")
	}
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.activeAppView != appViewUsage {
		t.Fatal("[ should wrap backward to Usage")
	}
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m.detail || m.activeAppView != appViewUsage {
		t.Fatal("h should return focus to Apps while preserving the selected app view")
	}
}
