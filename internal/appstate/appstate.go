// Package appstate reads an app's live pods from the Darkube app-pods websocket.
//
// Pods are not exposed over REST (`state.pods` is empty), and the app-state
// socket carries only aggregate replica counts. The console sources pod names
// from a separate stream:
//
//	wss://api.hamravesh.com/ws/app-pods/?app_id=<id>
//	Sec-WebSocket-Protocol: json, <console-jwt-access>, <org-slug>
//
// which streams "app_pods_update" frames whose "data" array holds the pods.
package appstate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	appPodsPath     = "/ws/app-pods/"
	subprotocolJSON = "json"
	consoleOrigin   = "https://console.hamravesh.com"

	fetchTimeout = 15 * time.Second
	maxMessages  = 10
)

// Options configures an app-state fetch.
type Options struct {
	BaseURL     string // https base; converted to wss
	AccessToken string // Console JWT access token (2nd subprotocol value)
	Org         string // X-Organization slug (3rd subprotocol value)
	AppID       string
	Debug       bool // dump raw JSON messages to stderr
}

// Container is one container of a pod, with the health the stream reports for
// it. RestartCount is the counter kubectl shows in its RESTARTS column, and is
// the fastest way to spot a crash loop the aggregate app state hides behind a
// steady "not ready".
type Container struct {
	Name         string `json:"name"`
	State        string `json:"state,omitempty"`
	Ready        bool   `json:"ready"`
	RestartCount int    `json:"restart_count"`
	LastState    string `json:"last_state,omitempty"`
}

// Pod is a running pod of an app.
type Pod struct {
	Name        string      `json:"name"`
	Namespace   string      `json:"namespace,omitempty"`
	Phase       string      `json:"phase,omitempty"`
	State       string      `json:"state,omitempty"`
	Ready       bool        `json:"ready"`
	Terminating bool        `json:"terminating,omitempty"`
	CreatedAt   time.Time   `json:"created_at,omitzero"`
	Containers  []Container `json:"containers,omitempty"`
}

// ContainerNames returns just the container names, for the -c picker.
func (p Pod) ContainerNames() []string {
	names := make([]string, 0, len(p.Containers))
	for _, c := range p.Containers {
		names = append(names, c.Name)
	}
	return names
}

// ReadyCount returns how many of the pod's containers are ready, and how many
// there are, for a kubectl-style "1/2" READY cell.
func (p Pod) ReadyCount() (int, int) {
	ready := 0
	for _, c := range p.Containers {
		if c.Ready {
			ready++
		}
	}
	return ready, len(p.Containers)
}

// Restarts returns the highest restart count across the pod's containers,
// which is what a crash loop shows up as.
func (p Pod) Restarts() int {
	most := 0
	for _, c := range p.Containers {
		if c.RestartCount > most {
			most = c.RestartCount
		}
	}
	return most
}

