package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

// `wait app --for ready` cannot follow a rollout: the platform reports the app
// healthy for as long as the OLD pod is up, so it passes the moment it is asked,
// before the new ReplicaSet has scheduled anything. Following a rollout needs
// the pods themselves, which only the app-pods websocket carries. The test is
// by name rather than by creation time so that it does not depend on this
// machine's clock agreeing with the cluster's: the rollout is done when no pod
// that existed before the change is left, and every pod that exists is ready.

const (
	flagWait = "wait"

	rolloutPollInterval = 3 * time.Second

	// noNewPodGrace is how long a rollout may show no replacement pod before
	// that is worth pointing out. A healthy one shows it within seconds.
	noNewPodGrace = 30 * time.Second
)

var errRolloutTimeout = errors.New("timed out waiting for the rollout")

func newRolloutCommand() *cli.Command {
	return &cli.Command{
		Name:  "rollout",
		Usage: "Restart an app's pods, or follow a rollout until it lands",
		Commands: []*cli.Command{
			{
				Name:      "restart",
				Usage:     "Replace every pod of an app with a fresh one",
				ArgsUsage: argRefUsage,
				Description: "  darkubectl rollout restart my-api\n" +
					"  darkubectl rollout restart my-api --wait --timeout 5m\n\n" +
					"Uses the platform's own restart action, which rolls the Deployment to a new\n" +
					"ReplicaSet like `kubectl rollout restart`. Nothing on the app object changes.\n\n" +
					"It is a surge: the old pods keep serving until their replacements are ready.\n\n" +
					"--wait blocks until every pod that existed before the restart is gone and every\n" +
					"pod that replaced it is ready.",
				Flags: append(rolloutWaitFlags(),
					&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}),
				Action: rolloutRestartAction,
			},
			{
				Name:      "status",
				Usage:     "Block until an app's pods are all ready and nothing is terminating",
				ArgsUsage: argRefUsage,
				Description: "  darkubectl rollout status my-api --timeout 10m\n\n" +
					"Unlike `wait app --for ready`, this reads the pods rather than the aggregate app\n" +
					"state, so it does not pass while an old pod is still serving and the new one is\n" +
					"crash-looping. Exits non-zero on timeout.",
				Flags: []cli.Flag{
					&cli.DurationFlag{Name: flagTimeout, Value: defaultWaitTimeout, Usage: "give up after this long"},
				},
				Action: rolloutStatusAction,
			},
			newRolloutHistoryCommand(),
		},
	}
}

// rolloutWaitFlags are the flags of a command that can follow its own rollout.
func rolloutWaitFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: flagWait, Usage: "block until the new pods are ready and the old ones are gone"},
		&cli.DurationFlag{Name: flagTimeout, Value: defaultWaitTimeout, Usage: "give up waiting after this long"},
	}
}

func rolloutRestartAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}

	// Snapshot the pods first: once the restart is sent, the old ones start
	// going and there is no telling them apart afterwards.
	var watcher *podWatcher
	if cmd.Bool(flagWait) {
		if watcher, err = newPodWatcher(ctx, cmd, app); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "About to restart every pod of app %q (%s) in tenant %q.\n", app.Name, app.ID, c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.RestartApp(ctx, app.ID); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s restarted\n", app.Name)

	if watcher == nil {
		return nil
	}
	return watcher.wait(ctx, cmd.Duration(flagTimeout))
}

func rolloutStatusAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}
	w, err := newPodWatcher(ctx, cmd, app)
	if err != nil {
		return err
	}
	// Nothing to wait out: status only asks that what is there settles.
	w.old = nil
	return w.wait(ctx, cmd.Duration(flagTimeout))
}

// podWatcher follows one app's pods until a rollout settles.
type podWatcher struct {
	opts appstate.Options
	name string
	// old holds the pods present before the change; the rollout is not done
	// while any of them remains.
	old []string
}

// newPodWatcher records the app's current pods, which a rollout must replace.
func newPodWatcher(ctx context.Context, cmd *cli.Command, app *client.App) (*podWatcher, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return nil, err
	}
	access, err := accessToken(ctx, cmd, cfg)
	if err != nil {
		return nil, err
	}
	w := &podWatcher{
		opts: appstate.Options{
			BaseURL:     resolveBaseURL(cmd, cfg),
			AccessToken: access,
			Org:         resolveOrg(cmd, cfg),
			AppID:       app.ID,
		},
		name: app.Name,
	}
	pods, _, err := appstate.FetchPods(ctx, w.opts)
	if err != nil {
		return nil, err
	}
	for _, p := range pods {
		w.old = append(w.old, p.Name)
	}
	return w, nil
}

// wait polls the pods until rolloutDone holds, reporting each new situation.
func (w *podWatcher) wait(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var last string
	start := time.Now()
	for {
		pods, _, err := appstate.FetchPods(ctx, w.opts)
		// The pod stream drops now and then; a missed read is a reason to ask
		// again, and the timeout is what ends the loop.
		if err == nil {
			done, summary := rolloutDone(pods, w.old, time.Since(start) > noNewPodGrace)
			if done {
				fmt.Fprintf(os.Stdout, "app/%s rolled out: %s\n", w.name, summary)
				return nil
			}
			if summary != last {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", w.name, summary)
				last = summary
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: app/%s is %s", errRolloutTimeout, w.name, dash(last))
		case <-time.After(rolloutPollInterval):
		}
	}
}

// rolloutDone reports whether a rollout has settled: at least one pod, every
// pod ready, none terminating, and none left over from before the change. The
// summary describes the pods either way, for progress output. overdue says the
// rollout has run long enough that a missing replacement pod is suspicious.
func rolloutDone(pods []appstate.Pod, old []string, overdue bool) (bool, string) {
	if len(pods) == 0 {
		return false, "no pods yet"
	}
	var ready, stale, terminating, restarts int
	for _, p := range pods {
		isOld := slices.Contains(old, p.Name)
		if isOld {
			stale++
		} else {
			restarts += p.Restarts()
		}
		if p.Terminating {
			terminating++
		}
		r, n := p.ReadyCount()
		if p.Ready || (n > 0 && r == n) {
			ready++
		}
	}

	parts := []string{fmt.Sprintf("%d/%d pods ready", ready, len(pods))}
	if stale > 0 {
		parts = append(parts, fmt.Sprintf("%d old", stale))
	}
	if overdue && stale == len(pods) && len(old) > 0 {
		// Seen 2026-09-27 with a tag that does not exist: the replacement pod
		// never shows up on the pod stream at all, and the old one keeps
		// serving. An image that cannot be pulled is the usual reason.
		parts = append(parts, "no new pod has appeared — if this persists, check that the image exists")
	}
	if terminating > 0 {
		parts = append(parts, fmt.Sprintf("%d terminating", terminating))
	}
	if restarts > 0 {
		// A new pod that keeps restarting will never be ready; saying so is the
		// difference between waiting out the timeout and going to read the logs.
		parts = append(parts, fmt.Sprintf("%d restarts — check `darkubectl logs`", restarts))
	}
	summary := strings.Join(parts, ", ")
	return ready == len(pods) && stale == 0 && terminating == 0, summary
}
