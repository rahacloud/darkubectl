package cmd

import (
	"testing"
	"time"
)

func TestAge(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name string
		when time.Time
		want string
	}{
		{"zero time", time.Time{}, "-"},
		{"in the future", now.Add(time.Hour), "-"},
		{"seconds", now.Add(-30 * time.Second), "30s"},
		{"minutes", now.Add(-90 * time.Minute), "1h"},
		{"hours", now.Add(-5 * time.Hour), "5h"},
		{"days", now.Add(-12 * 24 * time.Hour), "12d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := age(tc.when); got != tc.want {
				t.Errorf("age(%v) = %q, want %q", tc.when, got, tc.want)
			}
		})
	}
}

func TestAgeOf(t *testing.T) {
	t.Parallel()

	if got := ageOf(""); got != "-" {
		t.Errorf("ageOf(\"\") = %q, want \"-\"", got)
	}
	if got := ageOf("11/08/2026"); got != "-" {
		t.Errorf("ageOf on an unparsable timestamp = %q, want \"-\"", got)
	}
	stamp := time.Now().Add(-3 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if got := ageOf(stamp); got != "3d" {
		t.Errorf("ageOf(%q) = %q, want %q", stamp, got, "3d")
	}
}
