package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	mockMaintenanceNotificationID = 1003
	mockBillingNotificationID     = 1002
	mockStorageNotificationID     = 1001
	mockExternalDatabasePort      = 25432
	mockPrivateDatabasePort       = 5432
	mockPlanRAMLimit              = 1024
	newMockBrowserValue3          = 3
	newMockBrowserValue32000      = 32000
	newMockBrowserValue500        = 500
)

func mockNow() time.Time { return time.Date(2026, time.November, 19, 10, 15, 0, 0, time.UTC) }

func newMockBrowser(profile MockProfile) *browser {
	tenantProfiles := append([]MockTenant(nil), profile.Tenants...)
	if len(tenantProfiles) == 0 {
		tenantProfiles = []MockTenant{{Name: profile.Tenant, Apps: profile.Apps, Alerts: profile.Alerts, Namespaces: profile.Namespaces,
			AppData: profile.AppData, Registries: profile.Registries, Databases: profile.Databases}}
	}
	state, options := newMockBrowserData(tenantProfiles)
	initializeMockBuildData(state)
	m := &browser{
		tenant:                              profile.Tenant,
		mode:                                "MOCK PROFILE · OFFLINE",
		userEmail:                           "operator@mock.example.test",
		allowWrites:                         true,
		logWrap:                             true,
		logTail:                             logDefaultTailSize,
		tenants:                             options,
		activitySortAlertsDescending:        true,
		activitySortNotificationsDescending: true,
		activityAlertStatusFilter:           1,
	}
	setupMockTicketsCallbacks(m, state)
	setupMockActivityCallbacks(m, state)
	setupMockDatabasesCallbacks(m, state)
	setupMockAppsCallbacks(m, state)
	setupMockBuildsCallbacks(m, state)
	setupMockRegistryCallbacks(m, state)
	setupMockEnvironmentCallbacks(m, state)
	setupMockDomainsCallbacks(m, state)
	setupMockPortsCallbacks(m, state)
	setupMockRuntimeCallbacks(m, state)
	setupMockObservabilityCallbacks(m, state)
	setupMockGitSettings(m, state)
	if m.tenant == "" {
		m.contextFocused = true
	}
	return m
}

func setupMockObservabilityCallbacks(m *browser, state *mockBrowserData) {
	m.usage = func(_ context.Context, tenant, id string) (*AppUsage, error) {
		data, ok := state.dataByTenant[tenant][id]
		if !ok {
			return nil, errAppIsNotPresentInMockProfile
		}
		if data.Usage == nil {
			return nil, errMockUsageUnavailable
		}
		usage := *data.Usage
		return &usage, nil
	}
	m.usageHistory = func(_ context.Context, tenant, id string, window, step time.Duration) (*UsageHistory, error) {
		data, ok := state.dataByTenant[tenant][id]
		if !ok {
			return nil, errAppIsNotPresentInMockProfile
		}
		history := data.History
		history.Step = step
		history.End = mockNow()
		history.Start = history.End.Add(-window)
		return &history, nil
	}
	m.appLogs = func(_ context.Context, tenant, id string, opts client.LogOptions) ([]client.LogEntry, error) {
		data, ok := state.dataByTenant[tenant][id]
		if !ok {
			return nil, errAppIsNotPresentInMockProfile
		}
		entries := make([]client.LogEntry, 0, len(data.Logs))
		for _, log := range data.Logs {
			if log.Pod != "" && log.Pod != opts.PodName || log.Container != "" && log.Container != opts.Container || log.Previous != opts.Previous {
				continue
			}
			entries = append(entries, log.Entry)
		}
		if len(entries) > opts.Tail && opts.Tail > 0 {
			entries = entries[len(entries)-opts.Tail:]
		}
		return entries, nil
	}
}

func newMockBrowserData(tenants []MockTenant) (*mockBrowserData, []tenantOption) {
	state := &mockBrowserData{
		appsByTenant:            make(map[string][]client.App, len(tenants)),
		alertsByTenant:          make(map[string][]client.Alert, len(tenants)),
		dataByTenant:            make(map[string]map[string]MockAppData, len(tenants)),
		namespacesByTenant:      make(map[string][]client.Namespace, len(tenants)),
		registriesByTenant:      make(map[string][]client.Registry, len(tenants)),
		databasesByTenant:       make(map[string][]client.Database, len(tenants)),
		registryReposByTenant:   make(map[string]map[string][]client.Repository, len(tenants)),
		registryDigestsByTenant: make(map[string]map[string][]client.Digest, len(tenants)),
		egressByTenant:          make(map[string]map[string]client.ClusterEgress, len(tenants)),
		domainsByTenant:         make(map[string]map[string]DomainConfig, len(tenants)),
		portsByTenant:           make(map[string]map[string]PortConfig, len(tenants)),
		ticketRowsByTenant:      make(map[string][]client.Ticket, len(tenants)),
		ticketThreadsByTenant:   make(map[string]map[string][]client.TicketComment, len(tenants)),
		ticketMu:                &sync.RWMutex{},
		deletedApps:             make(map[string]map[string]bool),
		mockBuilds:              make(map[string][]client.Build),
		buildOwners:             make(map[string]string),
	}
	options := make([]tenantOption, 0, len(tenants))
	for _, tenant := range tenants {
		populateMockTenantData(state, tenant)
		options = append(options, tenantOption{name: tenant.Name, source: "Mock profile"})
	}
	if len(options) > 0 {
		options[0].status = labelCurrent
	}
	return state, options
}

