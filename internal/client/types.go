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
	IsHPAEnabled        bool      `json:"is_hpa_enbaled"` // API spells it this way
	RAMLimit            string    `json:"ram_limit"`
	CPURequest          string    `json:"cpu_request"`
	CustomDomainAddress string    `json:"custom_domain_address"`
	EnableSSL           bool      `json:"enable_SSL"`
}
