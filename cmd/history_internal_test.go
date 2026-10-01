package cmd

import (
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestParseAgo(t *testing.T) {
	t.Parallel()

	good := map[string]time.Duration{
		"7d":  7 * 24 * time.Hour,
		"0d":  0,
		"36h": 36 * time.Hour,
		"90m": 90 * time.Minute,
	}
	for in, want := range good {
		got, err := parseAgo(in)
		if err != nil || got != want {
			t.Errorf("parseAgo(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "xd", "-1d", "-5h", "week"} {
		if _, err := parseAgo(in); err == nil {
			t.Errorf("parseAgo(%q) accepted", in)
		}
	}
}

// Values arrive JSON-encoded inside strings; they should render the way a
// pending write's diff renders them, and fall back to the raw text otherwise.
func TestHistoryValue(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`["a.example","b.example"]`: `["a.example","b.example"]`,
		`"nginx:1.27"`:              "nginx:1.27",
		`2`:                         "2",
		`null`:                      "null",
		`not json`:                  "not json",
	}
	for in, want := range cases {
		if got := historyValue(in); got != want {
			t.Errorf("historyValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHistoryFieldsDeduplicates(t *testing.T) {
	t.Parallel()

	e := client.HistoryEntry{Changes: []client.HistoryChange{{Field: "envs"}, {Field: "replicas"}, {Field: "envs"}}}
	got := historyFields(e)
	if len(got) != 2 || got[0] != "envs" || got[1] != "replicas" {
		t.Errorf("historyFields = %v", got)
	}
}
