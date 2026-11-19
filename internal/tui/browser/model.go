package browser

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"time"
)

const browserKeyEscape = "esc"

const (
	keyLeft                  = "left"
	keyRight                 = "right"
	keyUp                    = "up"
	keyDown                  = "down"
	keyEnter                 = "enter"
	keyBackspace             = "backspace"
	keyCtrlU                 = "ctrl+u"
	logUnknownState          = "Unknown"
	appViewPageHeightPadding = 8
	appViewsCount            = 8 // includes the legacy scaling index, hidden inside Resources
	logRetryExponentCap      = 3
	keyCtrlH                 = "ctrl+h"
	helpModalMaxWidth        = 96
	browserModalMargin       = 4
)

type appView int

const (
	appViewSummary appView = iota
	appViewUsage
	appViewScaling
	appViewLogs
	appViewBuilds
	appViewEnv
	appViewDomains
	appViewPorts
)

type appSort int

const (
	sortName appSort = iota
	sortNamespace
	sortState
	sortUpdated
)

var appSortNames = [...]string{"Name", "Namespace", "State", "Last updated"}

type browserPage uint8

const (
	pageApps browserPage = iota
	pageActivity
	pageDB
	pageRegistry
)

type dbSort uint8

const (
	dbSortName dbSort = iota
	dbSortEngine
	dbSortStatus
)

var dbSortNames = [...]string{"Name", "Engine", "Status"}

type dbConnectionRole uint8

const (
	dbConnectionMaster dbConnectionRole = iota
	dbConnectionStandby
)

type activityTab uint8

const (
	activityAlerts activityTab = iota
	activityNotifications
)

type activitySort uint8

const (
	activitySortTime activitySort = iota
	activitySortSeverity
	activitySortStatus
	activitySortName
)

type spinnerMsg struct{}

type tenantOption struct {
	name   string
	source string
	status string
}

type keyHint struct {
	key    string
	action string
}

type requestKind uint8

const (
	requestProfile requestKind = iota
	requestApps
	requestNamespaces
	requestSummary
	requestUsage
	requestUsageHistory
	requestEgress
	requestPods
	requestLogs
	requestEnv
	requestBuilds
	requestBuild
	requestDomains
	requestPorts
	requestAlerts
	requestNotifications
	requestDatabases
	requestDatabaseDetail
	requestRegistry
)

type requestKey struct {
	kind  requestKind
	appID string
}

type inFlightRequest struct {
	meta   requestMeta
	cancel context.CancelFunc
}

type namespaceKey struct {
	id   int
	name string
}

type namespaceChoice struct {
	key    namespaceKey
	scoped bool
}

type treeNode struct {
	app     int
	key     namespaceKey
	heading string
	last    bool
}

