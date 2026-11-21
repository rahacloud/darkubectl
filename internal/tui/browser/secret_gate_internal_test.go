package browser

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestSecretPermissionModes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                     string
		secrets, writes, allowed bool
	}{
		{"default", false, false, false},
		{"secrets", true, false, true},
		{"writes", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			opts := gateSecretCallbacks(Options{
				AllowSecrets: tc.secrets, AllowWrites: tc.writes,
				SecretEnvs:  func(context.Context, string, string) ([]client.EnvVar, error) { calls++; return nil, nil },
				DeployToken: func(context.Context, string, string) (string, error) { calls++; return "token", nil },
			})
			if opts.AllowSecrets != tc.allowed || (opts.SecretEnvs != nil) != tc.allowed || (opts.DeployToken != nil) != tc.allowed {
				t.Fatal("incorrect secret callback permissions")
			}
			m := &browser{ctx: context.Background(), allowSecrets: opts.AllowSecrets, detail: true, activeAppView: appViewBuilds,
				apps: []client.App{{ID: "app", BuildMethod: client.BuildMethodWebhook}}, deployToken: opts.DeployToken,
				secretEnvs: opts.SecretEnvs, envSecretNames: []string{"SECRET"}}
			if cmd := m.requestCICDToken(false); (cmd != nil) != tc.allowed {
				t.Fatal("deploy token fetch does not match permission")
			}
			if cmd := m.revealSelectedSecret(); (cmd != nil) != tc.allowed {
				t.Fatal("secret reveal does not match permission")
			}
			if cmd := m.copySelectedSecret(); (cmd != nil) != tc.allowed {
				t.Fatal("secret copy does not match permission")
			}
			m.cicdSettingsLoaded = true
			m.cicdSettingsDraft.Args = []client.GitBuildArg{{Key: "ARG", Value: "value"}}
			m.cicdSettingsCursor = cicdSettingsFields
			m.handleKeyPressBuildPart0(tea.KeyPressMsg{Code: 'v', Text: "v"})
			if (m.cicdRevealKey == "ARG") != tc.allowed {
				t.Fatal("build argument reveal does not match permission")
			}
			if calls != 0 {
				t.Fatal("callbacks ran before their commands were executed")
			}
		})
	}
}

func TestSecretPermissionBlocksCachedToken(t *testing.T) {
	t.Parallel()
	m := &browser{detail: true, activeAppView: appViewBuilds, apps: []client.App{{ID: "app"}}, cicdToken: "token"}
	if m.requestCICDToken(true) != nil {
		t.Fatal("copying a cached token should be blocked")
	}
	m.handleKeyPressBuildPart0(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if m.cicdTokenReveal {
		t.Fatal("revealing a cached token should be blocked")
	}
}

func TestOpeningBuildsDoesNotFetchTokenWithoutPermission(t *testing.T) {
	t.Parallel()
	calls := 0
	m := &browser{ctx: context.Background(), apps: []client.App{{ID: "app", BuildMethod: client.BuildMethodWebhook}},
		deployToken: func(context.Context, string, string) (string, error) { calls++; return "token", nil }}
	cmd := m.loadBuildsView()
	if cmd != nil {
		cmd()
	}
	if calls != 0 || m.cicdLoading {
		t.Fatal("opening builds requested a deploy token without permission")
	}
}

func TestSecretAccessNoticeOnlyInPaneBorder(t *testing.T) {
	t.Parallel()
	for _, view := range []appView{appViewEnv, appViewBuilds} {
		m := &browser{detail: true, activeAppView: view, apps: []client.App{{ID: "app", BuildMethod: client.BuildMethodWebhook}},
			envMessage: secretAccessDisabled, cicdNotice: secretAccessDisabled}
		if got := m.currentAppPaneMessage(); got != secretAccessDisabled {
			t.Fatalf("view %v: border message = %q", view, got)
		}
		if strings.Contains(strings.Join(m.summaryPaneLines(), "\n"), secretAccessDisabled) {
			t.Fatalf("view %v: notice duplicated in pane body", view)
		}
	}
}
