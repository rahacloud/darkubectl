package appstate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// podsFrame builds an app_pods_update frame holding the named pods.
func podsFrame(names ...string) string {
	items := make([]string, 0, len(names))
	for _, n := range names {
		items = append(items, fmt.Sprintf(`{"pod_name":%q,"containers":[{"name":"main"}]}`, n))
	}
	return `{"type":"app_pods_update","data":[` + strings.Join(items, ",") + `]}`
}

// fakeAppPods serves /ws/app-pods/, writing frames with the given gaps before
// each, then holding the connection open until the client goes away.
func fakeAppPods(t *testing.T, frames []string, gaps []time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:   []string{subprotocolJSON},
			OriginPatterns: []string{"console.hamravesh.com"},
		})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()
		for i, f := range frames {
			if i < len(gaps) {
				time.Sleep(gaps[i])
			}
			if err := conn.Write(ctx, websocket.MessageText, []byte(f)); err != nil {
				return
			}
		}
		_, _, _ = conn.Read(ctx)
	}))
}

func podNames(pods []Pod) string {
	names := make([]string, 0, len(pods))
	for _, p := range pods {
		names = append(names, p.Name)
	}
	return strings.Join(names, ",")
}

// TestFetchPodsOutlastsOtherFrames pins that FetchPods waits for a pod list by
// time, not by frame count: it used to give up after ten frames of another
// type and report a running app as having no pods.
func TestFetchPodsOutlastsOtherFrames(t *testing.T) {
	t.Parallel()

	frames := make([]string, 0, 16)
	for range 15 {
		frames = append(frames, `{"type":"app_state_update","data":{}}`)
	}
	frames = append(frames, podsFrame("web-1"))
	srv := fakeAppPods(t, frames, nil)
	defer srv.Close()

	pods, raw, err := FetchPods(context.Background(), Options{BaseURL: srv.URL, AppID: "a"})
	if err != nil {
		t.Fatalf("FetchPods: %v", err)
	}
	if raw == nil || podNames(pods) != "web-1" {
		t.Fatalf("pods = %q, want web-1", podNames(pods))
	}
}

// TestFetchPodsTakesTheSettledList pins the stale-first-frame workaround: the
// list sent shortly after the first one wins, however many frames come between.
func TestFetchPodsTakesTheSettledList(t *testing.T) {
	t.Parallel()

	frames := make([]string, 0, 14)
	gaps := make([]time.Duration, 0, 14)
	frames = append(frames, podsFrame("web-old"))
	gaps = append(gaps, 0)
	for range 12 {
		frames = append(frames, `{"type":"app_state_update","data":{}}`)
		gaps = append(gaps, 0)
	}
	frames = append(frames, podsFrame("web-old", "web-new"))
	gaps = append(gaps, 150*time.Millisecond)
	srv := fakeAppPods(t, frames, gaps)
	defer srv.Close()

	pods, _, err := FetchPods(context.Background(), Options{BaseURL: srv.URL, AppID: "a"})
	if err != nil {
		t.Fatalf("FetchPods: %v", err)
	}
	if got := podNames(pods); got != "web-old,web-new" {
		t.Fatalf("pods = %q, want the settled list web-old,web-new", got)
	}
}

func TestWatchPodsDeliversEveryList(t *testing.T) {
	t.Parallel()

	srv := fakeAppPods(t, []string{
		podsFrame("web-1"),
		`{"type":"app_state_update","data":{}}`,
		podsFrame("web-1", "web-2"),
	}, nil)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var seen []string
	err := WatchPods(ctx, Options{BaseURL: srv.URL, AppID: "a"}, func(pods []Pod) error {
		seen = append(seen, podNames(pods))
		if len(seen) == 2 {
			cancel()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WatchPods: %v", err)
	}
	if strings.Join(seen, " | ") != "web-1 | web-1,web-2" {
		t.Fatalf("seen = %q", seen)
	}
}
