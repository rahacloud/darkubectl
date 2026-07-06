package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestListAppsSendsAuthAndParses(t *testing.T) {
	t.Parallel()

	var gotAuth, gotOrg, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrg = r.Header.Get("X-Organization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"count":1,"next":null,"previous":null,`+
			`"results":[{"id":"a1","name":"app1","replicas":2,"ram_limit":"500M"}]}`)
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("tok"), "talaland")
	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "app1" || apps[0].Replicas != 2 || apps[0].RAMLimit != "500M" {
		t.Errorf("apps = %+v, want one app1/2/500M", apps)
	}
	if gotAuth != "Api-key tok" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Api-key tok")
	}
	if gotOrg != "talaland" {
		t.Errorf("X-Organization = %q, want talaland", gotOrg)
	}
	if gotPath != "/api/v2/darkube/apps/" {
		t.Errorf("path = %q, want /api/v2/darkube/apps/", gotPath)
	}
}

func TestAPIErrorEnvelopeIsDecoded(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"detail":"nope","success":false,"code":"permission_denied"}`)
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("tok"), "org")
	_, err := c.ListApps(context.Background())

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *client.APIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Code != "permission_denied" {
		t.Errorf("APIError = %+v, want 403/permission_denied", apiErr)
	}
}

func TestResolveAppByNameAndAmbiguity(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"count":3,"next":null,"results":[`+
			`{"id":"1","name":"api"},{"id":"2","name":"web"},{"id":"3","name":"web"}]}`)
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("tok"), "org")
	ctx := context.Background()

	app, err := c.ResolveApp(ctx, "api")
	if err != nil || app.ID != "1" {
		t.Errorf("ResolveApp(api) = %+v, %v; want id 1", app, err)
	}
	if _, err := c.ResolveApp(ctx, "web"); !errors.Is(err, client.ErrAppAmbiguous) {
		t.Errorf("ResolveApp(web) err = %v, want ErrAppAmbiguous", err)
	}
	if _, err := c.ResolveApp(ctx, "missing"); !errors.Is(err, client.ErrAppNotFound) {
		t.Errorf("ResolveApp(missing) err = %v, want ErrAppNotFound", err)
	}
}
