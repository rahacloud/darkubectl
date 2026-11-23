package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
	ID             string          `json:"id"                        yaml:"id"`
	Name           string          `json:"name"                      yaml:"name"`
	StorageClass   string          `json:"storage_class"             yaml:"storageClass"`
	IsShared       bool            `json:"is_shared"                 yaml:"isShared"`
	TraefikAddress string          `json:"traefik_address"           yaml:"traefikAddress"`
	Buckets        []Bucket        `json:"buckets"                   yaml:"buckets"`
	Keys           []StorageKey    `json:"keys"                      yaml:"keys"`
	Domains        []StorageDomain `json:"domains"                   yaml:"domains"`
	PlanDiskGB     *float64        `json:"plan_disk_gb,omitempty"    yaml:"planDiskGB,omitempty"`
	PlanTrafficGB  *float64        `json:"plan_traffic_gb,omitempty" yaml:"planTrafficGB,omitempty"`
}

// StoragePlanAllowance holds billing-plan allowances in GB, not usage or hard quotas.
type StoragePlanAllowance struct {
	DiskGB    *float64
	TrafficGB *float64
}

// StoragePlanAllowances joins storage assignments to their current billing plans.
// Shared storages may have no assignment.
func (c *Client) StoragePlanAllowances(ctx context.Context) (map[string]StoragePlanAllowance, error) {
	var assignments []struct {
		ID            string `json:"id"`
		CurrentPlanID *int   `json:"current_plan_id"`
	}
	if err := c.getJSON(ctx, "/storage/v2/storage/plans/", nil, &assignments); err != nil {
		return nil, err
	}
	if len(assignments) == 0 {
		return map[string]StoragePlanAllowance{}, nil
	}
	var plans []struct {
		ID              int      `json:"id"`
		DiskCapacity    *float64 `json:"disk_capacity"`
		TrafficCapacity *float64 `json:"traffic_capacity"`
	}
	if err := c.getJSON(ctx, "/storage/v2/billing/plans/", nil, &plans); err != nil {
		return nil, err
	}
	byID := make(map[int]StoragePlanAllowance, len(plans))
	for _, plan := range plans {
		byID[plan.ID] = StoragePlanAllowance{DiskGB: plan.DiskCapacity, TrafficGB: plan.TrafficCapacity}
	}
	capacities := make(map[string]StoragePlanAllowance, len(assignments))
	for _, assignment := range assignments {
		if assignment.CurrentPlanID != nil {
			if capacity, ok := byID[*assignment.CurrentPlanID]; ok {
				capacities[assignment.ID] = capacity
			}
		}
	}
	return capacities, nil
}

// StorageTrafficSample is the latest reported traffic rate for one domain.
type StorageTrafficSample struct {
	Domain         string
	BytesPerSecond float64
	ObservedAt     time.Time
}

// StorageTrafficRates reads the console's traffic chart for the given time window.
// The API reports rates, not the amount consumed against the traffic plan.
func (c *Client) StorageTrafficRates(ctx context.Context, storageID string, start, end time.Time) ([]StorageTrafficSample, error) {
	query := url.Values{"storage_id": {storageID}, "start_time": {start.UTC().Format(time.RFC3339)},
		"end_time": {end.UTC().Format(time.RFC3339)}}
	var rows []struct {
		ID      string `json:"id"`
		Domains []struct {
			URL          string `json:"url"`
			TrafficUsage []struct {
				Values [][]json.RawMessage `json:"values"`
			} `json:"traffic_usage"`
		} `json:"domains"`
	}
	if err := c.getJSON(ctx, "/storage/v2/metrics/traffic", query, &rows); err != nil {
		return nil, err
	}
	var samples []StorageTrafficSample
	for _, row := range rows {
		if row.ID != storageID {
			continue
		}
		for _, domain := range row.Domains {
			if len(domain.TrafficUsage) == 0 {
				continue
			}
			if latest, ok := latestStorageTrafficSample(domain.URL, domain.TrafficUsage[0].Values); ok {
				samples = append(samples, latest)
			}
		}
	}
	return samples, nil
}

