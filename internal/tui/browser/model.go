package browser

import (
	"context"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"time"
)

const browserKeyEscape = "esc"

type appView uint8

const (
	appViewSummary appView = iota
	appViewUsage
	appViewScaling
)

type appSort int

const (
	sortName appSort = iota
	sortNamespace
	sortState
	sortUpdated
)

var appSortNames = [...]string{"Name", "Namespace", "State", "Last updated"}

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
	tenant              string
	mode                string
	listApps            func(context.Context, string) ([]client.App, error)
	getApp              func(context.Context, string, string) (*client.App, error)
	usage               func(context.Context, string, string) (*AppUsage, error)
	usageHistory        func(context.Context, string, string, time.Duration, time.Duration) (*UsageHistory, error)
	fetchPods           func(context.Context, string, string) ([]appstate.Pod, error)
	egressRoutes        func(context.Context, string) (map[string]client.ClusterEgress, error)
	listNamespaces      func(context.Context, string) ([]client.Namespace, error)
	profile             func(context.Context) (*client.Profile, error)
	ctx                 context.Context
	now                 func() time.Time
	nextRequestID       uint64
	tenantGeneration    uint64
	selectionGeneration uint64
	inFlight            map[requestKey]inFlightRequest
	localTenants        []string
	tenants             []tenantOption
	tenantCursor        int
	selectingTenant     bool
	contextFocused      bool
	contextRow          int
	allowWrites         bool
	actionMenu          bool
	actionCursor        int
	actionMenuMessage   string
	confirmAction       appAction
	confirmButton       int
	confirmName         string
	confirmError        string
	actionBusy          bool
	actionNotice        string
	actionTargetTenant  string
	actionTargetID      string
	actionTargetName    string
	restartApp          func(string, string) error
	deleteApp           func(string, string) error
	mockDeleteApp       func(string, string)
	profileState        requestState
	tenantStatus        string
	apps                []client.App
	appsState           requestState
	selected            int
	namespaceSelected   bool
	namespaceScope      namespaceKey
	namespaceScoped     bool
	namespaces          []client.Namespace
	namespaceFallback   bool
	namespaceState      requestState
	selectingNamespace  bool
	namespaceCursor     int
	namespaceQuery      string
	filterQuery         string
	filterBeforeEdit    string
	filterEditing       bool
	filterSelection     string
	collapsed           map[namespaceKey]bool
	detail              bool
	activeAppView       appView
	detailState         requestState
	detailApp           *client.App
	detailUsage         *AppUsage
	usageAppID          string
	usageState          requestState
	historyState        requestState
	podsState           requestState
	podList             []appstate.Pod
	egressState         requestState
	egressData          map[string]client.ClusterEgress
	history             *UsageHistory
	historyRange        int
	historyMetric       int
	historyPod          string
	historyPodChosen    bool
	usageScroll         int
	summaryScroll       int
	sorting             bool
	sortChoice          int
	sortBy              appSort
	sortDescending      bool
	help                bool
	spinnerFrame        int
	width               int
	height              int
}
