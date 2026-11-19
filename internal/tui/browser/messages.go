package browser

import (
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
)

type appsResult struct {
	meta requestMeta
	apps []client.App
	err  error
}

type profileResult struct {
	meta    requestMeta
	profile *client.Profile
	err     error
}

type appDetailResult struct {
	meta requestMeta
	app  *client.App
	err  error
}

type appUsageResult struct {
	meta  requestMeta
	usage *AppUsage
	err   error
}

type usageHistoryResult struct {
	meta    requestMeta
	history *UsageHistory
	err     error
}

type egressRoutesResult struct {
	meta   requestMeta
	routes map[string]client.ClusterEgress
	err    error
}

type podsResult struct {
	meta requestMeta
	pods []appstate.Pod
	err  error
}

type logsResult struct {
	meta      requestMeta
	entries   []client.LogEntry
	err       error
	previous  bool
	pod       string
	container string
}

type buildsResult struct {
	meta   requestMeta
	builds []client.Build
	err    error
}

type buildResult struct {
	meta  requestMeta
	id    client.BuildID
	build *client.Build
	err   error
}

type buildCopyResult struct {
	bytes int
	err   error
}

type cicdTokenResult struct {
	tenant, appID, token string
	generation           uint64
	copy, reveal         bool
	err                  error
}
type cicdCopyResult struct {
	tenant, appID string
	err           error
}

type buildAction uint8

const (
	buildActionNone buildAction = iota
	buildActionLatest
	buildActionRetry
	buildActionStop
)

type buildActionResult struct {
	tenant, appID string
	buildID       client.BuildID
	action        buildAction
	retryID       client.BuildID
	mock          bool
	err           error
}
type logsPollMsg struct {
	appID, pod, container string
	previous              bool
}

type logCopyResult struct {
	count int
	err   error
}

type appAction uint8

const (
	appActionNone appAction = iota
	appActionRestart
	appActionDelete
	appActionTerminal
)

type appActionResult struct {
	tenant string
	id     string
	name   string
	action appAction
	mock   bool
	err    error
}

type namespacesResult struct {
	meta       requestMeta
	namespaces []client.Namespace
	err        error
}

type alertsResult struct {
	meta   requestMeta
	alerts []client.Alert
	err    error
}

type notificationsResult struct {
	meta          requestMeta
	notifications []client.Notification
	err           error
}

type databaseDetailResult struct {
	meta   requestMeta
	detail *client.DatabaseDetail
	err    error
}

type databaseCopyResult struct {
	tenant, databaseID string
	err                error
}
