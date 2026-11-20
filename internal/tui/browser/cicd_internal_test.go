package browser

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestCICDModeGatesBuildHistory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method      string
		wantHistory bool
	}{
		{client.BuildMethodWebhook, true}, {client.BuildMethodGitlabCI, false}, {"unknown", false},
	} {
		m := &browser{apps: []client.App{{ID: "app", BuildMethod: tc.method}}, width: 120}
		m.builds = []client.Build{{CommitMessage: "sample commit"}}
		text := ansi.Strip(strings.Join(m.buildsPaneLines(), "\n"))
		if got := strings.Contains(text, "sample commit"); got != tc.wantHistory {
			t.Errorf("method %q: history = %v", tc.method, got)
		}
		if tc.method != client.BuildMethodWebhook && m.loadSelectedBuilds() != nil {
			t.Errorf("method %q requested builds", tc.method)
		}
	}
}

func TestBuildArgumentsMaskedUntilRevealed(t *testing.T) {
	t.Parallel()
	app := client.App{ID: "app", BuildMethod: client.BuildMethodWebhook,
		GitBuildArgs: json.RawMessage(`[{"name":"API_KEY","value":"secret-value"}]`)}
	m := &browser{apps: []client.App{app}}
	text := ansi.Strip(strings.Join(m.cicdDarkubeInfo(app), "\n"))
	if !strings.Contains(text, "API_KEY") || strings.Contains(text, "secret-value") {
		t.Fatalf("unexpected masked output: %q", text)
	}
	m.cicdReveal = true
	if text = ansi.Strip(strings.Join(m.cicdDarkubeInfo(app), "\n")); !strings.Contains(text, "secret-value") {
		t.Fatal("revealed value missing")
	}
}

func TestRepositoryCredentialsAreHidden(t *testing.T) {
	t.Parallel()
	got := redactRepoURL("https://user:secret@example.test/repo?token=secret")
	if strings.Contains(got, "secret") || got != "https://example.test/repo" {
		t.Fatalf("repository URL = %q", got)
	}
}