func populateMockTenantData(state *mockBrowserData, tenant MockTenant) {
	now := mockNow().Format("2006-01-02T15:04:05.000-0700")
	state.ticketRowsByTenant[tenant.Name] = []client.Ticket{
		{ID: client.TicketID("33421"), Summary: "مشکل اتصال به دیتابیس", Status: "Open",
			RequestType: client.TicketRequestType{Key: ticketRequestDatabase, Value: "Database"}, Created: now, Updated: now},
		{ID: client.TicketID("33408"), Summary: "پیگیری صورتحساب", Status: "Close",
			RequestType: client.TicketRequestType{Key: ticketRequestFinancial, Value: "Financial"}, Created: now, Updated: now},
	}
	state.ticketThreadsByTenant[tenant.Name] = map[string][]client.TicketComment{
		"33421": {{ID: "0", Body: "اتصال به پایگاه داده برقرار نمی\u200cشود.\nخطا: connection refused on port 5432",
			Author: client.TicketUser{DisplayName: "اپراتور"}, Created: client.TicketTime(now)}, {ID: "1",
			Body:   "لطفاً نام پروژه و زمان خطا را بفرستید.\n\n```text\nconnection refused\n```",
			Author: client.TicketUser{DisplayName: "پشتیبانی", Email: "support@hamravesh.com"}, Created: client.TicketTime(now)}},
		"33408": {{ID: "0", Body: "درخواست بررسی صورتحساب", Author: client.TicketUser{DisplayName: "اپراتور"}, Created: client.TicketTime(now)}},
	}
	state.appsByTenant[tenant.Name] = append([]client.App(nil), tenant.Apps...)
	state.databasesByTenant[tenant.Name] = append([]client.Database(nil), tenant.Databases...)
	state.alertsByTenant[tenant.Name] = append([]client.Alert(nil), tenant.Alerts...)
	state.dataByTenant[tenant.Name] = copyTenantMockAppData(tenant.AppData)
	state.namespacesByTenant[tenant.Name] = append([]client.Namespace(nil), tenant.Namespaces...)
	populateMockTenantRegistryData(state, tenant)
	state.egressByTenant[tenant.Name] = make(map[string]client.ClusterEgress)
	state.domainsByTenant[tenant.Name] = make(map[string]DomainConfig)
	state.portsByTenant[tenant.Name] = make(map[string]PortConfig)
	populateMockTenantAppData(state, tenant)
}

func populateMockTenantRegistryData(state *mockBrowserData, tenant MockTenant) {
	state.registryReposByTenant[tenant.Name] = map[string][]client.Repository{}
	state.registryDigestsByTenant[tenant.Name] = map[string][]client.Digest{}
	for _, registry := range tenant.Registries {
		registryID := string(registry.Registry.ID)
		state.registriesByTenant[tenant.Name] = append(state.registriesByTenant[tenant.Name], registry.Registry)
		for _, image := range registry.Images {
			state.registryReposByTenant[tenant.Name][registryID] = append(state.registryReposByTenant[tenant.Name][registryID], image.Repository)
			key := registryKey(registry.Registry.ID, image.Repository.Name)
			state.registryDigestsByTenant[tenant.Name][key] = append([]client.Digest(nil), image.Digests...)
		}
	}
}

func populateMockTenantAppData(state *mockBrowserData, tenant MockTenant) {
	for _, app := range tenant.Apps {
		state.portsByTenant[tenant.Name][app.ID] = mockPortConfig(app)
		state.domainsByTenant[tenant.Name][app.ID] = mockDomainConfig(app)
		cluster := app.Namespace.Cluster
		if cluster.ID == 0 && cluster.Name == "" {
			continue
		}
		key := strconv.Itoa(cluster.ID)
		if cluster.ID == 0 {
			key = cluster.Name
		}
		state.egressByTenant[tenant.Name][key] = client.ClusterEgress{Cluster: client.EgressCluster{ID: cluster.ID, Name: cluster.Name,
			DefaultOutgoingIP: "198.51.100.42"}}
	}
}

func copyTenantMockAppData(appData map[string]MockAppData) map[string]MockAppData {
	copied := make(map[string]MockAppData, len(appData))
	for id, data := range appData {
		copied[id] = copyMockAppData(data)
	}
	return copied
}

