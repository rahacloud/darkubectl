package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The API returns logs as a timestamp-keyed object, so decoding loses ordering.
// AppLogs must restore it, and must translate Tail into the window the endpoint
// expects.
func TestAppLogsOrdersEntriesAndBuildsWindow(t *testing.T) {
	t.Parallel()

	var gotQuery, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			// Deliberately out of order.
			"logs": map[string]string{
				"2026-08-11T15:38:13.108192830Z": "third",
				"2026-08-11T15:17:06.027982227Z": "first",
				"2026-08-11T15:20:00.000000000Z": "second",
			},
			"reference": 140,
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	entries, ref, err := c.AppLogs(context.Background(), "app-1", client.LogOptions{
		PodName: "pod-1", Container: "main", Tail: 25,
	})
	if err != nil {
		t.Fatalf("AppLogs: %v", err)
	}
	if ref != 140 {
		t.Errorf("reference = %d, want 140", ref)
	}
	want := []string{"first", "second", "third"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if entries[i].Text != w {
			t.Errorf("entry %d = %q, want %q", i, entries[i].Text, w)
		}
	}
	if gotPath != "/api/v1/darkube/apps/app-1/app_log/" {
		t.Errorf("path = %q", gotPath)
	}
	// Tail is expressed as a window past the end of the log, which the server clamps.
	for _, w := range []string{"from_index=20000000", "to_index=20000025", "previous=false", "pod_name=pod-1"} {
		if !strings.Contains(gotQuery, w) {
			t.Errorf("query %q missing %q", gotQuery, w)
		}
	}
}

func TestAppLogsDefaultsTail(t *testing.T) {
	t.Parallel()

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"logs":{},"reference":0}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	if _, _, err := c.AppLogs(context.Background(), "a", client.LogOptions{Previous: true}); err != nil {
		t.Fatalf("AppLogs: %v", err)
	}
	if !strings.Contains(gotQuery, "to_index=20000100") {
		t.Errorf("default tail not applied: %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "previous=true") {
		t.Errorf("previous not forwarded: %q", gotQuery)
	}
}
