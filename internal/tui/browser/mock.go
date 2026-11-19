package browser

import (
	"context"
	"fmt"
	"github.com/rahacloud/darkubectl/internal/client"
)

func newMockBrowser(profile MockProfile) *browser {
	tenantProfiles := append([]MockTenant(nil), profile.Tenants...)
	if len(tenantProfiles) == 0 {
		tenantProfiles = []MockTenant{{Name: profile.Tenant, Apps: profile.Apps, Namespaces: profile.Namespaces}}
	}
	appsByTenant := make(map[string][]client.App, len(tenantProfiles))
	namespacesByTenant := make(map[string][]client.Namespace, len(tenantProfiles))
	egressByTenant := make(map[string]map[string]client.ClusterEgress, len(tenantProfiles))
	options := make([]tenantOption, 0, len(tenantProfiles))
	for _, tenant := range tenantProfiles {
		appsByTenant[tenant.Name] = append([]client.App(nil), tenant.Apps...)
		namespacesByTenant[tenant.Name] = append([]client.Namespace(nil), tenant.Namespaces...)
		egressByTenant[tenant.Name] = make(map[string]client.ClusterEgress)
		for _, app := range tenant.Apps {
			cluster := app.Namespace.Cluster
			if cluster.ID == 0 && cluster.Name == "" {
				continue
			}
			key := fmt.Sprint(cluster.ID)
			if cluster.ID == 0 {
				key = cluster.Name
			}
			egressByTenant[tenant.Name][key] = client.ClusterEgress{Cluster: cluster, DefaultOutgoingIP: "198.51.100.42"}
		}
		options = append(options, tenantOption{name: tenant.Name, source: "Mock profile"})
	}
	if len(options) > 0 {
		options[0].status = "current"
	}
	deletedApps := make(map[string]map[string]bool)
	m := &browser{
		tenant:  profile.Tenant,
		mode:    "MOCK PROFILE · OFFLINE",
		logWrap: true,
		logTail: logDefaultTailSize,
		tenants: options,
		listApps: func(_ context.Context, tenant string) ([]client.App, error) {
			apps, ok := appsByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			remaining := make([]client.App, 0, len(apps))
			for _, app := range apps {
				if !deletedApps[tenant][app.ID] {
					remaining = append(remaining, app)
				}
			}
			return remaining, nil
		},
		getApp: func(_ context.Context, tenant, id string) (*client.App, error) {
			for _, app := range appsByTenant[tenant] {
				if app.ID == id && !deletedApps[tenant][id] {
					copy := app
					return &copy, nil
				}
			}
			return nil, fmt.Errorf("app %q is not present in mock tenant %q", id, tenant)
		},
		listNamespaces: func(_ context.Context, tenant string) ([]client.Namespace, error) {
			namespaces, ok := namespacesByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.Namespace(nil), namespaces...), nil
		},
		egressRoutes: func(_ context.Context, tenant string) (map[string]client.ClusterEgress, error) {
			routes, ok := egressByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return routes, nil
		},
		mockDeleteApp: func(tenant, id string) {
			if deletedApps[tenant] == nil {
				deletedApps[tenant] = make(map[string]bool)
			}
			deletedApps[tenant][id] = true
		},
	}
	if m.tenant == "" {
		m.contextFocused = true
	}
	return m
}
