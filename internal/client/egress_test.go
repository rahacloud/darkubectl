package client_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestGetEgressRoutesHTTPContract(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/ip-management/api/egress-route/" {
			t.Errorf("request = %s %s", r.Method, r.URL.RequestURI())
		}
		if got := r.Header.Get("Authorization"); got != "Api-key test-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("X-Organization"); got != "test-org" {
			t.Errorf("organization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"65":{"cluster":{"name":"test-cluster",
"default_outgoing_ip":"192.0.2.1",
"id":65},
"routes":[{"id":"route-id",
"selected_namespaces":[123,
"456"],
"apply_to_all_namespaces":true,
"egress_ip":"192.0.2.2",
"egress_postfix_name":"default"}]},
"46":{"cluster":{"id":46},
"routes":[]}}`)
	}))
	defer server.Close()
	c := client.New(server.URL, client.APIKey("test-key"), "test-org")
	got, err := c.GetEgressRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cluster := got["65"]
	if cluster.Cluster.ID != 65 ||
		cluster.Cluster.Name != "test-cluster" ||
		cluster.Cluster.DefaultOutgoingIP != "192.0.2.1" ||
		len(cluster.Routes) != 1 {
		t.Fatalf("cluster = %+v", cluster)
	}
	route := cluster.Routes[0]
	if route.ID != "route-id" ||
		!route.ApplyToAllNamespaces ||
		route.EgressIP != "192.0.2.2" ||
		route.EgressPostfixName != "default" ||
		len(route.SelectedNamespaces) != 2 ||
		route.SelectedNamespaces[0] != "123" ||
		route.SelectedNamespaces[1] != "456" {
		t.Fatalf("route = %+v", route)
	}
	if len(got["46"].Routes) != 0 {
		t.Fatalf("empty routes = %+v", got["46"])
	}
}

func TestGetEgressRoutesReturnsErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"http error", http.StatusForbidden, `{}`},
		{"invalid JSON", http.StatusOK, `{invalid`},
		{"invalid response type", http.StatusOK, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := client.New(server.URL, client.APIKey("test"), "org").GetEgressRoutes(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
