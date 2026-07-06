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

// The exec stream uses the Kubernetes remotecommand channel protocol: every
// binary frame is prefixed with a 1-byte channel id.
const (
	channelStdin  = 0 // client -> server: terminal input
	channelStdout = 1 // server -> client: stdout
	channelStderr = 2 // server -> client: stderr
	channelResize = 4 // client -> server: terminal size (JSON {"Width","Height"})
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
	_, data, err := s.conn.Read(ctx)
	if err != nil {
		s.logf("read end: %v", err)
		return nil, err //nolint:wrapcheck // callers switch on websocket.CloseStatus
	}
	s.logf("recv %d bytes: %q", len(data), data)
	return decodeOutput(data), nil
}

// SendInput writes terminal input (keystrokes / a command) on the stdin channel.
func (s *Session) SendInput(ctx context.Context, p []byte) error {
	frame := append([]byte{channelStdin}, p...)
	s.logf("send input %d bytes: %q", len(p), p)
	if err := s.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	return nil
}

// SendResize informs the remote PTY of a new window size on the resize channel.
func (s *Session) SendResize(ctx context.Context, cols, rows int) error {
	frame, err := encodeResize(cols, rows)
	if err != nil {
		return err
	}
	s.logf("send resize: %q", frame)
	if err := s.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		return fmt.Errorf("send resize: %w", err)
	}
	return nil
}

// Close ends the session.
func (s *Session) Close() error {
	return s.conn.Close(websocket.StatusNormalClosure, "")
}

// --- Kubernetes remotecommand channel protocol ---

// decodeOutput strips the leading channel byte and returns stdout/stderr bytes.
// Other channels (e.g. 3 = error/status) are not terminal output.
func decodeOutput(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	switch data[0] {
	case channelStdout, channelStderr:
		return data[1:]
	default:
		return nil
	}
}

// terminalSize is the resize payload, matching k8s remotecommand.TerminalSize.
type terminalSize struct {
	Width  int `json:"Width"`
	Height int `json:"Height"`
}

// encodeResize builds a channel-4 resize frame: the channel byte followed by a
// JSON {"Width":cols,"Height":rows} payload.
func encodeResize(cols, rows int) ([]byte, error) {
	payload, err := json.Marshal(terminalSize{Width: cols, Height: rows})
	if err != nil {
		return nil, fmt.Errorf("encode resize: %w", err)
	}
	return append([]byte{channelResize}, payload...), nil
}
