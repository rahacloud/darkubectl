package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The KaaS routes live under /podolica on the main host, carry the usual
// X-Organization, and each has its own shape: a bare list of clusters, the
// whitelist as {source_ranges}, a kubeconfig as YAML, and nodes nested under
// their pool.
func TestKaaSRoutes(t *testing.T) {
	t.Parallel()

	var posted map[string]any
	srv := httptest.NewServer(kaasHandler(t, &posted))

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	ctx := context.Background()

	clusters, err := c.ListClusters(ctx)
	if err != nil || len(clusters) != 1 || clusters[0].UniqueID != 216 || clusters[0].Datacenter.Zone != "Tehran" {
		t.Fatalf("ListClusters = %+v, %v", clusters, err)
	}
	wl, err := c.APIServerWhitelist(ctx, "acme-c216")
	if err != nil || !slices.Equal(wl.SourceRanges, []string{"1.2.3.4/32"}) || wl.SyncedAt == "" {
		t.Errorf("APIServerWhitelist = %+v, %v", wl, err)
	}
	if _, err := c.SetAPIServerWhitelist(ctx, "acme-c216", []string{"1.2.3.4/32", "5.6.7.8/32"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := posted["source_ranges"].([]any); !ok || len(got) != 2 || len(posted) != 1 {
		t.Errorf("whitelist body = %v, want only source_ranges with both entries", posted)
	}
	kc, err := c.Kubeconfig(ctx, "acme-c216")
	if err != nil || string(kc) != "kind: Config\n" {
		t.Errorf("Kubeconfig = %q, %v", kc, err)
	}
}

// Nodes arrive nested under their pool and are flattened with the pool named.
func TestKaaSNodesAndVersion(t *testing.T) {
	t.Parallel()

	var posted map[string]any
	srv := httptest.NewServer(kaasHandler(t, &posted))
	defer srv.Close()
	c := client.New(srv.URL, client.APIKey("k"), "acme")
	ctx := context.Background()

	nodes, err := c.ClusterNodes(ctx, "acme-c216")
	if err != nil || len(nodes) != 2 || nodes[1].Pool != "gw" || nodes[1].Status != "NotReady" {
		t.Errorf("ClusterNodes = %+v, %v", nodes, err)
	}
	st, err := c.ClusterUpdateStatus(ctx, "acme-c216")
	if err != nil || !st.IsLatest || st.CurrentVersion != "1.36" {
		t.Errorf("ClusterUpdateStatus = %+v, %v", st, err)
	}
}

// kaasHandler fakes the podolica routes TestKaaSRoutes calls, recording the
// whitelist body it is sent in posted.
func kaasHandler(t *testing.T, posted *map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Organization") != "acme" {
			t.Errorf("%s: X-Organization = %q", r.URL.Path, r.Header.Get("X-Organization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /podolica/v1/clusters/":
			_, _ = w.Write([]byte(`[{"id":873,"name":"acme-c216","unique_id":216,"status":"ready",
				"api_server_url":"x.k8s.onhamravesh.ir","datacenter":{"name":"Afranet","zone":"Tehran"},
				"organization":{"id":1,"name":"acme"}}]`))
		case "GET /podolica/v1/clusters/acme-c216/api-server-whitelist/":
			_, _ = w.Write([]byte(`{"source_ranges":["1.2.3.4/32"],"synced_at":"2026-11-18T23:15:58.266341Z"}`))
		case "POST /podolica/v1/clusters/acme-c216/api-server-whitelist/":
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, posted)
			_, _ = w.Write(data)
		case "GET /podolica/v1/kubeconfig/":
			if q := r.URL.Query(); q.Get("clusters") != "acme-c216" || q.Get("access_type") != "token" {
				t.Errorf("kubeconfig query = %v", q)
			}
			_, _ = w.Write([]byte("kind: Config\n"))
		case "GET /podolica/v1/nodes/":
			if r.URL.Query().Get("cluster") != "acme-c216" {
				t.Errorf("nodes query = %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"nodepool_list":[{"name":"apps","nodes":[{"name":"n1","status":"Ready"}]},
				{"name":"gw","nodes":[{"name":"n2","status":"NotReady"}]}]}`))
		case "GET /podolica/v1/cluster/acme-c216/update/status/":
			_, _ = w.Write([]byte(`{"cluster":"acme-c216","current_version":"1.36","is_latest":true}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// An empty list must go out as [], not null: the route replaces the whole
// list, and what it makes of a null is not known.
func TestSetAPIServerWhitelistSendsAnArray(t *testing.T) {
	t.Parallel()

	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		_, _ = w.Write([]byte(`{"source_ranges":[]}`))
	}))
	defer srv.Close()

	if _, err := client.New(srv.URL, client.APIKey("k"), "acme").SetAPIServerWhitelist(context.Background(), "c", nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != `{"source_ranges":[]}` {
		t.Errorf("body = %s", body)
	}
}
