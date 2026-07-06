package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func newTerminalCommand() *cli.Command {
	return &cli.Command{
		Name:    "terminal",
		Aliases: []string{"shell"},
		Usage:   "Open an interactive shell in an app's pod",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Open an interactive shell in an app's pod",
				ArgsUsage: argRefUsage,
				Flags:     podFlags(),
				Action:    terminalAppAction,
			},
		},
	}
}

func terminalAppAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())
	if !term.IsTerminal(stdinFd) || !term.IsTerminal(stdoutFd) {
		return errNotATTY
	}

	t, err := dialExec(ctx, cmd, name)
	if err != nil {
		return err
	}
	sess := t.sess
	defer func() { _ = sess.Close() }()
	fmt.Fprintf(os.Stderr, "connecting %s/%s (container %s) …\r\n", t.appName, t.pod, t.container)

	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return fmt.Errorf("enter raw mode: %w", err)
	}
	defer func() { _ = term.Restore(stdinFd, oldState) }()

	// Report the initial window size and follow resizes.
	sendSize := func() {
		if w, h, gerr := term.GetSize(stdoutFd); gerr == nil {
			_ = sess.SendResize(ctx, w, h)
		}
	}
	sendSize()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			sendSize()
		}
	}()

	// Local keystrokes -> remote PTY.
	go func() {
		buf := make([]byte, ioBufferSize)
		for {
			n, rerr := os.Stdin.Read(buf)
			if n > 0 {
				if werr := sess.SendInput(ctx, buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				return
			}
		}
	}()

	// Remote PTY -> local stdout, until the session ends.
	for {
		data, rerr := sess.Read(ctx)
		if rerr != nil {
			return nil
		}
		_, _ = os.Stdout.Write(data)
	}
}
