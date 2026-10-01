package client

import (
	"context"
	"math"
	"net/url"
	"strconv"
	"time"
)

// insightsPathV1 is the console's resource-usage source: a Prometheus
// query_range behind the API. It does not take PromQL; the server builds the
// query from insight_type and scopes it to app_id and pod_name ("*" for every
// pod). Found in the console's ResourceUsage tab, 2026-09-30.
const insightsPathV1 = "/api/v1/darkube/insights/proxy_new/api/v1/query_range"

// Insight types the console charts, with the units the console converts from.
const (
	InsightCPU            = "cpu_usage"       // cores
	InsightCPUThrottling  = "cpu_throttling"  // fraction of periods throttled, 0-1
	InsightRAM            = "ram_usage"       // bytes
	InsightDisk           = "disk_usage"      // bytes
	InsightNetworkReceive = "network_receive" // bytes per second
	InsightNetworkSend    = "network_send"    // bytes per second

	// AllPods scopes an insight to every pod of the app, one series each.
	AllPods = "*"

	// podLabel is the series label that names the pod.
	podLabel = "pod"
)

// InsightQuery selects one metric of one app over a window.
type InsightQuery struct {
	AppID string
	Type  string
	// Pod is a pod name, or AllPods.
	Pod        string
	Start, End time.Time
	Step       time.Duration
}

// Series is one pod's samples of a metric.
type Series struct {
	Pod    string            `json:"pod"    yaml:"pod"`
	Labels map[string]string `json:"labels" yaml:"labels"`
	Values []Sample          `json:"values" yaml:"values"`
}

// Sample is one point of a Series.
type Sample struct {
	Time  time.Time `json:"time"  yaml:"time"`
	Value float64   `json:"value" yaml:"value"`
}

// Latest is the most recent sample, and whether there is one.
func (s Series) Latest() (Sample, bool) {
	if len(s.Values) == 0 {
		return Sample{}, false
	}
	return s.Values[len(s.Values)-1], true
}

type promMatrix struct {
	Data struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// Insight runs one resource-usage query and returns a series per pod.
func (c *Client) Insight(ctx context.Context, q InsightQuery) ([]Series, error) {
	pod := q.Pod
	if pod == "" {
		pod = AllPods
	}
	v := url.Values{}
	v.Set("app_id", q.AppID)
	v.Set("insight_type", q.Type)
	v.Set("pod_name", pod)
	v.Set("start_time", strconv.FormatInt(q.Start.Unix(), 10))
	v.Set("end_time", strconv.FormatInt(q.End.Unix(), 10))
	v.Set("step_size", strconv.Itoa(int(q.Step.Seconds())))

	var m promMatrix
	if err := c.getJSON(ctx, insightsPathV1, v, &m); err != nil {
		return nil, err
	}
	out := make([]Series, 0, len(m.Data.Result))
	for _, r := range m.Data.Result {
		s := Series{Pod: r.Metric[podLabel], Labels: r.Metric}
		for _, pair := range r.Values {
			sample, ok := parseSample(pair)
			if ok {
				s.Values = append(s.Values, sample)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// parseSample decodes Prometheus's [<unix seconds>, "<value>"] pair.
func parseSample(pair [2]any) (Sample, bool) {
	ts, ok := pair[0].(float64)
	if !ok {
		return Sample{}, false
	}
	str, ok := pair[1].(string)
	if !ok {
		return Sample{}, false
	}
	val, err := strconv.ParseFloat(str, 64)
	// Prometheus answers "NaN" for a ratio with nothing to divide by, such as
	// throttling on a pod that ran no CPU periods; that is no reading at all.
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
		return Sample{}, false
	}
	sec := int64(ts)
	return Sample{Time: time.Unix(sec, int64((ts-float64(sec))*float64(time.Second))), Value: val}, true
}
