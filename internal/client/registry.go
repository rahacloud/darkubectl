package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// The container registry: the tenant's space on registry.hamdocker.ir, where a
// git-backed app's builds are pushed. The routes come from the console's
// container-registry page (console-remote, 2026-09-30). Every registry action
// hangs off the registry's id, and a repository is named in the body rather
// than the path, because repository names contain slashes.
const (
	registryPathV1   = "/api/v1/registry/"
	registryGCPathV1 = "/api/v1/registry-gc-strategies/"

	// keyRepositoryName is how every registry action names its repository.
	keyRepositoryName = "repository_name"

	// DefaultRegistryName is the registry the console's page opens on.
	DefaultRegistryName = "hamdocker"
)

// ErrMissingField means the API answered without a field it owes.
var ErrMissingField = errors.New("response is missing a field")

// Registry is one container registry the tenant can push to.
type Registry struct {
	ID       FlexID `json:"id"       yaml:"id"`
	Name     string `json:"name"     yaml:"name"`
	Address  string `json:"address"  yaml:"address"`
	Username string `json:"username" yaml:"username"`
	// IsProvidedAsSaaS marks the shared hamdocker registry, where the tenant's
	// images live under <address>/<username>/.
	IsProvidedAsSaaS  bool   `json:"is_provided_as_saas"           yaml:"is_provided_as_saas"`
	StorageUsageBytes *int64 `json:"storage_usage_bytes,omitempty" yaml:"storage_usage_bytes,omitempty"`
}

// PushPrefix is the image name prefix that images in this registry take.
func (r Registry) PushPrefix() string {
	if r.IsProvidedAsSaaS && r.Username != "" {
		return r.Address + "/" + r.Username
	}
	return r.Address
}

// Repository is one image repository in a registry.
type Repository struct {
	Name       string `json:"name"        yaml:"name"`
	LastPushed string `json:"last_pushed" yaml:"last_pushed"`
	RecentTag  string `json:"recent_tag"  yaml:"recent_tag"`
}

// Digest is one image manifest in a repository, with the tags pointing at it.
type Digest struct {
	Digest     string   `json:"digest_sha"  yaml:"digest_sha"`
	Tags       []string `json:"tags"        yaml:"tags"`
	Size       int64    `json:"size"        yaml:"size"`
	LastPushed string   `json:"last_pushed" yaml:"last_pushed"`
}

// GCStrategy is a registry retention rule: keep the newest KeepCount images,
// or those pushed within KeepDuration, and let the rest be collected. It
// applies either to a whole registry or to one image repository.
type GCStrategy struct {
	ID              FlexID       `json:"id"               yaml:"id"`
	DestinationType string       `json:"destination_type" yaml:"destination_type"`
	Registry        *RegistryRef `json:"registry"         yaml:"registry"`
	ImageRepo       string       `json:"image_repo"       yaml:"image_repo"`
	KeepType        string       `json:"keep_type"        yaml:"keep_type"`
	KeepCount       *int         `json:"keep_count"       yaml:"keep_count"`
	// KeepDuration is a Django duration, "<days> 00:00:00".
	KeepDuration *string `json:"keep_duration" yaml:"keep_duration"`
}