func mockPortConfig(app client.App) PortConfig {
	if app.Service == nil {
		return PortConfig{Type: "ClusterIP", Ports: map[string]client.Port{}}
	}
	return PortConfig{Type: app.Service.Type, InternalAddress: app.Service.InternalAddress,
		ExternalAddress: app.Service.ExternalAddress, ExternalIP: app.Service.ExternalIP, Ports: app.Service.Ports}
}

func mockDomainConfig(app client.App) DomainConfig {
	dns := make(map[string]bool, len(app.ExternalHosts))
	certs := make(map[string]string, len(app.ExternalHosts))
	for _, host := range app.ExternalHosts {
		dns[host] = true
		certs[host] = mockNow().AddDate(0, newMockBrowserValue3, 0).Format("2006-01-02")
	}
	return DomainConfig{Hosts: append([]string(nil), app.ExternalHosts...), Subdomain: app.CustomSubdomainAddr,
		BaseDomain: app.Namespace.Cluster.AppsCustomBaseDomain, CNAME: "ingress.mock.darkube.app", Challenge: dnsChallengeDNS01,
		SSL: app.EnableSSL, DNS: dns, Certificates: certs,
		ChallengeRecords: map[string]string{"_acme-challenge.example.com": "demo-one.acme-dns.mock.example",
			"_acme-challenge.example.org": "demo-two.acme-dns.mock.example"}}
}

func initializeMockBuildData(state *mockBrowserData) {
	for tenant, apps := range state.appsByTenant {
		for _, app := range apps {
			if app.CreationMethod != client.CreationMethodGitRepoURL {
				continue
			}
			addMockBuildsForApp(state, tenant, app)
		}
	}
}

func addMockBuildsForApp(state *mockBrowserData, tenant string, app client.App) {
	rows := []client.Build{
		{ID: client.BuildID(app.ID + "-mock-203"), Status: client.BuildSucceeded, GitBranch: defaultNameMain, ShortCommitHash: "8d91fab",
			CommitMessage: "Update application configuration", CreationTime: mockNow().Add(-2 * time.Hour).Format(time.RFC3339),
			EndTime: mockNow().Add(-time.Hour*2 + time.Minute*2).Format(time.RFC3339),
			Log:     "STEP 1/4: checkout source\nSTEP 2/4: install dependencies\nSTEP 3/4: build image\nBuild completed successfully"},
		{ID: client.BuildID(app.ID + "-mock-202"), Status: client.BuildFailed, GitBranch: defaultNameMain, ShortCommitHash: "020ab88",
			CommitMessage: "Fix deployment pipeline", CreationTime: mockNow().Add(-24 * time.Hour).Format(time.RFC3339),
			EndTime: mockNow().Add(-23 * time.Hour).Format(time.RFC3339), Errors: "Build command exited with status 1",
			Log: "STEP 1/4: checkout source\nSTEP 2/4: run tests\nERROR: tests failed"},
	}
	key := tenant + "/" + app.ID
	state.mockBuilds[key] = rows
	for _, row := range rows {
		state.buildOwners[string(row.ID)] = key
	}
}

func setupMockGitSettings(m *browser, state *mockBrowserData) {
	appsByTenant := state.appsByTenant
	m.updateGitBuildSettings = func(_ context.Context, tenant, id string, expected, desired client.GitBuildSettings) error {
		apps := appsByTenant[tenant]
		for i := range apps {
			if apps[i].ID != id {
				continue
			}
			if !settingsFromApp(apps[i]).Equal(expected) {
				return errMockGitSettingsChangedRefreshBeforeSaving
			}
			apps[i].GitRepoURL, apps[i].GitBranch, apps[i].GitProvider = desired.RepoURL, desired.Branch, desired.Provider
			apps[i].GitDockerfile, apps[i].GitContext, apps[i].GitWorkdir = desired.Dockerfile, desired.Context, desired.Workdir
			apps[i].Builder = desired.Builder
			apps[i].GitAutodeploy = copyMockAutodeploy(desired.Autodeploy)
			buildArgs, err := json.Marshal(desired.Args)
			if err != nil {
				return fmt.Errorf("encode mock Git build arguments: %w", err)
			}
			apps[i].GitBuildArgs = buildArgs
			apps[i].BuildMethod = desired.BuildMethod
			appsByTenant[tenant] = apps
			if tenant == m.tenant {
				m.syncMockAppSettings(id, apps[i])
			}
			return nil
		}
		return fmt.Errorf("%w: app %q is not present in mock tenant %q", errAppQIsNotPresentInMock, id, tenant)
	}
}

func copyMockAutodeploy(autodeploy *bool) *bool {
	if autodeploy == nil {
		return nil
	}
	value := *autodeploy
	return &value
}

