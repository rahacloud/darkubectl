package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

const (
	flagTail       = "tail"
	flagFollow     = "follow"
	flagPrevious   = "previous"
	flagTimestamps = "timestamps"

	defaultTail    = 100
	followInterval = 3 * time.Second
)

func newLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Print a container's logs",
		ArgsUsage: "[APP]",
		Description: "Reads one container's stdout/stderr. The pod is auto-detected from the\n" +
			"app-pods stream unless --pod is given, the same way `exec` picks one.\n\n" +
			"Use --previous to read the container instance that died, which is the\n" +
			"only way to see why a crashlooping app failed — a crashed pod cannot be\n" +
			"exec'd into.",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Print an app container's logs",
				ArgsUsage: argRefUsage,
				Flags: append(podFlags(),
					&cli.IntFlag{Name: flagTail, Value: defaultTail, Usage: "number of lines from the end of the log"},
					&cli.BoolFlag{Name: flagFollow, Aliases: []string{"f"}, Usage: "stream new lines as they arrive"},
					&cli.BoolFlag{Name: flagPrevious, Aliases: []string{"p"}, Usage: "logs of the previous container instance (after a crash/restart)"},
					&cli.BoolFlag{Name: flagTimestamps, Usage: "prefix every line with its API timestamp"},
				),
				Action: logsAppAction,
			},
		},
	}
}

func logsAppAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	c, cfg, err := buildClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}
	access, err := accessToken(ctx, cmd, cfg)
	if err != nil {
		return err
	}
	pod, container, err := selectPod(ctx, cmd, appstate.Options{
		BaseURL:     resolveBaseURL(cmd, cfg),
		AccessToken: access,
		Org:         resolveOrg(cmd, cfg),
		AppID:       app.ID,
		Debug:       cmd.Bool(flagDebug),
	})
	if err != nil {
		return err
	}

	opts := client.LogOptions{
		PodName:   pod,
		Container: container,
		Tail:      cmd.Int(flagTail),
		Previous:  cmd.Bool(flagPrevious),
	}
	withTimestamps := cmd.Bool(flagTimestamps)

	entries, _, err := c.AppLogs(ctx, app.ID, opts)
	if err != nil {
		return err
	}
	if len(entries) == 0 && opts.Previous {
		fmt.Fprintf(os.Stderr, "no previous-instance logs for %s/%s — the container has not restarted\n", pod, container)
		return nil
	}
	printLogs(entries, withTimestamps)

	if !cmd.Bool(flagFollow) {
		return nil
	}

	// The endpoint is a windowed read, not a stream, so following means polling
	// the tail and printing what we have not printed. Entry keys are timestamps
	// with nanosecond precision, which makes them a usable de-duplication key.
	last := ""
	if len(entries) > 0 {
		last = entries[len(entries)-1].Timestamp
	}
	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		next, _, ferr := c.AppLogs(ctx, app.ID, opts)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", ferr) // transient API errors are common; keep following
			continue
		}
		fresh := make([]client.LogEntry, 0, len(next))
		for _, e := range next {
			if e.Timestamp > last {
				fresh = append(fresh, e)
			}
		}
		if len(fresh) > 0 {
			last = fresh[len(fresh)-1].Timestamp
			printLogs(fresh, withTimestamps)
		}
	}
}

func printLogs(entries []client.LogEntry, withTimestamps bool) {
	for _, e := range entries {
		// One entry can carry several physical lines; the API separates them
		// with " \n " rather than a bare newline.
		for line := range strings.SplitSeq(e.Text, "\n") {
			line = strings.TrimRight(line, " \t\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			if withTimestamps {
				fmt.Fprintf(os.Stdout, "%s %s\n", e.Timestamp, line)
			} else {
				fmt.Fprintln(os.Stdout, line)
			}
		}
	}
}
