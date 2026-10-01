package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The list pages like any DRF list, and a build id may arrive as a number.
func TestListBuildsPagesAndDecodesNumericIDs(t *testing.T) {
	t.Parallel()

	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"count":2,"next":null,"results":[` +
			`{"id":42,"status":"running","git_branch":"main","short_commit_hash":"abc1234"},` +
			`{"id":"41","status":"succeeded"}]}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	builds, err := c.ListBuilds(context.Background(), "app-1", 10)
	if err != nil {
		t.Fatalf("ListBuilds: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/api/v1/darkube/build/app/app-1/?limit=10&offset=0" {
		t.Errorf("requests = %v", paths)
	}
	if len(builds) != 2 || builds[0].ID != "42" || builds[1].ID != "41" {
		t.Fatalf("builds = %+v", builds)
	}
	if !builds[0].InProgress() || builds[1].InProgress() {
		t.Errorf("InProgress = %v, %v", builds[0].InProgress(), builds[1].InProgress())
	}
}

// Retry is a GET that starts a build. An answer that is not a build must not
// turn a retry that happened into an error.
func TestRetryBuildToleratesANonBuildAnswer(t *testing.T) {
	t.Parallel()

	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`"ok"`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	b, err := c.RetryBuild(context.Background(), "41")
	if err != nil {
		t.Fatalf("RetryBuild: %v", err)
	}
	if method != http.MethodGet || path != "/api/v1/darkube/build/41/retry/" {
		t.Errorf("request = %s %s", method, path)
	}
	if b.ID != "" {
		t.Errorf("ID = %q, want empty", b.ID)
	}
}

func TestBuildLatestCommitAndStopRoutes(t *testing.T) {
	t.Parallel()

	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	if err := c.BuildLatestCommit(context.Background(), "app-1"); err != nil {
		t.Fatalf("BuildLatestCommit: %v", err)
	}
	if err := c.StopBuild(context.Background(), "41"); err != nil {
		t.Fatalf("StopBuild: %v", err)
	}
	want := []string{
		"GET /api/v1/darkube/build/build_last_commit/?app_id=app-1&deploy=true",
		"POST /api/v1/darkube/build/41/stop/?",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("requests = %v, want %v", got, want)
	}
}