func (m *browser) syncMockAppSettings(id string, app client.App) {
	for i := range m.apps {
		if m.apps[i].ID == id {
			m.apps[i] = app
			break
		}
	}
	if m.detailApp != nil && m.detailApp.ID == id {
		appCopy := app
		m.detailApp = &appCopy
	}
}

type mockBrowserData struct {
	appsByTenant            map[string][]client.App
	alertsByTenant          map[string][]client.Alert
	dataByTenant            map[string]map[string]MockAppData
	namespacesByTenant      map[string][]client.Namespace
	registriesByTenant      map[string][]client.Registry
	databasesByTenant       map[string][]client.Database
	registryReposByTenant   map[string]map[string][]client.Repository
	registryDigestsByTenant map[string]map[string][]client.Digest
	egressByTenant          map[string]map[string]client.ClusterEgress
	domainsByTenant         map[string]map[string]DomainConfig
	portsByTenant           map[string]map[string]PortConfig
	ticketRowsByTenant      map[string][]client.Ticket
	ticketThreadsByTenant   map[string]map[string][]client.TicketComment
	ticketMu                *sync.RWMutex
	deletedApps             map[string]map[string]bool
	mockBuilds              map[string][]client.Build
	buildOwners             map[string]string
}

func setupMockTicketsCallbacks(m *browser, state *mockBrowserData) {
	ticketRowsByTenant := state.ticketRowsByTenant
	ticketThreadsByTenant := state.ticketThreadsByTenant
	ticketMu := state.ticketMu
	m.listTickets = func(_ context.Context, tenant, status string, _ bool) (client.TicketList, error) {
		ticketMu.RLock()
		defer ticketMu.RUnlock()
		rows, ok := ticketRowsByTenant[tenant]
		if !ok {
			return client.TicketList{}, fmt.Errorf("%w", errTenantQIsNotPresentInThis)
		}
		out := make([]client.Ticket, 0, len(rows))
		open, closed := 0, 0
		for _, row := range rows {
			if row.IsOpen() {
				open++
			} else {
				closed++
			}
			if status == "" || status == client.TicketStatusAll || status == client.TicketStatusOpen && row.IsOpen() ||
				status == client.TicketStatusClosed && !row.IsOpen() {
				out = append(out, row)
			}
		}
		return client.TicketList{Total: len(rows), Open: open, Closed: closed, Tickets: out}, nil
	}
	m.getTicket = func(_ context.Context, tenant, id string) (client.Ticket, error) {
		ticketMu.RLock()
		defer ticketMu.RUnlock()
		for _, row := range ticketRowsByTenant[tenant] {
			if string(row.ID) == id {
				return row, nil
			}
		}
		return client.Ticket{}, errMockTicketNotFound
	}
	m.getTicketComments = func(_ context.Context, tenant, id string) ([]client.TicketComment, error) {
		ticketMu.RLock()
		defer ticketMu.RUnlock()
		return append([]client.TicketComment(nil), ticketThreadsByTenant[tenant][id]...), nil
	}
	m.getTicketCategories = func(context.Context, string) ([]client.TicketCategory, error) {
		return []client.TicketCategory{{Key: ticketRequestDatabase, Value: "Database"}, {Key: ticketRequestFinancial,
			Value: "Financial"}, {Key: ticketRequestDarkube, Value: "Darkube"}}, nil
	}
	m.getTicketPriorities = func(context.Context, string) ([]client.TicketPriority, error) {
		return []client.TicketPriority{{ID: "3", Keyword: "normal"}, {ID: "2", Keyword: "high"}, {ID: "10002", Keyword: statusCritical}}, nil
	}
	m.createTicket = func(_ context.Context, tenant string, ticket client.NewTicket) (string, error) {
		ticketMu.Lock()
		defer ticketMu.Unlock()
		id := strconv.Itoa(33000 + len(ticketRowsByTenant[tenant]) + 1)
		now := mockNow().Format(time.RFC3339)
		row := client.Ticket{ID: client.TicketID(id), Summary: ticket.Summary, Status: "Open",
			RequestType: client.TicketRequestType{Key: ticket.RequestType, Value: ticket.RequestType}, Created: now, Updated: now}
		ticketRowsByTenant[tenant] = append([]client.Ticket{row}, ticketRowsByTenant[tenant]...)
		ticketThreadsByTenant[tenant][id] = []client.TicketComment{{ID: "0", Body: ticket.Description,
			Author: client.TicketUser{DisplayName: "Mock operator"}, Created: client.TicketTime(mockNow().Format(time.RFC3339))}}
		return id, nil
	}
	m.replyTicket = func(_ context.Context, tenant, id, body string) error {
		ticketMu.Lock()
		defer ticketMu.Unlock()
		thread, ok := ticketThreadsByTenant[tenant][id]
		if !ok {
			return errMockTicketNotFound
		}
		thread = append(thread, client.TicketComment{ID: client.TicketID(strconv.Itoa(len(thread))), Body: body,
			Author: client.TicketUser{DisplayName: "Mock operator"}, Created: client.TicketTime(mockNow().Format(time.RFC3339))})
		ticketThreadsByTenant[tenant][id] = thread
		for i := range ticketRowsByTenant[tenant] {
			if string(ticketRowsByTenant[tenant][i].ID) == id {
				ticketRowsByTenant[tenant][i].Updated = mockNow().Format(time.RFC3339)
				break
			}
		}
		return nil
	}
}

