package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

// Waiting on an app is otherwise a hand-rolled polling loop in every script that
// creates or changes one — and a `sleep 30` in the ones that do not bother.
// Deploys are asynchronous end to end: create returns before the pod is
// scheduled, and a patch that fixes a crash loop takes a rollout to take effect.

const (
	flagFor      = "for"
	flagTimeout  = "timeout"
	flagInterval = "interval"

	condReady   = "ready"
	condDeleted = "deleted"

	// stateHealthy is the state_type the platform reports for an app whose
	// replicas are all up. The accompanying text reads "healthy (1/1)"; other
	// values seen are "not ready (0/1)" and "inaccessible (0/0)".
	stateHealthy = "healthy"

	defaultWaitTimeout  = 5 * time.Minute
	defaultWaitInterval = 5 * time.Second
)

var (
	errWaitTimeout   = errors.New("timed out waiting for the condition")
	errUnknownCond   = fmt.Errorf("--%s must be %q or %q", flagFor, condReady, condDeleted)
	errBadWaitTiming = fmt.Errorf("--%s and --%s must be positive durations", flagTimeout, flagInterval)
)

func newWaitCommand() *cli.Command {
	return &cli.Command{
		Name:  "wait",
		Usage: "Block until a resource reaches a condition",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Block until an app is ready, or gone",
				ArgsUsage: argRefUsage,
				Description: "  darkubectl wait app my-api --for ready --timeout 10m\n" +
					"  darkubectl wait app my-api --for deleted\n\n" +
					"Every deploy on this platform is asynchronous: create returns before the pod is\n" +
					"scheduled, and a patch that fixes a crash loop takes a rollout to land. This is\n" +
					"the alternative to a `sleep` in the script that follows.\n\n" +
					"`ready` means the platform reports the app as healthy, i.e. every replica is up.\n" +
					"It does not mean DNS resolves or the certificate has been issued, which the\n" +
					"platform does separately and can lag by minutes.\n\n" +
					"Exits non-zero if the timeout passes first, so `&&` does what you would expect.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  flagFor,
						Value: condReady,
						Usage: "condition to wait for: ready|deleted",
					},
					&cli.DurationFlag{
						Name:  flagTimeout,
						Value: defaultWaitTimeout,
						Usage: "give up after this long",
					},
					&cli.DurationFlag{
						Name:  flagInterval,
						Value: defaultWaitInterval,
						Usage: "how often to poll",
					},
				},
				Action: waitAppAction,
			},
		},
	}
}

func waitAppAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	condition := strings.ToLower(cmd.String(flagFor))
	if condition != condReady && condition != condDeleted {
		return errUnknownCond
	}
	timeout, interval := cmd.Duration(flagTimeout), cmd.Duration(flagInterval)
	if timeout <= 0 || interval <= 0 {
		return errBadWaitTiming
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	// Resolve once by name, then poll by id. ResolveApp lists every app in the
	// tenant, which is far too much work to repeat every few seconds.
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		if condition == condDeleted && errors.Is(err, client.ErrAppNotFound) {
			fmt.Fprintf(os.Stdout, "app/%s deleted\n", ref)
			return nil
		}
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return pollUntil(ctx, c, app.ID, app.Name, condition, interval)
}

// pollUntil polls one app until the condition holds or the context expires.
func pollUntil(
	ctx context.Context, c *client.Client, id, name, condition string, interval time.Duration,
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastState string
	for {
		done, state, err := checkCondition(ctx, c, id, condition)
		switch {
		case err != nil:
			return err
		case done:
			fmt.Fprintf(os.Stdout, "app/%s %s\n", name, condition)
			return nil
		}
		// Report only transitions, so a long wait does not scroll.
		if state != lastState && state != "" {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", name, state)
			lastState = state
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("%w: app/%s is %q, want %q", errWaitTimeout, name, dash(lastState), condition)
		}
	}
}

// checkCondition reports whether the condition currently holds, along with the
// app's state for progress reporting.
//
// Transient failures are swallowed: this API returns intermittent 5xx, and a
// wait that aborts on one is worse than useless because the caller usually
// cannot tell it apart from a real failure.
func checkCondition(ctx context.Context, c *client.Client, id, condition string) (bool, string, error) {
	app, err := c.GetAppTyped(ctx, id)
	if err != nil {
		if client.IsNotFound(err) {
			// Gone. Terminal for `deleted`; for `ready` it means someone removed
			// the app out from under us, which will never become ready.
			if condition == condDeleted {
				return true, "gone", nil
			}
			return false, "", fmt.Errorf("app %s disappeared while waiting for it: %w", id, err)
		}
		if client.IsTransient(err) || ctx.Err() != nil {
			// Deliberately discarded: a transient 5xx means "ask again", not
			// "give up". The caller's timeout is what ends the loop.
			return false, "", nil //nolint:nilerr // see above
		}
		return false, "", err
	}
	if condition == condDeleted {
		return false, stateLabel(app.State), nil
	}
	return app.State.StateType == stateHealthy, stateLabel(app.State), nil
}
