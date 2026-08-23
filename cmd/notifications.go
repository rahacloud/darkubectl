package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// Column indices used for status-aware coloring in the alert table.
const (
	alertSeverityCol = 1
	alertStatusCol   = 2
)

// flagFiring limits `get alerts` to alerts that have not resolved.
const flagFiring = "firing"

// flagLimit bounds how many notifications are fetched.
const flagLimit = "limit"

// defaultNotificationLimit is how far back `get notifications` reads.
const defaultNotificationLimit = 20

// htmlTag matches the markup the notification descriptions carry.
var htmlTag = regexp.MustCompile(`<[^>]*>`)

func newGetNotificationsCommand() *cli.Command {
	return &cli.Command{
		Name:    "notifications",
		Aliases: []string{"notification", "notifs"},
		Usage:   "List account notifications",
		Description: "The feed spans every organization this account belongs to, so it is not\n" +
			"filtered by the active tenant. Messages are Persian prose from the console.",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: flagLimit, Value: defaultNotificationLimit, Usage: "how many notifications to fetch"},
			&cli.BoolFlag{Name: "unread", Usage: "only show unread notifications"},
		},
		Action: getNotificationsAction,
	}
}

func getNotificationsAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	items, err := c.Notifications(ctx, cmd.Int(flagLimit))
	if err != nil {
		return err
	}
	if cmd.Bool("unread") {
		items = filterNotifications(items)
	}

	if handled, err := output.Structured(os.Stdout, format, items); handled {
		return err
	}
	if format == output.Name {
		for _, n := range items {
			fmt.Fprintln(os.Stdout, strconv.Itoa(n.Slug))
		}
		return nil
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "no notifications")
		return nil
	}

	rows := make([][]string, 0, len(items))
	for _, n := range items {
		marker := ""
		if n.Unread {
			marker = "*"
		}
		rows = append(rows, []string{marker, shortTime(n.Timestamp), dash(n.TargetType), oneLine(n.Title)})
	}
	return output.StyledTable(os.Stdout, []string{"NEW", "TIME", "TYPE", "TITLE"}, rows, nil)
}

func filterNotifications(items []client.Notification) []client.Notification {
	out := make([]client.Notification, 0, len(items))
	for _, n := range items {
		if n.Unread {
			out = append(out, n)
		}
	}
	return out
}

func newGetAlertsCommand() *cli.Command {
	return &cli.Command{
		Name:    "alerts",
		Aliases: []string{"alert"},
		Usage:   "List monitoring alerts for the current tenant",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagFiring, Usage: "only show alerts that have not resolved"},
		},
		Action: getAlertsAction,
	}
}

func getAlertsAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	alerts, err := c.Alerts(ctx)
	if err != nil {
		return err
	}
	if cmd.Bool(flagFiring) {
		alerts = filterFiring(alerts)
	}

	if handled, err := output.Structured(os.Stdout, format, alerts); handled {
		return err
	}
	if format == output.Name {
		for _, a := range alerts {
			fmt.Fprintln(os.Stdout, a.ID)
		}
		return nil
	}
	if len(alerts) == 0 {
		fmt.Fprintln(os.Stderr, "no alerts")
		return nil
	}

	rows := make([][]string, 0, len(alerts))
	for _, a := range alerts {
		rows = append(rows, []string{
			a.AlertName, dash(a.Severity), dash(a.Status),
			dash(a.Instance), dash(a.Condition), shortTime(a.StartsAt),
		})
	}
	header := []string{"ALERT", "SEVERITY", "STATUS", "INSTANCE", "CONDITION", "STARTED"}
	return output.StyledTable(os.Stdout, header, rows,
		output.StateCells(alertSeverityCol, alertStatusCol))
}

func filterFiring(alerts []client.Alert) []client.Alert {
	out := make([]client.Alert, 0, len(alerts))
	for _, a := range alerts {
		if a.IsFiring() {
			out = append(out, a)
		}
	}
	return out
}

// shortTime renders an API timestamp as a compact local date-time.
func shortTime(ts string) string {
	if ts == "" {
		return "-"
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, ts); err == nil {
			//nolint:gosmopolitan // a CLI renders timestamps in the operator's own zone
			return t.Local().Format("2006-01-02 15:04")
		}
	}
	return ts
}

// oneLine flattens the HTML-bearing, multi-line prose the API returns.
func oneLine(s string) string {
	s = htmlTag.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}
