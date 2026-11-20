package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/muesli/cancelreader"
	"github.com/rahacloud/darkubectl/internal/wsexec"
	"golang.org/x/term"
)

const (
	runValue1024 = 1024
)

const (
	runValue32  = 32
	runValue4   = 0x04
	runValue700 = 700
)

type tuiTerminalCommand struct {
	// Command lifetime owns the websocket session.
	ctx                                                    context.Context //nolint:containedctx
	baseURL, tenant, appID, appName, pod, container, token string
	tokenErr                                               error
	stdin                                                  io.Reader
	stdout, stderr                                         io.Writer
}

func (c *tuiTerminalCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *tuiTerminalCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *tuiTerminalCommand) SetStderr(w io.Writer) { c.stderr = w }

var _ tea.ExecCommand = (*tuiTerminalCommand)(nil)

func (c *tuiTerminalCommand) Run() error {
	if c.tokenErr != nil {
		return errors.New("console login required")
	}
	in, out, err := c.terminalFiles()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	session, err := wsexec.Dial(ctx, wsexec.Options{BaseURL: c.baseURL, AccessToken: c.token, Org: c.tenant, AppID: c.appID,
		PodName: c.pod, Container: c.container})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("enter terminal raw mode: %w", err)
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()
	const help = "\r\nConnected to %s · pod %s · container %s\r\n" +
		"Ctrl+D exits · Ctrl+C interrupts the remote process · Ctrl+L clears the screen. " +
		"Use your terminal's usual copy and paste shortcuts.\r\n\r\n"
	if _, err := fmt.Fprintf(c.stdout, help, c.appName, c.pod, c.container); err != nil {
		return fmt.Errorf("write terminal help: %w", err)
	}
	// Let terminal emulators bracket pasted text while the UI is suspended.
	_, _ = io.WriteString(c.stdout, "\x1b[?2004h")
	defer func() { _, _ = io.WriteString(c.stdout, "\x1b[?2004l\x1b[0m\r\n") }()
	var writeMu sync.Mutex
	send := func(fn func() error) error { writeMu.Lock(); defer writeMu.Unlock(); return fn() }
	sendSize := func() { sendTerminalSize(ctx, out, send, session) }
	sendSize()
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return fmt.Errorf("prepare terminal input: %w", err)
	}
	inputDone := startTerminalInput(ctx, cancel, session, reader, send)
	defer func() { reader.Cancel(); _ = reader.Close(); <-inputDone }()
	resize := time.NewTicker(runValue700 * time.Millisecond)
	defer resize.Stop()
	stopResize := startTerminalResize(ctx, resize.C, sendSize)
	defer stopResize()
	return c.readTerminalOutput(ctx, session)
}

func (c *tuiTerminalCommand) terminalFiles() (*os.File, *os.File, error) {
	in, ok := c.stdin.(*os.File)
	if !ok {
		return nil, nil, errors.New("interactive terminal input unavailable")
	}
	out, ok := c.stdout.(*os.File)
	if !ok {
		return nil, nil, errors.New("interactive terminal output unavailable")
	}
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return nil, nil, errors.New("interactive terminal required")
	}
	return in, out, nil
}

func sendTerminalSize(ctx context.Context, out *os.File, send func(func() error) error, session *wsexec.Session) {
	if width, height, err := term.GetSize(int(out.Fd())); err == nil {
		_ = send(func() error { return session.SendResize(ctx, width, height) })
	}
}

func startTerminalInput(ctx context.Context, cancel context.CancelFunc, session *wsexec.Session, reader cancelreader.CancelReader,
	send func(func() error) error) <-chan struct{} {
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		buf := make([]byte, runValue32*runValue1024)
		for {
			n, err := reader.Read(buf)
			if n > 0 && !sendTerminalInput(ctx, cancel, session, send, buf[:n]) {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return inputDone
}

func sendTerminalInput(ctx context.Context, cancel context.CancelFunc, session *wsexec.Session, send func(func() error) error,
	data []byte) bool {
	input, exit := inputBeforeEOF(data)
	if len(input) > 0 && send(func() error { return session.SendInput(ctx, input) }) != nil {
		return false
	}
	if exit {
		cancel()
		return false
	}
	return true
}

func inputBeforeEOF(data []byte) ([]byte, bool) {
	for i, b := range data {
		if b == runValue4 {
			return data[:i], true
		}
	}
	return data, false
}

func startTerminalResize(ctx context.Context, ticks <-chan time.Time, sendSize func()) func() {
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticks:
				sendSize()
			}
		}
	}()
	return func() { close(stop); <-stopped }
}

func (c *tuiTerminalCommand) readTerminalOutput(ctx context.Context, session *wsexec.Session) error {
	for {
		data, e := session.Read(ctx)
		if e != nil {
			return e
		}
		if len(data) > 0 {
			_, _ = c.stdout.Write(data)
		}
	}
}
