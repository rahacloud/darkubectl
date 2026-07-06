package client

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// decodeInto is a small helper so callers avoid importing encoding/json.
func decodeInto(data []byte, out any) error { return json.Unmarshal(data, out) }

// Plan listing lives on the v1 surface and is a global (non-tenant) resource.
const plansPathV1 = "/api/v1/darkube/plans/"

// ListPlans returns all resource/pricing plans (paginated DRF envelope).
func (c *Client) ListPlans(ctx context.Context) ([]Plan, error) {
	var all []Plan
	const limit = 200
	offset := 0
	for {
		q := url.Values{}
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))
		var p page[Plan]
		if err := c.getJSON(ctx, plansPathV1, q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		offset += limit
		if p.Next == "" || len(p.Results) == 0 || len(all) >= p.Count {
			break
		}
	}
	return all, nil
}

// Certificate is a TLS certificate entry. The certificates endpoint uses a
// different envelope than the paginated ones: {"data":{"items":[...]}}.
type Certificate struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CommonName string `json:"common_name"`
	State      string `json:"state"`
	Domain     string `json:"domain"`
}

type certsEnvelope struct {
	Data struct {
		Items []Certificate `json:"items"`
	} `json:"data"`
}

const certsPathV1 = "/api/v1/darkube/certificates/"

// ListCertificates returns TLS certificates for the current tenant.
func (c *Client) ListCertificates(ctx context.Context) ([]Certificate, error) {
	var env certsEnvelope
	if err := c.getJSON(ctx, certsPathV1, nil, &env); err != nil {
		return nil, err
	}
	return env.Data.Items, nil
}

// NamespacesFromApps derives the set of namespaces (projects) visible in the
// current tenant from the app list. The dedicated /namespaces/ endpoint requires
// a scope this account key lacks, so we reconstruct it from apps instead.
func (c *Client) NamespacesFromApps(ctx context.Context) ([]Namespace, error) {
	apps, err := c.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var out []Namespace
	for _, a := range apps {
		ns := a.Namespace
		if ns.ID == 0 || seen[ns.ID] {
			continue
		}
		seen[ns.ID] = true
		out = append(out, ns)
	}
	return out, nil
}
