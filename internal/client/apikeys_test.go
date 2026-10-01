package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// A create sends only the name, since the server ignores any expiry, and the
// key comes back once under "value". The routes need no X-Organization.
func TestAPIKeyLifecycle(t *testing.T) {
	t.Parallel()

	var created map[string]any
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Organization") != "" {
			t.Errorf("X-Organization sent to %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPost:
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &created)
			_, _ = w.Write([]byte(`{"value":"k-123"}`))
		case http.MethodDelete:
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{"count":1,"next":null,"results":[{"id":"a1","name":"ci","expiration_time":"2028-09-30T00:00:00Z"}]}`))
		}
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "")
	ctx := context.Background()
	v, err := c.CreateAPIKey(ctx, "ci")
	if err != nil || v != "k-123" {
		t.Fatalf("CreateAPIKey = %q, %v", v, err)
	}
	if len(created) != 1 || created["name"] != "ci" {
		t.Errorf("create body = %v, want only the name", created)
	}
	keys, err := c.ListAPIKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0].ExpirationTime == "" {
		t.Errorf("ListAPIKeys = %+v, %v", keys, err)
	}
	if err := c.DeleteAPIKey(ctx, "a1"); err != nil || deleted != "/api/v1/apikeys/a1/" {
		t.Errorf("DeleteAPIKey: %v, path %q", err, deleted)
	}
}
