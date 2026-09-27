package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

// Every field-level edit is the same dance: read the app, change one thing,
// show what would move, ask, write the whole object back, and then read it
// again — because a 202 from this API does not mean the change was stored.
// `set disk` found the platform accepting a field and silently dropping it, and
// custom_config is filtered against the chart schema with no error at all. So
// the re-read is part of every change, not a nicety, and it lives here once.

const (
	// verifyAttempts and verifyInterval bound the read-back. PUT answers 202,
	// which promises the write eventually rather than now, so a single immediate
	// read could report a change as ignored that simply had not landed yet.
	verifyAttempts = 5
	verifyInterval = 2 * time.Second
)

// errChangeIgnored is the outcome that looks like success and is not.
var errChangeIgnored = errors.New("the platform accepted the write but did not store it")

// appChange is one in-place edit of an app, run by runAppChange.
type appChange struct {
	// what names the change for the prompt: "change the image of".
	what string
	// apply mutates the normalized app object.
	apply func(raw map[string]any) error
	// took reports whether a fresh read shows the change. Nil skips the check,
	// for a change whose effect is not visible on the object.
	took func(raw map[string]any) bool
	// verifyDiff checks every field the diff says changed, instead of took.
	// For an edit that touches many fields at once, like `apply`.
	verifyDiff bool
	// confirmed skips the prompt, for a change the user has already approved
	// as part of a larger one.
	confirmed bool
	// notes are printed under the diff, before the prompt.
	notes []string
}

// mutationFlags are the flags every edit takes.
func mutationFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: flagDryRun, Usage: usageDryRun},
		&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
	}
}

// runAppChange runs one edit end to end. It reports whether anything was
// written: false for a dry run, a declined prompt that returned an error, or a
// change the app already had.
func runAppChange(ctx context.Context, cmd *cli.Command, c *client.Client, app *client.App, ch appChange) (bool, error) {
	before, after, err := c.PrepareAppUpdate(ctx, app.ID, ch.apply)
	if err != nil {
		return false, err
	}
	changes := diffApps(before, after)

	if cmd.Bool(flagDryRun) {
		fmt.Fprintf(os.Stdout, "app/%s: dry run, nothing was sent\n", app.Name)
		writeDiff(os.Stdout, changes)
		return false, nil
	}
	if len(changes) == 0 {
		fmt.Fprintf(os.Stdout, "app/%s unchanged: it already matches\n", app.Name)
		return false, nil
	}

	fmt.Fprintf(os.Stderr, "About to %s app %q (%s) in tenant %q:\n", ch.what, app.Name, app.ID, c.Org)
	writeDiff(os.Stderr, changes)
	for _, n := range ch.notes {
		fmt.Fprintf(os.Stderr, "note: %s\n", n)
	}
	if !ch.confirmed && !cmd.Bool(flagYes) && !confirm() {
		return false, errAborted
	}

	if _, err := c.UpdateApp(ctx, app.ID, ch.apply); err != nil {
		return false, err
	}
	took := ch.took
	if ch.verifyDiff {
		took = func(raw map[string]any) bool { return diffStored(raw, after, changes) }
	}
	if took == nil {
		return true, nil
	}
	return true, verifyChange(ctx, c.GetApp, app.ID, took)
}

// diffStored reports whether a fresh read holds every changed field as it was
// written. The read is normalized first, since it nests the relations the
// write sent as ids.
func diffStored(fresh, written map[string]any, changes []fieldChange) bool {
	client.NormalizeForPut(fresh)
	for _, fc := range changes {
		if !sameAfterWrite(fc.Key, fresh[fc.Key], written[fc.Key]) {
			return false
		}
	}
	return true
}

// sameAfterWrite compares one field as written with the same field read back.
// svc is the exception to comparing whole values: the platform fills in
// addresses and nodePorts on it, so only what a caller can write is compared.
func sameAfterWrite(key string, got, want any) bool {
	if key == "svc" {
		return renderValue(svcWritable(got)) == renderValue(svcWritable(want))
	}
	return renderValue(got) == renderValue(want)
}

// svcWritable projects svc onto its writable members: type and, per port, the
// container port, service port and protocol.
func svcWritable(v any) map[string]any {
	svc, _ := v.(map[string]any)
	out := map[string]any{"type": svc["type"]}
	ports, _ := svc["ports"].(map[string]any)
	proj := map[string]any{}
	for name, p := range ports {
		m, _ := p.(map[string]any)
		proj[name] = []any{jsonNumber(m["containerPort"]), jsonNumber(m["servicePort"]), m["protocol"]}
	}
	out["ports"] = proj
	return out
}

// jsonNumber folds the int a caller wrote and the float64 a read decodes into
// one representation.
func jsonNumber(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	default:
		return v
	}
}

// verifyChange re-reads the app until took holds, or reports errChangeIgnored.
func verifyChange(
	ctx context.Context, get func(context.Context, string) (map[string]any, error), id string,
	took func(map[string]any) bool,
) error {
	for attempt := range verifyAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(verifyInterval):
			}
		}
		raw, err := get(ctx, id)
		if err != nil {
			if client.IsTransient(err) {
				continue
			}
			return err
		}
		if took(raw) {
			return nil
		}
	}
	return fmt.Errorf("%w: a re-read %s later still shows the old value. The field is probably "+
		"filtered for this kind of app; check with `darkubectl describe app`",
		errChangeIgnored, verifyInterval*(verifyAttempts-1))
}
