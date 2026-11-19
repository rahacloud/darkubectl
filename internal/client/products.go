package client

import (
	"context"
	"net/url"
)

// Two more products the account reaches with the same credentials and
// X-Organization, each its own service with its own prefix (confirmed
// 2026-10-01, reads only):
//
//	GET /dbaas/api/v1/app/database/        managed databases (a bare list)
//	GET /marketplace/api/v1/app/saas/      marketplace services (a bare list)
//
// Neither is in the app list. A database's disks live in a tenant namespace,
// named <database>-data-stolon-keeper-<n> for PostgreSQL, which is how
// `get disks` attributes them.
const (
	databasesPath = "/dbaas/api/v1/app/database/"
	servicesPath  = "/marketplace/api/v1/app/saas/"
)

// Database is a managed database instance.
type Database struct {
	ID          string         `json:"id"           yaml:"id"`
	Name        string         `json:"name"         yaml:"name"`
	Engine      string         `json:"engine"       yaml:"engine"`
	Version     string         `json:"version"      yaml:"version"`
	Plan        string         `json:"plan"         yaml:"plan"`
	IsManaged   bool           `json:"is_managed"   yaml:"is_managed"`
	NamespaceID int            `json:"namespace_id" yaml:"namespace_id"`
	ClusterID   int            `json:"cluster_id"   yaml:"cluster_id"`
	NumNodes    int            `json:"num_node"     yaml:"num_node"`
	Status      DatabaseStatus `json:"status"       yaml:"status"`
	Nodes       []DatabaseNode `json:"nodes"        yaml:"nodes"`
}

// DatabaseStatus is a database's health as the platform summarizes it.
type DatabaseStatus struct {
	Role   string `json:"role"   yaml:"role"`
	Status string `json:"status" yaml:"status"`
	Detail string `json:"detail" yaml:"detail"`
}

// DatabaseNode is one member of a database cluster. CPU is in millicores, RAM
// in megabytes and Disk in gigabytes.
type DatabaseNode struct {
	Name   string `json:"name"   yaml:"name"`
	Status string `json:"status" yaml:"status"`
	CPU    int    `json:"cpu"    yaml:"cpu"`
	RAM    int    `json:"ram"    yaml:"ram"`
	Disk   int    `json:"disk"   yaml:"disk"`
}

// DatabaseConnection contains connection data returned by the DBaaS detail route.
// Password currently contains a Vault reference, not the usable database
// password; retrieving the password requires TOTP in the Console. Never expose
// the reference as a credential or include it in errors.
type DatabaseConnection struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	Database string `json:"database" yaml:"database"`
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`
}

type DatabaseConnectionSet struct {
	External *DatabaseConnection `json:"external" yaml:"external"`
	Private  *DatabaseConnection `json:"private" yaml:"private"`
}

type DatabaseConnections struct {
	Master  DatabaseConnectionSet `json:"master" yaml:"master"`
	Standby DatabaseConnectionSet `json:"standby" yaml:"standby"`
}

// DatabaseDetail is returned by GET /dbaas/api/v1/app/database/{id}/.
// It includes a Vault reference in place of the usable database password.
type DatabaseDetail struct {
	Connection               *DatabaseConnection `json:"connection" yaml:"connection"`
	PrivateConnection        *DatabaseConnection `json:"private_connection" yaml:"private_connection"`
	StandbyConnection        *DatabaseConnection `json:"standby_connection" yaml:"standby_connection"`
	PrivateStandbyConnection *DatabaseConnection `json:"private_standby_connection" yaml:"private_standby_connection"`
	Connections              DatabaseConnections `json:"connections" yaml:"connections"`
}

// Service is a marketplace service instance (Jira, Rocket.Chat, n8n, …).
type Service struct {
	ID          string `json:"id"           yaml:"id"`
	Name        string `json:"name"         yaml:"name"`
	Label       string `json:"label"        yaml:"label"`
	ProductType string `json:"product_type" yaml:"product_type"`
	Cluster     string `json:"cluster"      yaml:"cluster"`
	PodStatus   string `json:"pod_status"   yaml:"pod_status"`
	IsEnabled   bool   `json:"is_enabled"   yaml:"is_enabled"`
	Deleted     bool   `json:"deleted"      yaml:"deleted"`
}

// ListDatabases returns the tenant's managed databases.
func (c *Client) ListDatabases(ctx context.Context) ([]Database, error) {
	var out []Database
	if err := c.getJSON(ctx, databasesPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDatabase returns detail and connection credentials for one managed database.
func (c *Client) GetDatabase(ctx context.Context, id string) (*DatabaseDetail, error) {
	var out DatabaseDetail
	if err := c.getJSON(ctx, databasesPath+url.PathEscape(id)+"/", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListServices returns the tenant's marketplace services.
func (c *Client) ListServices(ctx context.Context) ([]Service, error) {
	var out []Service
	if err := c.getJSON(ctx, servicesPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
