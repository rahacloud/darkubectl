package client

import (
	"errors"
	"testing"
)

// The cluster id is not shown anywhere a person normally looks -- `get
// namespaces` prints the cluster's *name* -- so a reference has to accept both
// and say clearly when it matches nothing.
func fixture() []Namespace {
	return []Namespace{
		{ID: 62000, Name: "paradisehub-product", Cluster: Cluster{ID: 46, Name: "hamravesh-c11"}},
		{ID: 61997, Name: "paradisehub-product", Cluster: Cluster{ID: 65, Name: "hamravesh-c13"}},
	}
}

func TestResolveClusterAcceptsAName(t *testing.T) {
	t.Parallel()

	got, err := ResolveCluster("hamravesh-c11", fixture())
	if err != nil || got != 46 {
		t.Fatalf("want 46, got %d (err %v)", got, err)
	}
}

func TestResolveClusterAcceptsANumericID(t *testing.T) {
	t.Parallel()

	// An id need not be one the tenant already has a namespace on.
	got, err := ResolveCluster("99", fixture())
	if err != nil || got != 99 {
		t.Fatalf("want 99, got %d (err %v)", got, err)
	}
}

func TestResolveClusterRejectsTheUnknown(t *testing.T) {
	t.Parallel()

	if _, err := ResolveCluster("hamravesh-c99", fixture()); !errors.Is(err, ErrNoSuchCluster) {
		t.Errorf("want ErrNoSuchCluster, got %v", err)
	}
	if _, err := ResolveCluster("", nil); !errors.Is(err, ErrNoSuchCluster) {
		t.Errorf("empty reference must be rejected, got %v", err)
	}
}
