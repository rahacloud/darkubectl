package browser

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

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

func TestSummaryUsesConfiguredResourceTotalsWithoutFakeUsage(t *testing.T) {
	t.Parallel()
	app := client.App{
		ID: "app-1", Name: "api", CPURequest: "500m", RAMLimit: "1000M",
		Namespace: client.Namespace{ID: 1, Name: "prod"},
		Plan:      &client.Plan{ID: "plan-1", Name: "Standard", Detail: client.PlanDetail{CPURequest: 500, RAMLimit: 1000}},
	}
	m := &browser{tenant: "demo", detail: true, width: 120, height: 30, apps: []client.App{app}}
	got := ansi.Strip(strings.Join(m.summaryPaneLines(), "\n"))
	if !strings.Contains(got, " CPU") || !strings.Contains(got, "500m") || !strings.Contains(got,
		" RAM") || !strings.Contains(got, "1000M") || strings.Contains(got, "0m / 500m") {
		t.Fatalf("summary fabricated usage or lost configured totals: %s", got)
	}
	if strings.Contains(got, "Plan") {
		t.Fatalf("resource group should not include the plan row: %s", got)
	}
	used := 0.125
	m.detailUsage = &AppUsage{CPUCores: &used}
	m.usageAppID = app.ID
	got = ansi.Strip(strings.Join(m.summaryPaneLines(), "\n"))
	if !strings.Contains(got, "500m") || strings.Contains(got, "125m") || !strings.Contains(got, "1000M") {
		t.Fatalf("Summary should show configured totals without current samples: %s", got)
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

func TestAppDomainsIncludesServicePortForHsvc(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		host  string
		ports map[string]client.Port
		want  string
	}{
		{"service port", "app.hsvc.ir", map[string]client.Port{"main": {ServicePort: 27017, NodePort: 30161}}, "app.hsvc.ir:27017"},
		{"existing port", "app.hsvc.ir:1234", map[string]client.Port{"main": {ServicePort: 27017}}, "app.hsvc.ir:1234"},
		{"missing port", "app.hsvc.ir", nil, "app.hsvc.ir"},
		{"ambiguous ports", "app.hsvc.ir", map[string]client.Port{"one": {ServicePort: 1}, "two": {ServicePort: 2}}, "app.hsvc.ir"},
		{"other domain", "app.example.com", map[string]client.Port{"main": {ServicePort: 27017}}, "app.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := client.App{Service: &client.AppService{ExternalAddress: tt.host, Ports: tt.ports}}
			if got := appDomains(app); !reflect.DeepEqual(got, []string{tt.want}) {
				t.Fatalf("appDomains() = %v, want %q", got, tt.want)
			}
		})
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
		"Service", "LoadBalancer", "Port Service", "Internal Address", "api.prod.svc:80", "External IP", "192.0.2.42",
		"Readiness Probe", "HTTP GET /healthz",
		"Command", "/app/server --port 8080", "Inputs", "PUBLIC_BASE_URL",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %s", want, got)
		}
	}
	if !summaryHasValue(m, "Port Service", "80") || strings.Contains(got, "9090") || strings.Contains(got, "30421") {
		t.Fatalf("summary should show only the primary service port: %s", got)
	}
	if got, want := ansi.StringWidth(ansi.Strip(summaryLabel("Tenant"))),
		ansi.StringWidth(ansi.Strip(summaryLabel("Readiness Probe"))); got != want {
		t.Fatalf("summary label columns are not aligned: Tenant=%d Readiness Probe=%d", got, want)
	}
}

func TestSummaryShowsMissingExternalIPAsDash(t *testing.T) {
	t.Parallel()
	app := client.App{
		ID: "internal-only", Name: "worker", Namespace: client.Namespace{ID: 1, Name: "prod"},
		Service: &client.AppService{Type: "ClusterIP", InternalAddress: "worker.prod.svc",
			Ports: map[string]client.Port{"main": {ServicePort: 8080}}},
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
