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
		},
		Action: tuiAction,
	}
}

const flagMockFile = "mock-file"

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
	auth, err := resolveAuth(ctx, cmd, cfg)
	if err != nil {
		return err
	}
	baseURL := resolveBaseURL(cmd, cfg)
	org := resolveOrg(cmd, cfg)
	profileClient := client.New(baseURL, auth, org)
	defer func() { _ = profileClient.Close() }()
	return tui.RunBrowser(ctx, tui.BrowserOptions{
		Tenant:       org,
		LocalTenants: append([]string(nil), cfg.Tenants...),
		Profile:      profileClient.Profile,
		ListApps: func(requestCtx context.Context, tenant string) ([]client.App, error) {
			c := client.New(baseURL, auth, tenant)
			defer func() { _ = c.Close() }()
			return c.ListApps(requestCtx)
		},
		GetApp: func(requestCtx context.Context, tenant, id string) (*client.App, error) {
			c := client.New(baseURL, auth, tenant)
			defer func() { _ = c.Close() }()
			return c.GetAppTyped(requestCtx, id)
		},
		Usage: func(requestCtx context.Context, tenant, id string) (*tui.AppUsage, error) {
			c := client.New(baseURL, auth, tenant)
			defer func() { _ = c.Close() }()
			end := time.Now()
			usage := &tui.AppUsage{}
			for _, metric := range []struct {
				typeName string
				dest     **float64
			}{{client.InsightCPU, &usage.CPUCores}, {client.InsightRAM, &usage.RAMBytes}, {client.InsightDisk, &usage.DiskBytes}} {
				series, err := insightWithRetry(requestCtx, c, client.InsightQuery{
					AppID: id, Type: metric.typeName, Pod: client.AllPods,
					Start: end.Add(-5 * time.Minute), End: end, Step: time.Minute,
				})
				if err != nil {
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
		Namespaces: func(requestCtx context.Context, tenant string) ([]client.Namespace, error) {
			c := client.New(baseURL, auth, tenant)
			defer func() { _ = c.Close() }()
			return c.ListNamespaces(requestCtx)
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
