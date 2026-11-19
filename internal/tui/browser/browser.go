package browser

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"time"
)

// AppUsage is an optional latest resource snapshot, in CPU cores and bytes.
type AppUsage struct {
	CPUCores  *float64
	RAMBytes  *float64
	DiskBytes *float64
	Failed    UsageMetricFailures
}

// UsageMetricFailures records per-resource query failures so partial metric
// responses remain distinct from a successful query with no samples.
type UsageMetricFailures struct {
	CPU            bool
	CPUThrottle    bool
	RAM            bool
	Disk           bool
	NetworkReceive bool
	NetworkSend    bool
}

// BrowserOptions contains the live browser's reads and opt-in app lifecycle actions.
type BrowserOptions struct {
	Tenant       string
	LocalTenants []string
	AllowWrites  bool
	Profile      func(context.Context) (*client.Profile, error)
	ListApps     func(context.Context, string) ([]client.App, error)
	GetApp       func(context.Context, string, string) (*client.App, error)
	Usage        func(context.Context, string, string) (*AppUsage, error)
	UsageHistory func(context.Context, string, string, time.Duration, time.Duration) (*UsageHistory, error)
	Pods         func(context.Context, string, string) ([]appstate.Pod, error)
	AppLogs      func(context.Context, string, string, client.LogOptions) ([]client.LogEntry, error)
	EgressRoutes func(context.Context, string) (map[string]client.ClusterEgress, error)
	Namespaces   func(context.Context, string) ([]client.Namespace, error)
	RestartApp   func(context.Context, string, string) error
	DeleteApp    func(context.Context, string, string) error
}

// RunBrowser opens the app browser using the caller's scoped clients.
func RunBrowser(ctx context.Context, options BrowserOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &browser{tenant: options.Tenant, mode: "LIVE", profile: options.Profile, ctx: ctx,
		allowWrites: options.AllowWrites, localTenants: append([]string(nil), options.LocalTenants...),
		listApps: options.ListApps, logWrap: true, logTail: logDefaultTailSize}
	if options.GetApp != nil {
		m.getApp = options.GetApp
	}
	if options.Usage != nil {
		m.usage = options.Usage
	}
	m.usageHistory = options.UsageHistory
	m.fetchPods = options.Pods
	m.appLogs = options.AppLogs
	m.egressRoutes = options.EgressRoutes
	if options.Namespaces != nil {
		m.listNamespaces = options.Namespaces
	}
	if options.RestartApp != nil {
		m.restartApp = func(tenant, id string) error { return options.RestartApp(ctx, tenant, id) }
	}
	if options.DeleteApp != nil {
		m.deleteApp = func(tenant, id string) error { return options.DeleteApp(ctx, tenant, id) }
	}
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

// RunMockBrowser opens a browser backed only by the supplied local fixture.
func RunMockBrowser(ctx context.Context, profile MockProfile) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(newMockBrowser(profile), tea.WithContext(ctx)).Run()
	return err
}
