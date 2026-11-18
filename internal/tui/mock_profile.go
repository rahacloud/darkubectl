package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"gopkg.in/yaml.v3"
)

const (
	mockProfileVersion = 1
	mockProfileMaxSize = 1 << 20
	mockProfileMaxApps = 1000
)

// MockProfile contains only the safe, read-only fields the browser renders.
type MockProfile struct {
	Tenant     string
	Apps       []client.App
	Namespaces []client.Namespace
	Tenants    []MockTenant
}

// MockTenant is one tenant and its local, read-only app snapshot.
type MockTenant struct {
	Name       string
	Apps       []client.App
	Namespaces []client.Namespace
}

type mockProfileFile struct {
	Version    int                    `yaml:"version"`
	Tenant     string                 `yaml:"tenant"`
	Apps       []mockProfileApp       `yaml:"apps"`
	Namespaces []mockProfileNamespace `yaml:"namespaces"`
	Tenants    []mockProfileTenant    `yaml:"tenants"`
}

type mockProfileTenant struct {
	Name       string                 `yaml:"name"`
	Apps       []mockProfileApp       `yaml:"apps"`
	Namespaces []mockProfileNamespace `yaml:"namespaces"`
}

type mockProfileApp struct {
	ID                  string               `yaml:"id"`
	Name                string               `yaml:"name"`
	Namespace           mockProfileNamespace `yaml:"namespace"`
	State               mockProfileState     `yaml:"state"`
	Replicas            *int                 `yaml:"replicas"`
	CreationMethod      string               `yaml:"creation_method"`
	ImageRepo           string               `yaml:"image_repo"`
	ImageTag            string               `yaml:"image_tag"`
	CustomDomainAddress string               `yaml:"custom_domain_address"`
	ExternalHosts       []string             `yaml:"external_hosts"`
	Service             mockProfileService   `yaml:"service"`
	ReadinessProbePath  string               `yaml:"readiness_probe_path"`
	Command             string               `yaml:"command"`
	Args                string               `yaml:"args"`
	Inputs              []string             `yaml:"inputs"`
	RAMLimit            string               `yaml:"ram_limit"`
	CPURequest          string               `yaml:"cpu_request"`
	DiskSizeInGi        int                  `yaml:"disk_size_in_gi"`
}

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

type mockProfileCluster struct {
	ID   int    `yaml:"id"`
	Name string `yaml:"name"`
}

type mockProfileState struct {
	Type        string `yaml:"type"`
	Text        string `yaml:"text"`
	Description string `yaml:"description"`
}

