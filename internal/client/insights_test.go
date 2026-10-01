package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The proxy takes the insight type, not PromQL, and answers with a Prometheus
// matrix whose values are [unix seconds, "string"] pairs.
func TestInsightSendsTypeAndParsesMatrix(t *testing.T) {
	t.Parallel()

	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/darkube/insights/proxy_new/api/v1/query_range" {
			t.Errorf("path = %q", r.URL.Path)
		}
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"pod":"api-7d9-x"},"values":[[1759212000,"0.25"],[1759212060,"0.5"],[1759212120,"NaN"]]},` +
			`{"metric":{"pod":"api-7d9-y"},"values":[]}]}}`))
	}))
	defer srv.Close()

	end := time.Unix(1759212300, 0)
	c := client.New(srv.URL, client.APIKey("k"), "acme")
	series, err := c.Insight(context.Background(), client.InsightQuery{
		AppID: "app-1", Type: client.InsightCPU, Start: end.Add(-5 * time.Minute), End: end, Step: time.Minute,
	})
	if err != nil {
		t.Fatalf("Insight: %v", err)
	}
	want := map[string]string{
		"app_id": "app-1", "insight_type": "cpu_usage", "pod_name": "*",
		"start_time": "1759212000", "end_time": "1759212300", "step_size": "60",
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, got.Get(k), v)
		}
	}
	if len(series) != 2 || series[0].Pod != "api-7d9-x" {
		t.Fatalf("series = %+v", series)
	}
	// A NaN is no reading, so the latest sample is the last real one.
	if latest, ok := series[0].Latest(); !ok || latest.Value != 0.5 || len(series[0].Values) != 2 {
		t.Errorf("latest = %+v, %v (of %d samples)", latest, ok, len(series[0].Values))
	}
	if _, ok := series[1].Latest(); ok {
		t.Error("empty series reported a latest sample")
	}
}
