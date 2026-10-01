package cmd

import "testing"

// ram_limit arrives as the API writes it, with decimal suffixes; Kubernetes
// binary suffixes are accepted too.
func TestParseBytes(t *testing.T) {
	t.Parallel()

	good := map[string]float64{"500M": 500e6, "1G": 1e9, "512Mi": 512 << 20, "2Gi": 2 << 30, "1024": 1024}
	for in, want := range good {
		if got, ok := parseBytes(in); !ok || got != want {
			t.Errorf("parseBytes(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "M", "abc", "0", "-1G"} {
		if _, ok := parseBytes(in); ok {
			t.Errorf("parseBytes(%q) accepted", in)
		}
	}
}

func TestTopFormatting(t *testing.T) {
	t.Parallel()

	if got := millicores(0.2504); got != "250m" {
		t.Errorf("millicores = %q", got)
	}
	if got := ofLimit(250e6, 500e6); got != "50%" {
		t.Errorf("ofLimit = %q", got)
	}
	if got := ofLimit(1, 0); got != "-" {
		t.Errorf("ofLimit with no limit = %q", got)
	}
	v := 0.123
	if got := ratio(&v); got != "12%" {
		t.Errorf("ratio = %q", got)
	}
	if got := ratio(nil); got != "-" {
		t.Errorf("ratio(nil) = %q", got)
	}
}

func TestSortUsage(t *testing.T) {
	t.Parallel()

	u := []appUsage{{App: "b", RAMBytes: 1, CPUCores: 3}, {App: "a", RAMBytes: 5, CPUCores: 1}, {App: "c", RAMBytes: 5}}
	sortUsage(u, sortByMemory)
	if u[0].App != "a" || u[1].App != "c" || u[2].App != "b" {
		t.Errorf("by memory: %v", u)
	}
	sortUsage(u, sortByCPU)
	if u[0].App != "b" {
		t.Errorf("by cpu: %v", u)
	}
	sortUsage(u, sortByName)
	if u[0].App != "a" || u[2].App != "c" {
		t.Errorf("by name: %v", u)
	}
}
