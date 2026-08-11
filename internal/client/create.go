package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// appsPathV1 is the create endpoint (v1, not v2 — v2 POST 500s).
const appsPathV1 = "/api/v1/darkube/apps/"

// ErrNoOrganizationID is returned when the numeric org id can't be derived.
var ErrNoOrganizationID = errors.New("could not determine the organization id (no existing app to read it from)")

// Port is one entry of svc.ports, keyed by name in the API ("main", "amqp", …).
type Port struct {
	ContainerPort int    `json:"containerPort" yaml:"containerPort"`
	ServicePort   int    `json:"servicePort"   yaml:"servicePort"`
	Protocol      string `json:"protocol"      yaml:"protocol"`
}

// Partition is one mount inside a Disk.
type Partition struct {
	DisplayName string `json:"display_name" yaml:"name"`
	MountPath   string `json:"mount_path"   yaml:"mountPath"`
	SubPath     string `json:"sub_path"     yaml:"subPath"`
}

// Disk is the app's persistent volume. SizeInGi of 0 means "no disk".
type Disk struct {
	Partitions       []Partition `json:"partitions"                   yaml:"partitions"`
	SizeInGi         int         `json:"size_in_Gi"                   yaml:"sizeInGi"`
	StorageClassName string      `json:"storage_class_name,omitempty" yaml:"storageClassName"`
	SetFSGroup       bool        `json:"set_fsgroup"                  yaml:"setFsGroup"`
}

// EnvVar is one entry of envs or secret_envs. The API calls the key "name".
type EnvVar struct {
	Name  string `json:"name"  yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

// CreateAppInput describes a Docker-image app to create. Names have already been
// resolved to ids by the caller.
//
// Everything below Replicas is optional and, crucially, can only be set here:
// PATCH on an existing app 500s, so an app created without its ports, disk or
// environment cannot be completed through the API afterwards.
type CreateAppInput struct {
	Name           string
	NamespaceID    int
	OrganizationID int
	PlanID         string
	ImageRepo      string
	ImageTag       string
	Command        string
	Args           string
	Replicas       int

	SvcType    string
	Ports      map[string]Port
	Disk       *Disk
	Envs       []EnvVar
	SecretEnvs []EnvVar
}

// CreateApp creates a Docker-image app and returns the created object. The
// payload mirrors the console's confirmed POST /api/v1/darkube/apps/ request.
func (c *Client) CreateApp(ctx context.Context, in CreateAppInput) (map[string]any, error) {
	data, err := c.do(ctx, http.MethodPost, appsPathV1, nil, buildCreatePayload(in))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(data) > 0 {
		_ = decodeInto(data, &out)
	}
	return out, nil
}

func buildCreatePayload(in CreateAppInput) map[string]any {
	svcType := in.SvcType
	if svcType == "" {
		svcType = "ClusterIP"
	}
	ports := map[string]any{}
	for name, p := range in.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "TCP"
		}
		svcPort := p.ServicePort
		if svcPort == 0 {
			svcPort = p.ContainerPort
		}
		ports[name] = map[string]any{
			"containerPort": p.ContainerPort,
			"servicePort":   svcPort,
			"protocol":      proto,
		}
	}

	payload := map[string]any{
		"name":                 in.Name,
		"namespace":            in.NamespaceID,
		"organization":         in.OrganizationID,
		"plan":                 in.PlanID,
		"creation_method":      "docker_image",
		"image_repo":           in.ImageRepo,
		"image_tag":            in.ImageTag,
		"builder":              "dockerfile",
		"command":              in.Command,
		"args":                 in.Args,
		"replicas":             in.Replicas,
		"svc":                  map[string]any{"type": svcType, "ports": ports},
		"custom_config":        map[string]any{},
		"readiness_probe_path": "",
		"backup_config":        nil,
		"deploy_context":       nil,
		"ssl_challenge_type":   "dns01",
	}
	if in.Disk != nil && in.Disk.SizeInGi > 0 {
		payload["disk"] = in.Disk
	}
	if len(in.Envs) > 0 {
		payload["envs"] = in.Envs
	}
	if len(in.SecretEnvs) > 0 {
		payload["secret_envs"] = in.SecretEnvs
	}
	return payload
}

// OrganizationID returns the current tenant's numeric organization id, which the
// create payload requires. It is read from an existing app's v1 detail (the app
// list does not include it).
func (c *Client) OrganizationID(ctx context.Context) (int, error) {
	q := url.Values{}
	q.Set("limit", "1")
	q.Set("fields", "id")

	var list page[App]
	if err := c.getJSON(ctx, appsPathV2, q, &list); err != nil {
		return 0, err
	}
	if len(list.Results) == 0 {
		return 0, ErrNoOrganizationID
	}

	var detail struct {
		Organization struct {
			ID int `json:"id"`
		} `json:"organization"`
	}
	if err := c.getJSON(ctx, appsPathV1+url.PathEscape(list.Results[0].ID)+"/", nil, &detail); err != nil {
		return 0, err
	}
	if detail.Organization.ID == 0 {
		return 0, ErrNoOrganizationID
	}
	return detail.Organization.ID, nil
}
