package browser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"gopkg.in/yaml.v3"
)

const (
	mockProfileVersion = 1
	mockProfileMaxSize = 1 << 20
	mockProfileMaxApps = 1000
)

// MockProfile contains the local-only fields rendered by the fixture browser.
type MockProfile struct {
	Tenant     string
	Apps       []client.App
	Alerts     []client.Alert
	Namespaces []client.Namespace
	Tenants    []MockTenant
	AppData    map[string]MockAppData
	Registries []MockRegistry
	Databases  []client.Database
	Storages   []client.Storage
}

// MockAppData contains offline application fixture data.
type MockAppData struct {
	PlainEnvs   []client.EnvVar
	SecretNames []string
	Pods        []appstate.Pod
	Logs        []MockLog
	Usage       *AppUsage
	History     UsageHistory
}

// MockLog is one log entry in an offline mock profile.
type MockLog struct {
	Entry     client.LogEntry
	Pod       string
	Container string
	Previous  bool
}

// MockTenant is one tenant and its local app snapshot.
type MockTenant struct {
	Name       string
	Apps       []client.App
	Alerts     []client.Alert
	Namespaces []client.Namespace
	AppData    map[string]MockAppData
	Registries []MockRegistry
	Databases  []client.Database
	Storages   []client.Storage
}

// MockRegistry contains an offline registry fixture.
type MockRegistry struct {
	Registry client.Registry
	Images   []MockRegistryImage
}

// MockRegistryImage contains an offline registry image fixture.
type MockRegistryImage struct {
	Repository client.Repository
	Digests    []client.Digest
}

type mockProfileFile struct {
	Version    int                    `yaml:"version"`
	Tenant     string                 `yaml:"tenant"`
	Apps       []mockProfileApp       `yaml:"apps"`
	Alerts     []mockProfileAlert     `yaml:"alerts"`
	Namespaces []mockProfileNamespace `yaml:"namespaces"`
	Tenants    []mockProfileTenant    `yaml:"tenants"`
	Registries []mockProfileRegistry  `yaml:"registries"`
	Databases  []client.Database      `yaml:"databases"`
	Storages   []client.Storage       `yaml:"storages"`
}