func setupMockActivityCallbacks(m *browser, state *mockBrowserData) {
	alertsByTenant := state.alertsByTenant
	m.alerts = func(_ context.Context, tenant string) ([]client.Alert, error) {
		if tenant == "" {
			return nil, errChooseATenantToViewAlerts
		}
		return append([]client.Alert(nil), alertsByTenant[tenant]...), nil
	}
	m.notifications = func(context.Context) ([]client.Notification, error) {
		now := mockNow()
		return []client.Notification{
			{Slug: mockMaintenanceNotificationID, Title: "نگهداری سامانه",
				Description: "<p>امشب سامانه برای <strong>به\u200cروزرسانی</strong> در دسترس نخواهد بود.</p>",
				Timestamp:   now.Add(-20 * time.Minute).Format(time.RFC3339), TargetType: "platform", Unread: true},
			{Slug: mockBillingNotificationID, Title: "گزارش مصرف ماهانه", Description: "گزارش مصرف ماه گذشته آماده است.",
				Timestamp: now.Add(-3 * time.Hour).Format(time.RFC3339), TargetType: "billing"},
			{Slug: mockStorageNotificationID, Title: "به\u200cروزرسانی فضای ذخیره\u200cسازی",
				Description: "فضای ذخیره\u200cسازی با موفقیت به\u200cروزرسانی شد.", Timestamp: now.Add(-24 * time.Hour).Format(time.RFC3339),
				TargetType: "storage", Unread: true},
		}, nil
	}
}

