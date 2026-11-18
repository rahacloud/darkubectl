package client

import (
	"context"
	"net/http"
	"net/url"
)

// Hamravesh KaaS, the managed Kubernetes product, is served by its own service
// under /podolica (the platform's internal name for it), on the main API host
// with the same credential and X-Organization. Mapped 2026-11-18 from the
// console's console-podolica micro-frontend. Confirmed live against `talaland`:
//
//	GET  /podolica/v1/clusters/                              bare list of clusters
//	GET  /podolica/v1/clusters/<name>/                       one cluster (info/ answers the same)
//	GET  /podolica/v1/clusters/<name>/api-server-whitelist/  {source_ranges,synced_at}
//	POST /podolica/v1/clusters/<name>/api-server-whitelist/  {source_ranges} REPLACES the list
//	GET  /podolica/v1/kubeconfig/?clusters=<name>&access_type=token   kubeconfig YAML, not JSON
//	GET  /podolica/v1/nodepools/?cluster=<name>              bare list of pools
//	GET  /podolica/v1/nodes/?cluster=<name>                  {nodepool_list:[{name,nodes}]}
//	GET  /podolica/v1/clusters/<name>/etcd_backup/           bare list, newest first
//	GET  /podolica/v1/cluster/<name>/update/status/          {cluster,current_version,is_latest}
//
// Note the singular `cluster/` in the last one. Also in the bundle, and not
// wired: clusters/<name>/components/ (500 on c216), is_ready/ (answers about
// the creation workflow, "No workflows found" on a cluster made long ago) and
// services/ ({"services":[]}). POST /podolica/v1/kubeconfig/?clusters=<name>
// rotates the kubeconfig; it is wired from the console source and has never
// been sent.
const (
	kaasClustersPath   = "/podolica/v1/clusters/"
	kaasClusterPath    = "/podolica/v1/cluster/"
	kaasKubeconfigPath = "/podolica/v1/kubeconfig/"
	kaasNodePoolsPath  = "/podolica/v1/nodepools/"
	kaasNodesPath      = "/podolica/v1/nodes/"
)

// kaasClusterParam is the query parameter the node routes filter on.
const kaasClusterParam = "cluster"

// KubeconfigAccessToken is the access_type the console's "Download KubeConfig"
// asks for: a kubeconfig carrying a bearer token for the cluster's admin.
const KubeconfigAccessToken = "token"

// KaaSCluster is one KaaS cluster. APIServerURL is a bare hostname, not a URL.
type KaaSCluster struct {
	ID               int               `json:"id"                 yaml:"id"`
	Name             string            `json:"name"               yaml:"name"`
	UniqueID         int               `json:"unique_id"          yaml:"uniqueId"`
	Status           string            `json:"status"             yaml:"status"`
	Version          string            `json:"version"            yaml:"version"`
	Size             string            `json:"size"               yaml:"size"`
	Provider         string            `json:"provider"           yaml:"provider"`
	APIServerURL     string            `json:"api_server_url"     yaml:"apiServerUrl"`
	APIServerIP      string            `json:"api_server_ip"      yaml:"apiServerIp"`
	TraefikServiceIP string            `json:"traefik_service_ip" yaml:"traefikServiceIp"`
	FirewallPublicIP string            `json:"firewall_public_ip" yaml:"firewallPublicIp"`
	Datacenter       ClusterDatacenter `json:"datacenter"         yaml:"datacenter"`
	Organization     ClusterOrg        `json:"organization"       yaml:"organization"`
}

// ClusterDatacenter is where a cluster runs.
type ClusterDatacenter struct {
	Name     string `json:"name"     yaml:"name"`
	Zone     string `json:"zone"     yaml:"zone"`
	Location string `json:"location" yaml:"location"`
}