// FetchPods connects to the app-state websocket and returns the app's pods. It
// also returns the raw JSON of the last message read (useful for --debug and
// for refining the parser). An app with no running pods yields (nil, raw, nil).
func FetchPods(ctx context.Context, opts Options) ([]Pod, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	endpoint, err := buildURL(opts.BaseURL, opts.AppID)
	if err != nil {
		return nil, nil, err
	}

	httpClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	//nolint:bodyclose // coder/websocket owns and closes the upgrade response body
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{subprotocolJSON, opts.AccessToken, opts.Org},
		HTTPHeader:   http.Header{"Origin": {consoleOrigin}},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("dial app-state websocket: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	conn.SetReadLimit(-1)

	var lastRaw []byte
	for range maxMessages {
		_, data, rerr := conn.Read(ctx)
		if rerr != nil {
			if lastRaw != nil {
				break // connected and read at least once; just no pods yet
			}
			return nil, nil, fmt.Errorf("read app-state: %w", rerr)
		}
		lastRaw = data
		if opts.Debug {
			fmt.Fprintf(os.Stderr, "[appstate] recv %d bytes: %s\n", len(data), data)
		}
		if pods := ParsePods(data); len(pods) > 0 {
			return pods, data, nil
		}
	}
	return nil, lastRaw, nil
}

func buildURL(baseURL, appID string) (string, error) {
	base := strings.TrimRight(baseURL, "/")
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	u, err := url.Parse(base + appPodsPath)
	if err != nil {
		return "", fmt.Errorf("parse app-pods url: %w", err)
	}
	q := u.Query()
	q.Set("app_id", appID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// podsMessage is the confirmed shape of an "app_pods_update" frame:
//
//	{"type":"app_pods_update","data":[{
//	  "pod_name":"…","namespace":"…","phase":"Running","text":"error",
//	  "state_type":"error","is_ready":false,"is_terminating":false,
//	  "creation_time":"2026-08-11 07:17:31+00:00",
//	  "containers":[{"name":"main","text":"error","state_type":"error",
//	    "is_ready":false,"restart_count":3529,
//	    "last_state":{"state":"terminated","reason":"Error","message":null}}]}]}
//
// Everything past the name is health the REST API does not expose anywhere, so
// this frame is the only place a crash loop is visible per pod.
type podsMessage struct {
	Data []struct {
		PodName       string `json:"pod_name"`
		Name          string `json:"name"` // fallback for other message variants
		Namespace     string `json:"namespace"`
		Phase         string `json:"phase"`
		Text          string `json:"text"`
		StateType     string `json:"state_type"`
		IsReady       bool   `json:"is_ready"`
		IsTerminating bool   `json:"is_terminating"`
		CreationTime  string `json:"creation_time"`
		Containers    []struct {
			Name         string `json:"name"`
			Text         string `json:"text"`
			StateType    string `json:"state_type"`
			IsReady      bool   `json:"is_ready"`
			RestartCount int    `json:"restart_count"`
			LastState    struct {
				State   string `json:"state"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"last_state"`
		} `json:"containers"`
	} `json:"data"`
}

// ParsePods extracts pods from an app-pods websocket frame. Non-pod frames yield nil.
func ParsePods(data []byte) []Pod {
	var msg podsMessage
	if json.Unmarshal(data, &msg) != nil {
		return nil
	}
	var pods []Pod
	for _, p := range msg.Data {
		name := p.PodName
		if name == "" {
			name = p.Name
		}
		if name == "" {
			continue
		}
		var containers []Container
		for _, c := range p.Containers {
			if c.Name == "" {
				continue
			}
			containers = append(containers, Container{
				Name:         c.Name,
				State:        firstNonEmpty(c.Text, c.StateType),
				Ready:        c.IsReady,
				RestartCount: c.RestartCount,
				LastState:    lastStateLabel(c.LastState.State, c.LastState.Reason),
			})
		}
		pods = append(pods, Pod{
			Name:        name,
			Namespace:   p.Namespace,
			Phase:       p.Phase,
			State:       firstNonEmpty(p.Text, p.StateType),
			Ready:       p.IsReady,
			Terminating: p.IsTerminating,
			CreatedAt:   parseTime(p.CreationTime),
			Containers:  containers,
		})
	}
	return pods
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// lastStateLabel joins the previous container state and its reason into one
// cell, e.g. "terminated: Error" — the "why did it restart" half of a crash loop.
func lastStateLabel(state, reason string) string {
	switch {
	case state != "" && reason != "":
		return state + ": " + reason
	case state != "":
		return state
	default:
		return reason
	}
}

// podTimeLayouts are the timestamp shapes seen on the pod stream. The frame
// uses a space instead of RFC 3339's "T", so time.RFC3339 alone does not parse it.
var podTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05.999999Z07:00",
}

// parseTime decodes a pod creation timestamp, returning the zero time if the
// stream sends a shape we do not know (the age column then renders as "-").
func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range podTimeLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}
