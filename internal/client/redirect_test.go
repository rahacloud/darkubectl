package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// Django redirects a route missing its trailing slash, and Go replays a
// redirected PUT as a GET: the write "succeeds" and nothing is stored. A write
// must fail instead, while a read may still follow the redirect.
func TestWriteRedirectIsRefused(t *testing.T) {
	t.Parallel()

	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/v1/registry-gc-strategies/r1" {
			http.Redirect(w, r, "/api/v1/registry-gc-strategies/r1/", http.StatusMovedPermanently)
			return
		}
		_, _ = w.Write([]byte(`{"count":0,"results":[]}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	_, err := c.Raw(context.Background(), http.MethodPut, "/api/v1/registry-gc-strategies/r1", nil, []byte(`{}`))
	if !errors.Is(err, client.ErrWriteRedirected) {
		t.Fatalf("PUT err = %v, want ErrWriteRedirected", err)
	}
	for _, m := range methods {
		if m == "GET /api/v1/registry-gc-strategies/r1/" {
			t.Errorf("the redirected PUT was replayed: %v", methods)
		}
	}

	if _, err := c.Raw(context.Background(), http.MethodGet, "/api/v1/registry-gc-strategies/r1", nil, nil); err != nil {
		t.Errorf("GET through a redirect: %v", err)
	}
}
