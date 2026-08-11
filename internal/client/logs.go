package client

import (
	"context"
	"net/url"
	"sort"
	"strconv"
)

// tailAnchor is the from_index the console uses to mean "the newest lines".
// The endpoint clamps an out-of-range window to the end of the log, so any
// sufficiently large offset selects the tail; this is the console's own value.
const tailAnchor = 20000000

// LogEntry is one log record: the container's stdout/stderr at a point in time.
// The API returns a timestamp-keyed object, so entries carry their own key.
type LogEntry struct {
	Timestamp string // RFC3339 with nanoseconds, as sent by the API
	Text      string
}

// LogOptions selects which slice of an app's log to read.
type LogOptions struct {
	PodName   string
	Container string
	Tail      int  // number of lines back from the newest
	Previous  bool // the previous container instance, i.e. what a crashloop left behind
}

// logResponse is the confirmed shape of GET .../app_log/:
//
//	{"logs": {"<rfc3339>": "<text>", …}, "reference": 140}
//
// `reference` is the API's cursor into the stream; it is returned so callers can
// page, and grows as new lines arrive.
type logResponse struct {
	Logs      map[string]string `json:"logs"`
	Reference int               `json:"reference"`
}

// AppLogs returns the tail of one container's log, oldest entry first.
//
// The endpoint is index-based rather than time-based: from_index/to_index bound
// a window and reference_index anchors it. Reading the tail means asking for a
// window past the end and letting the server clamp, which is what the console
// does.
func (c *Client) AppLogs(ctx context.Context, appID string, opts LogOptions) ([]LogEntry, int, error) {
	if opts.Tail <= 0 {
		opts.Tail = 100
	}
	q := url.Values{}
	q.Set("from_index", strconv.Itoa(tailAnchor))
	q.Set("to_index", strconv.Itoa(tailAnchor+opts.Tail))
	q.Set("reference_index", "0")
	q.Set("pod_name", opts.PodName)
	q.Set("container_name", opts.Container)
	q.Set("previous", strconv.FormatBool(opts.Previous))

	var out logResponse
	if err := c.getJSON(ctx, appsPathV1+url.PathEscape(appID)+"/app_log/", q, &out); err != nil {
		return nil, 0, err
	}

	entries := make([]LogEntry, 0, len(out.Logs))
	for ts, text := range out.Logs {
		entries = append(entries, LogEntry{Timestamp: ts, Text: text})
	}
	// The API sends an object, so ordering is not preserved by JSON decoding.
	// The keys are RFC3339 with fixed-width nanoseconds, so they sort lexically.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp < entries[j].Timestamp })
	return entries, out.Reference, nil
}
