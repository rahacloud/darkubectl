package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// page is the standard Django REST Framework pagination envelope.
type page[T any] struct {
	Count    int    `json:"count"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
	Results  []T    `json:"results"`
}

// FlexID is an identifier whose JSON type the API does not pin down: the same
// id can arrive as a string on one route and a number on another, so it
// decodes from either.
type FlexID string

// UnmarshalJSON accepts a JSON string or number.
func (f *FlexID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	*f = FlexID(n.String())
	return nil
}

// Cluster is the physical cluster an app's namespace lives on.
type Cluster struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	LocationCountry string `json:"location_country"`
	IsOnPremise     bool   `json:"is_on_premise"`
}

// Namespace maps to a Darkube "project" (a Kubernetes namespace).
type Namespace struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Cluster Cluster `json:"cluster"`
}

// State is an app's live health, as reported by the platform.
type State struct {
	StateType   string `json:"state_type"`
	Text        string `json:"text"`
	Description string `json:"description"`
}

// Plan is a resource/pricing plan.
type Plan struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	CodeName        string     `json:"code_name"`
	PlanType        string     `json:"plan_type"`
	CostType        string     `json:"cost_type"`
	ShowInCreateApp bool       `json:"show_in_create_app"`
	Detail          PlanDetail `json:"detail"`
	Cluster         *Cluster   `json:"cluster"`
}

// PlanDetail holds an app plan's resource sizing (megabytes / millicores).
type PlanDetail struct {
	RAMLimit   int `json:"ram_limit"`
	CPURequest int `json:"cpu_request"`
}

// IsCreatable reports whether a plan can be picked when creating an app.
func (p Plan) IsCreatable() bool {
	return p.PlanType == "app" && p.ShowInCreateApp
}

// App is a Darkube application (maps to a Kubernetes workload).
//
// Only the commonly used fields are typed; `darkubectl describe`/`-o json` read
// the raw object so no data is lost to this partial view.
type App struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Namespace Namespace `json:"namespace"`
	State     State     `json:"state"`
	Plan      *Plan     `json:"plan"`
	Replicas  int       `json:"replicas"`
	// ReplicasPresent distinguishes an explicit zero from an omitted/null API value.
	ReplicasPresent     bool     `json:"-" yaml:"-"`
	IsEnabled           bool     `json:"is_enabled"`
	IsDeployable        bool     `json:"is_deployable"`
	IsHPAEnabled        bool     `json:"is_hpa_enabled"`
	RAMLimit            string   `json:"ram_limit"`
	CPURequest          string   `json:"cpu_request"`
	Disk                *Disk    `json:"disk"`
	ExternalHosts       []string `json:"external_hosts"`
	CustomDomainAddress string   `json:"custom_domain_address"`
	EnableSSL           bool     `json:"enable_SSL"`
	ReadinessProbePath  string   `json:"-" yaml:"-"`
	Command             string   `json:"-" yaml:"-"`
	Args                string   `json:"-" yaml:"-"`
	ImageRepo           string   `json:"image_repo"`
	ImageTag            string   `json:"image_tag"`
	CreationTime        string   `json:"creation_time"`
	UpdatedAt           string   `json:"updated_at"`

	// CreationMethod is how the app came to exist, and it is the field that
	// separates a plain workload from a managed service. There is no separate
	// "managed services" API: a oneclick Redis, Postgres, Grafana or Prometheus is
	// an ordinary app in this same list, carrying creation_method "redisnew",
	// "postgresqlnew", "grafana", "prometheus" and so on. Without surfacing it
	// there is no way to tell the two apart, which is why `get apps -o wide`
	// prints it and `--type` filters on it.
	CreationMethod string           `json:"creation_method"`
	Service        *AppService      `json:"-" yaml:"-"`
	CustomConfig   *AppCustomConfig `json:"-" yaml:"-"`
	Inputs         []string         `json:"-" yaml:"-"`
}

// AppService contains the platform-provided addresses for an app's service.
// The API uses camel-cased keys inside the otherwise snake-cased app payload.
type AppService struct {
	Type            string          `json:"type"`
	Ports           map[string]Port `json:"ports"`
	InternalAddress string          `json:"internalAddress"`
	ExternalAddress string          `json:"externalAddress"`
	ExternalIP      string          `json:"externalIP"`
}

// AppCustomConfig exposes only the container probe fields used in the Summary.
// Other chart configuration is intentionally omitted from this safe read model.
type AppCustomConfig struct {
	Container *AppContainerConfig `json:"container"`
}

type AppContainerConfig struct {
	ReadinessProbe *AppProbe `json:"readinessProbe"`
}

type AppProbe struct {
	HTTPGet             *AppHTTPGet    `json:"httpGet"`
	Exec                *AppExecAction `json:"exec"`
	TCPSocket           *AppTCPSocket  `json:"tcpSocket"`
	InitialDelaySeconds int            `json:"initialDelaySeconds"`
	PeriodSeconds       int            `json:"periodSeconds"`
	TimeoutSeconds      int            `json:"timeoutSeconds"`
	SuccessThreshold    int            `json:"successThreshold"`
	FailureThreshold    int            `json:"failureThreshold"`
}

type AppHTTPGet struct {
	Path string          `json:"path"`
	Port json.RawMessage `json:"port"`
}

type AppExecAction struct {
	Command []string `json:"command"`
}

type AppTCPSocket struct {
	Port json.RawMessage `json:"port"`
}

// UnmarshalJSON preserves whether desired replicas were supplied by the API.
// JSON null and an omitted field are both treated as unknown; explicit zero is
// a known value and remains available through Replicas as before.
func (a *App) UnmarshalJSON(data []byte) error {
	type appAlias App
	var decoded appAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	raw, exists := fields["replicas"]
	*a = App(decoded)
	a.ReplicasPresent = exists && len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	a.Inputs = inputNames(fields["inputs"])
	for name, target := range map[string]any{
		"svc":                  &a.Service,
		"custom_config":        &a.CustomConfig,
		"readiness_probe_path": &a.ReadinessProbePath,
		"command":              &a.Command,
		"args":                 &a.Args,
	} {
		value, present := fields[name]
		if present && len(value) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if err := json.Unmarshal(value, target); err != nil {
				return fmt.Errorf("app.%s: %w", name, err)
			}
		}
	}
	return nil
}

func inputNames(raw json.RawMessage) []string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var names []string
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) == nil {
		for _, value := range values {
			var name string
			if json.Unmarshal(value, &name) == nil {
				continue
			}
			names = appendInputName(names, value)
		}
	} else {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) == nil {
			for name := range object {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func appendInputName(names []string, raw json.RawMessage) []string {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return names
	}
	for _, key := range []string{"name", "key", "id"} {
		var name string
		if json.Unmarshal(object[key], &name) == nil && name != "" {
			return append(names, name)
		}
	}
	return names
}

// plainCreationMethods are the methods that produce an ordinary workload: an
// image pulled from a registry, a repository Darkube builds, or an upload.
// Every other value in the enum names a marketplace/oneclick service.
//
// The full set, from OPTIONS /api/v1/darkube/apps/ on 2026-08-28: confluence,
// docker_image, elasticsearch, file_upload, gitlab_runner, git_repo_url, grafana,
// jira, jirasm, jupyter, kafka, kibana, keycloak, mariadb, metabase, minio,
// minionew, mongodb, mssql, mysql, nextcloud, nginx, postgresql, postgresqlnew,
// prometheus, pyroscope, rabbitmq, redis, redisnew, stolon, rocketchat,
// wordpress, kenar_divar.
var plainCreationMethods = map[string]bool{
	CreationMethodDockerImage: true,
	CreationMethodGitRepoURL:  true,
	"file_upload":             true,
}

// IsManagedService reports whether the app is a marketplace/oneclick service
// (Redis, Postgres, Grafana, …) rather than a workload someone built.
func (a App) IsManagedService() bool {
	return a.CreationMethod != "" && !plainCreationMethods[a.CreationMethod]
}

// Image is the container image the app runs, as repo:tag. The v2 list route
// returns the whole object, so this costs no extra request — which is what
// makes "which build is each app on?" answerable for a whole namespace at once.
func (a App) Image() string {
	switch {
	case a.ImageRepo == "":
		return ""
	case a.ImageTag == "":
		return a.ImageRepo
	default:
		return a.ImageRepo + ":" + a.ImageTag
	}
}