func setupMockDatabasesCallbacks(m *browser, state *mockBrowserData) {
	databasesByTenant := state.databasesByTenant
	m.listDatabases = func(_ context.Context, tenant string) ([]client.Database, error) {
		rows, ok := databasesByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		return append([]client.Database(nil), rows...), nil
	}
	m.getDatabase = func(_ context.Context, tenant, id string) (*client.DatabaseDetail, error) {
		rows, ok := databasesByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		for _, db := range rows {
			if db.ID != id {
				continue
			}
			external := &client.DatabaseConnection{Username: "mock-user", Password: "mock-password-only", Database: "mock_" + db.Name,
				Host: "db-" + db.ID + ".mock.example.test", Port: mockExternalDatabasePort}
			private := &client.DatabaseConnection{Username: "mock-user", Password: "mock-password-only", Database: "mock_" + db.Name,
				Host: db.Name + ".svc.mock", Port: mockPrivateDatabasePort}
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
		return nil, fmt.Errorf("%w: database %q is not present in mock tenant %q", errDatabaseQIsNotPresentInMock, id, tenant)
	}
}

func setupMockAppsCallbacks(m *browser, state *mockBrowserData) {
	appsByTenant := state.appsByTenant
	deletedApps := state.deletedApps
	m.listApps = func(_ context.Context, tenant string) ([]client.App, error) {
		apps, ok := appsByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		remaining := make([]client.App, 0, len(apps))
		for _, app := range apps {
			if !deletedApps[tenant][app.ID] {
				remaining = append(remaining, app)
			}
		}
		return remaining, nil
	}
	m.getApp = func(_ context.Context, tenant, id string) (*client.App, error) {
		for _, app := range appsByTenant[tenant] {
			if app.ID == id && !deletedApps[tenant][id] {
				appCopy := app
				return &appCopy, nil
			}
		}
		return nil, fmt.Errorf("%w: app %q is not present in mock tenant %q", errAppQIsNotPresentInMock, id, tenant)
	}
	m.updateResources = func(_ context.Context, tenant, id string, cfg ResourceConfig) error {
		for i := range appsByTenant[tenant] {
			app := &appsByTenant[tenant][i]
			if app.ID != id || deletedApps[tenant][id] {
				continue
			}
			plan := client.Plan{ID: cfg.Plan, Name: cfg.Plan, PlanType: labelApp}
			if app.Plan != nil && app.Plan.ID == cfg.Plan {
				plan = *app.Plan
			}
			if cfg.Plan == "plan-standard-demo" {
				plan.Name, plan.CodeName, plan.CostType = "Standard", "standard", "fixed"
				plan.Detail = client.PlanDetail{CPURequest: newMockBrowserValue500, RAMLimit: mockPlanRAMLimit}
			}
			app.Plan = &plan
			app.CPURequest, app.RAMLimit = cfg.CPU, cfg.RAM
			app.Replicas, app.ReplicasPresent, app.IsHPAEnabled = cfg.Replicas, true, cfg.Autoscaling
			if app.Disk != nil {
				disk := *app.Disk
				disk.SizeInGi = cfg.DiskGi
				app.Disk = &disk
			}
			app.CustomConfig = &client.AppCustomConfig{HPA: &client.AppHPAConfig{Enabled: cfg.Autoscaling, MinReplicas: cfg.MinReplicas,
				MaxReplicas: cfg.MaxReplicas, TargetCPUUtilizationPercentage: cfg.CPUTarget}}
			return nil
		}
		return fmt.Errorf("%w: app %q is not present in mock tenant %q", errAppQIsNotPresentInMock, id, tenant)
	}
}

func setupMockBuildsCallbacks(m *browser, state *mockBrowserData) {
	mockBuilds := state.mockBuilds
	buildOwners := state.buildOwners
	m.listBuilds = func(_ context.Context, tenant, id string, limit int) ([]client.Build, error) {
		rows, ok := mockBuilds[tenant+"/"+id]
		if !ok {
			return nil, errNoMockBuildsForApp
		}
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return append([]client.Build(nil), rows...), nil
	}
	m.getBuild = func(_ context.Context, tenant string, id client.BuildID) (*client.Build, error) {
		owner, ok := buildOwners[string(id)]
		if !ok || !strings.HasPrefix(owner, tenant+"/") {
			return nil, errMockBuildNotFound
		}
		for _, row := range mockBuilds[owner] {
			if row.ID == id {
				rowCopy := row
				return &rowCopy, nil
			}
		}
		return nil, errMockBuildNotFound
	}
	m.buildLatest = func(_ context.Context, tenant, appID string) error {
		key := tenant + "/" + appID
		rows, ok := mockBuilds[key]
		if !ok {
			return errMockBuildAppNotFound
		}
		id := client.BuildID(fmt.Sprintf("%s-mock-%d", appID, len(rows)+1))
		rows = append([]client.Build{{ID: id, Status: client.BuildRunning, GitBranch: defaultNameMain, ShortCommitHash: "a1b2c3d",
			CommitMessage: "Mock branch head build", CreationTime: mockNow().Format(time.RFC3339), Log: "Mock build submitted"}}, rows...)
		mockBuilds[key] = rows
		buildOwners[string(id)] = key
		return nil
	}
	m.retryBuild = func(_ context.Context, tenant string, id client.BuildID) (*client.Build, error) {
		return retryMockBuild(mockBuilds, buildOwners, tenant, id)
	}
	m.stopBuild = func(_ context.Context, tenant string, id client.BuildID) error {
		return stopMockBuild(mockBuilds, buildOwners, tenant, id)
	}
}

func retryMockBuild(mockBuilds map[string][]client.Build, buildOwners map[string]string, tenant string,
	id client.BuildID) (*client.Build, error) {
	key, ok := buildOwners[string(id)]
	if !ok || !strings.HasPrefix(key, tenant+"/") {
		return nil, errMockBuildNotFound
	}
	rows := mockBuilds[key]
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		retriedBuild := rows[i]
		retriedBuild.ID = client.BuildID(fmt.Sprintf("%s-mock-%d", strings.Split(key, "/")[1], len(rows)+1))
		retriedBuild.Status = client.BuildRunning
		retriedBuild.CreationTime = mockNow().Format(time.RFC3339)
		retriedBuild.EndTime = ""
		retriedBuild.Log = "Mock retry submitted"
		mockBuilds[key] = append([]client.Build{retriedBuild}, rows...)
		buildOwners[string(retriedBuild.ID)] = key
		return &retriedBuild, nil
	}
	return nil, errMockBuildNotFound
}

func stopMockBuild(mockBuilds map[string][]client.Build, buildOwners map[string]string, tenant string, id client.BuildID) error {
	key, ok := buildOwners[string(id)]
	if !ok || !strings.HasPrefix(key, tenant+"/") {
		return errMockBuildNotFound
	}
	rows := mockBuilds[key]
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		if !rows[i].InProgress() {
			return errMockBuildIsAlreadyFinished
		}
		rows[i].Status = client.BuildCanceled
		rows[i].EndTime = mockNow().Format(time.RFC3339)
		mockBuilds[key] = rows
		return nil
	}
	return errMockBuildNotFound
}

