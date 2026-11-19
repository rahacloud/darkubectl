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
