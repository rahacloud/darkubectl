package tui

import (
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestLoadDemoMockProfile(t *testing.T) {
	t.Parallel()
	profile, err := LoadMockProfile("../../demo/mock-profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Tenant != "demo-acme" || len(profile.Apps) != 3 {
		t.Fatalf("unexpected profile scope or app count: tenant=%q apps=%d", profile.Tenant, len(profile.Apps))
	}
	if profile.Apps[0].Namespace.Name != "production" || profile.Apps[0].State.Text != "healthy (2/2)" {
		t.Fatalf("unexpected app projection: %+v", profile.Apps[0])
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

func TestMockBrowserUsesOfflineProfileData(t *testing.T) {
	t.Parallel()
	profile := MockProfile{Tenant: "demo", Apps: []client.App{{
		ID: "app-1", Name: "api", Namespace: client.Namespace{ID: 1, Name: "production"}, State: client.State{Text: "healthy (1/1)"}, Replicas: 1,
	}}}
	m := newMockBrowser(profile)
	cmd := m.Init()
	result := cmd().(appsResult)
	if result.err != nil || len(result.apps) != 1 {
		t.Fatalf("fixture load returned apps=%d err=%v", len(result.apps), result.err)
	}
	m.width, m.height = 100, 24
	m.Update(result)
	view := m.View().Content
	if !strings.Contains(view, "MOCK PROFILE") || !strings.Contains(view, "api") {
		t.Fatalf("mock view did not identify fixture data: %s", view)
	}
}
