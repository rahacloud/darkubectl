package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

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
