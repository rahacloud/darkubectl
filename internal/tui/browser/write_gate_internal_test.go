package browser

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

var errUnderlyingWriteCallback = errors.New("underlying write callback called")

func TestGateWriteCallbacksBlocksEveryLiveWriteAdapter(t *testing.T) {
	t.Parallel()
	var calls int
	called := func() error { calls++; return errUnderlyingWriteCallback }
	options := Options{
		CreateTicket: func(context.Context, string, client.NewTicket) (string, error) {
			calls++
			return "ticket", errUnderlyingWriteCallback
		},
		ReplyTicket: func(context.Context, string, string, string) error { return called() },
		UpdateGitBuildSettings: func(context.Context, string, string, client.GitBuildSettings, client.GitBuildSettings) error {
			return called()
		},
		BuildLatest: func(context.Context, string, string) error { return called() },
		RetryBuild: func(context.Context, string, client.BuildID) (*client.Build, error) {
			calls++
			return nil, errUnderlyingWriteCallback
		},
		StopBuild:       func(context.Context, string, client.BuildID) error { return called() },
		UpdatePlainEnv:  func(context.Context, string, string, []client.EnvVar) error { return called() },
		UpdateSecretEnv: func(context.Context, string, string, []client.EnvVar) error { return called() },
		UpdateDomains:   func(context.Context, string, string, DomainConfig) error { return called() },
		UpdatePorts:     func(context.Context, string, string, PortConfig) error { return called() },
		UpdateResources: func(context.Context, string, string, ResourceConfig) error {
			return called()
		},
		Terminal: func(context.Context, string, string, string, string, string) tea.ExecCommand {
			calls++
			return &mockTerminalCommand{}
		},
		DeleteRegistryRepository: func(context.Context, string, client.FlexID, string) error { return called() },
		DeleteRegistryDigests:    func(context.Context, string, client.FlexID, string, []string) error { return called() },
		DeleteRegistryTags:       func(context.Context, string, client.FlexID, string, string, []string) error { return called() },
		RestartApp:               func(context.Context, string, string) error { return called() },
		DeleteApp:                func(context.Context, string, string) error { return called() },
	}

	gated := gateWriteCallbacks(options)
	assertDenied := func(err error) {
		t.Helper()
		if !errors.Is(err, errWriteActionsAreDisabledRelaunchWithAllow) {
			t.Errorf("callback error = %v, want writes-disabled error", err)
		}
	}

	_, err := gated.CreateTicket(context.Background(), "tenant", client.NewTicket{})
	assertDenied(err)
	assertDenied(gated.ReplyTicket(context.Background(), "tenant", "id", "body"))
	assertDenied(gated.UpdateGitBuildSettings(context.Background(), "tenant", "app", client.GitBuildSettings{}, client.GitBuildSettings{}))
	assertDenied(gated.BuildLatest(context.Background(), "tenant", "app"))
	_, err = gated.RetryBuild(context.Background(), "tenant", client.BuildID("build"))
	assertDenied(err)
	assertDenied(gated.StopBuild(context.Background(), "tenant", client.BuildID("build")))
	assertDenied(gated.UpdatePlainEnv(context.Background(), "tenant", "app", nil))
	assertDenied(gated.UpdateSecretEnv(context.Background(), "tenant", "app", nil))
	assertDenied(gated.UpdateDomains(context.Background(), "tenant", "app", DomainConfig{}))
	assertDenied(gated.UpdatePorts(context.Background(), "tenant", "app", PortConfig{}))
	assertDenied(gated.UpdateResources(context.Background(), "tenant", "app", ResourceConfig{}))
	if command := gated.Terminal(context.Background(), "tenant", "app", "api", "pod", "main"); command != nil {
		t.Error("terminal callback returned a command while writes were disabled")
	}
	assertDenied(gated.DeleteRegistryRepository(context.Background(), "tenant", client.FlexID("registry"), "image"))
	assertDenied(gated.DeleteRegistryDigests(context.Background(), "tenant", client.FlexID("registry"), "image", nil))
	assertDenied(gated.DeleteRegistryTags(context.Background(), "tenant", client.FlexID("registry"), "image", "digest", nil))
	assertDenied(gated.RestartApp(context.Background(), "tenant", "app"))
	assertDenied(gated.DeleteApp(context.Background(), "tenant", "app"))

	if calls != 0 {
		t.Fatalf("underlying write callbacks called %d times with writes disabled", calls)
	}
}

func TestGateWriteCallbacksPassesThroughWhenAllowed(t *testing.T) {
	t.Parallel()
	called := false
	gated := gateWriteCallbacks(Options{
		AllowWrites: true,
		DeleteApp: func(context.Context, string, string) error {
			called = true
			return nil
		},
	})
	if err := gated.DeleteApp(context.Background(), "tenant", "app"); err != nil {
		t.Fatalf("allowed callback error = %v", err)
	}
	if !called {
		t.Fatal("allowed callback was not invoked")
	}
}
