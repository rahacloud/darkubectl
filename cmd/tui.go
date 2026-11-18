package cmd

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/tui"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func newTUICommand() *cli.Command {
	return &cli.Command{
		Name:  "tui",
		Usage: "Browse Darkube apps interactively (experimental)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagMockFile, Usage: "load a local YAML profile without connecting to Darkube"},
			&cli.BoolFlag{Name: flagTUIAllowWrites, Usage: "enable confirmed write actions in the TUI"},
		},
		Action: tuiAction,
	}
}

const (
	flagMockFile       = "mock-file"
	flagTUIAllowWrites = "allow-writes"
)

func tuiAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 0 {
		return errors.New("tui does not accept positional arguments")
	}
	if cmd.String(flagOutput) != "table" {
		return errors.New("tui only supports table output")
	}
	mockFileSet := cmd.IsSet(flagMockFile)
	mockPath := strings.TrimSpace(cmd.String(flagMockFile))
	if mockFileSet {
		if mockPath == "" {
			return errors.New("--mock-file requires a YAML file path")
		}
		if err := rejectMockConnectionFlags(cmd); err != nil {
			return err
		}
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("tui requires a terminal on stdin and stdout")
	}
	if mockFileSet {
		profile, err := tui.LoadMockProfile(mockPath)
		if err != nil {
			return err
		}
		return tui.RunMockBrowser(ctx, profile)
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	credential, err := newTUICredential(ctx, cmd, cfg)
	if err != nil {
		return err
	}
	baseURL := resolveBaseURL(cmd, cfg)
	org := resolveOrg(cmd, cfg)
	newClient := func(requestCtx context.Context, tenant string) (*client.Client, error) {
		auth, err := credential.current(requestCtx)
		if err != nil {
			return nil, err
		}
		return client.New(baseURL, auth, tenant), nil
	}
	return tui.RunBrowser(ctx, tui.BrowserOptions{
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
		Usage: func(requestCtx context.Context, tenant, id string) (*tui.AppUsage, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			end := time.Now()
			usage := &tui.AppUsage{}
			for _, metric := range []struct {
				typeName string
				dest     **float64
				failed   *bool
			}{
				{client.InsightCPU, &usage.CPUCores, &usage.Failed.CPU},
				{client.InsightRAM, &usage.RAMBytes, &usage.Failed.RAM},
				{client.InsightDisk, &usage.DiskBytes, &usage.Failed.Disk},
			} {
				series, err := insightWithRetry(requestCtx, c, client.InsightQuery{
					AppID: id, Type: metric.typeName, Pod: client.AllPods,
					Start: end.Add(-5 * time.Minute), End: end, Step: time.Minute,
				})
				if err != nil {
					if requestCtx.Err() != nil {
						return nil, requestCtx.Err()
					}
					if tuiCredentialRejected(err) {
						return nil, tuiAuthError(err)
					}
					*metric.failed = true
					continue
				}
				var total float64
				found := false
				for _, pod := range series {
					if sample, ok := pod.Latest(); ok {
						total += sample.Value
						found = true
					}
				}
				if found {
					value := total
					*metric.dest = &value
				}
			}
			return usage, nil
		},
		UsageHistory: func(requestCtx context.Context, tenant, id string, window, step time.Duration) (*tui.UsageHistory, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			end := time.Now().Truncate(time.Second)
			history := &tui.UsageHistory{Start: end.Add(-window), End: end, Step: step}
			for _, metric := range []struct {
				typeName string
				dest     *[]client.Series
				failed   *bool
			}{
				{client.InsightCPU, &history.CPU, &history.Failed.CPU},
				{client.InsightCPUThrottling, &history.CPUThrottle, &history.Failed.CPUThrottle},
				{client.InsightRAM, &history.RAM, &history.Failed.RAM},
				{client.InsightDisk, &history.Disk, &history.Failed.Disk},
				{client.InsightNetworkReceive, &history.NetworkReceive, &history.Failed.NetworkReceive},
				{client.InsightNetworkSend, &history.NetworkSend, &history.Failed.NetworkSend},
			} {
				series, err := insightWithRetry(requestCtx, c, client.InsightQuery{
					AppID: id, Type: metric.typeName, Pod: client.AllPods,
					Start: history.Start, End: end, Step: step,
				})
				if err != nil {
					if requestCtx.Err() != nil {
						return nil, requestCtx.Err()
					}
					if tuiCredentialRejected(err) {
						return nil, tuiAuthError(err)
					}
					*metric.failed = true
					continue
				}
				*metric.dest = series
			}
			return history, nil
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
	})
}

func rejectMockConnectionFlags(cmd *cli.Command) error {
	for _, name := range []string{flagConfig, flagToken, flagOrg, flagBaseURL} {
		if cmd.IsSet(name) {
			return errors.New("--mock-file cannot be combined with --" + name + "; fixture mode is offline")
		}
	}
	return nil
}
