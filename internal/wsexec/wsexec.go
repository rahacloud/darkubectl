// Package wsexec opens the Darkube exec websocket and relays a terminal session.
//
// Connection is proven from a console capture:
//
//	wss://api.hamravesh.com/ws/aexec/?app_id=<id>&pod_name=<pod>&container_name=<c>
//	Sec-WebSocket-Protocol: terminal, <console-jwt-access>, <org-slug>
//	Origin: https://console.hamravesh.com
//
// The *frame* format (how stdin, stdout and resize are encoded on the wire) is
// not yet confirmed; it is isolated in encodeInput/encodeResize/decodeOutput
// below and marked with TODO(protocol) pending a Messages-tab capture.
package wsexec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/coder/websocket"
)

const (
	execPath      = "/ws/aexec/"
	subprotocol   = "terminal"
	consoleOrigin = "https://console.hamravesh.com"
)

// Options configures an exec session.
type Options struct {
	BaseURL     string // https base; converted to wss
	AccessToken string // Console JWT access token (2nd subprotocol value)
	Org         string // X-Organization slug (3rd subprotocol value)
	AppID       string
	PodName     string
	Container   string
	Debug       bool // log every frame to stderr (protocol discovery)
}

// Session is a live exec websocket connection.
type Session struct {
	conn  *websocket.Conn
	debug bool
}

func (s *Session) logf(format string, args ...any) {
	if s.debug {
		fmt.Fprintf(os.Stderr, "[wsexec] "+format+"\n", args...)
	}
}

// Dial opens the exec websocket for the given options.
func Dial(ctx context.Context, opts Options) (*Session, error) {
	endpoint, err := buildURL(opts)
	if err != nil {
		return nil, err
	}

	// api.hamravesh.com is publicly reachable; honor proxy env if the user set it.
	httpClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}

	//nolint:bodyclose // coder/websocket owns and closes the upgrade response body
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{subprotocol, opts.AccessToken, opts.Org},
		HTTPHeader:   http.Header{"Origin": {consoleOrigin}},
	})
	if err != nil {
		return nil, fmt.Errorf("dial exec websocket: %w", err)
	}
	// Terminal streams can be large and long-lived.
	conn.SetReadLimit(-1)
	s := &Session{conn: conn, debug: opts.Debug}
	s.logf("connected: negotiated subprotocol=%q", conn.Subprotocol())
	return s, nil
}

func buildURL(opts Options) (string, error) {
	base := strings.TrimRight(opts.BaseURL, "/")
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)

	u, err := url.Parse(base + execPath)
	if err != nil {
		return "", fmt.Errorf("parse websocket url: %w", err)
	}
	q := u.Query()
	q.Set("app_id", opts.AppID)
	q.Set("pod_name", opts.PodName)
	q.Set("container_name", opts.Container)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Read returns the next chunk of terminal output (decoded from a frame).
func (s *Session) Read(ctx context.Context) ([]byte, error) {
	typ, data, err := s.conn.Read(ctx)
	if err != nil {
		s.logf("read end: %v", err)
		return nil, err //nolint:wrapcheck // callers switch on websocket.CloseStatus
	}
	s.logf("recv [%s] %d bytes: %q", typ, len(data), data)
	return decodeOutput(typ, data), nil
}

// SendInput writes terminal input (keystrokes / a command).
func (s *Session) SendInput(ctx context.Context, p []byte) error {
	typ, data := encodeInput(p)
	s.logf("send input [%s] %d bytes: %q", typ, len(data), data)
	if err := s.conn.Write(ctx, typ, data); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	return nil
}

// SendResize informs the remote PTY of a new window size.
func (s *Session) SendResize(ctx context.Context, cols, rows int) error {
	typ, data, ok := encodeResize(cols, rows)
	if !ok {
		return nil
	}
	s.logf("send resize [%s]: %q", typ, data)
	if err := s.conn.Write(ctx, typ, data); err != nil {
		return fmt.Errorf("send resize: %w", err)
	}
	return nil
}

// Close ends the session.
func (s *Session) Close() error {
	return s.conn.Close(websocket.StatusNormalClosure, "")
}

// --- wire protocol (TODO(protocol): confirm from a Messages-tab capture) ---

// encodeInput encodes local keystrokes into an outbound frame. Assumed raw text.
func encodeInput(p []byte) (websocket.MessageType, []byte) {
	return websocket.MessageText, p
}

// decodeOutput extracts terminal output from an inbound frame. Assumed raw bytes.
func decodeOutput(_ websocket.MessageType, data []byte) []byte {
	return data
}

// resizeMessage is a guess at the resize control frame shape.
type resizeMessage struct {
	Resize struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	} `json:"resize"`
}

// encodeResize builds a resize control frame. The bool reports whether the
// protocol supports resize (false → the caller skips sending one).
func encodeResize(cols, rows int) (websocket.MessageType, []byte, bool) {
	var m resizeMessage
	m.Resize.Cols = cols
	m.Resize.Rows = rows
	data, err := json.Marshal(m)
	if err != nil {
		return 0, nil, false
	}
	return websocket.MessageText, data, true
}
