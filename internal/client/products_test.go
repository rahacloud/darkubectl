package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
	"gopkg.in/yaml.v3"
)

func TestDatabaseDetailStructuredOutputOmitsPasswordButDecodesIt(t *testing.T) {
	t.Parallel()

	const vaultReference = "REPLACE_PASSWORD"
	connection := func() *client.DatabaseConnection {
		return &client.DatabaseConnection{Username: "db-user", Password: vaultReference, Database: "app", Host: "db.internal", Port: 5432}
	}
	detail := client.DatabaseDetail{
		Connection: connection(), PrivateConnection: connection(), StandbyConnection: connection(), PrivateStandbyConnection: connection(),
		Connections: client.DatabaseConnections{
			Master:  client.DatabaseConnectionSet{External: connection(), Private: connection()},
			Standby: client.DatabaseConnectionSet{External: connection(), Private: connection()},
		},
	}

	jsonOutput, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonOutput), vaultReference) || strings.Contains(string(jsonOutput), `"password"`) {
		t.Fatalf("JSON output exposed database password: %s", jsonOutput)
	}

	yamlOutput, err := yaml.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(yamlOutput), vaultReference) || strings.Contains(string(yamlOutput), "password:") {
		t.Fatalf("YAML output exposed database password: %s", yamlOutput)
	}

	const apiResponse = `{"connection":{"username":"db-user","password":"vault-ref","database":"app","host":"db.internal","port":5432},` +
		`"connections":{"master":{"external":{"username":"db-user","password":"nested-vault-ref"}}}}`
	var decoded client.DatabaseDetail
	if err := json.Unmarshal([]byte(apiResponse), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Connection == nil || decoded.Connection.Password != "vault-ref" ||
		decoded.Connections.Master.External == nil || decoded.Connections.Master.External.Password != "nested-vault-ref" {
		t.Fatalf("API password references were not decoded: %+v", decoded)
	}
}

// The PVC list wraps its rows in pvc_list; the database and service lists are
// bare arrays under their own service prefixes.
func TestPVCsDatabasesAndServicesDecode(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/darkube/pvcs/":
			if r.URL.Query().Get("namespace_name") != "dev" || r.URL.Query().Get("cluster_id") != "46" {
				t.Errorf("pvcs query = %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"namespace":"dev","pvc_list":[` +
				`{"name":"web-data","size":"4Gi","storage_class":"rawfile-btrfs","status":"Bound"}]}`))
		case "/dbaas/api/v1/app/database/":
			_, _ = w.Write([]byte(`[{"id":"d1","name":"db-0","engine":"PostgreSQL","version":"16.0","namespace_id":7,` +
				`"num_node":2,"status":{"role":"master","status":"running","detail":"ok"},` +
				`"nodes":[{"name":"master","cpu":1000,"ram":2000,"disk":30}]}]`))
		case "/marketplace/api/v1/app/saas/":
			_, _ = w.Write([]byte(`[{"id":"s1","name":"jira-x","label":"Jira","product_type":"jira",` +
				`"cluster":"c11","pod_status":"running","is_enabled":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	ctx := context.Background()
	pvcs, err := c.ListPVCs(ctx, "dev", 46)
	if err != nil || len(pvcs) != 1 || pvcs[0].StorageClass != "rawfile-btrfs" {
		t.Errorf("ListPVCs = %+v, %v", pvcs, err)
	}
	dbs, err := c.ListDatabases(ctx)
	if err != nil || len(dbs) != 1 || dbs[0].NamespaceID != 7 || dbs[0].Status.Status != "running" ||
		dbs[0].Nodes[0].Disk != 30 {
		t.Errorf("ListDatabases = %+v, %v", dbs, err)
	}
	svcs, err := c.ListServices(ctx)
	if err != nil || len(svcs) != 1 || svcs[0].ProductType != "jira" || !svcs[0].IsEnabled {
		t.Errorf("ListServices = %+v, %v", svcs, err)
	}
}
