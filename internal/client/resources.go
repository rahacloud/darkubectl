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

// Namespace listing lives on the v1 surface. It needs the user context a
// Console JWT carries; an Api-key is rejected, so callers fall back to
// NamespacesFromApps.
const namespacesPathV1 = "/api/v1/darkube/namespaces/"

// ListNamespaces returns every namespace (project) in the current tenant,
// including ones that hold no apps yet. Requires a JWT.
func (c *Client) ListNamespaces(ctx context.Context) ([]Namespace, error) {
	var all []Namespace
	const limit = 200
	offset := 0
	for {
		q := url.Values{}
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))
		var p page[Namespace]
		if err := c.getJSON(ctx, namespacesPathV1, q, &p); err != nil {
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

// Namespaces returns the tenant's namespaces, preferring the dedicated endpoint
// and falling back to deriving them from apps.
//
// The distinction matters: the derived list can only contain namespaces that
// already hold at least one app, so a freshly created, still-empty project is
// invisible to it — which is exactly when you need to look one up in order to
// create the first app in it.
func (c *Client) Namespaces(ctx context.Context) ([]Namespace, error) {
	if ns, err := c.ListNamespaces(ctx); err == nil && len(ns) > 0 {
		return ns, nil
	}
	return c.NamespacesFromApps(ctx)
}

// NamespacesFromApps derives the set of namespaces (projects) visible in the
// current tenant from the app list. This is the fallback for credentials that
// cannot reach the dedicated endpoint; it omits namespaces with no apps.
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
