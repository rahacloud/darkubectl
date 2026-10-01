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

// Repository names hold slashes, so every registry action names the repository
// in the body, and the digest list answers with a bare array.
func TestRegistryActionsSendRepositoryInBody(t *testing.T) {
	t.Parallel()

	type call struct {
		Method, Path string
		Body         map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		calls = append(calls, call{r.Method, r.URL.Path, body})
		if r.URL.Path == "/api/v1/registry/7/list_repository_digests/" {
			_, _ = w.Write([]byte(`[{"digest_sha":"sha256:ab","tags":["v1"],"size":2048,"last_pushed":"2026-09-01T00:00:00Z"}]`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	ctx := context.Background()
	digests, err := c.ListDigests(ctx, "7", "acme/my-api")
	if err != nil || len(digests) != 1 || digests[0].Tags[0] != "v1" || digests[0].Size != 2048 {
		t.Fatalf("ListDigests = %+v, %v", digests, err)
	}
	if err := c.DeleteTags(ctx, "7", "acme/my-api", "sha256:ab", []string{"v1"}); err != nil {
		t.Fatalf("DeleteTags: %v", err)
	}
	if err := c.DeleteDigests(ctx, "7", "acme/my-api", []string{"sha256:ab"}); err != nil {
		t.Fatalf("DeleteDigests: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	for _, c := range calls {
		if c.Method != http.MethodPost || c.Body["repository_name"] != "acme/my-api" {
			t.Errorf("call %+v: want a POST naming the repository in the body", c)
		}
	}
	if calls[1].Path != "/api/v1/registry/7/delete_tags/" || calls[1].Body["digest_sha"] != "sha256:ab" {
		t.Errorf("delete_tags call = %+v", calls[1])
	}
	if shas, _ := calls[2].Body["digests_sha"].([]any); calls[2].Path != "/api/v1/registry/7/delete_digests/" || len(shas) != 1 {
		t.Errorf("delete_digests call = %+v", calls[2])
	}
}

// A read nests the whole registry in a retention rule; a create sends its bare
// id. Both must decode, or `gc set` cannot find the rule it should update.
func TestGCStrategyDecodesNestedRegistry(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"count":2,"next":null,"results":[` +
			`{"id":"r1","destination_type":"registry","image_repo":null,` +
			`"registry":{"id":"reg-9","name":"hamdocker","organization":{"id":1}},"keep_type":"count","keep_count":50},` +
			`{"id":"r2","destination_type":"image_repo","image_repo":"registry.hamdocker.ir/acme/web","registry":"",` +
			`"keep_type":"duration","keep_duration":"30 00:00:00"}]}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	rules, err := c.ListGCStrategies(context.Background())
	if err != nil {
		t.Fatalf("ListGCStrategies: %v", err)
	}
	if len(rules) != 2 || rules[0].Registry == nil || rules[0].Registry.ID != "reg-9" || rules[0].Registry.Name != "hamdocker" {
		t.Fatalf("rules[0] = %+v", rules[0])
	}
	if rules[1].Registry == nil || rules[1].Registry.ID != "" || rules[1].ImageRepo != "registry.hamdocker.ir/acme/web" {
		t.Errorf("rules[1] = %+v", rules[1])
	}
}
