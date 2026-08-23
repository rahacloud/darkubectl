package cmd

import (
	"errors"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestApplyDomainChangeAddsRemovesAndDeduplicates(t *testing.T) {
	t.Parallel()

	app := map[string]any{"external_hosts": []any{"keep.example.com", "drop.example.com"}}

	err := applyDomainChange(app,
		[]string{"new.example.com", "keep.example.com"}, []string{"drop.example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := client.ExternalHosts(app)
	want := []string{"keep.example.com", "new.example.com"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("host %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestApplyDomainChangeRejectsUnknownRemoval(t *testing.T) {
	t.Parallel()

	app := map[string]any{"external_hosts": []any{"a.example.com"}}
	err := applyDomainChange(app, nil, []string{"b.example.com"})
	if !errors.Is(err, client.ErrNoSuchHost) {
		t.Errorf("want ErrNoSuchHost, got %v", err)
	}
}
