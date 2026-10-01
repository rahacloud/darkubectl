package client

import (
	"context"
	"net/url"
	"strconv"
)

// pvcsPathV1 lists a namespace's persistent volume claims, as the console's
// backup pages read them: GET pvcs/?namespace_name=&cluster_id= →
// {"namespace", "pvc_list":[{name,size,storage_class,status}]}. It lists
// every claim in the namespace, including those whose app is gone, which is
// what makes it worth having: a deleted app's disk stays, bound and billed.
// Confirmed 2026-10-01.
const pvcsPathV1 = "/api/v1/darkube/pvcs/"

// PVC is one persistent volume claim.
type PVC struct {
	Name         string `json:"name"          yaml:"name"`
	Size         string `json:"size"          yaml:"size"`
	StorageClass string `json:"storage_class" yaml:"storage_class"`
	Status       string `json:"status"        yaml:"status"`
}

// ListPVCs returns the claims in one namespace.
func (c *Client) ListPVCs(ctx context.Context, namespace string, clusterID int) ([]PVC, error) {
	q := url.Values{}
	q.Set("namespace_name", namespace)
	q.Set("cluster_id", strconv.Itoa(clusterID))
	var out struct {
		PVCs []PVC `json:"pvc_list"`
	}
	if err := c.getJSON(ctx, pvcsPathV1, q, &out); err != nil {
		return nil, err
	}
	return out.PVCs, nil
}
