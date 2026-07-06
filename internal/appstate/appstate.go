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

// Pod is a running pod of an app.
type Pod struct {
	Name       string   `json:"name"`
	Containers []string `json:"containers,omitempty"`
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
//	{"type":"app_pods_update","data":[{"pod_name":"…","containers":[{"name":"…"}]}]}
type podsMessage struct {
	Data []struct {
		PodName    string `json:"pod_name"`
		Name       string `json:"name"` // fallback for other message variants
		Containers []struct {
			Name string `json:"name"`
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
		var containers []string
		for _, c := range p.Containers {
			if c.Name != "" {
				containers = append(containers, c.Name)
			}
		}
		pods = append(pods, Pod{Name: name, Containers: containers})
	}
	return pods
}
