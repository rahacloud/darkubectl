package cmd

import (
	"context"
	"time"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/config"
	browserUI "github.com/rahacloud/darkubectl/internal/tui/browser"
	"github.com/urfave/cli/v3"
)

func newTUIBrowserOptions(ctx context.Context, cmd *cli.Command, cfg *config.Config, credential *tuiCredential, baseURL, org string) browserUI.BrowserOptions {
	newClient := func(requestCtx context.Context, tenant string) (*client.Client, error) {
		auth, err := credential.current(requestCtx)
		if err != nil {
			return nil, err
		}
		return client.New(baseURL, auth, tenant), nil
	}
	return browserUI.BrowserOptions{
		Tenant:       org,
		LocalTenants: append([]string(nil), cfg.Tenants...),
		AllowWrites:  cmd.Bool(flagTUIAllowWrites),
		Profile: func(requestCtx context.Context) (*client.Profile, error) {
			c, err := newClient(requestCtx, org)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			profile, err := c.Profile(requestCtx)
			return profile, tuiAuthError(err)
		},
		ListApps: func(requestCtx context.Context, tenant string) ([]client.App, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			apps, err := c.ListApps(requestCtx)
			return apps, tuiAuthError(err)
		},
		GetApp: func(requestCtx context.Context, tenant, id string) (*client.App, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			app, err := c.GetAppTyped(requestCtx, id)
			return app, tuiAuthError(err)
		},
		Usage: func(requestCtx context.Context, tenant, id string) (*browserUI.AppUsage, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			return readTUIUsage(requestCtx, c, id)
		},
		UsageHistory: func(requestCtx context.Context, tenant, id string, window, step time.Duration) (*browserUI.UsageHistory, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			return readTUIUsageHistory(requestCtx, c, id, window, step)
		},
		Pods: func(requestCtx context.Context, tenant, id string) ([]appstate.Pod, error) {
			token, err := credential.consoleAccessToken(requestCtx)
			if err != nil {
				return nil, err
			}
			pods, _, err := appstate.FetchPods(requestCtx, appstate.Options{
				BaseURL: baseURL, AccessToken: token, Org: tenant, AppID: id,
			})
			return pods, tuiAuthError(err)
		},
		AppLogs: func(requestCtx context.Context, tenant, id string, opts client.LogOptions) ([]client.LogEntry, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			entries, _, err := c.AppLogs(requestCtx, id, opts)
			return entries, tuiAuthError(err)
		},
		EgressRoutes: func(requestCtx context.Context, tenant string) (map[string]client.ClusterEgress, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			routes, err := c.GetEgressRoutes(requestCtx)
			return routes, tuiAuthError(err)
		},
		Namespaces: func(requestCtx context.Context, tenant string) ([]client.Namespace, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			ns, err := c.ListNamespaces(requestCtx)
			return ns, tuiAuthError(err)
		},
		RestartApp: func(requestCtx context.Context, tenant, id string) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.RestartApp(requestCtx, id))
		},
		DeleteApp: func(requestCtx context.Context, tenant, id string) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.DeleteApp(requestCtx, id))
		},
	}
}
