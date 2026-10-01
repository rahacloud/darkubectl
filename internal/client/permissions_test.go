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

// Omitting a member from the PUT leaves their access as it was, so a revoke
// has to send the row with an empty list. The write must also echo each row's
// other fields as read.
func TestSetAppPermissionsSendsEmptyRowsAndEchoesFields(t *testing.T) {
	t.Parallel()

	var put []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &put)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.URL.Query().Get("just_me") != "false" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[` +
			`{"user":{"id":1,"email":"a@example.com","full_name":"A","is_staff":false},"permissions":["change","delete","view"]},` +
			`{"user":{"id":2,"email":"b@example.com","full_name":"b@example.com"},"permissions":["view"]}]`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	rows, err := c.AppPermissions(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("AppPermissions: %v", err)
	}
	if len(rows) != 2 || !rows[0].Has(client.PermDelete) || rows[1].Has(client.PermChange) {
		t.Fatalf("rows = %+v", rows)
	}
	rows[1].Permissions = nil
	if err := c.SetAppPermissions(context.Background(), "app-1", rows); err != nil {
		t.Fatalf("SetAppPermissions: %v", err)
	}
	if len(put) != 2 {
		t.Fatalf("PUT sent %d rows, want both", len(put))
	}
	if perms, ok := put[1]["permissions"].([]any); !ok || len(perms) != 0 {
		t.Errorf("revoked row permissions = %#v, want an empty list", put[1]["permissions"])
	}
	if user, _ := put[0]["user"].(map[string]any); user["is_staff"] != false {
		t.Errorf("row fields were not echoed: %v", put[0])
	}
}
