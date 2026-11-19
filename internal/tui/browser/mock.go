package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
)

func newMockBrowser(profile MockProfile) *browser {
	tenantProfiles := append([]MockTenant(nil), profile.Tenants...)
	if len(tenantProfiles) == 0 {
		tenantProfiles = []MockTenant{{Name: profile.Tenant, Apps: profile.Apps, Alerts: profile.Alerts, Namespaces: profile.Namespaces, AppData: profile.AppData, Registries: profile.Registries, Databases: profile.Databases}}
	}
	appsByTenant := make(map[string][]client.App, len(tenantProfiles))
	alertsByTenant := make(map[string][]client.Alert, len(tenantProfiles))
	dataByTenant := make(map[string]map[string]MockAppData, len(tenantProfiles))
	namespacesByTenant := make(map[string][]client.Namespace, len(tenantProfiles))
	registriesByTenant := make(map[string][]client.Registry, len(tenantProfiles))
	databasesByTenant := make(map[string][]client.Database, len(tenantProfiles))
	registryReposByTenant := make(map[string]map[string][]client.Repository, len(tenantProfiles))
	registryDigestsByTenant := make(map[string]map[string][]client.Digest, len(tenantProfiles))
	egressByTenant := make(map[string]map[string]client.ClusterEgress, len(tenantProfiles))
	domainsByTenant := make(map[string]map[string]DomainConfig, len(tenantProfiles))
	portsByTenant := make(map[string]map[string]PortConfig, len(tenantProfiles))
	options := make([]tenantOption, 0, len(tenantProfiles))
	for _, tenant := range tenantProfiles {
		appsByTenant[tenant.Name] = append([]client.App(nil), tenant.Apps...)
		databasesByTenant[tenant.Name] = append([]client.Database(nil), tenant.Databases...)
		alertsByTenant[tenant.Name] = append([]client.Alert(nil), tenant.Alerts...)
		dataByTenant[tenant.Name] = make(map[string]MockAppData, len(tenant.AppData))
		for id, data := range tenant.AppData {
			dataByTenant[tenant.Name][id] = copyMockAppData(data)
		}
		namespacesByTenant[tenant.Name] = append([]client.Namespace(nil), tenant.Namespaces...)
		registryReposByTenant[tenant.Name] = map[string][]client.Repository{}
		registryDigestsByTenant[tenant.Name] = map[string][]client.Digest{}
		for _, registry := range tenant.Registries {
			registriesByTenant[tenant.Name] = append(registriesByTenant[tenant.Name], registry.Registry)
			for _, image := range registry.Images {
				registryReposByTenant[tenant.Name][string(registry.Registry.ID)] = append(registryReposByTenant[tenant.Name][string(registry.Registry.ID)], image.Repository)
				registryDigestsByTenant[tenant.Name][registryKey(registry.Registry.ID, image.Repository.Name)] = append([]client.Digest(nil), image.Digests...)
			}
		}
		egressByTenant[tenant.Name] = make(map[string]client.ClusterEgress)
		domainsByTenant[tenant.Name] = make(map[string]DomainConfig)
		portsByTenant[tenant.Name] = make(map[string]PortConfig)
		for _, app := range tenant.Apps {
			if app.Service != nil {
				portsByTenant[tenant.Name][app.ID] = PortConfig{Type: app.Service.Type, InternalAddress: app.Service.InternalAddress, ExternalAddress: app.Service.ExternalAddress, ExternalIP: app.Service.ExternalIP, Ports: app.Service.Ports}
			} else {
				portsByTenant[tenant.Name][app.ID] = PortConfig{Type: "ClusterIP", Ports: map[string]client.Port{}}
			}
			dns := map[string]bool{}
			certs := map[string]string{}
			for _, host := range app.ExternalHosts {
				dns[host] = true
				certs[host] = time.Now().AddDate(0, 3, 0).Format("2006-01-02")
			}
			records := map[string]string{"_acme-challenge.example.com": "demo-one.acme-dns.mock.example", "_acme-challenge.example.org": "demo-two.acme-dns.mock.example"}
			domainsByTenant[tenant.Name][app.ID] = DomainConfig{Hosts: append([]string(nil), app.ExternalHosts...), Subdomain: app.CustomSubdomainAddr, BaseDomain: app.Namespace.Cluster.AppsCustomBaseDomain, CNAME: "ingress.mock.darkube.app", Challenge: "dns01", SSL: app.EnableSSL, DNS: dns, Certificates: certs, ChallengeRecords: records}
			cluster := app.Namespace.Cluster
			if cluster.ID == 0 && cluster.Name == "" {
				continue
			}
			key := fmt.Sprint(cluster.ID)
			if cluster.ID == 0 {
				key = cluster.Name
			}
			egressByTenant[tenant.Name][key] = client.ClusterEgress{Cluster: client.EgressCluster{ID: cluster.ID, Name: cluster.Name, DefaultOutgoingIP: "198.51.100.42"}}
		}
		options = append(options, tenantOption{name: tenant.Name, source: "Mock profile"})
	}
	if len(options) > 0 {
		options[0].status = "current"
	}
	deletedApps := make(map[string]map[string]bool)
	mockBuilds := make(map[string][]client.Build)
	buildOwners := make(map[string]string)
	for tenant, apps := range appsByTenant {
		for _, app := range apps {
			if app.CreationMethod != client.CreationMethodGitRepoURL {
				continue
			}
			rows := []client.Build{
				{ID: client.BuildID(app.ID + "-mock-203"), Status: client.BuildSucceeded, GitBranch: "main", ShortCommitHash: "8d91fab", CommitMessage: "Update application configuration", CreationTime: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), EndTime: time.Now().Add(-time.Hour*2 + time.Minute*2).Format(time.RFC3339), Log: "STEP 1/4: checkout source\nSTEP 2/4: install dependencies\nSTEP 3/4: build image\nBuild completed successfully"},
				{ID: client.BuildID(app.ID + "-mock-202"), Status: client.BuildFailed, GitBranch: "main", ShortCommitHash: "020ab88", CommitMessage: "Fix deployment pipeline", CreationTime: time.Now().Add(-24 * time.Hour).Format(time.RFC3339), EndTime: time.Now().Add(-23 * time.Hour).Format(time.RFC3339), Errors: "Build command exited with status 1", Log: "STEP 1/4: checkout source\nSTEP 2/4: run tests\nERROR: tests failed"},
			}
			mockBuilds[tenant+"/"+app.ID] = rows
			for _, row := range rows {
				buildOwners[string(row.ID)] = tenant + "/" + app.ID
			}
		}
	}
	m := &browser{
		tenant: profile.Tenant,
		listDatabases: func(_ context.Context, tenant string) ([]client.Database, error) {
			rows, ok := databasesByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.Database(nil), rows...), nil
		},
		getDatabase: func(_ context.Context, tenant, id string) (*client.DatabaseDetail, error) {
			rows, ok := databasesByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			for _, db := range rows {
				if db.ID != id {
					continue
				}
				external := &client.DatabaseConnection{Username: "mock-user", Password: "mock-password-only", Database: "mock_" + db.Name, Host: "db-" + db.ID + ".mock.example.test", Port: 25432}
				private := &client.DatabaseConnection{Username: "mock-user", Password: "mock-password-only", Database: "mock_" + db.Name, Host: db.Name + ".svc.mock", Port: 5432}
				detail := &client.DatabaseDetail{
					Connection: external, PrivateConnection: private,
					Connections: client.DatabaseConnections{Master: client.DatabaseConnectionSet{External: external, Private: private}},
				}
				if db.NumNodes > 1 {
					standbyExternal := *external
					standbyExternal.Host += "-standby"
					standbyPrivate := *private
					standbyPrivate.Host += "-standby"
					detail.StandbyConnection = &standbyExternal
					detail.PrivateStandbyConnection = &standbyPrivate
					detail.Connections.Standby = client.DatabaseConnectionSet{External: &standbyExternal, Private: &standbyPrivate}
				}
				return detail, nil
			}
			return nil, fmt.Errorf("database %q is not present in mock tenant %q", id, tenant)
		},
		mode:                                "MOCK PROFILE · OFFLINE",
		allowWrites:                         true,
		logWrap:                             true,
		logTail:                             logDefaultTailSize,
		tenants:                             options,
		activitySortAlertsDescending:        true,
		activitySortNotificationsDescending: true,
		activityAlertStatusFilter:           1,
		alerts: func(_ context.Context, tenant string) ([]client.Alert, error) {
			if tenant == "" {
				return nil, fmt.Errorf("choose a tenant to view alerts")
			}
			return append([]client.Alert(nil), alertsByTenant[tenant]...), nil
		},
		notifications: func(context.Context) ([]client.Notification, error) {
			now := time.Now()
			return []client.Notification{
				{Slug: 1003, Title: "نگهداری سامانه", Description: "<p>امشب سامانه برای <strong>به‌روزرسانی</strong> در دسترس نخواهد بود.</p>", Timestamp: now.Add(-20 * time.Minute).Format(time.RFC3339), TargetType: "platform", Unread: true},
				{Slug: 1002, Title: "گزارش مصرف ماهانه", Description: "گزارش مصرف ماه گذشته آماده است.", Timestamp: now.Add(-3 * time.Hour).Format(time.RFC3339), TargetType: "billing"},
				{Slug: 1001, Title: "به‌روزرسانی فضای ذخیره‌سازی", Description: "فضای ذخیره‌سازی با موفقیت به‌روزرسانی شد.", Timestamp: now.Add(-24 * time.Hour).Format(time.RFC3339), TargetType: "storage", Unread: true},
			}, nil
		},
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
		updateResources: func(_ context.Context, tenant, id string, cfg ResourceConfig) error {
			for i := range appsByTenant[tenant] {
				app := &appsByTenant[tenant][i]
				if app.ID != id || deletedApps[tenant][id] {
					continue
				}
				plan := client.Plan{ID: cfg.Plan, Name: cfg.Plan, PlanType: "app"}
				if app.Plan != nil && app.Plan.ID == cfg.Plan {
					plan = *app.Plan
				}
				if cfg.Plan == "plan-standard-demo" {
					plan.Name, plan.CodeName, plan.CostType = "Standard", "standard", "fixed"
					plan.Detail = client.PlanDetail{CPURequest: 500, RAMLimit: 1024}
				}
				app.Plan = &plan
				app.CPURequest, app.RAMLimit = cfg.CPU, cfg.RAM
				app.Replicas, app.ReplicasPresent, app.IsHPAEnabled = cfg.Replicas, true, cfg.Autoscaling
				if app.Disk != nil {
					disk := *app.Disk
					disk.SizeInGi = cfg.DiskGi
					app.Disk = &disk
				}
				app.CustomConfig = &client.AppCustomConfig{HPA: &client.AppHPAConfig{Enabled: cfg.Autoscaling, MinReplicas: cfg.MinReplicas, MaxReplicas: cfg.MaxReplicas, TargetCPUUtilizationPercentage: cfg.CPUTarget}}
				return nil
			}
			return fmt.Errorf("app %q is not present in mock tenant %q", id, tenant)
		},
		listBuilds: func(_ context.Context, tenant, id string, limit int) ([]client.Build, error) {
			rows, ok := mockBuilds[tenant+"/"+id]
			if !ok {
				return nil, fmt.Errorf("no mock builds for app")
			}
			if len(rows) > limit {
				rows = rows[:limit]
			}
			return append([]client.Build(nil), rows...), nil
		},
		getBuild: func(_ context.Context, tenant string, id client.BuildID) (*client.Build, error) {
			owner, ok := buildOwners[string(id)]
			if !ok || !strings.HasPrefix(owner, tenant+"/") {
				return nil, fmt.Errorf("mock build not found")
			}
			for _, row := range mockBuilds[owner] {
				if row.ID == id {
					copy := row
					return &copy, nil
				}
			}
			return nil, fmt.Errorf("mock build not found")
		},
		buildLatest: func(_ context.Context, tenant, appID string) error {
			key := tenant + "/" + appID
			rows, ok := mockBuilds[key]
			if !ok {
				return fmt.Errorf("mock build app not found")
			}
			id := client.BuildID(fmt.Sprintf("%s-mock-%d", appID, time.Now().UnixNano()))
			rows = append([]client.Build{{ID: id, Status: client.BuildRunning, GitBranch: "main", ShortCommitHash: "a1b2c3d", CommitMessage: "Mock branch head build", CreationTime: time.Now().Format(time.RFC3339), Log: "Mock build submitted"}}, rows...)
			mockBuilds[key] = rows
			buildOwners[string(id)] = key
			return nil
		},
		retryBuild: func(_ context.Context, tenant string, id client.BuildID) (*client.Build, error) {
			key, ok := buildOwners[string(id)]
			if !ok || !strings.HasPrefix(key, tenant+"/") {
				return nil, fmt.Errorf("mock build not found")
			}
			rows := mockBuilds[key]
			var original *client.Build
			for i := range rows {
				if rows[i].ID == id {
					original = &rows[i]
					break
				}
			}
			if original == nil {
				return nil, fmt.Errorf("mock build not found")
			}
			copy := *original
			copy.ID = client.BuildID(fmt.Sprintf("%s-mock-%d", strings.Split(key, "/")[1], time.Now().UnixNano()))
			copy.Status = client.BuildRunning
			copy.CreationTime = time.Now().Format(time.RFC3339)
			copy.EndTime = ""
			copy.Log = "Mock retry submitted"
			mockBuilds[key] = append([]client.Build{copy}, rows...)
			buildOwners[string(copy.ID)] = key
			return &copy, nil
		},
		stopBuild: func(_ context.Context, tenant string, id client.BuildID) error {
			key, ok := buildOwners[string(id)]
			if !ok || !strings.HasPrefix(key, tenant+"/") {
				return fmt.Errorf("mock build not found")
			}
			rows := mockBuilds[key]
			for i := range rows {
				if rows[i].ID == id {
					if !rows[i].InProgress() {
						return fmt.Errorf("mock build is already finished")
					}
					rows[i].Status = client.BuildCanceled
					rows[i].EndTime = time.Now().Format(time.RFC3339)
					mockBuilds[key] = rows
					return nil
				}
			}
			return fmt.Errorf("mock build not found")
		},
		listNamespaces: func(_ context.Context, tenant string) ([]client.Namespace, error) {
			namespaces, ok := namespacesByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.Namespace(nil), namespaces...), nil
		},
		listRegistries: func(_ context.Context, tenant string) ([]client.Registry, error) {
			rows, ok := registriesByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.Registry(nil), rows...), nil
		},
		listRegistryRepositories: func(_ context.Context, tenant string, id client.FlexID) ([]client.Repository, error) {
			rows, ok := registryReposByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.Repository(nil), rows[string(id)]...), nil
		},
		listRegistryDigests: func(_ context.Context, tenant string, id client.FlexID, image string) ([]client.Digest, error) {
			rows, ok := registryDigestsByTenant[tenant]
			if !ok {
				return nil, fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			return append([]client.Digest(nil), rows[registryKey(id, image)]...), nil
		},
		deleteRegistryRepository: func(_ context.Context, tenant string, id client.FlexID, image string) error {
			repos, ok := registryReposByTenant[tenant]
			if !ok {
				return fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			kept := repos[string(id)][:0]
			for _, repo := range repos[string(id)] {
				if repo.Name != image {
					kept = append(kept, repo)
				}
			}
			repos[string(id)] = kept
			delete(registryDigestsByTenant[tenant], registryKey(id, image))
			return nil
		},
		deleteRegistryDigests: func(_ context.Context, tenant string, id client.FlexID, image string, digests []string) error {
			rows, ok := registryDigestsByTenant[tenant]
			if !ok {
				return fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			remove := map[string]bool{}
			for _, digest := range digests {
				remove[digest] = true
			}
			kept := rows[registryKey(id, image)][:0]
			for _, digest := range rows[registryKey(id, image)] {
				if !remove[digest.Digest] {
					kept = append(kept, digest)
				}
			}
			rows[registryKey(id, image)] = kept
			return nil
		},
		deleteRegistryTags: func(_ context.Context, tenant string, id client.FlexID, image, digest string, tags []string) error {
			rows, ok := registryDigestsByTenant[tenant]
			if !ok {
				return fmt.Errorf("tenant %q is not present in this mock profile", tenant)
			}
			remove := map[string]bool{}
			for _, tag := range tags {
				remove[tag] = true
			}
			list := rows[registryKey(id, image)]
			for i := range list {
				if list[i].Digest == digest {
					kept := list[i].Tags[:0]
					for _, tag := range list[i].Tags {
						if !remove[tag] {
							kept = append(kept, tag)
						}
					}
					list[i].Tags = kept
				}
			}
			rows[registryKey(id, image)] = list
			return nil
		},
		environment: func(_ context.Context, tenant, id string) ([]client.EnvVar, []string, error) {
			data, ok := dataByTenant[tenant][id]
			if !ok {
				return nil, nil, fmt.Errorf("app is not present in mock profile")
			}
			names := append([]string(nil), data.SecretNames...)
			sort.Strings(names)
			return append([]client.EnvVar(nil), data.PlainEnvs...), names, nil
		},
		secretEnvs: func(_ context.Context, tenant, id string) ([]client.EnvVar, error) {
			data, ok := dataByTenant[tenant][id]
			if !ok {
				return nil, fmt.Errorf("app is not present in mock profile")
			}
			out := make([]client.EnvVar, 0, len(data.SecretNames))
			for _, name := range data.SecretNames {
				out = append(out, client.EnvVar{Name: name, Value: "MOCK_ONLY_SECRET_VALUE"})
			}
			return out, nil
		},
		updatePlainEnv: func(_ context.Context, tenant, id string, envs []client.EnvVar) error {
			data, ok := dataByTenant[tenant][id]
			if !ok {
				return fmt.Errorf("app is not present in mock profile")
			}
			data.PlainEnvs = append([]client.EnvVar(nil), envs...)
			dataByTenant[tenant][id] = data
			return nil
		},
		updateSecretEnv: func(_ context.Context, tenant, id string, envs []client.EnvVar) error {
			data, ok := dataByTenant[tenant][id]
			if !ok {
				return fmt.Errorf("app is not present in mock profile")
			}
			data.SecretNames = data.SecretNames[:0]
			for _, env := range envs {
				data.SecretNames = append(data.SecretNames, env.Name)
			}
			dataByTenant[tenant][id] = data
			return nil
		},
		domains: func(_ context.Context, tenant, id string) (DomainConfig, error) {
			cfg, ok := domainsByTenant[tenant][id]
			if !ok {
				return DomainConfig{}, fmt.Errorf("app is not present in mock profile")
			}
			return cloneDomainConfig(cfg), nil
		},
		updateDomains: func(_ context.Context, tenant, id string, cfg DomainConfig) error {
			if _, ok := domainsByTenant[tenant][id]; !ok {
				return fmt.Errorf("app is not present in mock profile")
			}
			domainsByTenant[tenant][id] = cloneDomainConfig(cfg)
			return nil
		},
		ports: func(_ context.Context, tenant, id string) (PortConfig, error) {
			cfg, ok := portsByTenant[tenant][id]
			if !ok {
				return PortConfig{}, fmt.Errorf("app is not present in mock profile")
			}
			return clonePortConfig(cfg), nil
		},
		updatePorts: func(_ context.Context, tenant, id string, cfg PortConfig) error {
			if _, ok := portsByTenant[tenant][id]; !ok {
				return fmt.Errorf("app is not present in mock profile")
			}
			cfg = clonePortConfig(cfg)
			apps := appsByTenant[tenant]
			for i := range apps {
				if apps[i].ID == id {
					if cfg.Type == "LoadBalancer" || cfg.Type == "NodePort" {
						if cfg.ExternalAddress == "" {
							cfg.ExternalAddress = apps[i].Name + "-" + apps[i].Namespace.Name + ".hsvc.mock.darkube.app"
						}
						if cfg.ExternalIP == "" {
							cfg.ExternalIP = "192.0.2.88"
						}
						names := make([]string, 0, len(cfg.Ports))
						for name := range cfg.Ports {
							names = append(names, name)
						}
						sort.Strings(names)
						for index, name := range names {
							port := cfg.Ports[name]
							if port.NodePort == 0 {
								port.NodePort = 32000 + index
							}
							cfg.Ports[name] = port
						}
					}
					portsByTenant[tenant][id] = clonePortConfig(cfg)
					apps[i].Service = &client.AppService{Type: cfg.Type, Ports: cfg.Ports, InternalAddress: cfg.InternalAddress, ExternalAddress: cfg.ExternalAddress, ExternalIP: cfg.ExternalIP}
					appsByTenant[tenant] = apps
					return nil
				}
			}
			return fmt.Errorf("app is not present in mock profile")
		},
		fetchPods: func(_ context.Context, tenant, id string) ([]appstate.Pod, error) {
			data, ok := dataByTenant[tenant][id]
			if !ok {
				return nil, fmt.Errorf("app is not present in mock profile")
			}
			return append([]appstate.Pod(nil), data.Pods...), nil
		},
		terminal: func(_ context.Context, tenant, id, appName, pod, container string) tea.ExecCommand {
			return &mockTerminalCommand{appName: appName, pod: pod, container: container}
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
	m.updateGitBuildSettings = func(_ context.Context, tenant, id string, expected, desired client.GitBuildSettings) error {
		apps := appsByTenant[tenant]
		for i := range apps {
			if apps[i].ID != id {
				continue
			}
			if !settingsFromApp(apps[i]).Equal(expected) {
				return fmt.Errorf("mock Git settings changed; refresh before saving")
			}
			apps[i].GitRepoURL, apps[i].GitBranch, apps[i].GitProvider = desired.RepoURL, desired.Branch, desired.Provider
			apps[i].GitDockerfile, apps[i].GitContext, apps[i].GitWorkdir = desired.Dockerfile, desired.Context, desired.Workdir
			apps[i].Builder = desired.Builder
			if desired.Autodeploy == nil {
				apps[i].GitAutodeploy = nil
			} else {
				auto := *desired.Autodeploy
				apps[i].GitAutodeploy = &auto
			}
			apps[i].GitBuildArgs, _ = json.Marshal(desired.Args)
			apps[i].BuildMethod = desired.BuildMethod
			appsByTenant[tenant] = apps
			if tenant == m.tenant {
				for j := range m.apps {
					if m.apps[j].ID == id {
						m.apps[j] = apps[i]
						break
					}
				}
				if m.detailApp != nil && m.detailApp.ID == id {
					copy := apps[i]
					m.detailApp = &copy
				}
			}
			return nil
		}
		return fmt.Errorf("app %q is not present in mock tenant %q", id, tenant)
	}
	if m.tenant == "" {
		m.contextFocused = true
	}
	return m
}

func copyMockAppData(in MockAppData) MockAppData {
	out := in
	out.PlainEnvs = append([]client.EnvVar(nil), in.PlainEnvs...)
	out.SecretNames = append([]string(nil), in.SecretNames...)
	out.Pods = append([]appstate.Pod(nil), in.Pods...)
	return out
}
