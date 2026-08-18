package kube_test

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/kube"
)

// The shape that motivated this package: talaland-dev, where two Deployments
// outlived the apps that created them and one app had no workload at all.
func TestDiffFindsOrphanAndMissingWorkload(t *testing.T) {
	t.Parallel()

	apps := []kube.AppRef{
		{ID: "app-idp", Name: "idp-dev", Namespace: "talaland-dev"},
		{ID: "app-ghost", Name: "ghost-dev", Namespace: "talaland-dev"},
	}
	workloads := []kube.Workload{
		{Name: "idp-dev", Namespace: "talaland-dev", AppID: "app-idp"},
		{Name: "redis-dev", Namespace: "talaland-dev", AppID: "app-redis"},
	}

	got := kube.Diff(apps, workloads)
	if len(got) != 2 {
		t.Fatalf("want 2 divergences, got %d: %+v", len(got), got)
	}

	want := map[string]kube.Kind{
		"redis-dev": kube.Orphaned,
		"ghost-dev": kube.NoWorkload,
	}
	for _, d := range got {
		kind, ok := want[d.Name]
		if !ok {
			t.Errorf("unexpected divergence for %q", d.Name)
			continue
		}
		if d.Kind != kind {
			t.Errorf("%s: want kind %q, got %q", d.Name, kind, d.Kind)
		}
	}
}

// A healthy namespace must be silent, or the command is noise.
func TestDiffAgreementIsEmpty(t *testing.T) {
	t.Parallel()

	apps := []kube.AppRef{{ID: "a", Name: "one", Namespace: "ns"}}
	workloads := []kube.Workload{{Name: "one", Namespace: "ns", AppID: "a"}}

	if got := kube.Diff(apps, workloads); len(got) != 0 {
		t.Fatalf("want no divergence, got %+v", got)
	}
}

// Apps live across several clusters while one kubeconfig context reaches one of
// them, so an app in a namespace the cluster never returned must not be reported
// as missing.
func TestDiffIgnoresNamespacesNotInCluster(t *testing.T) {
	t.Parallel()

	apps := []kube.AppRef{
		{ID: "a", Name: "here", Namespace: "ns-a"},
		{ID: "b", Name: "elsewhere", Namespace: "ns-b"},
	}
	workloads := []kube.Workload{{Name: "here", Namespace: "ns-a", AppID: "a"}}

	if got := kube.Diff(apps, workloads); len(got) != 0 {
		t.Fatalf("want no divergence for an unseen namespace, got %+v", got)
	}
}

// Workloads that Darkube did not create carry no app-id label and are nobody's
// orphan; reporting them would drown the real finding in every other Deployment.
func TestDiffSkipsUnlabelledWorkloads(t *testing.T) {
	t.Parallel()

	workloads := []kube.Workload{{Name: "hand-rolled", Namespace: "ns", AppID: ""}}

	if got := kube.Diff(nil, workloads); len(got) != 0 {
		t.Fatalf("want unlabelled workloads ignored, got %+v", got)
	}
}

// Output order has to be stable for the table and for diffing two runs.
func TestDiffIsSortedByNamespaceThenKindThenName(t *testing.T) {
	t.Parallel()

	workloads := []kube.Workload{
		{Name: "zeta", Namespace: "ns-b", AppID: "z"},
		{Name: "alpha", Namespace: "ns-b", AppID: "a"},
		{Name: "mid", Namespace: "ns-a", AppID: "m"},
	}

	got := kube.Diff(nil, workloads)
	want := []string{"mid", "alpha", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("want %d divergences, got %d", len(want), len(got))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("position %d: want %q, got %q", i, name, got[i].Name)
		}
	}
}
