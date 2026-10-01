package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The history route takes its window as RFC 3339 timestamps and answers with
// the entries under "diffs".
func TestAppHistoryDecodesDiffsAndSendsWindow(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		_, _ = w.Write([]byte(`{"diffs":[{"history_date":"2026-09-27T10:00:00+03:30","history_type":"~",` +
			`"user_email":"a@example.com","user_id":7,"changes":[{"field":"replicas","old":"1","new":"2"}]}]}`))
	}))
	defer srv.Close()

	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	c := client.New(srv.URL, client.APIKey("k"), "acme")
	entries, err := c.AppHistory(context.Background(), "app-1", since, until)
	if err != nil {
		t.Fatalf("AppHistory: %v", err)
	}
	if gotPath != "/api/v1/darkube/stateless_apps/app-1/history/" {
		t.Errorf("path = %q", gotPath)
	}
	if got := gotQuery["start_time"]; len(got) != 1 || got[0] != "2026-09-01T00:00:00Z" {
		t.Errorf("start_time = %v", got)
	}
	if got := gotQuery["end_time"]; len(got) != 1 || got[0] != "2026-09-30T12:00:00Z" {
		t.Errorf("end_time = %v", got)
	}
	if len(entries) != 1 || entries[0].UserEmail != "a@example.com" || len(entries[0].Changes) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	if ch := entries[0].Changes[0]; ch.Field != "replicas" || ch.Old != "1" || ch.New != "2" {
		t.Errorf("change = %+v", ch)
	}
}
