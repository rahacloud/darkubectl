package cmd

import (
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestBuildDuration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		b    client.Build
		want string
	}{
		{client.Build{CreationTime: "2026-09-30T10:00:00Z", EndTime: "2026-09-30T10:03:05Z"}, "3m5s"},
		{client.Build{CreationTime: "2026-09-30T10:00:00+03:30", EndTime: "2026-09-30T06:31:00Z"}, "1m0s"},
		{client.Build{CreationTime: ""}, "-"},
		{client.Build{CreationTime: "2026-09-30T10:00:00Z", EndTime: "2026-09-30T09:00:00Z"}, "-"},
	}
	for _, tc := range cases {
		if got := buildDuration(tc.b); got != tc.want {
			t.Errorf("buildDuration(%+v) = %q, want %q", tc.b, got, tc.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	t.Parallel()

	if got := firstLine("fix: things\n\nlonger body"); got != "fix: things" {
		t.Errorf("firstLine = %q", got)
	}
	long := strings.Repeat("é", maxCommitMessage+10)
	if got := []rune(firstLine(long)); len(got) != maxCommitMessage || got[len(got)-1] != '…' {
		t.Errorf("firstLine did not cut to %d runes: %d", maxCommitMessage, len(got))
	}
}

func TestRequireGitApp(t *testing.T) {
	t.Parallel()

	if err := requireGitApp(&client.App{Name: "a", CreationMethod: client.CreationMethodGitRepoURL}); err != nil {
		t.Errorf("git app rejected: %v", err)
	}
	if err := requireGitApp(&client.App{Name: "a", CreationMethod: client.CreationMethodDockerImage}); err == nil {
		t.Error("docker-image app accepted")
	}
}