// ClusterOrg is the tenant that owns a cluster.
type ClusterOrg struct {
	ID   int    `json:"id"   yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

// APIServerWhitelist is the set of source ranges allowed to reach a cluster's
// API server. SyncedAt is when the platform last pushed it to the cluster.
type APIServerWhitelist struct {
	SourceRanges []string `json:"source_ranges" yaml:"sourceRanges"`
	SyncedAt     string   `json:"synced_at"     yaml:"syncedAt"`
}

// NodePool is one pool of a cluster. CPU is in cores, RAM and disks in GB.
type NodePool struct {
	ID       int     `json:"id"        yaml:"id"`
	Name     string  `json:"name"      yaml:"name"`
	CPU      float64 `json:"cpu"       yaml:"cpu"`
	RAM      float64 `json:"ram"       yaml:"ram"`
	OSDisk   float64 `json:"os_disk"   yaml:"osDisk"`
	DataDisk float64 `json:"data_disk" yaml:"dataDisk"`
	DiskType string  `json:"disk_type" yaml:"diskType"`
	TypeName string  `json:"type_name" yaml:"typeName"`
	TypeSize string  `json:"type_size" yaml:"typeSize"`
	HasTaint bool    `json:"has_taint" yaml:"hasTaint"`
}

// ClusterNode is one node, as the nodes route lists it under its pool.
type ClusterNode struct {
	Pool   string `json:"pool"   yaml:"pool"`
	Name   string `json:"name"   yaml:"name"`
	Status string `json:"status" yaml:"status"`
	IP     string `json:"ip"     yaml:"ip"`
	CPU    int    `json:"cpu"    yaml:"cpu"`
}

// EtcdBackup is one scheduled etcd snapshot of a cluster.
type EtcdBackup struct {
	ID        int    `json:"id"         yaml:"id"`
	Date      string `json:"date"       yaml:"date"`
	Status    string `json:"status"     yaml:"status"`
	SizeBytes int64  `json:"size_bytes" yaml:"sizeBytes"`
	Size      string `json:"size"       yaml:"size"`
}

// ClusterUpdateStatus says whether a newer Kubernetes version is offered.
type ClusterUpdateStatus struct {
	Cluster        string `json:"cluster"         yaml:"cluster"`
	CurrentVersion string `json:"current_version" yaml:"currentVersion"`
	IsLatest       bool   `json:"is_latest"       yaml:"isLatest"`
}

func clusterPath(name, suffix string) string {
	return kaasClustersPath + url.PathEscape(name) + "/" + suffix
}

// ListClusters returns the tenant's KaaS clusters.
func (c *Client) ListClusters(ctx context.Context) ([]KaaSCluster, error) {
	var out []KaaSCluster
	if err := c.getJSON(ctx, kaasClustersPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetClusterRaw returns one cluster as the API sends it, every field included.
func (c *Client) GetClusterRaw(ctx context.Context, name string) (map[string]any, error) {
	var out map[string]any
	if err := c.getJSON(ctx, clusterPath(name, ""), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// APIServerWhitelist reads the source ranges allowed to reach a cluster's API.
func (c *Client) APIServerWhitelist(ctx context.Context, cluster string) (*APIServerWhitelist, error) {
	var out APIServerWhitelist
	if err := c.getJSON(ctx, clusterPath(cluster, "api-server-whitelist/"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetAPIServerWhitelist replaces the whole list. There is no add or remove
// route: whatever is not in ranges is dropped, so a caller changing one entry
// must send every other one back.
func (c *Client) SetAPIServerWhitelist(ctx context.Context, cluster string, ranges []string) (*APIServerWhitelist, error) {
	if ranges == nil {
		ranges = []string{}
	}
	var out APIServerWhitelist
	body := map[string]any{"source_ranges": ranges}
	if err := c.postJSON(ctx, clusterPath(cluster, "api-server-whitelist/"), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func kubeconfigQuery(cluster string) url.Values {
	return url.Values{"clusters": {cluster}}
}

// Kubeconfig downloads a cluster's admin kubeconfig. The body is YAML, and it
// is a credential: it carries a bearer token with cluster-admin rights.
func (c *Client) Kubeconfig(ctx context.Context, cluster string) ([]byte, error) {
	q := kubeconfigQuery(cluster)
	q.Set("access_type", KubeconfigAccessToken)
	return c.do(ctx, http.MethodGet, kaasKubeconfigPath, q, nil)
}

// RotateKubeconfig issues a new kubeconfig for a cluster and invalidates the
// one every existing copy carries. Taken from the console source; it has not
// been called against the live API, so what it answers is not known.
func (c *Client) RotateKubeconfig(ctx context.Context, cluster string) error {
	_, err := c.do(ctx, http.MethodPost, kaasKubeconfigPath, kubeconfigQuery(cluster), map[string]any{})
	return err
}

// NodePools lists a cluster's node pools.
func (c *Client) NodePools(ctx context.Context, cluster string) ([]NodePool, error) {
	var out []NodePool
	if err := c.getJSON(ctx, kaasNodePoolsPath, url.Values{kaasClusterParam: {cluster}}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ClusterNodes lists a cluster's nodes, flattened out of their pools.
func (c *Client) ClusterNodes(ctx context.Context, cluster string) ([]ClusterNode, error) {
	var resp struct {
		NodepoolList []struct {
			Name  string        `json:"name"`
			Nodes []ClusterNode `json:"nodes"`
		} `json:"nodepool_list"`
	}
	if err := c.getJSON(ctx, kaasNodesPath, url.Values{kaasClusterParam: {cluster}}, &resp); err != nil {
		return nil, err
	}
	var out []ClusterNode
	for _, p := range resp.NodepoolList {
		for _, n := range p.Nodes {
			n.Pool = p.Name
			out = append(out, n)
		}
	}
	return out, nil
}

// EtcdBackups lists a cluster's etcd snapshots, newest first.
func (c *Client) EtcdBackups(ctx context.Context, cluster string) ([]EtcdBackup, error) {
	var out []EtcdBackup
	if err := c.getJSON(ctx, clusterPath(cluster, "etcd_backup/"), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ClusterUpdateStatus reports the cluster's version and whether it is the latest.
func (c *Client) ClusterUpdateStatus(ctx context.Context, cluster string) (*ClusterUpdateStatus, error) {
	var out ClusterUpdateStatus
	path := kaasClusterPath + url.PathEscape(cluster) + "/update/status/"
	if err := c.getJSON(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
