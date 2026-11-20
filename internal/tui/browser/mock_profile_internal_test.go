package browser

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestLoadDemoMockProfile(t *testing.T) {
	t.Parallel()
	profile, err := LoadMockProfile("../../../demo/mock-profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Tenant != "hamravesh" || len(profile.Apps) != 3 {
		t.Fatalf("unexpected profile scope or app count: tenant=%q apps=%d", profile.Tenant, len(profile.Apps))
	}
	assertDemoAppProjection(t, profile.Apps[0])
	assertDemoRegistryFixture(t, profile)
}

func assertDemoAppProjection(t *testing.T, app client.App) {
	t.Helper()
	if app.CustomSubdomainAddr != "api-prod" || app.Namespace.Cluster.AppsCustomBaseDomain != "demo-west.darkube.app" {
		t.Fatalf("platform subdomain fixture was not loaded: label=%q base=%q", app.CustomSubdomainAddr,
			app.Namespace.Cluster.AppsCustomBaseDomain)
	}
	if app.Namespace.Name != "production" || app.State.Text != "healthy (2/2)" || !app.ReplicasPresent || app.Service == nil ||
		app.Service.InternalAddress != "api.production.demo.svc" || app.Service.Ports["http"].NodePort != 30421 ||
		app.ReadinessProbePath != "/healthz" || app.Command != "/app/server" || app.Args != "--port 8080" ||
		!reflect.DeepEqual(app.Inputs, []string{"LOG_LEVEL", "PUBLIC_BASE_URL"}) {
		t.Fatalf("unexpected app projection: %+v", app)
	}
}

func assertDemoRegistryFixture(t *testing.T, profile MockProfile) {
	t.Helper()
	if len(profile.Registries) != 1 || len(profile.Registries[0].Images) == 0 {
		t.Fatalf("registry fixture missing: %+v", profile.Registries)
	}
	api := profile.Registries[0].Images[0]
	if len(api.Digests) < 2 || !reflect.DeepEqual(api.Digests[0].Tags, []string{"release-53", "latest"}) || len(api.Digests[2].Tags) != 0 {
		t.Fatalf("expected alias tags and untagged digest: %+v", api.Digests)
	}
}

func TestMockRegistryDeleteTagPreservesManifest(t *testing.T) {
	t.Parallel()
	profile, err := LoadMockProfile("../../../demo/mock-profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := profile.Registries[0].Registry
	image := profile.Registries[0].Images[0]
	m := newMockBrowser(profile)
	if err := m.deleteRegistryTags(nil, profile.Tenant, registry.ID, image.Repository.Name, image.Digests[0].Digest,
		[]string{"latest"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.listRegistryDigests(nil, profile.Tenant, registry.ID, image.Repository.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !reflect.DeepEqual(got[0].Tags, []string{"release-53"}) {
		t.Fatalf("tag deletion changed manifest unexpectedly: %+v", got)
	}
}

func TestParseMockProfileRejectsUnknownFieldsWithoutEchoingValues(t *testing.T) {
	t.Parallel()
	data := []byte("version: 1\ntenant: demo\napps: []\napi_key: should-not-be-printed\n")
	_, err := ParseMockProfile(data)
	if err == nil || strings.Contains(err.Error(), "should-not-be-printed") {
		t.Fatalf("expected safe unknown-field error, got %v", err)
	}
}

func TestParseMockProfileRejectsMultipleDocuments(t *testing.T) {
	t.Parallel()
	_, err := ParseMockProfile([]byte("version: 1\ntenant: demo\napps: []\n---\nversion: 1\ntenant: other\napps: []\n"))
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected multiple-document error, got %v", err)
	}
}

func TestMockReplicasMustBePresentAndNonNull(t *testing.T) {
	t.Parallel()
	base := "version: 1\ntenant: demo\napps:\n  - id: app-1\n    name: api\n    namespace:\n      id: 1\n      name: prod\n"
	tests := []struct {
		name      string
		replicas  string
		wantValue int
		wantKnown bool
		wantError bool
	}{
		{name: "omitted", wantError: true},
		{name: "null", replicas: "    replicas: null\n", wantError: true},
		{name: "zero", replicas: "    replicas: 0\n", wantValue: 0, wantKnown: true},
		{name: "positive", replicas: "    replicas: 2\n", wantValue: 2, wantKnown: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profile, err := ParseMockProfile([]byte(base + test.replicas))
			if (err != nil) != test.wantError {
				t.Fatalf("ParseMockProfile error = %v, wantError %t", err, test.wantError)
			}
			if err != nil {
				return
			}
			app := profile.Apps[0]
			if app.Replicas != test.wantValue || app.ReplicasPresent != test.wantKnown {
				t.Fatalf("replicas=(%d,present=%t), want (%d,present=%t)", app.Replicas, app.ReplicasPresent, test.wantValue, test.wantKnown)
			}
		})
	}
}

func TestMockBrowserUsesOfflineProfileData(t *testing.T) {
	t.Parallel()
	profile := MockProfile{Tenant: "demo", Apps: []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "production"},
		State: client.State{Text: "healthy (1/1)"}, Replicas: 1, ReplicasPresent: true,
	}}}
	m := newMockBrowser(profile)
	cmd := m.Init()
	var result appsResult
	for _, batched := range requireMessage[tea.BatchMsg](t, cmd()) {
		switch msg := batched().(type) {
		case appsResult:
			result = msg
			m.Update(msg)
		case egressRoutesResult:
			m.Update(msg)
		}
	}
	if result.err != nil || len(result.apps) != 1 {
		t.Fatalf("fixture load returned apps=%d err=%v", len(result.apps), result.err)
	}
	m.width, m.height = 100, 24
	view := m.View().Content
	if !strings.Contains(view, "MOCK PROFILE") || !strings.Contains(view, "api") {
		t.Fatalf("mock view did not identify fixture data: %s", view)
	}
}