func setupMockRegistryCallbacks(m *browser, state *mockBrowserData) {
	namespacesByTenant := state.namespacesByTenant
	registriesByTenant := state.registriesByTenant
	registryReposByTenant := state.registryReposByTenant
	registryDigestsByTenant := state.registryDigestsByTenant
	m.listNamespaces = func(_ context.Context, tenant string) ([]client.Namespace, error) {
		namespaces, ok := namespacesByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		return append([]client.Namespace(nil), namespaces...), nil
	}
	m.listRegistries = func(_ context.Context, tenant string) ([]client.Registry, error) {
		rows, ok := registriesByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		return append([]client.Registry(nil), rows...), nil
	}
	m.listRegistryRepositories = func(_ context.Context, tenant string, id client.FlexID) ([]client.Repository, error) {
		rows, ok := registryReposByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		return append([]client.Repository(nil), rows[string(id)]...), nil
	}
	m.listRegistryDigests = func(_ context.Context, tenant string, id client.FlexID, image string) ([]client.Digest, error) {
		rows, ok := registryDigestsByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		return append([]client.Digest(nil), rows[registryKey(id, image)]...), nil
	}
	m.deleteRegistryRepository = func(_ context.Context, tenant string, id client.FlexID, image string) error {
		return deleteMockRegistryRepository(registryReposByTenant, registryDigestsByTenant, tenant, id, image)
	}
	m.deleteRegistryDigests = func(_ context.Context, tenant string, id client.FlexID, image string, digests []string) error {
		return deleteMockRegistryDigests(registryDigestsByTenant, tenant, id, image, digests)
	}
	m.deleteRegistryTags = func(_ context.Context, tenant string, id client.FlexID, image, digest string, tags []string) error {
		return deleteMockRegistryTags(registryDigestsByTenant, tenant, id, image, digest, tags)
	}
}

func deleteMockRegistryRepository(repositories map[string]map[string][]client.Repository,
	digests map[string]map[string][]client.Digest, tenant string, id client.FlexID, image string) error {
	rows, ok := repositories[tenant]
	if !ok {
		return fmt.Errorf("%w: tenant %q", errTenantQIsNotPresentInThis, tenant)
	}
	kept := rows[string(id)][:0]
	for _, repo := range rows[string(id)] {
		if repo.Name != image {
			kept = append(kept, repo)
		}
	}
	rows[string(id)] = kept
	delete(digests[tenant], registryKey(id, image))
	return nil
}

func deleteMockRegistryDigests(digests map[string]map[string][]client.Digest, tenant string, id client.FlexID, image string,
	removeValues []string) error {
	rows, ok := digests[tenant]
	if !ok {
		return fmt.Errorf("%w: tenant %q", errTenantQIsNotPresentInThis, tenant)
	}
	remove := make(map[string]bool, len(removeValues))
	for _, digest := range removeValues {
		remove[digest] = true
	}
	key := registryKey(id, image)
	kept := rows[key][:0]
	for _, digest := range rows[key] {
		if !remove[digest.Digest] {
			kept = append(kept, digest)
		}
	}
	rows[key] = kept
	return nil
}

func deleteMockRegistryTags(digests map[string]map[string][]client.Digest, tenant string, id client.FlexID, image, digest string,
	removeValues []string) error {
	rows, ok := digests[tenant]
	if !ok {
		return fmt.Errorf("%w: tenant %q", errTenantQIsNotPresentInThis, tenant)
	}
	remove := make(map[string]bool, len(removeValues))
	for _, tag := range removeValues {
		remove[tag] = true
	}
	key := registryKey(id, image)
	list := rows[key]
	for i := range list {
		if list[i].Digest != digest {
			continue
		}
		kept := list[i].Tags[:0]
		for _, tag := range list[i].Tags {
			if !remove[tag] {
				kept = append(kept, tag)
			}
		}
		list[i].Tags = kept
	}
	rows[key] = list
	return nil
}

func setupMockEnvironmentCallbacks(m *browser, state *mockBrowserData) {
	dataByTenant := state.dataByTenant
	m.environment = func(_ context.Context, tenant, id string) ([]client.EnvVar, []string, error) {
		data, ok := dataByTenant[tenant][id]
		if !ok {
			return nil, nil, errAppIsNotPresentInMockProfile
		}
		names := append([]string(nil), data.SecretNames...)
		sort.Strings(names)
		return append([]client.EnvVar(nil), data.PlainEnvs...), names, nil
	}
	m.secretEnvs = func(_ context.Context, tenant, id string) ([]client.EnvVar, error) {
		data, ok := dataByTenant[tenant][id]
		if !ok {
			return nil, errAppIsNotPresentInMockProfile
		}
		out := make([]client.EnvVar, 0, len(data.SecretNames))
		for _, name := range data.SecretNames {
			out = append(out, client.EnvVar{Name: name, Value: "MOCK_ONLY_SECRET_VALUE"})
		}
		return out, nil
	}
	m.updatePlainEnv = func(_ context.Context, tenant, id string, envs []client.EnvVar) error {
		data, ok := dataByTenant[tenant][id]
		if !ok {
			return errAppIsNotPresentInMockProfile
		}
		data.PlainEnvs = append([]client.EnvVar(nil), envs...)
		dataByTenant[tenant][id] = data
		return nil
	}
	m.updateSecretEnv = func(_ context.Context, tenant, id string, envs []client.EnvVar) error {
		data, ok := dataByTenant[tenant][id]
		if !ok {
			return errAppIsNotPresentInMockProfile
		}
		data.SecretNames = data.SecretNames[:0]
		for _, env := range envs {
			data.SecretNames = append(data.SecretNames, env.Name)
		}
		dataByTenant[tenant][id] = data
		return nil
	}
}

