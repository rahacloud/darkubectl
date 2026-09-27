package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrNoSuchCluster is returned when a cluster reference matches nothing the
// tenant can see.
var ErrNoSuchCluster = errors.New("no such cluster")

// Payload keys shared by the create and update paths.
const (
	fieldName         = "name"
	fieldOrganization = "organization"
)

// CreateNamespace creates a project (namespace) in the current tenant.
//
// This goes to the same v1 surface as ListNamespaces and carries the same
// requirement: it needs the user context a Console JWT provides, and an
// Api-key is rejected. That is why there is no way to do this with an account
// token, and why it belongs in the tool rather than in a shell one-liner —
// the credential stays here.
//
// The organization goes in the body as well as the X-Organization header,
// matching CreateApp: the API takes it from the payload on create.
//
// Note the cluster key is "cluster_id", not "cluster" -- a namespace reads
// back as a nested cluster object, so the obvious guess is wrong and the API
// answers `400 cluster_id: این مقدار لازم است` when it is.
func (c *Client) CreateNamespace(ctx context.Context, name string, clusterID int) (Namespace, error) {
	orgID, err := c.OrganizationID(ctx)
	if err != nil {
		return Namespace{}, err
	}

	payload := map[string]any{
		fieldName:         name,
		"cluster_id":      clusterID,
		fieldOrganization: orgID,
	}

	data, err := c.do(ctx, http.MethodPost, namespacesPathV1, nil, payload)
	if err != nil {
		return Namespace{}, err
	}

	var out Namespace
	if len(data) > 0 {
		_ = decodeInto(data, &out)
	}
	return out, nil
}

// DeleteNamespace removes a namespace (project) by id.
//
// Like create, this needs a Console JWT. The API does not check whether the
// namespace still holds apps, so the caller must -- deleting a populated one
// takes its workloads with it.
func (c *Client) DeleteNamespace(ctx context.Context, id int) error {
	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("%s%d/", namespacesPathV1, id), nil, nil)
	return err
}

// ResolveCluster turns a cluster reference — a numeric id, or a name such as
// "hamravesh-c11" — into a cluster id, using the clusters the tenant's existing
// namespaces already sit on.
//
// A name is worth supporting because the id is not shown anywhere a person
// normally looks: `get namespaces` prints the cluster's name, so asking for the
// id means going and finding it in JSON.
func ResolveCluster(ref string, known []Namespace) (int, error) {
	if ref == "" {
		return 0, fmt.Errorf("%w: no cluster given", ErrNoSuchCluster)
	}
	var id int
	if _, err := fmt.Sscanf(ref, "%d", &id); err == nil && id > 0 {
		return id, nil
	}
	for _, n := range known {
		if n.Cluster.Name == ref && n.Cluster.ID != 0 {
			return n.Cluster.ID, nil
		}
	}
	return 0, fmt.Errorf("%w: %q is neither a cluster id nor a cluster this tenant has a namespace on", ErrNoSuchCluster, ref)
}
