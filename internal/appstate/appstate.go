// Package appstate reads an app's live state (notably its pods) from the
// Darkube app-state websocket.
//
// Pods are not exposed over REST (`state.pods` is empty); the console sources
// them from:
//
//	wss://api.hamravesh.com/ws/app-state/?app_id=<id>
//	Sec-WebSocket-Protocol: json, <console-jwt-access>, <org-slug>
//
// which streams the app state as JSON. Pod extraction is deliberately defensive
// (it searches the payload for a "pods" array) until the exact shape is pinned.
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
	appStatePath    = "/ws/app-state/"
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
		if pods := parsePods(data); len(pods) > 0 {
			return pods, data, nil
		}
	}
	return nil, lastRaw, nil
}

func buildURL(baseURL, appID string) (string, error) {
	base := strings.TrimRight(baseURL, "/")
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	u, err := url.Parse(base + appStatePath)
	if err != nil {
		return "", fmt.Errorf("parse app-state url: %w", err)
	}
	q := u.Query()
	q.Set("app_id", appID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// parsePods searches a decoded JSON payload for a "pods" array and extracts pod
// names (and containers when present).
func parsePods(data []byte) []Pod {
	var root any
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	var pods []Pod
	for _, entry := range findPods(root) {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name := asString(m["name"])
		if name == "" {
			continue
		}
		pods = append(pods, Pod{Name: name, Containers: extractContainers(m)})
	}
	return pods
}

// findPods returns the first "pods" array found anywhere in the payload.
func findPods(v any) []any {
	switch t := v.(type) {
	case map[string]any:
		if pods, ok := t["pods"].([]any); ok {
			return pods
		}
		for _, val := range t {
			if found := findPods(val); found != nil {
				return found
			}
		}
	case []any:
		for _, e := range t {
			if found := findPods(e); found != nil {
				return found
			}
		}
	}
	return nil
}

func extractContainers(m map[string]any) []string {
	for _, key := range []string{"containers", "container_names"} {
		raw, ok := m[key].([]any)
		if !ok {
			continue
		}
		var out []string
		for _, c := range raw {
			switch cv := c.(type) {
			case string:
				out = append(out, cv)
			case map[string]any:
				if name := asString(cv["name"]); name != "" {
					out = append(out, name)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func asString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
