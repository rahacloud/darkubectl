package cmd

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
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
		Alerts: func(requestCtx context.Context, tenant string) ([]client.Alert, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			alerts, err := c.Alerts(requestCtx)
			return alerts, tuiAuthError(err)
		},
		Notifications: func(requestCtx context.Context) ([]client.Notification, error) {
			c, err := newClient(requestCtx, org)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			notifications, err := c.Notifications(requestCtx, 50)
			return notifications, tuiAuthError(err)
		},
		ListDatabases: func(requestCtx context.Context, tenant string) ([]client.Database, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			databases, err := c.ListDatabases(requestCtx)
			return databases, tuiAuthError(err)
		},
		GetDatabase: func(requestCtx context.Context, tenant, id string) (*client.DatabaseDetail, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			database, err := c.GetDatabase(requestCtx, id)
			return database, tuiAuthError(err)
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
		ListBuilds: func(requestCtx context.Context, tenant, id string, limit int) ([]client.Build, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			builds, err := c.ListBuilds(requestCtx, id, limit)
			return builds, tuiAuthError(err)
		},
		DeployToken: func(requestCtx context.Context, tenant, id string) (string, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return "", err
			}
			defer func() { _ = c.Close() }()
			return c.DeployToken(requestCtx, id)
		},
		UpdateGitBuildSettings: func(requestCtx context.Context, tenant, id string, expected, desired client.GitBuildSettings) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.UpdateGitBuildSettings(requestCtx, id, expected, desired))
		},
		GetBuild: func(requestCtx context.Context, tenant string, id client.BuildID) (*client.Build, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			build, err := c.GetBuild(requestCtx, id)
			return build, tuiAuthError(err)
		},
		BuildLatest: func(requestCtx context.Context, tenant, appID string) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.BuildLatestCommit(requestCtx, appID))
		},
		RetryBuild: func(requestCtx context.Context, tenant string, id client.BuildID) (*client.Build, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			build, err := c.RetryBuild(requestCtx, id)
			return build, tuiAuthError(err)
		},
		StopBuild: func(requestCtx context.Context, tenant string, id client.BuildID) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.StopBuild(requestCtx, id))
		},
		Environment: func(requestCtx context.Context, tenant, id string) ([]client.EnvVar, []string, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, nil, err
			}
			defer func() { _ = c.Close() }()
			raw, err := c.GetApp(requestCtx, id)
			if err != nil {
				return nil, nil, tuiAuthError(err)
			}
			return client.EnvVars(raw), client.SecretEnvNames(raw), nil
		},
		SecretEnvs: func(requestCtx context.Context, tenant, id string) ([]client.EnvVar, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			values, err := c.SecretEnvs(requestCtx, id)
			return values, tuiAuthError(err)
		},
		UpdatePlainEnv: func(requestCtx context.Context, tenant, id string, envs []client.EnvVar) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			_, err = c.UpdateApp(requestCtx, id, func(app map[string]any) error { client.SetEnvVars(app, envs); return nil })
			return tuiAuthError(err)
		},
		UpdateSecretEnv: func(requestCtx context.Context, tenant, id string, envs []client.EnvVar) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			_, err = c.UpdateSecretEnvs(requestCtx, id, func([]client.EnvVar) ([]client.EnvVar, error) { return envs, nil })
			return tuiAuthError(err)
		},
		Domains: func(requestCtx context.Context, tenant, id string) (browserUI.DomainConfig, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return browserUI.DomainConfig{}, err
			}
			defer func() { _ = c.Close() }()
			raw, err := c.GetApp(requestCtx, id)
			if err != nil {
				return browserUI.DomainConfig{}, tuiAuthError(err)
			}
			cfg := browserUI.DomainConfig{Hosts: client.ExternalHosts(raw), DNS: map[string]bool{}, Certificates: map[string]string{}}
			cfg.CNAME, _ = raw["ingress_cname_address"].(string)
			cfg.Subdomain, _ = raw["custom_subdomain_addr"].(string)
			cfg.BaseDomain = clusterBaseDomain(raw)
			cfg.Challenge, _ = raw["ssl_challenge_type"].(string)
			cfg.SSL, _ = raw["enable_SSL"].(bool)
			cfg.RedirectHTTPS, _ = raw["redirect_SSL"].(bool)
			cfg.HTTP2, _ = raw["enable_httpv2"].(bool)
			if len(cfg.Hosts) > 0 {
				cfg.ChallengeRecords, err = c.SSLCertificateRecords(requestCtx, id, cfg.Hosts)
				cfg.ChallengeRecordsFailed = err != nil
				if dns, e := c.CheckDNS(requestCtx, cfg.Hosts); e == nil {
					cfg.DNS = dns
				}
				if certs, e := c.CertificateStatus(requestCtx, id); e == nil {
					for host, expiry := range certs {
						if expiry != nil {
							cfg.Certificates[host] = expiry.Format("2006-01-02")
						} else {
							cfg.Certificates[host] = ""
						}
					}
				}
			}
			return cfg, nil
		},
		UpdateDomains: func(requestCtx context.Context, tenant, id string, cfg browserUI.DomainConfig) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			mutate := func(raw map[string]any) error {
				client.SetExternalHosts(raw, cfg.Hosts)
				if cfg.Subdomain == "" {
					raw["custom_subdomain_addr"] = nil
				} else {
					raw["custom_subdomain_addr"] = cfg.Subdomain
				}
				raw["ssl_challenge_type"] = cfg.Challenge
				raw["enable_SSL"] = cfg.SSL || cfg.Subdomain != ""
				raw["redirect_SSL"] = cfg.RedirectHTTPS
				raw["enable_httpv2"] = cfg.HTTP2
				return nil
			}
			err = updateTUIAppAndVerify(requestCtx, c, id, mutate)
			if err != nil {
				return tuiAuthError(err)
			}
			_, err = c.UpdateApp(requestCtx, id, func(map[string]any) error { return nil })
			return tuiAuthError(err)
		},
		Ports: func(requestCtx context.Context, tenant, id string) (browserUI.PortConfig, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return browserUI.PortConfig{}, err
			}
			defer func() { _ = c.Close() }()
			raw, err := c.GetApp(requestCtx, id)
			if err != nil {
				return browserUI.PortConfig{}, tuiAuthError(err)
			}
			cfg := browserUI.PortConfig{Type: "ClusterIP", Ports: map[string]client.Port{}}
			svc, _ := raw["svc"].(map[string]any)
			if svc != nil {
				if value, ok := svc["type"].(string); ok && value != "" {
					cfg.Type = value
				}
				cfg.InternalAddress, _ = svc["internalAddress"].(string)
				cfg.ExternalAddress, _ = svc["externalAddress"].(string)
				cfg.ExternalIP, _ = svc["externalIP"].(string)
				if ports, ok := svc["ports"].(map[string]any); ok {
					for name, value := range ports {
						entry, ok := value.(map[string]any)
						if !ok {
							continue
						}
						protocol, _ := entry["protocol"].(string)
						cfg.Ports[name] = client.Port{ContainerPort: jsonInt(entry["containerPort"]), ServicePort: jsonInt(entry["servicePort"]), NodePort: jsonInt(entry["nodePort"]), Protocol: protocol}
					}
				}
			}
			return cfg, nil
		},
		UpdatePorts: func(requestCtx context.Context, tenant, id string, cfg browserUI.PortConfig) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			mutate := func(raw map[string]any) error {
				// Firewall rules are outside this svc.ports and svc.type projection.
				svc, _ := raw["svc"].(map[string]any)
				if svc == nil {
					svc = map[string]any{}
				}
				svc["type"] = cfg.Type
				ports := make(map[string]any, len(cfg.Ports))
				for name, p := range cfg.Ports {
					servicePort := p.ServicePort
					if servicePort == 0 {
						servicePort = p.ContainerPort
					}
					protocol := p.Protocol
					if protocol == "" {
						protocol = "TCP"
					}
					entry := map[string]any{"containerPort": p.ContainerPort, "servicePort": servicePort, "protocol": protocol}
					if p.NodePort > 0 {
						entry["nodePort"] = p.NodePort
					}
					ports[name] = entry
				}
				svc["ports"] = ports
				raw["svc"] = svc
				return nil
			}
			return tuiAuthError(updateTUIAppAndVerify(requestCtx, c, id, mutate))
		},
		UpdateResources: func(requestCtx context.Context, tenant, id string, cfg browserUI.ResourceConfig) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			plans, err := c.ListPlans(requestCtx)
			if err != nil {
				return tuiAuthError(err)
			}
			var selected *client.Plan
			for i := range plans {
				if plans[i].ID == cfg.Plan || plans[i].Name == cfg.Plan || plans[i].CodeName == cfg.Plan {
					selected = &plans[i]
					break
				}
			}
			if selected == nil {
				return fmt.Errorf("no plan matching %q", cfg.Plan)
			}
			mutate := func(raw map[string]any) error {
				raw["plan"] = selected.ID
				if selected.CostType == "dynamic" {
					raw["cpu_request"], raw["ram_limit"] = cfg.CPU, cfg.RAM
				}
				if cfg.DiskGi > 0 {
					disk, ok := raw["disk"].(map[string]any)
					if !ok || len(disk) == 0 {
						return fmt.Errorf("this app has no disk and one cannot be added after creation")
					}
					current, ok := disk["size_in_Gi"].(float64)
					if !ok {
						return fmt.Errorf("the current disk size is unreadable")
					}
					if float64(cfg.DiskGi) < current {
						return fmt.Errorf("disk cannot be shrunk")
					}
					disk["size_in_Gi"] = cfg.DiskGi
				}
				if !cfg.Autoscaling {
					raw["replicas"] = cfg.Replicas
				}
				custom, _ := raw["custom_config"].(map[string]any)
				if custom == nil {
					custom = map[string]any{}
				}
				hpa, _ := custom["hpa"].(map[string]any)
				if hpa == nil {
					hpa = map[string]any{}
				}
				hpa["enabled"] = cfg.Autoscaling
				if cfg.Autoscaling {
					hpa["minReplicas"], hpa["maxReplicas"], hpa["targetCPUUtilizationPercentage"] = cfg.MinReplicas, cfg.MaxReplicas, cfg.CPUTarget
				}
				custom["hpa"], raw["custom_config"], raw["is_hpa_enabled"] = hpa, custom, cfg.Autoscaling
				return nil
			}
			return tuiAuthError(updateTUIAppAndVerify(requestCtx, c, id, mutate))
		},
		ListResourcePlans: func(requestCtx context.Context, tenant string) ([]client.Plan, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			plans, err := c.ListPlans(requestCtx)
			return plans, tuiAuthError(err)
		},
		Terminal: func(requestCtx context.Context, tenant, id, appName, pod, container string) tea.ExecCommand {
			token, err := credential.consoleAccessToken(requestCtx)
			return &tuiTerminalCommand{ctx: requestCtx, baseURL: baseURL, tenant: tenant, appID: id, appName: appName, pod: pod, container: container,
				token: token, tokenErr: tuiAuthError(err)}
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
		ListRegistries: func(requestCtx context.Context, tenant string) ([]client.Registry, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			rows, err := c.ListRegistries(requestCtx)
			return rows, tuiAuthError(err)
		},
		ListRegistryRepositories: func(requestCtx context.Context, tenant string, id client.FlexID) ([]client.Repository, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			rows, err := c.ListRepositories(requestCtx, id)
			return rows, tuiAuthError(err)
		},
		ListRegistryDigests: func(requestCtx context.Context, tenant string, id client.FlexID, image string) ([]client.Digest, error) {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return nil, err
			}
			defer func() { _ = c.Close() }()
			rows, err := c.ListDigests(requestCtx, id, image)
			return rows, tuiAuthError(err)
		},
		DeleteRegistryRepository: func(requestCtx context.Context, tenant string, id client.FlexID, image string) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.DeleteRepository(requestCtx, id, image))
		},
		DeleteRegistryDigests: func(requestCtx context.Context, tenant string, id client.FlexID, image string, digests []string) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.DeleteDigests(requestCtx, id, image, digests))
		},
		DeleteRegistryTags: func(requestCtx context.Context, tenant string, id client.FlexID, image, digest string, tags []string) error {
			c, err := newClient(requestCtx, tenant)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			return tuiAuthError(c.DeleteTags(requestCtx, id, image, digest, tags))
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

// updateTUIAppAndVerify follows the same prepare, PUT and read-back path as
// command mutations after the TUI has shown and confirmed its staged diff.
func updateTUIAppAndVerify(ctx context.Context, c *client.Client, id string, mutate func(map[string]any) error) error {
	before, after, err := c.PrepareAppUpdate(ctx, id, mutate)
	if err != nil {
		return err
	}
	changes := diffApps(before, after)
	if len(changes) == 0 {
		return nil
	}
	if _, err = c.UpdateApp(ctx, id, mutate); err != nil {
		return err
	}
	return verifyChange(ctx, c.GetApp, id, func(fresh map[string]any) bool { return diffStored(fresh, after, changes) })
}
