package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiffAppsReportsOnlyChangedFields(t *testing.T) {
	t.Parallel()

	before := map[string]any{
		"name":     "api",
		"replicas": float64(1),
		"envs":     []any{map[string]any{"name": "A", "value": "1"}},
	}
	after := map[string]any{
		"name":     "api",
		"replicas": float64(3),
		"envs":     []any{map[string]any{"name": "A", "value": "1"}},
	}

	changes := diffApps(before, after)
	if len(changes) != 1 {
		t.Fatalf("changes = %+v, want exactly one", changes)
	}
	if changes[0].Key != "replicas" || changes[0].Before != "1" || changes[0].After != "3" {
		t.Errorf("change = %+v, want replicas 1 -> 3", changes[0])
	}
}

func TestDiffAppsDetectsAddedAndRemovedKeys(t *testing.T) {
	t.Parallel()

	changes := diffApps(
		map[string]any{"gone": "yes"},
		map[string]any{"added": "yes"},
	)
	got := map[string]fieldChange{}
	for _, c := range changes {
		got[c.Key] = c
	}
	if len(got) != 2 {
		t.Fatalf("changes = %+v, want two", changes)
	}
	if got["gone"].After != "null" {
		t.Errorf("removed key After = %q, want null", got["gone"].After)
	}
	if got["added"].Before != "null" {
		t.Errorf("added key Before = %q, want null", got["added"].Before)
	}
}

func TestDiffAppsIsStableForEqualNestedValues(t *testing.T) {
	t.Parallel()

	// Nested values are compared by their rendered form, so structurally equal
	// objects must not show up as a spurious change.
	nested := func() any {
		return map[string]any{"ports": map[string]any{"main": float64(9115)}}
	}
	if changes := diffApps(map[string]any{"svc": nested()}, map[string]any{"svc": nested()}); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

func TestTruncateReportsFullLength(t *testing.T) {
	t.Parallel()

	// App fields hold base64 blobs measured in kilobytes; the diff must not
	// scroll them off the screen.
	long := strings.Repeat("x", maxDiffValue*2)
	got := truncate(long)
	if len(got) >= len(long) {
		t.Errorf("truncate did not shorten a %d-byte value", len(long))
	}
	if !strings.Contains(got, "bytes)") {
		t.Errorf("truncate = %q, want it to report the full length", got)
	}
	short := "fits"
	if truncate(short) != short {
		t.Errorf("truncate(%q) = %q, want it unchanged", short, truncate(short))
	}
}

func TestWriteDiffSaysNothingChanged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeDiff(&buf, nil)
	if !strings.Contains(buf.String(), "no changes") {
		t.Errorf("output = %q, want a no-changes message", buf.String())
	}
}

func TestWriteDiffRendersBeforeAndAfter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeDiff(&buf, []fieldChange{{Key: "replicas", Before: "1", After: "3"}})
	out := buf.String()
	for _, want := range []string{"replicas:", "- 1", "+ 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}