// RegistryRef is the registry a retention rule names. A read nests the whole
// registry object; a write takes its bare id, and "" when the rule is on one
// image. It decodes from either.
type RegistryRef struct {
	ID   FlexID `json:"id"   yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

// UnmarshalJSON accepts a registry object, a bare id, or an empty string.
func (r *RegistryRef) UnmarshalJSON(data []byte) error {
	var id FlexID
	if err := json.Unmarshal(data, &id); err == nil {
		*r = RegistryRef{ID: id}
		return nil
	}
	type plain RegistryRef
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	*r = RegistryRef(p)
	return nil
}

// GC strategy destinations and keep types, as the console sends them.
const (
	GCDestinationRegistry  = "registry"
	GCDestinationImageRepo = "image_repo"
	GCKeepCount            = "count"
	GCKeepDuration         = "duration"
)

// ListRegistries returns the registries the tenant can use.
func (c *Client) ListRegistries(ctx context.Context) ([]Registry, error) {
	var p page[Registry]
	if err := c.getJSON(ctx, registryPathV1, nil, &p); err != nil {
		return nil, err
	}
	return p.Results, nil
}

// ListRepositories returns every image repository in a registry.
func (c *Client) ListRepositories(ctx context.Context, registryID FlexID) ([]Repository, error) {
	// The console pages by 10; ask for more at a time, since the whole list is
	// wanted.
	const pageSize = 100
	var all []Repository
	for offset := 0; ; offset += pageSize {
		q := url.Values{}
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(pageSize))
		var p page[Repository]
		if err := c.getJSON(ctx, registryAction(registryID, "list_repositories/"), q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		if p.Next == "" || len(p.Results) == 0 || len(all) >= p.Count {
			return all, nil
		}
	}
}

// ListDigests returns the manifests of one repository with their tags.
func (c *Client) ListDigests(ctx context.Context, registryID FlexID, repo string) ([]Digest, error) {
	var out []Digest
	if err := c.postJSON(ctx, registryAction(registryID, "list_repository_digests/"),
		map[string]any{keyRepositoryName: repo}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteRepository deletes an image repository with every tag in it.
func (c *Client) DeleteRepository(ctx context.Context, registryID FlexID, repo string) error {
	return c.postJSON(ctx, registryAction(registryID, "delete_repository/"),
		map[string]any{keyRepositoryName: repo}, nil)
}

// DeleteDigests deletes manifests, and so every tag pointing at them.
func (c *Client) DeleteDigests(ctx context.Context, registryID FlexID, repo string, digests []string) error {
	return c.postJSON(ctx, registryAction(registryID, "delete_digests/"),
		map[string]any{keyRepositoryName: repo, "digests_sha": digests}, nil)
}

// DeleteTags removes tags from one manifest, leaving the manifest itself.
func (c *Client) DeleteTags(ctx context.Context, registryID FlexID, repo, digest string, tags []string) error {
	return c.postJSON(ctx, registryAction(registryID, "delete_tags/"),
		map[string]any{keyRepositoryName: repo, "tags": tags, "digest_sha": digest}, nil)
}

// RegistryStorageUsage returns how many bytes a registry holds.
func (c *Client) RegistryStorageUsage(ctx context.Context, registryID FlexID) (int64, error) {
	var out struct {
		Bytes *int64 `json:"storage_usage_bytes"`
	}
	if err := c.getJSON(ctx, registryAction(registryID, "storage_usage"), nil, &out); err != nil {
		return 0, err
	}
	if out.Bytes == nil {
		return 0, fmt.Errorf("%w: storage_usage_bytes", ErrMissingField)
	}
	return *out.Bytes, nil
}

// RegistryPassword reads a registry's password out of the vault.
func (c *Client) RegistryPassword(ctx context.Context, registryID FlexID) (string, error) {
	var out struct {
		Password string `json:"vault_password"`
	}
	if err := c.postJSON(ctx, registryAction(registryID, "read_vault_secret/"),
		map[string]any{"secret_fields": []string{"vault_password"}}, &out); err != nil {
		return "", err
	}
	if out.Password == "" {
		return "", fmt.Errorf("%w: vault_password", ErrMissingField)
	}
	return out.Password, nil
}

// ListGCStrategies returns the tenant's registry retention rules.
func (c *Client) ListGCStrategies(ctx context.Context) ([]GCStrategy, error) {
	var p page[GCStrategy]
	if err := c.getJSON(ctx, registryGCPathV1, nil, &p); err != nil {
		return nil, err
	}
	return p.Results, nil
}

// CreateGCStrategy adds a retention rule. body carries destination_type,
// registry or image_repo, keep_type and keep_count or keep_duration.
func (c *Client) CreateGCStrategy(ctx context.Context, body map[string]any) error {
	return c.postJSON(ctx, registryGCPathV1, body, nil)
}

// UpdateGCStrategy changes a rule's keep_type, keep_count and keep_duration.
//
// The trailing slash is required. The console omits it, and without it the
// server redirects and a PUT replayed as a GET changes nothing (2026-09-30).
func (c *Client) UpdateGCStrategy(ctx context.Context, id FlexID, body map[string]any) error {
	_, err := c.do(ctx, http.MethodPut, registryGCPathV1+url.PathEscape(string(id))+"/", nil, body)
	return err
}

// DeleteGCStrategy removes a retention rule.
func (c *Client) DeleteGCStrategy(ctx context.Context, id FlexID) error {
	_, err := c.do(ctx, http.MethodDelete, registryGCPathV1+url.PathEscape(string(id))+"/", nil, nil)
	return err
}

func registryAction(id FlexID, action string) string {
	return registryPathV1 + url.PathEscape(string(id)) + "/" + action
}

// postJSON performs a POST and decodes the body into out, if out is non-nil.
func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	data, err := c.do(ctx, http.MethodPost, path, nil, body)
	if err != nil || out == nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
