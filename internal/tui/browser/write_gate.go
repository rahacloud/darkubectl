package browser

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

// gateWriteCallbacks keeps the browser's write adapters behind the launch-time
// opt-in, even when a view or callback is reached without its normal UI guard.
//
//nolint:gocyclo,cyclop,funlen // Every callback is wrapped so no storage write bypasses the launch gate.
func gateWriteCallbacks(options Options) Options {
	if options.AllowWrites {
		return options
	}
	if options.CreateStorageBucket != nil {
		options.CreateStorageBucket = func(context.Context, string, string, string) (string, error) {
			return "", errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.DeleteStorageBucket != nil {
		options.DeleteStorageBucket = func(context.Context, string, string, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.SetStorageBucketPublic != nil {
		options.SetStorageBucketPublic = func(context.Context, string, string, bool, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.CreateStorageKey != nil {
		options.CreateStorageKey = func(context.Context, string, string, []string, []string, string) (*client.StorageKeySecret, error) {
			return nil, errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.UpdateStorageKey != nil {
		options.UpdateStorageKey = func(context.Context, string, string, []string, []string, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.RotateStorageKey != nil {
		options.RotateStorageKey = func(context.Context, string, string, string) (*client.StorageKeySecret, error) {
			return nil, errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.DeleteStorageKey != nil {
		options.DeleteStorageKey = func(context.Context, string, string, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.CreateTicket != nil {
		options.CreateTicket = func(context.Context, string, client.NewTicket) (string, error) {
			return "", errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.ReplyTicket != nil {
		options.ReplyTicket = func(context.Context, string, string, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.UpdateGitBuildSettings != nil {
		options.UpdateGitBuildSettings = func(context.Context, string, string, client.GitBuildSettings, client.GitBuildSettings) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.BuildLatest != nil {
		options.BuildLatest = func(context.Context, string, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.RetryBuild != nil {
		options.RetryBuild = func(context.Context, string, client.BuildID) (*client.Build, error) {
			return nil, errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.StopBuild != nil {
		options.StopBuild = func(context.Context, string, client.BuildID) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.UpdatePlainEnv != nil {
		options.UpdatePlainEnv = func(context.Context, string, string, []client.EnvVar) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.UpdateSecretEnv != nil {
		options.UpdateSecretEnv = func(context.Context, string, string, []client.EnvVar) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.UpdateDomains != nil {
		options.UpdateDomains = func(context.Context, string, string, DomainConfig) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.UpdatePorts != nil {
		options.UpdatePorts = func(context.Context, string, string, PortConfig) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.UpdateResources != nil {
		options.UpdateResources = func(context.Context, string, string, ResourceConfig) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.Terminal != nil {
		options.Terminal = func(context.Context, string, string, string, string, string) tea.ExecCommand {
			return nil
		}
	}
	if options.DeleteRegistryRepository != nil {
		options.DeleteRegistryRepository = func(context.Context, string, client.FlexID, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.DeleteRegistryDigests != nil {
		options.DeleteRegistryDigests = func(context.Context, string, client.FlexID, string, []string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.DeleteRegistryTags != nil {
		options.DeleteRegistryTags = func(context.Context, string, client.FlexID, string, string, []string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.RestartApp != nil {
		options.RestartApp = func(context.Context, string, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	if options.DeleteApp != nil {
		options.DeleteApp = func(context.Context, string, string) error {
			return errWriteActionsAreDisabledRelaunchWithAllow
		}
	}
	return options
}
