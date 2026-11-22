package browser

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	m.cicdRevealKey, m.cicdSettingsCursor = "API_KEY", cicdSettingsFields
	if text = ansi.Strip(strings.Join(m.cicdDarkubeInfo(app), "\n")); !strings.Contains(text, "secret-value") {
		t.Fatal("revealed value missing")
	}
}

func TestBuildArgumentChangeMarkers(t *testing.T) {
	t.Parallel()
	m := &browser{allowWrites: true}
	m.cicdSettingsOriginal.Args = []client.GitBuildArg{
		{Key: "UNCHANGED", Value: "same"}, {Key: "CHANGED", Value: "old-secret"}, {Key: "REMOVED", Value: "removed-secret"},
	}
	args := []client.GitBuildArg{
		{Key: "UNCHANGED", Value: "same"}, {Key: "CHANGED", Value: "new-secret"}, {Key: "ADDED", Value: "added-secret"},
	}
	text := ansi.Strip(strings.Join(m.cicdDarkubeArgumentLines(args), "\n"))
	for _, want := range []string{"~  CHANGED", "+  ADDED", "-  REMOVED"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	for _, secret := range []string{"old-secret", "new-secret", "removed-secret", "added-secret"} {
		if strings.Contains(text, secret) {
			t.Errorf("argument value exposed: %q", secret)
		}
	}
}

func TestBuildArgumentColumnsAlign(t *testing.T) {
	t.Parallel()
	m := &browser{allowWrites: true, cicdSettingsCursor: cicdSettingsFields, width: 150}
	m.cicdSettingsDraft.Args = []client.GitBuildArg{
		{Key: "NEXT_PUBLIC_METRIX_TOKEN", Value: "value"},
		{Key: "NEXT_PUBLIC_METRIX_URL", Value: "other"},
	}
	m.cicdSettingsOriginal.Args = append([]client.GitBuildArg(nil), m.cicdSettingsDraft.Args...)
	lines := m.cicdDarkubeArgumentLines(m.cicdSettingsDraft.Args)
	header, argument, second, add := ansi.Strip(lines[0]), ansi.Strip(lines[1]), ansi.Strip(lines[2]), ansi.Strip(lines[3])
	column := func(line, value string) int {
		prefix, _, found := strings.Cut(line, value)
		if !found {
			return -1
		}
		return ansi.StringWidth(prefix)
	}
	keyColumn := column(header, "KEY")
	valueColumn := column(header, "VALUE")
	if column(argument, "NEXT_PUBLIC_METRIX_TOKEN") != keyColumn || column(second, "NEXT_PUBLIC_METRIX_URL") != keyColumn ||
		column(argument, maskedValueGlyphs) != valueColumn || column(add, "Add build argument") != keyColumn {
		t.Fatalf("misaligned columns:\n%s\n%s\n%s", header, argument, add)
	}
}

func TestBuildArgumentEditKeepsValueEndVisible(t *testing.T) {
	t.Parallel()
	const value = "https://api.example.com/a/very/long/path/endpoint"
	m := &browser{allowWrites: true, width: 150, cicdSettingsCursor: cicdSettingsFields,
		cicdEditing: true, cicdEditArg: true, cicdEditField: cicdSettingsFields, cicdEditValue: value}
	m.cicdSettingsDraft.Args = []client.GitBuildArg{{Key: "API_URL", Value: value}}
	line := ansi.Strip(m.cicdDarkubeArgumentLines(m.cicdSettingsDraft.Args)[1])
	if !strings.Contains(line, "/endpoint█") || !strings.Contains(line, "…") || strings.Contains(line, "https://api.example.com") {
		t.Fatalf("edited value end/cursor not visible: %q", line)
	}
}

func TestCICDSettingsCountEachArgumentChange(t *testing.T) {
	t.Parallel()
	m := &browser{}
	m.cicdSettingsOriginal.Args = []client.GitBuildArg{{Key: "EDITED", Value: "old"}, {Key: "REMOVED", Value: "gone"}}
	m.cicdSettingsDraft.Args = []client.GitBuildArg{{Key: "EDITED", Value: "new"}, {Key: "ADDED", Value: "fresh"}}
	if got := m.cicdSettingsChangeCount(); got != 3 {
		t.Fatalf("change count = %d, want 3", got)
	}
}

func TestCICDSettingsSummaryRowsAlign(t *testing.T) {
	t.Parallel()
	m := &browser{allowSecrets: true}
	app := client.App{ID: "app-id", BuildMethod: client.BuildMethodWebhook}
	lines := m.cicdDarkubeInfo(app)
	valueColumn := func(line, value string) int {
		prefix, _, found := strings.Cut(ansi.Strip(line), value)
		if !found {
			return -1
		}
		return ansi.StringWidth(prefix)
	}
	if valueColumn(lines[1], "Darkube") != valueColumn(lines[2], "app-id") ||
		valueColumn(lines[2], "app-id") != valueColumn(lines[3], "Unavailable") {
		t.Fatalf("summary value columns differ:\n%s\n%s\n%s", ansi.Strip(lines[1]), ansi.Strip(lines[2]), ansi.Strip(lines[3]))
	}
}

func TestCICDReviewShowsArgumentValuesOnlyWithSecretAccess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		secrets bool
		want    string
		avoid   string
	}{{"masked", false, maskedValueGlyphs, "new-secret"}, {"revealed", true, "new-secret", maskedValueGlyphs}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &browser{allowSecrets: tc.secrets, cicdReview: true}
			m.cicdSettingsOriginal.Args = []client.GitBuildArg{{Key: "API_URL", Value: "old-secret"}}
			m.cicdSettingsDraft.Args = []client.GitBuildArg{{Key: "API_URL", Value: "new-secret"}}
			text := ansi.Strip(strings.Join(m.cicdSettingsReviewLines(), "\n"))
			if !strings.Contains(text, tc.want) || strings.Contains(text, tc.avoid) {
				t.Fatalf("unexpected review values: %q", text)
			}
		})
	}
}

