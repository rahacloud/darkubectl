package wsexec

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestBuildURL(t *testing.T) {
	t.Parallel()

	got, err := buildURL(Options{
		BaseURL: "https://api.hamravesh.com/", AppID: "a-1", PodName: "web-0", Container: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "wss" || u.Host != "api.hamravesh.com" || u.Path != execPath {
		t.Errorf("url = %s, want wss://api.hamravesh.com%s", got, execPath)
	}
	q := u.Query()
	if q.Get("app_id") != "a-1" || q.Get("pod_name") != "web-0" || q.Get("container_name") != "main" {
		t.Errorf("query = %v", q)
	}

	plain, err := buildURL(Options{BaseURL: "http://localhost:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := url.Parse(plain); u.Scheme != "ws" {
		t.Errorf("http base became %s, want ws://", plain)
	}
}

func TestDecodeOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   []byte
		want []byte
	}{
		{"empty", nil, nil},
		{"stdout", []byte{channelStdout, 'h', 'i'}, []byte("hi")},
		{"stderr", []byte{channelStderr, 'e'}, []byte("e")},
		{"status is not output", []byte{channelError, '{', '}'}, nil},
		{"stdin echo is not output", []byte{channelStdin, 'x'}, nil},
	} {
		if got := decodeOutput(tc.in); !bytes.Equal(got, tc.want) {
			t.Errorf("%s: decodeOutput = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestEncodeResize(t *testing.T) {
	t.Parallel()

	got, err := encodeResize(120, 40)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{channelResize}, `{"Width":120,"Height":40}`...)
	if !bytes.Equal(got, want) {
		t.Errorf("encodeResize = %q, want %q", got, want)
	}
}

// TestSessionRoundTrip runs a session against an in-process server: input goes
// out on channel 0, output comes back on channel 1, and a channel-3 frame ends
// the session with ErrExited.
func TestSessionRoundTrip(t *testing.T) {
	t.Parallel()

	gotInput := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The token and tenant ride in the subprotocol list, in this order.
		if proto := strings.ReplaceAll(r.Header.Get("Sec-WebSocket-Protocol"), " ", ""); proto != "terminal,tok,org" {
			t.Errorf("subprotocols = %q, want terminal,tok,org", proto)
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:   []string{subprotocol},
			OriginPatterns: []string{"console.hamravesh.com"},
		})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()
		_, in, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		gotInput <- in
		_ = conn.Write(ctx, websocket.MessageBinary, append([]byte{channelStdout}, "hello\n"...))
		_ = conn.Write(ctx, websocket.MessageBinary, append([]byte{channelError}, `{"status":"Success"}`...))
		_, _, _ = conn.Read(ctx) // hold the connection until the client closes
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := Dial(ctx, Options{BaseURL: srv.URL, AccessToken: "tok", Org: "org", AppID: "a"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.SendInput(ctx, []byte("ls\n")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if in := <-gotInput; !bytes.Equal(in, append([]byte{channelStdin}, "ls\n"...)) {
		t.Errorf("server got %q, want channel 0 + ls", in)
	}
	out, err := s.Read(ctx)
	if err != nil || string(out) != "hello\n" {
		t.Fatalf("Read = %q, %v; want hello", out, err)
	}
	if _, err := s.Read(ctx); !errors.Is(err, ErrExited) {
		t.Fatalf("Read after status = %v, want ErrExited", err)
	}
}
