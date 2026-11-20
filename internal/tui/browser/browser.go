package browser

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
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

// ResourceConfig is the staged, editable resource and scaling projection.
type ResourceConfig struct {
	Plan, CPU, RAM                                        string
	DiskGi, Replicas, MinReplicas, MaxReplicas, CPUTarget int
	Autoscaling                                           bool
}

// Options contains the live browser's reads and opt-in write actions.
type Options struct {
	Tenant                   string
	Version                  string
	LocalTenants             []string
	AllowWrites              bool
	ListTickets              func(context.Context, string, string, bool) (client.TicketList, error)
	GetTicket                func(context.Context, string, string) (client.Ticket, error)
	GetTicketComments        func(context.Context, string, string) ([]client.TicketComment, error)
	GetTicketCategories      func(context.Context, string) ([]client.TicketCategory, error)
	GetTicketPriorities      func(context.Context, string) ([]client.TicketPriority, error)
	CreateTicket             func(context.Context, string, client.NewTicket) (string, error)
	ReplyTicket              func(context.Context, string, string, string) error
	Profile                  func(context.Context) (*client.Profile, error)
	Alerts                   func(context.Context, string) ([]client.Alert, error)
	Notifications            func(context.Context) ([]client.Notification, error)
	ListDatabases            func(context.Context, string) ([]client.Database, error)
	GetDatabase              func(context.Context, string, string) (*client.DatabaseDetail, error)
	ListApps                 func(context.Context, string) ([]client.App, error)
	GetApp                   func(context.Context, string, string) (*client.App, error)
	Usage                    func(context.Context, string, string) (*AppUsage, error)
	ListPVCs                 func(context.Context, string, string, int) ([]client.PVC, error)
	UsageHistory             func(context.Context, string, string, time.Duration, time.Duration) (*UsageHistory, error)
	Pods                     func(context.Context, string, string) ([]appstate.Pod, error)
	AppLogs                  func(context.Context, string, string, client.LogOptions) ([]client.LogEntry, error)
	ListBuilds               func(context.Context, string, string, int) ([]client.Build, error)
	GetBuild                 func(context.Context, string, client.BuildID) (*client.Build, error)
	DeployToken              func(context.Context, string, string) (string, error)
	UpdateGitBuildSettings   func(context.Context, string, string, client.GitBuildSettings, client.GitBuildSettings) error
	BuildLatest              func(context.Context, string, string) error
	RetryBuild               func(context.Context, string, client.BuildID) (*client.Build, error)
	StopBuild                func(context.Context, string, client.BuildID) error
	Environment              func(context.Context, string, string) ([]client.EnvVar, []string, error)
	SecretEnvs               func(context.Context, string, string) ([]client.EnvVar, error)
	UpdatePlainEnv           func(context.Context, string, string, []client.EnvVar) error
	UpdateSecretEnv          func(context.Context, string, string, []client.EnvVar) error
	Domains                  func(context.Context, string, string) (DomainConfig, error)
	UpdateDomains            func(context.Context, string, string, DomainConfig) error
	Ports                    func(context.Context, string, string) (PortConfig, error)
	UpdatePorts              func(context.Context, string, string, PortConfig) error
	UpdateResources          func(context.Context, string, string, ResourceConfig) error
	ListResourcePlans        func(context.Context, string) ([]client.Plan, error)
	Terminal                 func(context.Context, string, string, string, string, string) tea.ExecCommand
	EgressRoutes             func(context.Context, string) (map[string]client.ClusterEgress, error)
	Namespaces               func(context.Context, string) ([]client.Namespace, error)
	ListRegistries           func(context.Context, string) ([]client.Registry, error)
	ListRegistryRepositories func(context.Context, string, client.FlexID) ([]client.Repository, error)
	ListRegistryDigests      func(context.Context, string, client.FlexID, string) ([]client.Digest, error)
	DeleteRegistryRepository func(context.Context, string, client.FlexID, string) error
	DeleteRegistryDigests    func(context.Context, string, client.FlexID, string, []string) error
	DeleteRegistryTags       func(context.Context, string, client.FlexID, string, string, []string) error
	RestartApp               func(context.Context, string, string) error
	DeleteApp                func(context.Context, string, string) error
}

// RunBrowser opens the app browser using the caller's scoped clients.
func RunBrowser(ctx context.Context, options Options) error {
	options = gateWriteCallbacks(options)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &browser{tenant: options.Tenant, version: options.Version, mode: connectionLabelLive, profile: options.Profile, ctx: ctx,
		activityAlertStatusFilter: 1, activitySortAlertsDescending: true, activitySortNotificationsDescending: true,
		allowWrites: options.AllowWrites, localTenants: append([]string(nil), options.LocalTenants...),
		listApps: options.ListApps, logWrap: true, logTail: logDefaultTailSize}
	if options.GetApp != nil {
		m.getApp = options.GetApp
	}
	if options.Usage != nil {
		m.usage = options.Usage
	}
	m.listPVCs = options.ListPVCs
	m.usageHistory = options.UsageHistory
	m.fetchPods = options.Pods
	m.appLogs = options.AppLogs
	m.listBuilds, m.getBuild = options.ListBuilds, options.GetBuild
	m.deployToken = options.DeployToken
	m.updateGitBuildSettings = options.UpdateGitBuildSettings
	m.buildLatest, m.retryBuild, m.stopBuild = options.BuildLatest, options.RetryBuild, options.StopBuild
	m.environment, m.secretEnvs = options.Environment, options.SecretEnvs
	m.updatePlainEnv, m.updateSecretEnv = options.UpdatePlainEnv, options.UpdateSecretEnv
	m.domains, m.updateDomains = options.Domains, options.UpdateDomains
	m.ports, m.updatePorts = options.Ports, options.UpdatePorts
	m.updateResources = options.UpdateResources
	m.listResourcePlans = options.ListResourcePlans
	m.terminal = options.Terminal
	m.alerts, m.notifications = options.Alerts, options.Notifications
	m.listDatabases = options.ListDatabases
	m.getDatabase = options.GetDatabase
	m.listTickets = options.ListTickets
	if options.GetTicket != nil {
		m.getTicket = options.GetTicket
	}
	m.getTicketComments, m.getTicketCategories, m.getTicketPriorities = options.GetTicketComments, options.GetTicketCategories,
		options.GetTicketPriorities
	m.createTicket, m.replyTicket = options.CreateTicket, options.ReplyTicket
	m.egressRoutes = options.EgressRoutes
	if options.Namespaces != nil {
		m.listNamespaces = options.Namespaces
	}
	m.listRegistries, m.listRegistryRepositories, m.listRegistryDigests = options.ListRegistries,
		options.ListRegistryRepositories, options.ListRegistryDigests
	m.deleteRegistryRepository, m.deleteRegistryDigests, m.deleteRegistryTags = options.DeleteRegistryRepository,
		options.DeleteRegistryDigests, options.DeleteRegistryTags
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
func RunMockBrowser(ctx context.Context, profile MockProfile, version string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newMockBrowser(profile)
	m.version = version
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}
