package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const (
	flagSince   = "since"
	flagUntil   = "until"
	flagDetails = "details"
)

var errBadSince = errors.New("invalid duration")

func newRolloutHistoryCommand() *cli.Command {
	return &cli.Command{
		Name:      "history",
		Usage:     "Show who changed an app, when, and which fields",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl rollout history my-api\n" +
			"  darkubectl rollout history my-api --since 30d --details\n" +
			"  darkubectl rollout history my-api -o json\n\n" +
			"Reads the app's audit log: one entry per write, newest first, naming the account\n" +
			"that made it. It covers every write, from this CLI or the console alike, and\n" +
			"since every write rolls the pods, it doubles as the app's rollout history.\n\n" +
			"--details prints each changed field's old and new value.",
		Flags: []cli.Flag{
			// The console's history tab opens on the last week too.
			&cli.StringFlag{Name: flagSince, Value: "7d", Usage: "how far back to look (a duration such as 36h or 30d)"},
			&cli.StringFlag{Name: flagUntil, Usage: "end of the window as a duration ago (default: now)"},
			&cli.BoolFlag{Name: flagDetails, Aliases: []string{"d"}, Usage: "print the old and new value of every changed field"},
		},
		Action: rolloutHistoryAction,
	}
}

func rolloutHistoryAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	since, err := parseAgo(cmd.String(flagSince))
	if err != nil {
		return err
	}
	var until time.Duration
	if s := cmd.String(flagUntil); s != "" {
		if until, err = parseAgo(s); err != nil {
			return err
		}
	}
	now := time.Now()

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}
	// Past `until` by a minute, so that a write made a moment ago is inside the
	// window even if this machine's clock is a little behind the server's.
	entries, err := c.AppHistory(ctx, app.ID, now.Add(-since), now.Add(-until+time.Minute))
	if err != nil {
		return err
	}

	if handled, err := output.Structured(os.Stdout, format, entries); handled {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No changes to app %q in the last %s.\n", app.Name, cmd.String(flagSince))
		return nil
	}
	if cmd.Bool(flagDetails) {
		writeHistoryDetails(os.Stdout, entries)
		return nil
	}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{ageOf(e.Date), dash(e.UserEmail), strings.Join(historyFields(e), ",")})
	}
	return output.StyledTable(os.Stdout, []string{colAge, "USER", "FIELDS"}, rows, nil)
}

// historyFields lists the distinct fields an entry changed, in order.
func historyFields(e client.HistoryEntry) []string {
	var fields []string
	for _, ch := range e.Changes {
		if !slices.Contains(fields, ch.Field) {
			fields = append(fields, ch.Field)
		}
	}
	return fields
}

// writeHistoryDetails prints each entry as a header line followed by the same
// -/+ diff that a write shows before it is sent.
func writeHistoryDetails(w io.Writer, entries []client.HistoryEntry) {
	for i, e := range entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s  %s (%s ago)\n", dash(e.UserEmail), dash(e.Date), ageOf(e.Date))
		changes := make([]fieldChange, 0, len(e.Changes))
		for _, ch := range e.Changes {
			changes = append(changes, fieldChange{Key: ch.Field, Before: historyValue(ch.Old), After: historyValue(ch.New)})
		}
		if len(changes) == 0 {
			fmt.Fprintln(w, "  (no field changes recorded)")
			continue
		}
		writeDiff(w, changes)
	}
}

// historyValue renders a history value, which arrives JSON-encoded inside a
// string, the same way a pending write's diff renders it.
func historyValue(s string) string {
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return s
	}
	return renderValue(v)
}

// parseAgo parses a lookback duration. On top of Go's units it accepts a
// trailing "d" for days, which is how far back anyone asks about an audit log.
func parseAgo(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%w %q: want a number of days such as 7d", errBadSince, s)
		}
		return time.Duration(n) * hoursPerDay * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%w %q: want a duration such as 36h or 30d", errBadSince, s)
	}
	return d, nil
}
