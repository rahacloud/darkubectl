package client

import (
	"context"
)

const egressRoutesPath = "/ip-management/api/egress-route/"

// EgressRoute describes an outbound IP override for a cluster's namespaces.
type EgressRoute struct {
	ID                   string   `json:"id"`
	SelectedNamespaces   []FlexID `json:"selected_namespaces"`
	ApplyToAllNamespaces bool     `json:"apply_to_all_namespaces"`
	EgressIP             string   `json:"egress_ip"`
	EgressPostfixName    string   `json:"egress_postfix_name"`
}

// EgressCluster is the cluster information returned by the egress API.
type EgressCluster struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	DefaultOutgoingIP string `json:"default_outgoing_ip"`
}

// ClusterEgress contains the default outbound IP and namespace overrides.
type ClusterEgress struct {
	Cluster EgressCluster `json:"cluster"`
	Routes  []EgressRoute `json:"routes"`
}

// GetEgressRoutes returns outbound IP routing configuration keyed by cluster ID.
func (c *Client) GetEgressRoutes(ctx context.Context) (map[string]ClusterEgress, error) {
	var routes map[string]ClusterEgress
	if err := c.getJSON(ctx, egressRoutesPath, nil, &routes); err != nil {
		return nil, err
	}

	return routes, nil
}