type browser struct {
	page                                     browserPage
	listDatabases                            func(context.Context, string) ([]client.Database, error)
	databases                                []client.Database
	databaseState                            requestState
	getDatabase                              func(context.Context, string, string) (*client.DatabaseDetail, error)
	databaseDetail                           *client.DatabaseDetail
	databaseDetailState                      requestState
	databaseDetailTarget                     string
	databaseConnectionRole                   dbConnectionRole
	databaseNotice                           string
	databaseCursor                           int
	databaseSelected                         string
	databaseFilter                           string
	databaseFilterBefore                     string
	databaseFilterEditing                    bool
	databaseSortBy                           dbSort
	databaseSortDescending                   bool
	databaseScroll                           int
	listRegistries                           func(context.Context, string) ([]client.Registry, error)
	listRegistryRepositories                 func(context.Context, string, client.FlexID) ([]client.Repository, error)
	listRegistryDigests                      func(context.Context, string, client.FlexID, string) ([]client.Digest, error)
	deleteRegistryRepository                 func(context.Context, string, client.FlexID, string) error
	deleteRegistryDigests                    func(context.Context, string, client.FlexID, string, []string) error
	deleteRegistryTags                       func(context.Context, string, client.FlexID, string, string, []string) error
	registryState                            requestState
	registries                               []client.Registry
	registryRepos                            map[string][]client.Repository
	registryDigests                          map[string][]client.Digest
	registryDigestErrors                     map[string]error
	registryCursor                           int
	registryRows                             []registryTreeRow
	registryConfirm                          bool
	registryConfirmText                      string
	registryNotice                           string
	registryBusy                             bool
	registryTagMode                          bool
	registryTagCursor                        int
	registryDeleteTag                        bool
	registryTagCopy                          bool
	registryCollapsed                        map[string]bool
	tenant                                   string
	mode                                     string
	listApps                                 func(context.Context, string) ([]client.App, error)
	getApp                                   func(context.Context, string, string) (*client.App, error)
	usage                                    func(context.Context, string, string) (*AppUsage, error)
	usageHistory                             func(context.Context, string, string, time.Duration, time.Duration) (*UsageHistory, error)
	fetchPods                                func(context.Context, string, string) ([]appstate.Pod, error)
	appLogs                                  func(context.Context, string, string, client.LogOptions) ([]client.LogEntry, error)
	listBuilds                               func(context.Context, string, string, int) ([]client.Build, error)
	getBuild                                 func(context.Context, string, client.BuildID) (*client.Build, error)
	deployToken                              func(context.Context, string, string) (string, error)
	updateGitBuildSettings                   func(context.Context, string, string, client.GitBuildSettings, client.GitBuildSettings) error
	cicdToken, cicdNotice                    string
	cicdReveal, cicdTokenReveal, cicdLoading bool
	cicdGeneration                           uint64
	cicdSettingsOriginal                     client.GitBuildSettings
	cicdSettingsDraft                        client.GitBuildSettings
	cicdSettingsCursor                       int
	cicdEditField                            int
	cicdEditValue                            string
	cicdEditKey                              string
	cicdEditArg                              bool
	cicdEditAdding                           bool
	cicdEditing                              bool
	cicdReview                               bool
	cicdSaving                               bool
	cicdSettingsLoaded                       bool
	cicdSettingsNotice                       string
	cicdHistoryFocus                         bool
	cicdCursorLine                           int
	buildLatest                              func(context.Context, string, string) error
	retryBuild                               func(context.Context, string, client.BuildID) (*client.Build, error)
	stopBuild                                func(context.Context, string, client.BuildID) error
	environment                              func(context.Context, string, string) ([]client.EnvVar, []string, error)
	secretEnvs                               func(context.Context, string, string) ([]client.EnvVar, error)
	updatePlainEnv                           func(context.Context, string, string, []client.EnvVar) error
	updateSecretEnv                          func(context.Context, string, string, []client.EnvVar) error
	domains                                  func(context.Context, string, string) (DomainConfig, error)
	updateDomains                            func(context.Context, string, string, DomainConfig) error
	ports                                    func(context.Context, string, string) (PortConfig, error)
	updatePorts                              func(context.Context, string, string, PortConfig) error
	terminal                                 func(context.Context, string, string, string, string, string) tea.ExecCommand
	egressRoutes                             func(context.Context, string) (map[string]client.ClusterEgress, error)
	listNamespaces                           func(context.Context, string) ([]client.Namespace, error)
	profile                                  func(context.Context) (*client.Profile, error)
	alerts                                   func(context.Context, string) ([]client.Alert, error)
	notifications                            func(context.Context) ([]client.Notification, error)
	ctx                                      context.Context
	now                                      func() time.Time
	nextRequestID                            uint64
	tenantGeneration                         uint64
	selectionGeneration                      uint64
	inFlight                                 map[requestKey]inFlightRequest
	localTenants                             []string
	tenants                                  []tenantOption
	tenantCursor                             int
	selectingTenant                          bool
	contextFocused                           bool
	contextRow                               int
	activityTab                              activityTab
	activityAlertsData                       []client.Alert
	activityAlertsState                      requestState
	activityAlertCursor                      int
	activityAlertSelected                    string
	activityAlertFilter                      string
	activityAlertStatusFilter                int
	activityNotificationsData                []client.Notification
	activityNotificationsState               requestState
	activityNotificationCursor               int
	activityNotificationSelected             int
	activityNotificationFilter               string
	activityNotificationUnreadFilter         int
	activityFilterEditing                    bool
	activityFilterBefore                     string
	activitySortAlerts                       activitySort
	activitySortNotifications                activitySort
	activitySortAlertsDescending             bool
	activitySortNotificationsDescending      bool
	activityScroll                           int
	activityDetailScroll                     int
	allowWrites                              bool
	actionMenu                               bool
	actionCursor                             int
	actionMenuMessage                        string
	confirmAction                            appAction
	confirmButton                            int
	confirmName                              string
	confirmError                             string
	actionBusy                               bool
	actionNotice                             string
	actionTargetTenant                       string
	actionTargetID                           string
	actionTargetName                         string
	restartApp                               func(string, string) error
	deleteApp                                func(string, string) error
	mockDeleteApp                            func(string, string)
	profileState                             requestState
	tenantStatus                             string
	apps                                     []client.App
	appsState                                requestState
	selected                                 int
	namespaceSelected                        bool
	namespaceScope                           namespaceKey
	namespaceScoped                          bool
	namespaces                               []client.Namespace
	namespaceFallback                        bool
	namespaceState                           requestState
	selectingNamespace                       bool
	namespaceCursor                          int
	namespaceQuery                           string
	filterQuery                              string
	filterBeforeEdit                         string
	filterEditing                            bool
	filterSelection                          string
	collapsed                                map[namespaceKey]bool
	detail                                   bool
	activeAppView                            appView
	detailState                              requestState
	detailApp                                *client.App
	detailUsage                              *AppUsage
	usageAppID                               string
	usageState                               requestState
	historyState                             requestState
	podsState                                requestState
	podList                                  []appstate.Pod
	egressState                              requestState
	egressData                               map[string]client.ClusterEgress
	history                                  *UsageHistory
	historyRange                             int
	historyMetric                            int
	historyPod                               string
	historyPodChosen                         bool
	usageScroll                              int
	summaryScroll                            int
	logEntries                               []client.LogEntry
	logState                                 requestState
	logPod                                   string
	logContainer                             string
	logPrevious                              bool
	logFollowing                             bool
	logFollowPausedByUser                    bool
	logPollInFlight                          bool
	logRetryCount                            int
	logRetryDelay                            int
	logTargetPicker                          bool
	logPickerColumn                          int
	logPodCursor                             int
	logContainerCursor                       int
	logScroll                                int
	logNewEntries                            int
	logNotice                                string
	logSearch                                string
	logSearchBefore                          string
	logSearchEditing                         bool
	logMatch                                 int
	logWrap                                  bool
	logUTC                                   bool
	logMaximized                             bool
	logTail                                  int
	logCopyStatus                            string
	builds                                   []client.Build
	buildsState                              requestState
	buildSelectedID                          client.BuildID
	buildListCursor                          int
	buildLastClickRow                        int
	buildLastClickAt                         time.Time
	buildDetail                              *client.Build
	buildDetailState                         requestState
	buildScroll                              int
	buildListScroll                          int
	buildSearch                              string
	buildSearchBefore                        string
	buildSearchEditing                       bool
	buildMatch                               int
	buildCopyStatus                          string
	buildNotice                              string
	buildActionMenu                          bool
	buildActionCursor                        int
	buildConfirm                             buildAction
	buildConfirmButton                       int
	buildConfirmTenant                       string
	buildConfirmAppID                        string
	buildConfirmAppName                      string
	buildConfirmBuildID                      client.BuildID
	buildActionBusy                          bool
	buildActionNotice                        string
	buildActionAwaiting                      buildAction
	buildActionAwaitingID                    client.BuildID
	buildActionRetryID                       client.BuildID
	envPlain                                 []client.EnvVar
	envSecretNames                           []string
	envState                                 requestState
	envCursor                                int
	envCursorLine                            int
	envEditMode                              uint8
	envEditName                              string
	envEditValue                             string
	envMessage                               string
	envBusy                                  bool
	envRevealName                            string
	envRevealValue                           string
	envConfirm                               bool
	envOriginalPlain                         []client.EnvVar
	envOriginalSecretNames                   []string
	envSecretValues                          map[string]string
	envDeletedPlain                          map[string]bool
	envDeletedSecrets                        map[string]bool
	envMaximized                             bool
	domainConfig                             DomainConfig
	domainOriginal                           DomainConfig
	domainState                              requestState
	domainCursor                             int
	domainCursorLine                         int
	domainEditing                            bool
	domainEditSubdomain                      bool
	domainConfirm                            bool
	domainInput                              string
	domainMessage                            string
	domainBusy                               bool
	domainMaximized                          bool
	portConfig                               PortConfig
	portOriginal                             PortConfig
	portState                                requestState
	portCursorRow                            int
	portCursorCol                            int
	portCursorLine                           int
	portEditing                              bool
	portEditValue                            string
	portConfirm                              bool
	portBusy                                 bool
	portMessage                              string
	portMaximized                            bool
	resourceDraft                            ResourceConfig
	resourceOriginal                         ResourceConfig
	resourceCursor                           int
	resourceEditing                          bool
	resourceEditValue                        string
	resourceReview                           bool
	resourceBusy                             bool
	resourceAwaitingRefresh                  bool
	resourceMessage                          string
	updateResources                          func(context.Context, string, string, ResourceConfig) error
	listResourcePlans                        func(context.Context, string) ([]client.Plan, error)
	resourcePlans                            []client.Plan
	resourcePlanPicker                       bool
	resourcePlanCursor                       int
	resourcePlanErr                          string
	terminalPod                              string
	terminalContainer                        string
	terminalChoosing                         bool
	terminalRequestPending                   bool
	sorting                                  bool
	sortChoice                               int
	sortBy                                   appSort
	sortDescending                           bool
	help                                     bool
	spinnerFrame                             int
	width                                    int
	height                                   int
}
