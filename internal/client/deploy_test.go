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

// A token deploy carries its credential in the body. A client built without
// one must not send an Authorization header, empty or otherwise.
func TestDeployWithTokenSendsNoAuthorization(t *testing.T) {
	t.Parallel()

	var (
		method, path string
		auth         []string
		body         map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Values("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, "", "")
	err := c.DeployWithToken(context.Background(), client.DeployInput{
		AppID: "app-1", DeployToken: "tok", ImageTag: "abc1234", JobID: "77",
	})
	if err != nil {
		t.Fatalf("DeployWithToken: %v", err)
	}
	if method != http.MethodPut || path != "/api/v1/darkube/apps/update_from_cli/" {
		t.Errorf("request = %s %s", method, path)
	}
	if len(auth) != 0 {
		t.Errorf("Authorization = %q, want none", auth)
	}
	want := map[string]string{"app_id": "app-1", "trigger_deploy_token": "tok", "image_tag": "abc1234", "job_id": "77"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%s] = %v, want %q", k, body[k], v)
		}
	}
}
