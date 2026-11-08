package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
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
	flagGrep       = "grep"
	flagErrors     = "errors"

	defaultTail    = 100
	followInterval = 3 * time.Second
)

// errorPattern is what --errors looks for: connection and authentication
// failures from the .NET (SqlClient, MassTransit), Redis and Postgres clients,
// the permission failures of a login that works but lacks a grant, and the
// error/fatal levels of Serilog-style output. It is the check to run after a
// credential change, where the question after each restart is "did anything
// fail to log in, or log in and then fail to read".
const errorPattern = `(?i)login failed|sqlexception|brokerunreachable|access_refused|noauth|wrongpass|` +
	`authentication failed|password authentication|unhandled exception|` +
	`permission was denied|cannot open database|not able to access the database|permission denied for|` +
	`\[(error|fatal|err|ftl)\b`

func newLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Print a container's logs",
		ArgsUsage: "APP|ID",
		Description: "Reads one container's stdout/stderr. The pod is auto-detected from the\n" +
			"app-pods stream unless --pod is given, the same way `exec` picks one.\n\n" +
			"Use --previous to read the container instance that died, which is the\n" +
			"only way to see why a crashlooping app failed — a crashed pod cannot be\n" +
			"exec'd into.\n\n" +
			"--grep REGEX keeps only the log entries that match (a multi-line entry, such as\n" +
			"an exception with its stack, is kept whole). --errors does the same with a\n" +
			"built-in pattern for connection, authentication and error-level lines, prints\n" +
			"a count, and exits 1 if anything matched — the check to run after changing a\n" +
			"credential.\n\n" +
			"`darkubectl logs NAME` is the kubectl-shaped shorthand for `logs app NAME`.",
		Flags:  logsFlags(),
		Action: logsAppAction,
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Print an app container's logs",
				ArgsUsage: argRefUsage,
				Flags:     logsFlags(),
				Action:    logsAppAction,
			},
		},
	}
}

// logsFlags builds the log-reading flag set. It is built twice — once for the
// `logs app NAME` form and once for the bare `logs NAME` shorthand — because
// urfave/cli does not share a parent's non-persistent flags with a subcommand.
func logsFlags() []cli.Flag {
	return append(podFlags(),
		&cli.IntFlag{Name: flagTail, Value: defaultTail, Usage: "number of lines from the end of the log"},
		&cli.BoolFlag{Name: flagFollow, Aliases: []string{"f"}, Usage: "stream new lines as they arrive"},
		&cli.BoolFlag{Name: flagPrevious, Aliases: []string{"p"}, Usage: "logs of the previous container instance (after a crash/restart)"},
		&cli.BoolFlag{Name: flagTimestamps, Usage: "prefix every line with its API timestamp"},
		&cli.StringFlag{Name: flagGrep, Usage: "keep only entries matching this regular expression"},
		&cli.BoolFlag{Name: flagErrors, Usage: "keep only connection/auth/error entries; exit 1 if any"},
	)
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
	filter, filtering, err := logFilter(cmd)
	if err != nil {
		return err
	}
	if !filtering {
		filter = nil
	}

	entries, _, err := c.AppLogs(ctx, app.ID, opts)
	if err != nil {
		return err
	}
	if len(entries) == 0 && opts.Previous {
		fmt.Fprintf(os.Stderr, "no previous-instance logs for %s/%s — the container has not restarted\n", pod, container)
		return nil
	}
	scanned := len(entries)
	entries = filterLogs(entries, filter)
	printLogs(entries, withTimestamps)

	if !cmd.Bool(flagFollow) {
		if cmd.Bool(flagErrors) {
			return reportErrorScan(app.Name+"/"+pod, len(entries), scanned)
		}
		return nil
	}

	return followLogs(ctx, c, app.ID, opts, entries, filter, withTimestamps)
}

// followLogs polls the log tail after the first read and prints what is new.
func followLogs(
	ctx context.Context, c *client.Client, appID string, opts client.LogOptions,
	entries []client.LogEntry, filter *regexp.Regexp, withTimestamps bool,
) error {
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
		next, _, ferr := c.AppLogs(ctx, appID, opts)
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
			printLogs(filterLogs(fresh, filter), withTimestamps)
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

// reportErrorScan prints the --errors summary and fails when anything matched.
func reportErrorScan(where string, matched, scanned int) error {
	fmt.Fprintf(os.Stderr, "%s: %d of the last %d log entries match the error pattern\n", where, matched, scanned)
	if matched > 0 {
		return cli.Exit("", 1)
	}
	return nil
}

// logFilter builds the entry filter from --grep and --errors, and reports
// whether either was given. Given both, an entry is kept if it matches either.
func logFilter(cmd *cli.Command) (*regexp.Regexp, bool, error) {
	var parts []string
	if g := cmd.String(flagGrep); g != "" {
		if _, err := regexp.Compile(g); err != nil {
			return nil, false, fmt.Errorf("--%s: %w", flagGrep, err)
		}
		parts = append(parts, "(?:"+g+")")
	}
	if cmd.Bool(flagErrors) {
		parts = append(parts, "(?:"+errorPattern+")")
	}
	if len(parts) == 0 {
		return nil, false, nil
	}
	return regexp.MustCompile(strings.Join(parts, "|")), true, nil
}

// filterLogs keeps the entries whose text matches. It works on entries rather
// than lines, so an exception's stack stays attached to its message.
func filterLogs(entries []client.LogEntry, re *regexp.Regexp) []client.LogEntry {
	if re == nil {
		return entries
	}
	out := entries[:0:0]
	for _, e := range entries {
		if re.MatchString(e.Text) {
			out = append(out, e)
		}
	}
	return out
}
