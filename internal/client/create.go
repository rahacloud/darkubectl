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
var ErrNoOrganizationID = errors.New("could not determine the numeric organization id")

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
// Everything below Replicas is optional. It can also be changed afterwards
// through UpdateApp, which PUTs the whole object back — the API has no partial
// update, but it is not read-only either.
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

	// Git, when non-nil, switches the app from pulling a prebuilt image to being
	// built by Darkube from a repository. ImageRepo/ImageTag are then chosen by
	// the platform (it pushes to registry.hamdocker.ir/<tenant>/<app>) and must
	// be left empty.
	Git *GitSource
}

// GitSource describes a repository Darkube builds the app from, producing
// creation_method "git_repo_url" rather than "docker_image".
//
// Every field maps to one the API returns on such an app; the zero values are
// filled from the defaults the console uses, so RepoURL alone is enough.
type GitSource struct {
	RepoURL    string
	Branch     string
	Provider   string // Github | Gitlab
	Dockerfile string
	Context    string
	Workdir    string
	Builder    string // dockerfile | heroku_buildpacks
	// BuildMethod is gitlabci or webhook. Autodeploy has a pointer type so an
	// explicit false is distinguishable from "unset".
	BuildMethod string
	Autodeploy  *bool
}

// Valid values for the enumerated build fields, as advertised by OPTIONS on
// /api/v1/darkube/apps/ (confirmed 2026-08-28).
const (
	CreationMethodDockerImage = "docker_image"
	CreationMethodGitRepoURL  = "git_repo_url"

	ProviderGithub = "Github"
	ProviderGitlab = "Gitlab"

	BuilderDockerfile      = "dockerfile"
	BuilderHerokuBuildpack = "heroku_buildpacks"

	BuildMethodGitlabCI = "gitlabci"
	BuildMethodWebhook  = "webhook"
)

// withDefaults fills the fields the console always sends, so a caller supplying
// only a repository URL still produces a payload the API accepts.
func (g GitSource) withDefaults() GitSource {
	if g.Branch == "" {
		g.Branch = "main"
	}
	if g.Provider == "" {
		g.Provider = ProviderGitlab
	}
	if g.Dockerfile == "" {
		g.Dockerfile = "./Dockerfile"
	}
	if g.Context == "" {
		g.Context = "."
	}
	if g.Workdir == "" {
		g.Workdir = "."
	}
	if g.Builder == "" {
		g.Builder = BuilderDockerfile
	}
	if g.BuildMethod == "" {
		g.BuildMethod = BuildMethodGitlabCI
	}
	if g.Autodeploy == nil {
		yes := true
		g.Autodeploy = &yes
	}
	return g
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
		"creation_method":      CreationMethodDockerImage,
		"image_repo":           in.ImageRepo,
		"image_tag":            in.ImageTag,
		"builder":              BuilderDockerfile,
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
	if in.Git != nil {
		// A git-built app names no image: Darkube builds one and pushes it to the
		// tenant's registry, then fills image_repo/image_tag itself.
		g := in.Git.withDefaults()
		payload["creation_method"] = CreationMethodGitRepoURL
		payload["builder"] = g.Builder
		payload["git_repo_url"] = g.RepoURL
		payload["git_branch_name"] = g.Branch
		payload["git_provider_type"] = g.Provider
		payload["git_build_dockerfile"] = g.Dockerfile
		payload["git_build_context"] = g.Context
		payload["git_build_workdir"] = g.Workdir
		payload["git_build_args"] = []any{}
		payload["build_method"] = g.BuildMethod
		payload["autodeploy_on_git_push"] = *g.Autodeploy
		delete(payload, "image_repo")
		delete(payload, "image_tag")
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
// create payload requires.
//
// The user profile is the authoritative source and, crucially, works for a
// tenant that holds no apps yet — the case the app-detail fallback below cannot
// serve, which used to make it impossible to create the *first* app in a new
// organization.
func (c *Client) OrganizationID(ctx context.Context) (int, error) {
	if org, err := c.Organization(ctx); err == nil && org.ID != 0 {
		return org.ID, nil
	}
	return c.organizationIDFromApp(ctx)
}

// organizationIDFromApp derives the organization id from an existing app's v1
// detail. It is the fallback for credentials that cannot read the profile, and
// fails on a tenant with no apps.
func (c *Client) organizationIDFromApp(ctx context.Context) (int, error) {
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
