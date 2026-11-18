package browser

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestEgressIPPrefersNamespaceRouteThenGlobalThenClusterDefault(t *testing.T) {
	t.Parallel()
	app := client.App{Namespace: client.Namespace{ID: 5, Cluster: client.Cluster{ID: 2, Name: "c11"}}}
	m := &browser{
		egressState: requestState{status: requestReady},
		egressData: map[string]client.ClusterEgress{
			"2": {
				Cluster:           client.Cluster{ID: 2, Name: "c11"},
				DefaultOutgoingIP: "192.0.2.1",
				Routes: []client.EgressRoute{
					{ApplyToAllNamespaces: true, EgressIP: "192.0.2.2"},
					{SelectedNamespaces: []client.FlexID{"5"}, EgressIP: "192.0.2.3"},
				},
			},
		},
	}
	if got := m.egressIPForApp(app); got != "192.0.2.3" {
		t.Fatalf("namespace route = %q, want %q", got, "192.0.2.3")
	}
	app.Namespace.ID = 6
	if got := m.egressIPForApp(app); got != "192.0.2.2" {
		t.Fatalf("all-namespace route = %q, want %q", got, "192.0.2.2")
	}
	m.egressData["2"] = client.ClusterEgress{Cluster: client.Cluster{ID: 2}, DefaultOutgoingIP: "192.0.2.1"}
	if got := m.egressIPForApp(app); got != "192.0.2.1" {
		t.Fatalf("cluster default = %q, want %q", got, "192.0.2.1")
	}
}
