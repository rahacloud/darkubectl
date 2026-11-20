package browser

import (
	"strconv"

	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) egressIPForApp(a client.App) string {
	switch m.egressState.status {
	case requestLoading, requestRefreshing:
		return m.spinnerGlyph()
	case requestFailed, requestUnavailable, requestEmpty, requestIdle:
		return "-"
	case requestReady, requestStale, requestPartial:
		break
	}
	clusterID := a.Namespace.Cluster.ID
	clusterKey := strconv.Itoa(clusterID)
	clusterRoutes, ok := m.egressData[clusterKey]
	if !ok {
		for _, candidate := range m.egressData {
			matches := clusterID != 0 && candidate.Cluster.ID == clusterID
			if clusterID == 0 {
				matches = candidate.Cluster.Name == a.Namespace.Cluster.Name
			}
			if matches {
				clusterRoutes, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return "-"
	}
	effectiveIP := clusterRoutes.Cluster.DefaultOutgoingIP
	var matchedRoute *client.EgressRoute
	for i := range clusterRoutes.Routes {
		route := &clusterRoutes.Routes[i]
		if !route.ApplyToAllNamespaces && egressRouteMatchesNamespace(*route, a.Namespace.ID) {
			matchedRoute = route
			break
		}
	}
	if matchedRoute == nil {
		for i := range clusterRoutes.Routes {
			route := &clusterRoutes.Routes[i]
			if route.ApplyToAllNamespaces {
				break
			}
		}
	}
	if matchedRoute != nil && matchedRoute.EgressIP != "" {
		effectiveIP = matchedRoute.EgressIP
	}
	return displayValue(effectiveIP)
}

func egressRouteMatchesNamespace(route client.EgressRoute, namespaceID int) bool {
	if route.ApplyToAllNamespaces {
		return true
	}
	for _, selected := range route.SelectedNamespaces {
		if string(selected) == strconv.Itoa(namespaceID) {
			return true
		}
	}
	return false
}