func TestBuildArgumentAddRendersKeyAndValueColumns(t *testing.T) {
	t.Parallel()
	m := &browser{allowWrites: true, cicdEditing: true, cicdEditAdding: true, cicdEditKey: "API_URL", cicdEditValue: "secret"}
	text := ansi.Strip(strings.Join(m.cicdDarkubeArgumentLines(nil), "\n"))
	for _, want := range []string{"KEY", "VALUE", "API_URL", "secret"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestBuildArgumentKeyEntryResizesToTypedKey(t *testing.T) {
	t.Parallel()
	const key = "NEXT_PUBLIC_METRIX_TOKEN"
	m := &browser{allowWrites: true, width: 150, cicdEditing: true, cicdEditAdding: true, cicdEditValue: key}
	lines := m.cicdDarkubeArgumentLines(nil)
	if got := ansi.Strip(lines[2]); !strings.Contains(got, key+"█") {
		t.Fatalf("typed key is clipped before completion: %q", got)
	}
	m.handleCICDEditingKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	lines = m.cicdDarkubeArgumentLines(nil)
	if got := ansi.Strip(lines[2]); !strings.Contains(got, key) {
		t.Fatalf("staged key is clipped while entering its value: %q", got)
	}
}

func TestCICDAddArgumentNavigationAndValueEditing(t *testing.T) {
	t.Parallel()
	m := &browser{allowWrites: true, cicdSettingsCursor: 7}
	m.moveCICDSettings(1)
	if m.cicdSettingsCursor != cicdSettingsFields || m.cicdSettingsRows()-1 != m.cicdSettingsCursor {
		t.Fatal("empty argument list does not select the add row")
	}
	m.handleCICDNavigationKey("enter")
	m.handleCICDEditingKey(tea.KeyPressMsg{Code: 'K', Text: "KEY"})
	m.handleCICDEditingKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m.handleCICDEditingKey(tea.KeyPressMsg{Code: 'v', Text: "value"})
	text := ansi.Strip(strings.Join(m.cicdDarkubeArgumentLines(nil), "\n"))
	if !strings.Contains(text, "KEY") || !strings.Contains(text, "value") {
		t.Fatalf("editing row does not show key and revealed value: %q", text)
	}
	m.handleCICDEditingKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.cicdSettingsDraft.Args) != 1 || m.cicdSettingsDraft.Args[0] != (client.GitBuildArg{Key: "KEY", Value: "value"}) {
		t.Fatalf("argument was not staged: %#v", m.cicdSettingsDraft.Args)
	}
}

func TestCICDRemovedArgumentCanBeSelectedRevealedAndRestored(t *testing.T) {
	t.Parallel()
	m := &browser{allowWrites: true}
	m.cicdSettingsOriginal.Args = []client.GitBuildArg{
		{Key: "FIRST", Value: "one"}, {Key: "REMOVED", Value: "secret"}, {Key: "LAST", Value: "three"},
	}
	m.cicdSettingsDraft.Args = []client.GitBuildArg{{Key: "FIRST", Value: "one"}, {Key: "LAST", Value: "three"}}
	m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args)
	m.moveCICDSettings(1)
	arg, ok := m.cicdSelectedArgument()
	if !ok || arg.Key != "REMOVED" {
		t.Fatal("removed argument is not selectable")
	}
	m.cicdRevealKey = arg.Key
	text := ansi.Strip(strings.Join(m.cicdDarkubeArgumentLines(m.cicdSettingsDraft.Args), "\n"))
	if !strings.Contains(text, "secret") {
		t.Fatal("selected removed value is not revealed")
	}
	m.handleCICDWriteKey("d")
	if !m.cicdSettingsOriginal.Equal(m.cicdSettingsDraft) {
		t.Fatalf("restore did not restore original ordering: %#v", m.cicdSettingsDraft.Args)
	}
}

func TestCICDSettingChangeMarkers(t *testing.T) {
	t.Parallel()
	on, off := true, false
	m := &browser{allowWrites: true, cicdSettingsLoaded: true}
	m.cicdSettingsOriginal = client.GitBuildSettings{BuildMethod: client.BuildMethodWebhook,
		RepoURL: "https://old.example/repo", Branch: "main", Dockerfile: "Dockerfile", Autodeploy: &on}
	m.cicdSettingsDraft = client.GitBuildSettings{BuildMethod: client.BuildMethodWebhook,
		RepoURL: "https://new.example/repo", Branch: "main", Context: "app", Autodeploy: &off}
	text := ansi.Strip(strings.Join(m.cicdDarkubeInfo(client.App{ID: "app"}), "\n"))
	for _, want := range []string{"~ Repository", "- Dockerfile path", "+ Build context", "~ Auto deploy"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "~ Branch") {
		t.Error("unchanged branch marked as modified")
	}
	m.cicdSettingsDraft.BuildMethod = client.BuildMethodGitlabCI
	text = ansi.Strip(strings.Join(m.customBuildLines(client.App{ID: "app"}, nil), "\n"))
	if !strings.Contains(text, "~ Deployment type") {
		t.Errorf("deployment type change missing in %q", text)
	}
}

func TestRepositoryCredentialsAreHidden(t *testing.T) {
	t.Parallel()
	got := redactRepoURL("https://user:secret@example.test/repo?token=secret")
	if strings.Contains(got, "secret") || got != "https://example.test/repo" {
		t.Fatalf("repository URL = %q", got)
	}
}
