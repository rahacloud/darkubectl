package client

import "context"

// The object-storage API is its own tree under /storage/v2, not part of
// /api/v1/darkube. Every collection returns the SAME list of storages and
// hangs the thing you asked for off each one, so the endpoint, the buckets
// and the keys arrive as four separate reads of one shape.
const (
	storagePathV2 = "/storage/v2/storage/"
	bucketPathV2  = "/storage/v2/bucket/"
	keyPathV2     = "/storage/v2/key/"
	domainPathV2  = "/storage/v2/domain/"
)

// Bucket is one bucket on a storage. Size is a string of bytes.
type Bucket struct {
	ID         string `json:"id"          yaml:"id"`
	Name       string `json:"name"        yaml:"name"`
	Status     string `json:"status"      yaml:"status"`
	Size       string `json:"size"        yaml:"size"`
	IsPublic   bool   `json:"is_public"   yaml:"isPublic"`
	CreateTime string `json:"create_time" yaml:"createTime"`
}

// StorageKey is an access key. The secret is returned only once, by the call
// that creates it, and is not readable afterwards.
type StorageKey struct {
	Name    string   `json:"name"    yaml:"name"`
	Actions []string `json:"actions" yaml:"actions"`
	Buckets []string `json:"buckets" yaml:"buckets"`
}

// StorageDomain is the S3 endpoint a storage answers on. The non-custom one is
// the address to give an S3 client: a per-tenant name under hs3.ir, which
// cannot be derived from the tenant slug because it carries a random suffix.
type StorageDomain struct {
	URL             string `json:"url"               yaml:"url"`
	Custom          bool   `json:"custom"            yaml:"custom"`
	IsDNSChallenged bool   `json:"is_dns_challenged" yaml:"isDnsChallenged"`
}

// Storage is one object-storage cluster the tenant can use.
type Storage struct {
	ID             string          `json:"id"              yaml:"id"`
	Name           string          `json:"name"            yaml:"name"`
	StorageClass   string          `json:"storage_class"   yaml:"storageClass"`
	IsShared       bool            `json:"is_shared"       yaml:"isShared"`
	TraefikAddress string          `json:"traefik_address" yaml:"traefikAddress"`
	Buckets        []Bucket        `json:"buckets"         yaml:"buckets"`
	Keys           []StorageKey    `json:"keys"            yaml:"keys"`
	Domains        []StorageDomain `json:"domains"         yaml:"domains"`
}

// Endpoint returns the S3 endpoint URL to configure a client with, or "" when
// the storage has no domain yet. A custom domain wins over the platform one.
func (s Storage) Endpoint() string {
	for _, d := range s.Domains {
		if d.Custom {
			return "https://" + d.URL
		}
	}
	for _, d := range s.Domains {
		return "https://" + d.URL
	}
	return ""
}

// Storages returns the tenant's object storages with their buckets, keys and
// endpoints. Four reads, because each collection is a separate route that
// returns the whole storage list with only its own field populated.
func (c *Client) Storages(ctx context.Context) ([]Storage, error) {
	var base, withBuckets, withKeys, withDomains []Storage
	for _, step := range []struct {
		path string
		out  *[]Storage
	}{
		{storagePathV2, &base},
		{bucketPathV2, &withBuckets},
		{keyPathV2, &withKeys},
		{domainPathV2, &withDomains},
	} {
		if err := c.getJSON(ctx, step.path, nil, step.out); err != nil {
			return nil, err
		}
	}

	buckets := make(map[string][]Bucket, len(withBuckets))
	for _, s := range withBuckets {
		buckets[s.ID] = s.Buckets
	}
	keys := make(map[string][]StorageKey, len(withKeys))
	for _, s := range withKeys {
		keys[s.ID] = s.Keys
	}
	domains := make(map[string][]StorageDomain, len(withDomains))
	for _, s := range withDomains {
		domains[s.ID] = s.Domains
	}
	for i := range base {
		base[i].Buckets = buckets[base[i].ID]
		base[i].Keys = keys[base[i].ID]
		base[i].Domains = domains[base[i].ID]
	}
	return base, nil
}