type mockProfileTenant struct {
	Name       string                 `yaml:"name"`
	Apps       []mockProfileApp       `yaml:"apps"`
	Alerts     []mockProfileAlert     `yaml:"alerts"`
	Namespaces []mockProfileNamespace `yaml:"namespaces"`
	Registries []mockProfileRegistry  `yaml:"registries"`
	Databases  []client.Database      `yaml:"databases"`
	Storages   []client.Storage       `yaml:"storages"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileRegistry struct {
	ID                string                     `yaml:"id"`
	Name              string                     `yaml:"name"`
	Address           string                     `yaml:"address"`
	Username          string                     `yaml:"username"`
	IsProvidedAsSaaS  bool                       `yaml:"is_provided_as_saas"`
	StorageUsageBytes *int64                     `yaml:"storage_usage_bytes"`
	Images            []mockProfileRegistryImage `yaml:"images"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileRegistryImage struct {
	Name       string                      `yaml:"name"`
	LastPushed string                      `yaml:"last_pushed"`
	RecentTag  string                      `yaml:"recent_tag"`
	Digests    []mockProfileRegistryDigest `yaml:"digests"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileRegistryDigest struct {
	Digest     string   `yaml:"digest"`
	Tags       []string `yaml:"tags"`
	Size       int64    `yaml:"size"`
	LastPushed string   `yaml:"last_pushed"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileAlert struct {
	ID               string `yaml:"id"`
	AlertName        string `yaml:"alertname"`
	Status           string `yaml:"status"`
	Severity         string `yaml:"severity"`
	Instance         string `yaml:"instance"`
	ServiceOwner     string `yaml:"service_owner"`
	Condition        string `yaml:"condition"`
	AlertDescription string `yaml:"alert_description"`
	DescriptionFA    string `yaml:"description_fa"`
	TimeWindow       string `yaml:"timewindow"`
	StartsAt         string `yaml:"starts_at"`
	EndsAt           string `yaml:"ends_at"`
	ActionLink       string `yaml:"action_link"`
	ForMe            bool   `yaml:"for_me"`
}

func convertMockAlerts(input []mockProfileAlert) ([]client.Alert, error) {
	if len(input) > mockProfileMaxApps {
		return nil, fmt.Errorf("%w: mock profile has more than %d alerts", errInvalidMockProfile, mockProfileMaxApps)
	}
	out := make([]client.Alert, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for i, alert := range input {
		id := strings.TrimSpace(alert.ID)
		if id == "" || strings.TrimSpace(alert.AlertName) == "" {
			return nil, fmt.Errorf("%w: mock profile alert %d requires id and alertname", errInvalidMockProfile, i+1)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("%w: mock profile has duplicate alert id %q", errInvalidMockProfile, id)
		}
		seen[id] = struct{}{}
		out = append(out, client.Alert{ID: id, AlertName: alert.AlertName, Status: alert.Status, Severity: alert.Severity,
			Instance: alert.Instance, ServiceOwner: alert.ServiceOwner, Condition: alert.Condition,
			AlertDescription: alert.AlertDescription, DescriptionFA: alert.DescriptionFA, TimeWindow: alert.TimeWindow,
			StartsAt: alert.StartsAt, EndsAt: alert.EndsAt, ActionLink: alert.ActionLink, ForMe: alert.ForMe})
	}
	return out, nil
}

func convertMockRegistries(input []mockProfileRegistry) ([]MockRegistry, error) {
	if len(input) > mockProfileMaxApps {
		return nil, fmt.Errorf("%w: mock profile has more than %d registries", errInvalidMockProfile, mockProfileMaxApps)
	}
	out := make([]MockRegistry, 0, len(input))
	ids := map[string]bool{}
	names := map[string]bool{}
	totalImages := 0
	for i, raw := range input {
		id, name := strings.TrimSpace(raw.ID), strings.TrimSpace(raw.Name)
		if id == "" || name == "" || strings.TrimSpace(raw.Address) == "" {
			return nil, fmt.Errorf("%w: mock profile registry %d requires id, name and address", errInvalidMockProfile, i+1)
		}
		if ids[id] || names[name] {
			return nil, fmt.Errorf("%w: mock profile has duplicate registry id or name %q", errInvalidMockProfile, name)
		}
		ids[id], names[name] = true, true
		reg := client.Registry{ID: client.FlexID(id), Name: name, Address: raw.Address, Username: raw.Username,
			IsProvidedAsSaaS: raw.IsProvidedAsSaaS, StorageUsageBytes: raw.StorageUsageBytes}
		images, err := convertMockRegistryImages(raw.Images, name)
		if err != nil {
			return nil, err
		}
		totalImages += len(images)
		if totalImages > mockProfileMaxApps {
			return nil, fmt.Errorf("%w: mock profile has more than %d registry images", errInvalidMockProfile, mockProfileMaxApps)
		}
		out = append(out, MockRegistry{Registry: reg, Images: images})
	}
	return out, nil
}

func convertMockRegistryImages(input []mockProfileRegistryImage, registryName string) ([]MockRegistryImage, error) {
	images := make([]MockRegistryImage, 0, len(input))
	seen := make(map[string]bool, len(input))
	for _, raw := range input {
		name := strings.TrimSpace(raw.Name)
		if name == "" || seen[name] {
			return nil, fmt.Errorf("%w: mock profile registry %q has an empty or duplicate image", errInvalidMockProfile, registryName)
		}
		seen[name] = true
		image, err := convertMockRegistryImage(raw, name)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, nil
}

func convertMockRegistryImage(raw mockProfileRegistryImage, name string) (MockRegistryImage, error) {
	digests := make([]client.Digest, 0, len(raw.Digests))
	seenTags := make(map[string]bool)
	for _, rawDigest := range raw.Digests {
		digest, err := convertMockRegistryDigest(rawDigest, name, seenTags)
		if err != nil {
			return MockRegistryImage{}, err
		}
		digests = append(digests, digest)
	}
	return MockRegistryImage{
		Repository: client.Repository{Name: name, LastPushed: raw.LastPushed, RecentTag: raw.RecentTag},
		Digests:    digests,
	}, nil
}

func convertMockRegistryDigest(raw mockProfileRegistryDigest, imageName string, seenTags map[string]bool) (client.Digest, error) {
	if strings.TrimSpace(raw.Digest) == "" {
		return client.Digest{}, fmt.Errorf("%w: mock profile image %q has a digest without a digest value", errInvalidMockProfile, imageName)
	}
	tags := append([]string(nil), raw.Tags...)
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seenTags[tag] {
			return client.Digest{}, fmt.Errorf("%w: mock profile image %q has an empty or duplicate tag", errInvalidMockProfile, imageName)
		}
		seenTags[tag] = true
	}
	return client.Digest{Digest: raw.Digest, Tags: tags, Size: raw.Size, LastPushed: raw.LastPushed}, nil
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileApp struct {
	ID                  string                     `yaml:"id"`
	Name                string                     `yaml:"name"`
	Namespace           mockProfileNamespace       `yaml:"namespace"`
	State               mockProfileState           `yaml:"state"`
	Replicas            *int                       `yaml:"replicas"`
	CreationMethod      string                     `yaml:"creation_method"`
	BuildMethod         string                     `yaml:"build_method"`
	GitRepoURL          string                     `yaml:"git_repo_url"`
	GitBranch           string                     `yaml:"git_branch_name"`
	GitProvider         string                     `yaml:"git_provider_type"`
	GitDockerfile       string                     `yaml:"git_build_dockerfile"`
	GitContext          string                     `yaml:"git_build_context"`
	GitWorkdir          string                     `yaml:"git_build_workdir"`
	Builder             string                     `yaml:"builder"`
	GitAutodeploy       *bool                      `yaml:"autodeploy_on_git_push"`
	GitBuildArgs        map[string]string          `yaml:"git_build_args"`
	ImageRepo           string                     `yaml:"image_repo"`
	ImageTag            string                     `yaml:"image_tag"`
	CustomDomainAddress string                     `yaml:"custom_domain_address"`
	CustomSubdomainAddr string                     `yaml:"custom_subdomain_addr"`
	ExternalHosts       []string                   `yaml:"external_hosts"`
	Service             mockProfileService         `yaml:"service"`
	ReadinessProbePath  string                     `yaml:"readiness_probe_path"`
	Command             string                     `yaml:"command"`
	Args                string                     `yaml:"args"`
	RAMLimit            string                     `yaml:"ram_limit"`
	CPURequest          string                     `yaml:"cpu_request"`
	DiskSizeInGi        int                        `yaml:"disk_size_in_gi"`
	DiskPartitions      []mockProfileDiskPartition `yaml:"disk_partitions"`
	Plan                *mockProfilePlan           `yaml:"plan"`
	CustomConfig        mockProfileCustomConfig    `yaml:"custom_config"`
	Envs                []client.EnvVar            `yaml:"envs"`
	SecretEnvNames      []string                   `yaml:"secret_envs"`
	Pods                []mockProfilePod           `yaml:"pods"`
	Logs                []mockProfileLog           `yaml:"logs"`
	Usage               mockProfileUsage           `yaml:"usage"`
	History             mockProfileHistory         `yaml:"history"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileDiskPartition struct {
	DisplayName string `yaml:"display_name"`
	MountPath   string `yaml:"mount_path"`
	SubPath     string `yaml:"sub_path"`
}

type mockProfileLog struct {
	Timestamp string `yaml:"timestamp"`
	Text      string `yaml:"text"`
	Pod       string `yaml:"pod"`
	Container string `yaml:"container"`
	Previous  bool   `yaml:"previous"`
}

//nolint:tagliatelle // These snake_case YAML keys are part of the mock profile format.
type mockProfileUsage struct {
	CPU  *float64 `yaml:"cpu_cores"`
	RAM  *float64 `yaml:"ram_bytes"`
	Disk *float64 `yaml:"disk_bytes"`
}

//nolint:tagliatelle // These snake_case YAML keys are part of the mock profile format.
type mockProfileHistory struct {
	CPU            []client.Series `yaml:"cpu"`
	CPUThrottle    []client.Series `yaml:"cpu_throttle"`
	RAM            []client.Series `yaml:"ram"`
	Disk           []client.Series `yaml:"disk"`
	NetworkReceive []client.Series `yaml:"network_receive"`
	NetworkSend    []client.Series `yaml:"network_send"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfilePlan struct {
	ID              string                `yaml:"id"`
	Name            string                `yaml:"name"`
	CodeName        string                `yaml:"code_name"`
	PlanType        string                `yaml:"plan_type"`
	CostType        string                `yaml:"cost_type"`
	ShowInCreateApp bool                  `yaml:"show_in_create_app"`
	Detail          mockProfilePlanDetail `yaml:"detail"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfilePlanDetail struct {
	RAMLimit   int `yaml:"ram_limit"`
	CPURequest int `yaml:"cpu_request"`
}
type mockProfileCustomConfig struct {
	HPA *mockProfileHPA `yaml:"hpa"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileHPA struct {
	Enabled                        bool `yaml:"enabled"`
	MinReplicas                    int  `yaml:"minReplicas"`
	MaxReplicas                    int  `yaml:"maxReplicas"`
	TargetCPUUtilizationPercentage int  `yaml:"targetCPUUtilizationPercentage"`
}

type mockProfilePod struct {
	Name       string   `yaml:"name"`
	Containers []string `yaml:"containers"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileService struct {
	Type            string                 `yaml:"type"`
	Ports           map[string]client.Port `yaml:"ports"`
	InternalAddress string                 `yaml:"internal_address"`
	ExternalAddress string                 `yaml:"external_address"`
	ExternalIP      string                 `yaml:"external_ip"`
}

type mockProfileNamespace struct {
	ID      int                `yaml:"id"`
	Name    string             `yaml:"name"`
	Cluster mockProfileCluster `yaml:"cluster"`
}

//nolint:tagliatelle // YAML keys are part of the versioned mock profile format.
type mockProfileCluster struct {
	ID                   int    `yaml:"id"`
	Name                 string `yaml:"name"`
	AppsCustomBaseDomain string `yaml:"apps_custom_base_domain"`
}

type mockProfileState struct {
	Type        string `yaml:"type"`
	Text        string `yaml:"text"`
	Description string `yaml:"description"`
}

// LoadMockProfile reads a bounded, versioned YAML fixture. Unknown fields fail
// parsing so secrets and unsupported data cannot silently enter the UI model.
func LoadMockProfile(path string) (MockProfile, error) {
	f, err := os.Open(path) //nolint:gosec // The caller explicitly selects the local fixture path.
	if err != nil {
		return MockProfile{}, fmt.Errorf("open mock profile %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, mockProfileMaxSize+1))
	if err != nil {
		return MockProfile{}, fmt.Errorf("read mock profile %q: %w", path, err)
	}
	if len(data) > mockProfileMaxSize {
		return MockProfile{}, fmt.Errorf("%w: mock profile exceeds the %d-byte limit", errInvalidMockProfile, mockProfileMaxSize)
	}
	return ParseMockProfile(data)
}

// ParseMockProfile decodes one strict YAML document without exposing parser
// snippets, which can contain values copied into a malformed fixture.
func ParseMockProfile(data []byte) (MockProfile, error) {
	input, err := decodeMockProfile(data)
	if err != nil {
		return MockProfile{}, err
	}
	if len(input.Tenants) > 0 {
		if mockProfileMixesTopLevelTenants(input) {
			return MockProfile{}, errMockProfileCannotMixTopLevelTenant
		}
		return parseMockTenants(input.Tenants)
	}
	return parseTopLevelMockProfile(input)
}

func decodeMockProfile(data []byte) (mockProfileFile, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var input mockProfileFile
	if err := decoder.Decode(&input); err != nil {
		return mockProfileFile{}, errMockProfileContainsInvalidOrUnsupportedYAML
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return mockProfileFile{}, errMockProfileMustContainExactlyOneYAML
	}
	if input.Version != mockProfileVersion {
		return mockProfileFile{}, fmt.Errorf("%w: unsupported mock profile version %d; expected %d", errInvalidMockProfile,
			input.Version, mockProfileVersion)
	}
	return input, nil
}

func mockProfileMixesTopLevelTenants(input mockProfileFile) bool {
	return strings.TrimSpace(input.Tenant) != "" || len(input.Apps) > 0 || len(input.Alerts) > 0 ||
		len(input.Namespaces) > 0 || len(input.Registries) > 0 || len(input.Storages) > 0
}

func parseMockTenants(input []mockProfileTenant) (MockProfile, error) {
	if len(input) > mockProfileMaxApps {
		return MockProfile{}, errMockProfileHasTooManyTenants
	}
	tenants := make([]MockTenant, 0, len(input))
	totalApps := 0
	seen := make(map[string]struct{}, len(input))
	for _, raw := range input {
		name := strings.TrimSpace(raw.Name)
		if name == "" {
			return MockProfile{}, errMockProfileTenantNameIsRequired
		}
		if _, exists := seen[name]; exists {
			return MockProfile{}, fmt.Errorf("%w: mock profile has duplicate tenant %q", errInvalidMockProfile, name)
		}
		seen[name] = struct{}{}
		totalApps += len(raw.Apps)
		if totalApps > mockProfileMaxApps {
			return MockProfile{}, fmt.Errorf("%w: mock profile has more than %d apps", errInvalidMockProfile, mockProfileMaxApps)
		}
		tenant, err := convertMockTenant(raw, name)
		if err != nil {
			return MockProfile{}, err
		}
		tenants = append(tenants, tenant)
	}
	return MockProfile{
		Tenant: tenants[0].Name, Apps: tenants[0].Apps, Alerts: tenants[0].Alerts,
		Namespaces: tenants[0].Namespaces, Registries: tenants[0].Registries,
		Databases: tenants[0].Databases, Storages: tenants[0].Storages, Tenants: tenants,
	}, nil
}

func convertMockTenant(raw mockProfileTenant, name string) (MockTenant, error) {
	apps, err := convertMockApps(raw.Apps)
	if err != nil {
		return MockTenant{}, err
	}
	alerts, err := convertMockAlerts(raw.Alerts)
	if err != nil {
		return MockTenant{}, err
	}
	namespaces, err := convertMockNamespaces(raw.Namespaces)
	if err != nil {
		return MockTenant{}, err
	}
	registries, err := convertMockRegistries(raw.Registries)
	if err != nil {
		return MockTenant{}, err
	}
	if len(raw.Databases) > mockProfileMaxApps {
		return MockTenant{}, fmt.Errorf("%w: mock tenant %q has too many databases", errInvalidMockProfile, name)
	}
	return MockTenant{Name: name, Apps: apps, Alerts: alerts, Namespaces: mergeNamespaces(namespaces,
		namespacesFromApps(apps)), AppData: convertMockAppData(raw.Apps), Registries: registries,
		Databases: append([]client.Database(nil), raw.Databases...), Storages: append([]client.Storage(nil), raw.Storages...)}, nil
}

func parseTopLevelMockProfile(input mockProfileFile) (MockProfile, error) {
	if strings.TrimSpace(input.Tenant) == "" {
		return MockProfile{}, errMockProfileTenantIsRequired
	}
	if len(input.Apps) > mockProfileMaxApps {
		return MockProfile{}, fmt.Errorf("%w: mock profile has more than %d apps", errInvalidMockProfile, mockProfileMaxApps)
	}
	apps, err := convertMockApps(input.Apps)
	if err != nil {
		return MockProfile{}, err
	}
	appData := convertMockAppData(input.Apps)
	alerts, err := convertMockAlerts(input.Alerts)
	if err != nil {
		return MockProfile{}, err
	}
	namespaces, err := convertMockNamespaces(input.Namespaces)
	if err != nil {
		return MockProfile{}, err
	}
	registries, err := convertMockRegistries(input.Registries)
	if err != nil {
		return MockProfile{}, err
	}
	if len(input.Databases) > mockProfileMaxApps {
		return MockProfile{}, errMockProfileHasTooManyDatabases
	}
	return MockProfile{Tenant: input.Tenant, Apps: apps, Alerts: alerts, Namespaces: mergeNamespaces(namespaces,
		namespacesFromApps(apps)), AppData: appData, Registries: registries, Databases: append([]client.Database(nil),
		input.Databases...), Storages: append([]client.Storage(nil), input.Storages...)}, nil
}

func convertMockAppData(input []mockProfileApp) map[string]MockAppData {
	data := make(map[string]MockAppData, len(input))
	for _, app := range input {
		item := MockAppData{PlainEnvs: append([]client.EnvVar(nil), app.Envs...), SecretNames: append([]string(nil), app.SecretEnvNames...),
			Usage: &AppUsage{CPUCores: app.Usage.CPU, RAMBytes: app.Usage.RAM, DiskBytes: app.Usage.Disk},
			History: UsageHistory{CPU: app.History.CPU, CPUThrottle: app.History.CPUThrottle, RAM: app.History.RAM,
				Disk: app.History.Disk, NetworkReceive: app.History.NetworkReceive, NetworkSend: app.History.NetworkSend}}
		for _, log := range app.Logs {
			item.Logs = append(item.Logs, MockLog{Entry: client.LogEntry{Timestamp: log.Timestamp, Text: log.Text},
				Pod: log.Pod, Container: log.Container, Previous: log.Previous})
		}
		for _, pod := range app.Pods {
			containers := make([]appstate.Container, 0, len(pod.Containers))
			for _, name := range pod.Containers {
				containers = append(containers, appstate.Container{Name: name, Ready: true})
			}
			item.Pods = append(item.Pods, appstate.Pod{Name: pod.Name, Phase: "Running", State: "Running", Ready: true, Containers: containers})
		}
		data[app.ID] = item
	}
	return data
}

func convertMockNamespaces(input []mockProfileNamespace) ([]client.Namespace, error) {
	if len(input) > mockProfileMaxApps {
		return nil, fmt.Errorf("%w: mock profile has more than %d namespaces", errInvalidMockProfile, mockProfileMaxApps)
	}
	out := make([]client.Namespace, 0, len(input))
	seen := make(map[int]struct{}, len(input))
	for i, namespace := range input {
		if namespace.ID < 1 || strings.TrimSpace(namespace.Name) == "" {
			return nil, fmt.Errorf("%w: mock profile namespace %d requires a positive id and name", errInvalidMockProfile, i+1)
		}
		if _, exists := seen[namespace.ID]; exists {
			return nil, fmt.Errorf("%w: mock profile has duplicate namespace id %d", errInvalidMockProfile, namespace.ID)
		}
		seen[namespace.ID] = struct{}{}
		out = append(out, client.Namespace{
			ID: namespace.ID, Name: namespace.Name,
			Cluster: client.Cluster{ID: namespace.Cluster.ID, Name: namespace.Cluster.Name,
				AppsCustomBaseDomain: namespace.Cluster.AppsCustomBaseDomain},
		})
	}
	return out, nil
}

func namespacesFromApps(apps []client.App) []client.Namespace {
	namespaces := make([]client.Namespace, 0, len(apps))
	for _, app := range apps {
		namespaces = append(namespaces, app.Namespace)
	}
	return uniqueNamespaces(namespaces)
}

func convertMockApps(input []mockProfileApp) ([]client.App, error) {
	apps := make([]client.App, 0, len(input))
	seenIDs := make(map[string]struct{}, len(input))
	for i, app := range input {
		if strings.TrimSpace(app.ID) == "" || strings.TrimSpace(app.Name) == "" {
			return nil, fmt.Errorf("%w: mock profile app %d requires id and name", errInvalidMockProfile, i+1)
		}
		if app.Namespace.ID < 1 || strings.TrimSpace(app.Namespace.Name) == "" {
			return nil, fmt.Errorf("%w: mock profile app %q requires a namespace id and name", errInvalidMockProfile, app.ID)
		}
		if app.Replicas == nil {
			return nil, fmt.Errorf("%w: mock profile app %q requires replicas", errInvalidMockProfile, app.ID)
		}
		if *app.Replicas < 0 {
			return nil, fmt.Errorf("%w: mock profile app %q has negative replicas", errInvalidMockProfile, app.ID)
		}
		if app.DiskSizeInGi < 0 {
			return nil, fmt.Errorf("%w: mock profile app %q has negative disk size", errInvalidMockProfile, app.ID)
		}
		if app.DiskSizeInGi == 0 && len(app.DiskPartitions) > 0 {
			return nil, fmt.Errorf("%w: mock profile app %q has disk partitions but no disk size", errInvalidMockProfile, app.ID)
		}
		if _, exists := seenIDs[app.ID]; exists {
			return nil, fmt.Errorf("%w: mock profile has duplicate app id %q", errInvalidMockProfile, app.ID)
		}
		seenIDs[app.ID] = struct{}{}
		var service *client.AppService
		if app.Service.Type != "" || len(app.Service.Ports) > 0 || app.Service.InternalAddress != "" ||
			app.Service.ExternalAddress != "" || app.Service.ExternalIP != "" {
			service = &client.AppService{
				Type:            app.Service.Type,
				Ports:           app.Service.Ports,
				InternalAddress: app.Service.InternalAddress,
				ExternalAddress: app.Service.ExternalAddress,
				ExternalIP:      app.Service.ExternalIP,
			}
		}
		buildArgs, err := json.Marshal(app.GitBuildArgs)
		if err != nil {
			return nil, fmt.Errorf("marshal mock app build arguments: %w", err)
		}
		var plan *client.Plan
		if app.Plan != nil {
			plan = &client.Plan{ID: app.Plan.ID, Name: app.Plan.Name, CodeName: app.Plan.CodeName, PlanType: app.Plan.PlanType,
				CostType: app.Plan.CostType, ShowInCreateApp: app.Plan.ShowInCreateApp,
				Detail: client.PlanDetail{RAMLimit: app.Plan.Detail.RAMLimit, CPURequest: app.Plan.Detail.CPURequest}}
		}
		var customConfig *client.AppCustomConfig
		if app.CustomConfig.HPA != nil {
			h := app.CustomConfig.HPA
			customConfig = &client.AppCustomConfig{HPA: &client.AppHPAConfig{Enabled: h.Enabled, MinReplicas: h.MinReplicas,
				MaxReplicas: h.MaxReplicas, TargetCPUUtilizationPercentage: h.TargetCPUUtilizationPercentage}}
		}
		apps = append(apps, client.App{
			ID:   app.ID,
			Name: app.Name,
			Namespace: client.Namespace{
				ID:   app.Namespace.ID,
				Name: app.Namespace.Name,
				Cluster: client.Cluster{
					ID:                   app.Namespace.Cluster.ID,
					Name:                 app.Namespace.Cluster.Name,
					AppsCustomBaseDomain: app.Namespace.Cluster.AppsCustomBaseDomain,
				},
			},
			State: client.State{
				StateType:   app.State.Type,
				Text:        app.State.Text,
				Description: app.State.Description,
			},
			Replicas:        *app.Replicas,
			ReplicasPresent: true,
			CreationMethod:  app.CreationMethod,
			BuildMethod:     app.BuildMethod,
			GitRepoURL:      app.GitRepoURL, GitBranch: app.GitBranch, GitProvider: app.GitProvider,
			GitDockerfile: app.GitDockerfile, GitContext: app.GitContext, GitWorkdir: app.GitWorkdir,
			Builder: app.Builder, GitAutodeploy: app.GitAutodeploy, GitBuildArgs: buildArgs,
			ReadinessProbePath:  app.ReadinessProbePath,
			Command:             app.Command,
			Args:                app.Args,
			ImageRepo:           app.ImageRepo,
			ImageTag:            app.ImageTag,
			RAMLimit:            app.RAMLimit,
			CPURequest:          app.CPURequest,
			Plan:                plan,
			CustomConfig:        customConfig,
			ExternalHosts:       append([]string(nil), app.ExternalHosts...),
			CustomDomainAddress: app.CustomDomainAddress,
			CustomSubdomainAddr: app.CustomSubdomainAddr,
			Service:             service,
			Disk:                mockDisk(app.DiskSizeInGi, app.DiskPartitions),
		})
	}
	return apps, nil
}

func mockDisk(size int, partitions []mockProfileDiskPartition) *client.Disk {
	if size <= 0 {
		return nil
	}
	converted := make([]client.Partition, 0, len(partitions))
	for _, partition := range partitions {
		converted = append(converted, client.Partition{DisplayName: partition.DisplayName, MountPath: partition.MountPath,
			SubPath: partition.SubPath})
	}
	return &client.Disk{SizeInGi: size, Partitions: converted}
}
