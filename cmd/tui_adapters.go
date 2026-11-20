package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/config"
	browserUI "github.com/rahacloud/darkubectl/internal/tui/browser"
	"github.com/urfave/cli/v3"
)

const (
	newTUIBrowserOptionsValue50 = 50
)

func newTUIBrowserOptions(cmd *cli.Command, cfg *config.Config, credential *tuiCredential, baseURL, org string) browserUI.Options {
	newClient := func(requestCtx context.Context, tenant string) (*client.Client, error) {
		auth, err := credential.current(requestCtx)
		if err != nil {
			return nil, err
		}
		return client.New(baseURL, auth, tenant), nil
	}
	deps := tuiAdapterDeps{newClient: newClient, credential: credential, baseURL: baseURL, org: org}
	options := browserUI.Options{
		Tenant: org, Version: buildVersion(), LocalTenants: append([]string(nil), cfg.Tenants...), AllowWrites: cmd.Bool(flagTUIAllowWrites),
	}
	addTUIAccountOptions(&options, deps)
	addTUIDataOptions(&options, deps)
	addTUIAppMutationOptions(&options, deps)
	addTUIOtherOptions(&options, deps)
	return options
}

func loadDomainHostStatus(ctx context.Context, c *client.Client, id string, cfg *browserUI.DomainConfig) {
	var err error
	cfg.ChallengeRecords, err = c.SSLCertificateRecords(ctx, id, cfg.Hosts)
	cfg.ChallengeRecordsFailed = err != nil
	if dns, err := c.CheckDNS(ctx, cfg.Hosts); err == nil {
		cfg.DNS = dns
	}
	certs, err := c.CertificateStatus(ctx, id)
	if err != nil {
		return
	}
	for host, expiry := range certs {
		if expiry == nil {
			cfg.Certificates[host] = ""
			continue
		}
		cfg.Certificates[host] = expiry.Format("2006-01-02")
	}
}

// updateTUIAppAndVerify follows the same prepare, PUT and read-back path as
// command mutations after the TUI has shown and confirmed its staged diff.
func updateTUIAppAndVerify(ctx context.Context, c *client.Client, id string, mutate func(map[string]any) error) (bool, error) {
	before, after, err := c.PrepareAppUpdate(ctx, id, mutate)
	if err != nil {
		return false, err
	}
	changes := diffApps(before, after)
	if len(changes) == 0 {
		return false, nil
	}
	err = applyPreparedAppChange(ctx, c, id, appChange{apply: mutate, verifyDiff: true}, after, changes)
	return err == nil, err
}

type tuiAdapterDeps struct {
	newClient  func(context.Context, string) (*client.Client, error)
	credential *tuiCredential
	baseURL    string
	org        string
}

func addTUIAccountOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	addTUITicketOptions(options, deps)
	addTUIAccountFeedOptions(options, deps)
}

func addTUITicketOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient := deps.newClient
	options.ListTickets = func(requestCtx context.Context, tenant, status string, mine bool) (client.TicketList, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return client.TicketList{}, err
		}
		defer func() { _ = c.Close() }()
		return c.Tickets(requestCtx, status, "all", mine)
	}
	options.GetTicket = func(requestCtx context.Context, tenant, id string) (client.Ticket, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return client.Ticket{}, err
		}
		defer func() { _ = c.Close() }()
		return c.Ticket(requestCtx, id)
	}
	options.GetTicketComments = func(requestCtx context.Context, tenant, id string) ([]client.TicketComment, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		return c.TicketComments(requestCtx, id)
	}
	options.GetTicketCategories = func(requestCtx context.Context, tenant string) ([]client.TicketCategory, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		return c.TicketCategories(requestCtx)
	}
	options.GetTicketPriorities = func(requestCtx context.Context, tenant string) ([]client.TicketPriority, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		return c.TicketPriorities(requestCtx)
	}
	options.CreateTicket = func(requestCtx context.Context, tenant string, ticket client.NewTicket) (string, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return "", err
		}
		defer func() { _ = c.Close() }()
		return c.CreateTicket(requestCtx, ticket)
	}
	options.ReplyTicket = func(requestCtx context.Context, tenant, id, body string) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return c.ReplyToTicket(requestCtx, id, body, nil)
	}
}

func addTUIAccountFeedOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient, org := deps.newClient, deps.org
	options.Profile = func(requestCtx context.Context) (*client.Profile, error) {
		c, err := newClient(requestCtx, org)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		profile, err := c.Profile(requestCtx)
		return profile, tuiAuthError(err)
	}
	options.Alerts = func(requestCtx context.Context, tenant string) ([]client.Alert, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		alerts, err := c.Alerts(requestCtx)
		return alerts, tuiAuthError(err)
	}
	options.Notifications = func(requestCtx context.Context) ([]client.Notification, error) {
		c, err := newClient(requestCtx, org)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		notifications, err := c.Notifications(requestCtx, newTUIBrowserOptionsValue50)
		return notifications, tuiAuthError(err)
	}
}

func addTUIDataOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	addTUIInventoryOptions(options, deps.newClient)
	addTUIObservationOptions(options, deps)
	addTUIBuildOptions(options, deps.newClient)
}

func addTUIInventoryOptions(options *browserUI.Options, newClient func(context.Context, string) (*client.Client, error)) {
	options.ListDatabases = func(requestCtx context.Context, tenant string) ([]client.Database, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		databases, err := c.ListDatabases(requestCtx)
		return databases, tuiAuthError(err)
	}
	options.GetDatabase = func(requestCtx context.Context, tenant, id string) (*client.DatabaseDetail, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		database, err := c.GetDatabase(requestCtx, id)
		return database, tuiAuthError(err)
	}
	options.ListApps = func(requestCtx context.Context, tenant string) ([]client.App, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		apps, err := c.ListApps(requestCtx)
		return apps, tuiAuthError(err)
	}
	options.GetApp = func(requestCtx context.Context, tenant, id string) (*client.App, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		app, err := c.GetAppTyped(requestCtx, id)
		return app, tuiAuthError(err)
	}
}

func addTUIObservationOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient, credential, baseURL := deps.newClient, deps.credential, deps.baseURL
	options.Usage = func(requestCtx context.Context, tenant, id string) (*browserUI.AppUsage, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		return readTUIUsage(requestCtx, c, id)
	}
	options.UsageHistory = func(requestCtx context.Context, tenant, id string, window, step time.Duration) (*browserUI.UsageHistory, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		return readTUIUsageHistory(requestCtx, c, id, window, step)
	}
	options.Pods = func(requestCtx context.Context, tenant, id string) ([]appstate.Pod, error) {
		token, err := credential.consoleAccessToken(requestCtx)
		if err != nil {
			return nil, err
		}
		pods, _, err := appstate.FetchPods(requestCtx, appstate.Options{
			BaseURL: baseURL, AccessToken: token, Org: tenant, AppID: id,
		})
		return pods, tuiAuthError(err)
	}
	options.AppLogs = func(requestCtx context.Context, tenant, id string, opts client.LogOptions) ([]client.LogEntry, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		entries, _, err := c.AppLogs(requestCtx, id, opts)
		return entries, tuiAuthError(err)
	}
}

func addTUIBuildOptions(options *browserUI.Options, newClient func(context.Context, string) (*client.Client, error)) {
	options.ListBuilds = func(requestCtx context.Context, tenant, id string, limit int) ([]client.Build, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		builds, err := c.ListBuilds(requestCtx, id, limit)
		return builds, tuiAuthError(err)
	}
	options.DeployToken = func(requestCtx context.Context, tenant, id string) (string, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return "", err
		}
		defer func() { _ = c.Close() }()
		return c.DeployToken(requestCtx, id)
	}
	options.UpdateGitBuildSettings = func(requestCtx context.Context, tenant, id string, expected, desired client.GitBuildSettings) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.UpdateGitBuildSettings(requestCtx, id, expected, desired))
	}
	options.GetBuild = func(requestCtx context.Context, tenant string, id client.BuildID) (*client.Build, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		build, err := c.GetBuild(requestCtx, id)
		return build, tuiAuthError(err)
	}
	options.BuildLatest = func(requestCtx context.Context, tenant, appID string) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.BuildLatestCommit(requestCtx, appID))
	}
	options.RetryBuild = func(requestCtx context.Context, tenant string, id client.BuildID) (*client.Build, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		build, err := c.RetryBuild(requestCtx, id)
		return build, tuiAuthError(err)
	}
	options.StopBuild = func(requestCtx context.Context, tenant string, id client.BuildID) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.StopBuild(requestCtx, id))
	}
}

func addTUIAppMutationOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	addTUIEnvironmentOptions(options, deps)
	addTUIDomainsOptions(options, deps)
	addTUIPortsOptions(options, deps)
	addTUIResourcesOptions(options, deps)
}

func addTUIOtherOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	addTUIClusterOptions(options, deps)
	addTUIRegistryOptions(options, deps.newClient)
	addTUIAppActionsOptions(options, deps.newClient)
}

func addTUIClusterOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient, credential, baseURL := deps.newClient, deps.credential, deps.baseURL
	options.Terminal = func(requestCtx context.Context, tenant, id, appName, pod, container string) tea.ExecCommand {
		token, err := credential.consoleAccessToken(requestCtx)
		return &tuiTerminalCommand{ctx: requestCtx, baseURL: baseURL, tenant: tenant, appID: id, appName: appName, pod: pod, container: container,
			token: token, tokenErr: tuiAuthError(err)}
	}
	options.EgressRoutes = func(requestCtx context.Context, tenant string) (map[string]client.ClusterEgress, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		routes, err := c.GetEgressRoutes(requestCtx)
		return routes, tuiAuthError(err)
	}
	options.Namespaces = func(requestCtx context.Context, tenant string) ([]client.Namespace, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		ns, err := c.ListNamespaces(requestCtx)
		return ns, tuiAuthError(err)
	}
}

func addTUIRegistryOptions(options *browserUI.Options, newClient func(context.Context, string) (*client.Client, error)) {
	options.ListRegistries = func(requestCtx context.Context, tenant string) ([]client.Registry, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		rows, err := c.ListRegistries(requestCtx)
		return rows, tuiAuthError(err)
	}
	options.ListRegistryRepositories = func(requestCtx context.Context, tenant string, id client.FlexID) ([]client.Repository, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		rows, err := c.ListRepositories(requestCtx, id)
		return rows, tuiAuthError(err)
	}
	options.ListRegistryDigests = func(requestCtx context.Context, tenant string, id client.FlexID, image string) ([]client.Digest, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		rows, err := c.ListDigests(requestCtx, id, image)
		return rows, tuiAuthError(err)
	}
	options.DeleteRegistryRepository = func(requestCtx context.Context, tenant string, id client.FlexID, image string) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.DeleteRepository(requestCtx, id, image))
	}
	options.DeleteRegistryDigests = func(requestCtx context.Context, tenant string, id client.FlexID, image string, digests []string) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.DeleteDigests(requestCtx, id, image, digests))
	}
	options.DeleteRegistryTags = func(requestCtx context.Context, tenant string, id client.FlexID, image, digest string, tags []string) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.DeleteTags(requestCtx, id, image, digest, tags))
	}
}

func addTUIAppActionsOptions(options *browserUI.Options, newClient func(context.Context, string) (*client.Client, error)) {
	options.RestartApp = func(requestCtx context.Context, tenant, id string) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.RestartApp(requestCtx, id))
	}
	options.DeleteApp = func(requestCtx context.Context, tenant, id string) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return tuiAuthError(c.DeleteApp(requestCtx, id))
	}
}

func addTUIEnvironmentOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient := deps.newClient
	options.Environment = func(requestCtx context.Context, tenant, id string) ([]client.EnvVar, []string, error) {
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
	}
	options.SecretEnvs = func(requestCtx context.Context, tenant, id string) ([]client.EnvVar, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		values, err := c.SecretEnvs(requestCtx, id)
		return values, tuiAuthError(err)
	}
	options.UpdatePlainEnv = func(requestCtx context.Context, tenant, id string, envs []client.EnvVar) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		_, err = updateTUIAppAndVerify(requestCtx, c, id, func(app map[string]any) error {
			client.SetEnvVars(app, envs)
			return nil
		})
		return tuiAuthError(err)
	}
	options.UpdateSecretEnv = func(requestCtx context.Context, tenant, id string, envs []client.EnvVar) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		want, err := c.UpdateSecretEnvs(requestCtx, id, func([]client.EnvVar) ([]client.EnvVar, error) { return envs, nil })
		if err != nil {
			return tuiAuthError(err)
		}
		ok, err := retryReadBack(requestCtx, func(ctx context.Context) (bool, error) {
			got, err := c.SecretEnvs(ctx, id)
			if err != nil {
				return false, err
			}
			return sameEnvs(got, want), nil
		})
		if err != nil {
			return tuiAuthError(err)
		}
		if !ok {
			return errChangeIgnored
		}
		return tuiAuthError(c.RestartApp(requestCtx, id))
	}
}

func addTUIDomainsOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient := deps.newClient
	options.Domains = func(requestCtx context.Context, tenant, id string) (browserUI.DomainConfig, error) {
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
			loadDomainHostStatus(requestCtx, c, id, &cfg)
		}
		return cfg, nil
	}
	options.UpdateDomains = func(requestCtx context.Context, tenant, id string, cfg browserUI.DomainConfig) error {
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
		wrote, err := updateTUIAppAndVerify(requestCtx, c, id, mutate)
		if err != nil {
			return tuiAuthError(err)
		}
		if !wrote {
			return nil
		}
		_, err = c.UpdateApp(requestCtx, id, func(map[string]any) error { return nil })
		return tuiAuthError(err)
	}
}

func addTUIPortsOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient := deps.newClient
	options.Ports = func(requestCtx context.Context, tenant, id string) (browserUI.PortConfig, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return browserUI.PortConfig{}, err
		}
		defer func() { _ = c.Close() }()
		raw, err := c.GetApp(requestCtx, id)
		if err != nil {
			return browserUI.PortConfig{}, tuiAuthError(err)
		}
		return tuiPortConfigFromApp(raw), nil
	}
	options.UpdatePorts = func(requestCtx context.Context, tenant, id string, cfg browserUI.PortConfig) error {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		_, err = updateTUIAppAndVerify(requestCtx, c, id, func(raw map[string]any) error {
			return setTUIServicePorts(raw, cfg)
		})
		return tuiAuthError(err)
	}
}

func tuiPortConfigFromApp(raw map[string]any) browserUI.PortConfig {
	cfg := browserUI.PortConfig{Type: "ClusterIP", Ports: map[string]client.Port{}}
	svc, _ := raw["svc"].(map[string]any)
	if svc == nil {
		return cfg
	}
	if value, ok := svc["type"].(string); ok && value != "" {
		cfg.Type = value
	}
	cfg.InternalAddress, _ = svc["internalAddress"].(string)
	cfg.ExternalAddress, _ = svc["externalAddress"].(string)
	cfg.ExternalIP, _ = svc["externalIP"].(string)
	ports, _ := svc["ports"].(map[string]any)
	for name, value := range ports {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		protocol, _ := entry["protocol"].(string)
		cfg.Ports[name] = client.Port{ContainerPort: jsonInt(entry["containerPort"]), ServicePort: jsonInt(entry["servicePort"]),
			NodePort: jsonInt(entry["nodePort"]), Protocol: protocol}
	}
	return cfg
}

func setTUIServicePorts(raw map[string]any, cfg browserUI.PortConfig) error {
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
			protocol = defaultPortProtocolTCP
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

func addTUIResourcesOptions(options *browserUI.Options, deps tuiAdapterDeps) {
	newClient := deps.newClient
	options.ListPVCs = func(requestCtx context.Context, tenant, namespace string, clusterID int) ([]client.PVC, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		pvcs, err := c.ListPVCs(requestCtx, namespace, clusterID)
		return pvcs, tuiAuthError(err)
	}
	options.UpdateResources = func(requestCtx context.Context, tenant, id string, cfg browserUI.ResourceConfig) error {
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
		_, err = updateTUIAppAndVerify(requestCtx, c, id, func(raw map[string]any) error {
			return applyTUIResourceConfig(raw, cfg, selected)
		})
		return tuiAuthError(err)
	}
	options.ListResourcePlans = func(requestCtx context.Context, tenant string) ([]client.Plan, error) {
		c, err := newClient(requestCtx, tenant)
		if err != nil {
			return nil, err
		}
		defer func() { _ = c.Close() }()
		plans, err := c.ListPlans(requestCtx)
		return plans, tuiAuthError(err)
	}
}

func applyTUIResourceConfig(raw map[string]any, cfg browserUI.ResourceConfig, selected *client.Plan) error {
	raw["plan"] = selected.ID
	if selected.CostType == "dynamic" {
		raw["cpu_request"], raw["ram_limit"] = cfg.CPU, cfg.RAM
	}
	if err := updateTUIDiskSize(raw, cfg.DiskGi); err != nil {
		return err
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

func updateTUIDiskSize(raw map[string]any, requested int) error {
	if requested <= 0 {
		return nil
	}
	disk, ok := raw["disk"].(map[string]any)
	if !ok || len(disk) == 0 {
		return errors.New("this app has no disk and one cannot be added after creation")
	}
	current, ok := disk["size_in_Gi"].(float64)
	if !ok {
		return errors.New("the current disk size is unreadable")
	}
	if float64(requested) < current {
		return errors.New("disk cannot be shrunk")
	}
	disk["size_in_Gi"] = requested
	return nil
}