func latestStorageTrafficSample(domain string, points [][]json.RawMessage) (StorageTrafficSample, bool) {
	const pointFields = 2
	var latest StorageTrafficSample
	for _, point := range points {
		if len(point) != pointFields {
			continue
		}
		var timestamp float64
		var raw string
		if json.Unmarshal(point[0], &timestamp) != nil || json.Unmarshal(point[1], &raw) != nil {
			continue
		}
		rate, err := strconv.ParseFloat(raw, 64)
		if err != nil || rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) ||
			timestamp <= 0 || math.IsNaN(timestamp) || math.IsInf(timestamp, 0) {
			continue
		}
		observed := time.Unix(int64(timestamp), 0)
		if observed.After(latest.ObservedAt) {
			latest = StorageTrafficSample{Domain: domain, BytesPerSecond: rate, ObservedAt: observed}
		}
	}
	return latest, !latest.ObservedAt.IsZero()
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

// The writes below were confirmed 2026-11-17 on a throwaway bucket and key in
// `parham-alvani`, on the shared "Tehran - Asiatech" storage:
//
//	POST   /storage/v2/bucket/ {name,storage_id,is_public} → the new id, a bare JSON string;
//	                                                409 when the name is taken, by any tenant
//	DELETE /storage/v2/bucket/<id>                          (2FA)
//	POST   /storage/v2/bucket/<id>/visibility {is_public}  (2FA; empty body back)
//	POST   /storage/v2/key/ {name,actions,bucket_ids}      (2FA) → the key with access_key and secret_key
//	PATCH  /storage/v2/key/ {name,actions,bucket_ids}      (2FA; answers null; can only ADD:
//	                                                dropping an action or a bucket is a 400
//	                                                "You cannot remove buckets or actions")
//	POST   /storage/v2/key/<name>/rotate                   (2FA) → the key with a new pair
//	DELETE /storage/v2/key/<name>                          (2FA)
//
// A route marked 2FA wants the account's TOTP code in an `x-otp` header and
// answers 403 "2fa code is not correct." without it (a 500 "2-fa verification
// error" until some time before November 2026). The same code is accepted
// again within its 30-second window. The bucket and key lists name only the
// storages that hold one, so an empty tenant reads back as [].
//
// GET /storage/v2/bucket/check-name?name= is what the console's form calls, and
// it is not to be trusted: it answered true for a name this very tenant already
// held, and the create then failed with the 409. So nothing here asks it.

// otpHeader is spelled in lower case on purpose: the storage service compares
// the name case-sensitively, and doWithHeaders sends it as written.
const otpHeader = "x-otp"

// Storage key actions, as the API spells them.
const (
	StorageRead  = "Read"
	StorageWrite = "Write"
	StorageList  = "List"
)

var (
	// ErrOTPRequired means the platform refused a write for want of a valid
	// two-factor code.
	ErrOTPRequired = errors.New("this change needs a valid two-factor (TOTP) code")
	// ErrBucketNameTaken means another bucket, in any tenant, has the name.
	ErrBucketNameTaken = errors.New("bucket name is already taken")
	// ErrNoStorageKeySecret means a key was made but its secret did not come back.
	ErrNoStorageKeySecret = fmt.Errorf("%w: secret_key", ErrMissingField)
)

// StorageKeySecret is a key as returned by the two calls that mint a secret:
// create and rotate. Neither value can be read again afterwards.
type StorageKeySecret struct {
	StorageKey `yaml:",inline"`

	AccessKey string `json:"access_key" yaml:"accessKey"`
	SecretKey string `json:"secret_key" yaml:"secretKey"`
}

func otpHeaders(otp string) http.Header {
	if otp == "" {
		return nil
	}
	return http.Header{otpHeader: []string{otp}}
}

