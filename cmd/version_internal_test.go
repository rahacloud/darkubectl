package cmd

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	t.Parallel()

	module := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/rahacloud/darkubectl", Version: v}}
	}

	tests := []struct {
		name   string
		linked string
		info   *debug.BuildInfo
		want   string
	}{
		{name: "release build", linked: "0.15.0", info: module("(devel)"), want: "0.15.0"},
		{name: "linker wins over module", linked: "0.15.0", info: module("v0.14.0"), want: "0.15.0"},
		{name: "go install of a tag", info: module("v0.15.0"), want: "0.15.0"},
		{
			name: "go install of a commit",
			info: module("v0.15.1-0.20261119070956-0e99d19a1b2c"),
			want: "0.15.1-0.20261119070956-0e99d19a1b2c",
		},
		{name: "dirty checkout", info: module("v0.15.0+dirty"), want: "0.15.0+dirty"},
		{name: "devel", info: module("(devel)"), want: "dev"},
		{name: "empty module version", info: module(""), want: "dev"},
		{name: "no build info", info: nil, want: "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveVersion(tt.linked, tt.info); got != tt.want {
				t.Errorf("resolveVersion(%q) = %q, want %q", tt.linked, got, tt.want)
			}
		})
	}
}

// A test binary carries no module version and no linker flag, so the fallback
// is what the real lookup must give here.
func TestBuildVersionFallsBackInTests(t *testing.T) {
	t.Parallel()

	if got := buildVersion(); got != devVersion {
		t.Errorf("buildVersion() = %q, want %q", got, devVersion)
	}
}
