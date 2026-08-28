package client

// page is the standard Django REST Framework pagination envelope.
type page[T any] struct {
	Count    int    `json:"count"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
	Results  []T    `json:"results"`
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
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Namespace           Namespace `json:"namespace"`
	State               State     `json:"state"`
	Plan                *Plan     `json:"plan"`
	Replicas            int       `json:"replicas"`
	IsEnabled           bool      `json:"is_enabled"`
	IsDeployable        bool      `json:"is_deployable"`
	IsHPAEnabled        bool      `json:"is_hpa_enabled"`
	RAMLimit            string    `json:"ram_limit"`
	CPURequest          string    `json:"cpu_request"`
	CustomDomainAddress string    `json:"custom_domain_address"`
	EnableSSL           bool      `json:"enable_SSL"`
	ImageRepo           string    `json:"image_repo"`
	ImageTag            string    `json:"image_tag"`
	CreationTime        string    `json:"creation_time"`
	UpdatedAt           string    `json:"updated_at"`

	// CreationMethod is how the app came to exist, and it is the field that
	// separates a plain workload from a managed service. There is no separate
	// "managed services" API: a oneclick Redis, Postgres, Grafana or Prometheus is
	// an ordinary app in this same list, carrying creation_method "redisnew",
	// "postgresqlnew", "grafana", "prometheus" and so on. Without surfacing it
	// there is no way to tell the two apart, which is why `get apps -o wide`
	// prints it and `--type` filters on it.
	CreationMethod string `json:"creation_method"`
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