// LoadMockProfile reads a bounded, versioned YAML fixture. Unknown fields fail
// parsing so secrets and unsupported data cannot silently enter the UI model.
func LoadMockProfile(path string) (MockProfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return MockProfile{}, fmt.Errorf("open mock profile %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, mockProfileMaxSize+1))
	if err != nil {
		return MockProfile{}, fmt.Errorf("read mock profile %q: %w", path, err)
	}
	if len(data) > mockProfileMaxSize {
		return MockProfile{}, fmt.Errorf("mock profile exceeds the %d-byte limit", mockProfileMaxSize)
	}
	return ParseMockProfile(data)
}

// ParseMockProfile decodes one strict YAML document without exposing parser
// snippets, which can contain values copied into a malformed fixture.
func ParseMockProfile(data []byte) (MockProfile, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var input mockProfileFile
	if err := decoder.Decode(&input); err != nil {
		return MockProfile{}, errors.New("mock profile contains invalid or unsupported YAML")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return MockProfile{}, errors.New("mock profile must contain exactly one YAML document")
	}
	if input.Version != mockProfileVersion {
		return MockProfile{}, fmt.Errorf("unsupported mock profile version %d; expected %d", input.Version, mockProfileVersion)
	}
	if len(input.Tenants) > 0 {
		if strings.TrimSpace(input.Tenant) != "" || len(input.Apps) > 0 || len(input.Namespaces) > 0 {
			return MockProfile{}, errors.New("mock profile cannot mix top-level tenant/apps with tenants")
		}
		if len(input.Tenants) > mockProfileMaxApps {
			return MockProfile{}, errors.New("mock profile has too many tenants")
		}
		tenants := make([]MockTenant, 0, len(input.Tenants))
		totalApps := 0
		seenTenants := make(map[string]struct{}, len(input.Tenants))
		for _, tenant := range input.Tenants {
			name := strings.TrimSpace(tenant.Name)
			if name == "" {
				return MockProfile{}, errors.New("mock profile tenant name is required")
			}
			if _, exists := seenTenants[name]; exists {
				return MockProfile{}, fmt.Errorf("mock profile has duplicate tenant %q", name)
			}
			seenTenants[name] = struct{}{}
			totalApps += len(tenant.Apps)
			if totalApps > mockProfileMaxApps {
				return MockProfile{}, fmt.Errorf("mock profile has more than %d apps", mockProfileMaxApps)
			}
			apps, err := convertMockApps(tenant.Apps)
			if err != nil {
				return MockProfile{}, err
			}
			namespaces, err := convertMockNamespaces(tenant.Namespaces)
			if err != nil {
				return MockProfile{}, err
			}
			tenants = append(tenants, MockTenant{Name: name, Apps: apps, Namespaces: mergeNamespaces(namespaces, namespacesFromApps(apps))})
		}
		return MockProfile{Tenant: tenants[0].Name, Apps: tenants[0].Apps, Namespaces: tenants[0].Namespaces, Tenants: tenants}, nil
	}
	if strings.TrimSpace(input.Tenant) == "" {
		return MockProfile{}, errors.New("mock profile tenant is required")
	}
	if len(input.Apps) > mockProfileMaxApps {
		return MockProfile{}, fmt.Errorf("mock profile has more than %d apps", mockProfileMaxApps)
	}
	apps, err := convertMockApps(input.Apps)
	if err != nil {
		return MockProfile{}, err
	}
	namespaces, err := convertMockNamespaces(input.Namespaces)
	if err != nil {
		return MockProfile{}, err
	}
	return MockProfile{Tenant: input.Tenant, Apps: apps, Namespaces: mergeNamespaces(namespaces, namespacesFromApps(apps))}, nil
}

func convertMockNamespaces(input []mockProfileNamespace) ([]client.Namespace, error) {
	if len(input) > mockProfileMaxApps {
		return nil, fmt.Errorf("mock profile has more than %d namespaces", mockProfileMaxApps)
	}
	out := make([]client.Namespace, 0, len(input))
	seen := make(map[int]struct{}, len(input))
	for i, namespace := range input {
		if namespace.ID < 1 || strings.TrimSpace(namespace.Name) == "" {
			return nil, fmt.Errorf("mock profile namespace %d requires a positive id and name", i+1)
		}
		if _, exists := seen[namespace.ID]; exists {
			return nil, fmt.Errorf("mock profile has duplicate namespace id %d", namespace.ID)
		}
		seen[namespace.ID] = struct{}{}
		out = append(out, client.Namespace{
			ID: namespace.ID, Name: namespace.Name,
			Cluster: client.Cluster{ID: namespace.Cluster.ID, Name: namespace.Cluster.Name},
		})
	}
	return out, nil
}

func namespacesFromApps(apps []client.App) []client.Namespace {
	var namespaces []client.Namespace
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
			return nil, fmt.Errorf("mock profile app %d requires id and name", i+1)
		}
		if app.Namespace.ID < 1 || strings.TrimSpace(app.Namespace.Name) == "" {
			return nil, fmt.Errorf("mock profile app %q requires a namespace id and name", app.ID)
		}
		if app.Replicas == nil {
			return nil, fmt.Errorf("mock profile app %q requires replicas", app.ID)
		}
		if *app.Replicas < 0 {
			return nil, fmt.Errorf("mock profile app %q has negative replicas", app.ID)
		}
		if app.DiskSizeInGi < 0 {
			return nil, fmt.Errorf("mock profile app %q has negative disk size", app.ID)
		}
		if _, exists := seenIDs[app.ID]; exists {
			return nil, fmt.Errorf("mock profile has duplicate app id %q", app.ID)
		}
		seenIDs[app.ID] = struct{}{}
		var service *client.AppService
		if app.Service.Type != "" || len(app.Service.Ports) > 0 || app.Service.InternalAddress != "" || app.Service.ExternalAddress != "" || app.Service.ExternalIP != "" {
			service = &client.AppService{
				Type:            app.Service.Type,
				Ports:           app.Service.Ports,
				InternalAddress: app.Service.InternalAddress,
				ExternalAddress: app.Service.ExternalAddress,
				ExternalIP:      app.Service.ExternalIP,
			}
		}
		apps = append(apps, client.App{
			ID:   app.ID,
			Name: app.Name,
			Namespace: client.Namespace{
				ID:   app.Namespace.ID,
				Name: app.Namespace.Name,
				Cluster: client.Cluster{
					ID:   app.Namespace.Cluster.ID,
					Name: app.Namespace.Cluster.Name,
				},
			},
			State: client.State{
				StateType:   app.State.Type,
				Text:        app.State.Text,
				Description: app.State.Description,
			},
			Replicas:            *app.Replicas,
			ReplicasPresent:     true,
			CreationMethod:      app.CreationMethod,
			ReadinessProbePath:  app.ReadinessProbePath,
			Command:             app.Command,
			Args:                app.Args,
			Inputs:              append([]string(nil), app.Inputs...),
			ImageRepo:           app.ImageRepo,
			ImageTag:            app.ImageTag,
			RAMLimit:            app.RAMLimit,
			CPURequest:          app.CPURequest,
			ExternalHosts:       append([]string(nil), app.ExternalHosts...),
			CustomDomainAddress: app.CustomDomainAddress,
			Service:             service,
			Disk:                mockDisk(app.DiskSizeInGi),
		})
	}
	return apps, nil
}

func mockDisk(size int) *client.Disk {
	if size <= 0 {
		return nil
	}
	return &client.Disk{SizeInGi: size}
}
