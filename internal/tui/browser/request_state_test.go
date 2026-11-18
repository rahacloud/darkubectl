package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestRequestStateTracksRefreshAndStaleData(t *testing.T) {
	t.Parallel()
	first := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	state := requestState{}
	meta := requestMeta{tenant: "acme", requestID: 1}
	state.begin(meta, first)
	if state.status != requestLoading || !state.matches(meta) {
		t.Fatalf("first request should be loading: %+v", state)
	}
	state.succeed(false, first.Add(time.Second))
	if state.status != requestReady || state.lastSuccessAt != first.Add(time.Second) {
		t.Fatalf("successful request should record ready snapshot: %+v", state)
	}
	meta.requestID++
	state.begin(meta, second)
	if state.status != requestRefreshing {
		t.Fatalf("refresh should preserve loaded status: %s", state.status)
	}
	wantErr := errors.New("temporary failure")
	state.fail(wantErr)
	if state.status != requestStale || state.err != wantErr || state.lastSuccessAt != first.Add(time.Second) {
		t.Fatalf("failed refresh should leave a stale snapshot: %+v", state)
	}
}

func TestOutdatedAppsResultCannotReplaceNewTenantScope(t *testing.T) {
	t.Parallel()
	m := &browser{tenant: "acme", listApps: func(ctx context.Context, tenant string) ([]client.App, error) {
		return []client.App{{ID: "new", Name: tenant}}, nil
	}}
	oldCmd := m.load()
	oldResult := oldCmd().(appsResult)
	m.tenant = "other"
	m.tenantGeneration++
	otherCmd := m.load()
	m.Update(otherCmd().(appsResult))
	m.tenant = "acme"
	m.tenantGeneration++
	newCmd := m.load()
	newResult := newCmd().(appsResult)
	m.Update(newResult)
	m.Update(oldResult)
	if len(m.apps) != 1 || m.apps[0].Name != "acme" {
		t.Fatalf("late result replaced current tenant apps: %+v", m.apps)
	}
}

func TestSupersededAppsRefreshCannotWinInSameScope(t *testing.T) {
	t.Parallel()
	calls := 0
	m := &browser{tenant: "acme", listApps: func(ctx context.Context, _ string) ([]client.App, error) {
		calls++
		name := "superseded"
		if calls == 2 {
			name = "latest"
		}
		return []client.App{{ID: "current", Name: name}}, nil
	}}
	oldCmd := m.load()
	oldResult := oldCmd().(appsResult)
	newCmd := m.load()
	newResult := newCmd().(appsResult)
	m.Update(newResult)
	m.Update(oldResult)
	if len(m.apps) != 1 || m.apps[0].Name != "latest" {
		t.Fatalf("superseded request replaced the latest snapshot: %+v", m.apps)
	}
}

func TestNamespaceInventoryUsesDistinctEmptyAndPartialStates(t *testing.T) {
	t.Parallel()
	m := &browser{tenant: "acme", listNamespaces: func(context.Context, string) ([]client.Namespace, error) {
		return nil, nil
	}}
	cmd := m.loadNamespaces()
	m.Update(cmd().(namespacesResult))
	if m.namespaceState.status != requestEmpty {
		t.Fatalf("empty inventory should be explicit: %s", m.namespaceState.status)
	}
	m.apps = []client.App{{ID: "a", Name: "api", Namespace: client.Namespace{ID: 1, Name: "prod"}}}
	m.namespaceState.reset()
	cmd = m.loadNamespaces()
	m.Update(cmd().(namespacesResult))
	if m.namespaceState.status != requestPartial || !m.namespaceFallback || len(m.namespaces) != 1 {
		t.Fatalf("empty inventory with apps should be a partial fallback: state=%s fallback=%t namespaces=%v", m.namespaceState.status, m.namespaceFallback, m.namespaces)
	}
}

func TestProfileRefreshFailureRetainsStaleState(t *testing.T) {
	t.Parallel()
	fail := false
	m := &browser{profile: func(context.Context) (*client.Profile, error) {
		if fail {
			return nil, errors.New("offline")
		}
		return nil, nil
	}}
	cmd := m.loadProfile()
	m.Update(cmd().(profileResult))
	if m.profileState.status != requestEmpty {
		t.Fatalf("empty profile should be explicit: %s", m.profileState.status)
	}
	fail = true
	cmd = m.loadProfile()
	if m.profileState.status != requestRefreshing {
		t.Fatalf("retry should show refreshing: %s", m.profileState.status)
	}
	m.Update(cmd().(profileResult))
	if m.profileState.status != requestStale || m.profileState.err == nil {
		t.Fatalf("failed retry should retain stale profile state: %+v", m.profileState)
	}
}

func TestUsageFailureDoesNotFailSummaryDetail(t *testing.T) {
	t.Parallel()
	m := &browser{tenant: "acme", apps: []client.App{{ID: "app-1", Name: "api"}}}
	detailMeta := requestMeta{tenant: "acme", appID: "app-1", scopeGeneration: m.selectionGeneration, requestID: 1}
	usageMeta := requestMeta{tenant: "acme", appID: "app-1", scopeGeneration: m.selectionGeneration, requestID: 2}
	m.detailState.begin(detailMeta, time.Now())
	m.usageState.begin(usageMeta, time.Now())
	m.Update(appUsageResult{meta: usageMeta, err: errors.New("metrics unavailable")})
	if m.usageState.status != requestFailed || m.detailState.status != requestLoading {
		t.Fatalf("usage failure must not fail detail: usage=%s detail=%s", m.usageState.status, m.detailState.status)
	}
	m.Update(appDetailResult{meta: detailMeta, app: &client.App{ID: "app-1", Name: "api"}})
	if m.detailState.status != requestReady {
		t.Fatalf("detail should succeed independently: %s", m.detailState.status)
	}
}

func TestSelectionChangeCancelsOldSummaryAndRejectsLateResult(t *testing.T) {
	t.Parallel()
	m := &browser{tenant: "acme", detail: true, apps: []client.App{
		{ID: "alpha", Name: "alpha"},
		{ID: "beta", Name: "beta"},
	}, getApp: func(context.Context, string, string) (*client.App, error) { return nil, nil }}
	meta, requestCtx := m.startRequest(requestSummary, "acme", "alpha", m.selectionGeneration)
	m.detailState.begin(meta, time.Now())
	if cmd := m.selectTree(2); cmd == nil {
		t.Fatal("changing the selected app should start the selected app summary request")
	}
	if requestCtx.Err() == nil {
		t.Fatal("changing apps should cancel the previous summary request")
	}
	m.Update(appDetailResult{meta: meta, app: &client.App{ID: "alpha", Name: "stale alpha"}})
	if m.detailApp == nil || m.detailApp.ID != "beta" {
		t.Fatalf("late summary replaced the selected app: %+v", m.detailApp)
	}
}
