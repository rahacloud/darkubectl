package cmd

import (
	"context"
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

type tuiTerminalCommand struct {
	ctx                                                    context.Context
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
		return fmt.Errorf("Console login required")
	}
	in, ok := c.stdin.(*os.File)
	if !ok {
		return fmt.Errorf("interactive terminal input unavailable")
	}
	out, ok := c.stdout.(*os.File)
	if !ok {
		return fmt.Errorf("interactive terminal output unavailable")
	}
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return fmt.Errorf("interactive terminal required")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	session, err := wsexec.Dial(ctx, wsexec.Options{BaseURL: c.baseURL, AccessToken: c.token, Org: c.tenant, AppID: c.appID, PodName: c.pod, Container: c.container})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("enter terminal raw mode: %w", err)
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()
	if _, err := fmt.Fprintf(c.stdout, "\r\nConnected to %s · pod %s · container %s\r\nCtrl+D exits · Ctrl+C interrupts the remote process · Ctrl+L clears the screen. Use your terminal's usual copy and paste shortcuts.\r\n\r\n", c.appName, c.pod, c.container); err != nil {
		return fmt.Errorf("write terminal help: %w", err)
	}
	// Let terminal emulators bracket pasted text while the UI is suspended.
	_, _ = io.WriteString(c.stdout, "\x1b[?2004h")
	defer func() { _, _ = io.WriteString(c.stdout, "\x1b[?2004l\x1b[0m\r\n") }()
	var writeMu sync.Mutex
	send := func(fn func() error) error { writeMu.Lock(); defer writeMu.Unlock(); return fn() }
	sendSize := func() {
		if w, h, e := term.GetSize(int(out.Fd())); e == nil {
			_ = send(func() error { return session.SendResize(ctx, w, h) })
		}
	}
	sendSize()
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return fmt.Errorf("prepare terminal input: %w", err)
	}
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		buf := make([]byte, 32*1024)
		for {
			n, e := reader.Read(buf)
			if n > 0 {
				input := buf[:n]
				exit := false
				for i, b := range input {
					if b == 0x04 {
						input = input[:i]
						exit = true
						break
					}
				}
				if len(input) > 0 {
					if sendErr := send(func() error { return session.SendInput(ctx, input) }); sendErr != nil {
						return
					}
				}
				if exit {
					cancel()
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	defer func() { reader.Cancel(); _ = reader.Close(); <-inputDone }()
	resize := time.NewTicker(700 * time.Millisecond)
	defer resize.Stop()
	resizeDone := make(chan struct{})
	resizeStopped := make(chan struct{})
	defer func() { close(resizeDone); <-resizeStopped }()
	go func() {
		defer close(resizeStopped)
		for {
			select {
			case <-resizeDone:
				return
			case <-ctx.Done():
				return
			case <-resize.C:
				sendSize()
			}
		}
	}()
	for {
		data, e := session.Read(ctx)
		if e != nil {
			return nil
		}
		if len(data) > 0 {
			_, _ = c.stdout.Write(data)
		}
	}
}