func setupMockDomainsCallbacks(m *browser, state *mockBrowserData) {
	domainsByTenant := state.domainsByTenant
	m.domains = func(_ context.Context, tenant, id string) (DomainConfig, error) {
		cfg, ok := domainsByTenant[tenant][id]
		if !ok {
			return DomainConfig{}, errAppIsNotPresentInMockProfile
		}
		return cloneDomainConfig(cfg), nil
	}
	m.updateDomains = func(_ context.Context, tenant, id string, cfg DomainConfig) error {
		if _, ok := domainsByTenant[tenant][id]; !ok {
			return errAppIsNotPresentInMockProfile
		}
		domainsByTenant[tenant][id] = cloneDomainConfig(cfg)
		return nil
	}
}

func setupMockPortsCallbacks(m *browser, state *mockBrowserData) {
	appsByTenant := state.appsByTenant
	portsByTenant := state.portsByTenant
	m.ports = func(_ context.Context, tenant, id string) (PortConfig, error) {
		cfg, ok := portsByTenant[tenant][id]
		if !ok {
			return PortConfig{}, errAppIsNotPresentInMockProfile
		}
		return clonePortConfig(cfg), nil
	}
	m.updatePorts = func(_ context.Context, tenant, id string, cfg PortConfig) error {
		if _, ok := portsByTenant[tenant][id]; !ok {
			return errAppIsNotPresentInMockProfile
		}
		cfg = clonePortConfig(cfg)
		apps := appsByTenant[tenant]
		for i := range apps {
			if apps[i].ID != id {
				continue
			}
			cfg = completeMockPortConfig(cfg, apps[i])
			portsByTenant[tenant][id] = clonePortConfig(cfg)
			apps[i].Service = &client.AppService{Type: cfg.Type, Ports: cfg.Ports, InternalAddress: cfg.InternalAddress,
				ExternalAddress: cfg.ExternalAddress, ExternalIP: cfg.ExternalIP}
			appsByTenant[tenant] = apps
			return nil
		}
		return errAppIsNotPresentInMockProfile
	}
}

func setupMockRuntimeCallbacks(m *browser, state *mockBrowserData) {
	dataByTenant := state.dataByTenant
	egressByTenant := state.egressByTenant
	deletedApps := state.deletedApps
	m.fetchPods = func(_ context.Context, tenant, id string) ([]appstate.Pod, error) {
		data, ok := dataByTenant[tenant][id]
		if !ok {
			return nil, errAppIsNotPresentInMockProfile
		}
		return append([]appstate.Pod(nil), data.Pods...), nil
	}
	m.terminal = func(_ context.Context, _, _, appName, pod, container string) tea.ExecCommand {
		return &mockTerminalCommand{appName: appName, pod: pod, container: container}
	}
	m.egressRoutes = func(_ context.Context, tenant string) (map[string]client.ClusterEgress, error) {
		routes, ok := egressByTenant[tenant]
		if !ok {
			return nil, fmt.Errorf("%w: tenant %q is not present in this mock profile", errTenantQIsNotPresentInThis, tenant)
		}
		return routes, nil
	}
	m.mockDeleteApp = func(tenant, id string) {
		if deletedApps[tenant] == nil {
			deletedApps[tenant] = make(map[string]bool)
		}
		deletedApps[tenant][id] = true
	}
}

func completeMockPortConfig(cfg PortConfig, app client.App) PortConfig {
	if cfg.Type != serviceTypeLoadBalancer && cfg.Type != serviceTypeNodePort {
		return cfg
	}
	if cfg.ExternalAddress == "" {
		cfg.ExternalAddress = app.Name + "-" + app.Namespace.Name + ".hsvc.mock.darkube.app"
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
			port.NodePort = newMockBrowserValue32000 + index
		}
		cfg.Ports[name] = port
	}
	return cfg
}

func copyMockAppData(in MockAppData) MockAppData {
	out := in
	out.PlainEnvs = append([]client.EnvVar(nil), in.PlainEnvs...)
	out.SecretNames = append([]string(nil), in.SecretNames...)
	out.Pods = append([]appstate.Pod(nil), in.Pods...)
	out.Logs = append([]MockLog(nil), in.Logs...)
	if in.Usage != nil {
		usage := *in.Usage
		out.Usage = &usage
	}
	return out
}
