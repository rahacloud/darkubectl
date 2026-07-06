package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/urfave/cli/v3"
)

func newExecCommand() *cli.Command {
	return &cli.Command{
		Name:  "exec",
		Usage: "Run a command in an app's pod",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Run a command in an app's pod (over the exec websocket)",
				ArgsUsage: "NAME|ID -- COMMAND [ARGS...]",
				Flags:     podFlags(),
				Action:    execAppAction,
			},
		},
	}
}

func execAppAction(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errMissingAppRef
	}
	name, command := args[0], args[1:]
	if len(command) == 0 {
		return errNoCommand
	}

	t, err := dialExec(ctx, cmd, name)
	if err != nil {
		return err
	}
	sess := t.sess
	defer func() { _ = sess.Close() }()

	fmt.Fprintf(os.Stderr, "exec in %s (pod %s, container %s)\n", t.appName, t.pod, t.container)

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			data, rerr := sess.Read(sigCtx)
			if rerr != nil {
				return
			}
			_, _ = os.Stdout.Write(data)
		}
	}()

	if err := sess.SendInput(sigCtx, []byte(strings.Join(command, " ")+"\n")); err != nil {
		return err
	}

	// TODO(protocol): a confirmed frame protocol will let us detect
	// single-command completion and surface the exit status. For now, stream
	// output until the server closes the connection or the user interrupts.
	select {
	case <-sigCtx.Done():
	case <-done:
	}
	return nil
}