// asOTPError turns the platform's refusal of a missing or wrong code into
// ErrOTPRequired, so a caller can ask for one instead of printing a bare 403.
func asOTPError(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	if detail := strings.ToLower(apiErr.Detail); strings.Contains(detail, "2fa") || strings.Contains(detail, "2-fa") {
		return fmt.Errorf("%w: %s", ErrOTPRequired, apiErr.Detail)
	}
	return err
}

// CreateBucket creates a private bucket on a storage and returns its id. The
// API takes an is_public field and ignores it, so a bucket is made public
// afterwards, with SetBucketPublic. Names are global to the storage service,
// not to the tenant; a taken one is ErrBucketNameTaken.
func (c *Client) CreateBucket(ctx context.Context, storageID, name string) (string, error) {
	var id string
	body := map[string]any{fieldName: name, "storage_id": storageID, "is_public": false}
	if err := c.postJSON(ctx, bucketPathV2, body, &id); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
			return "", fmt.Errorf("%w: %q", ErrBucketNameTaken, name)
		}
		return "", err
	}
	return id, nil
}

// DeleteBucket deletes a bucket and everything in it.
func (c *Client) DeleteBucket(ctx context.Context, id, otp string) error {
	_, err := c.doWithHeaders(ctx, http.MethodDelete, bucketPathV2+url.PathEscape(id), nil, nil, otpHeaders(otp))
	return asOTPError(err)
}

// SetBucketPublic makes a bucket readable by anyone, or private again.
func (c *Client) SetBucketPublic(ctx context.Context, id string, public bool, otp string) error {
	path := bucketPathV2 + url.PathEscape(id) + "/visibility"
	_, err := c.doWithHeaders(ctx, http.MethodPost, path, nil, map[string]any{"is_public": public}, otpHeaders(otp))
	return asOTPError(err)
}

func storageKeyBody(name string, actions, bucketIDs []string) map[string]any {
	return map[string]any{fieldName: name, "actions": actions, "bucket_ids": bucketIDs}
}

// CreateStorageKey creates an access key limited to actions on the given
// buckets, and returns it with its secret.
func (c *Client) CreateStorageKey(ctx context.Context, name string, actions, bucketIDs []string, otp string) (*StorageKeySecret, error) {
	data, err := c.doWithHeaders(ctx, http.MethodPost, keyPathV2, nil, storageKeyBody(name, actions, bucketIDs), otpHeaders(otp))
	if err != nil {
		return nil, asOTPError(err)
	}
	return decodeStorageKeySecret(data)
}

// UpdateStorageKey sets a key's actions and buckets, which may only grow: the
// platform refuses a list that drops something the key already has. The pair
// itself does not change.
func (c *Client) UpdateStorageKey(ctx context.Context, name string, actions, bucketIDs []string, otp string) error {
	_, err := c.doWithHeaders(ctx, http.MethodPatch, keyPathV2, nil, storageKeyBody(name, actions, bucketIDs), otpHeaders(otp))
	return asOTPError(err)
}

// RotateStorageKey issues a new access and secret key under the same name and
// grants; the old pair stops working.
func (c *Client) RotateStorageKey(ctx context.Context, name, otp string) (*StorageKeySecret, error) {
	data, err := c.doWithHeaders(ctx, http.MethodPost, keyPathV2+url.PathEscape(name)+"/rotate", nil, nil, otpHeaders(otp))
	if err != nil {
		return nil, asOTPError(err)
	}
	return decodeStorageKeySecret(data)
}

// DeleteStorageKey revokes a key.
func (c *Client) DeleteStorageKey(ctx context.Context, name, otp string) error {
	_, err := c.doWithHeaders(ctx, http.MethodDelete, keyPathV2+url.PathEscape(name), nil, nil, otpHeaders(otp))
	return asOTPError(err)
}

func decodeStorageKeySecret(data []byte) (*StorageKeySecret, error) {
	var key StorageKeySecret
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, fmt.Errorf("decode storage key: %w", err)
	}
	if key.SecretKey == "" {
		return nil, ErrNoStorageKeySecret
	}
	return &key, nil
}
