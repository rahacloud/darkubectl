package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const (
	argBuildUsage = argRefUsage + " BUILD"

	defaultBuildLimit = 10
	buildPollInterval = 3 * time.Second
	// newBuildGrace bounds how long `build start` waits for the build it asked
	// for to show up in the list before giving up on following it.
	newBuildGrace = time.Minute

	maxCommitMessage = 60
)

var (
	errMissingBuildID = errors.New("missing build id: pass it after the app, as listed by `get builds`")
	errNotGitApp      = errors.New("app is not built from git")
	errNoBuilds       = errors.New("app has no builds")
	errBuildFailed    = errors.New("build did not succeed")
	errBuildNotSeen   = errors.New("the platform accepted the build but it has not appeared in the build list")
)

func newBuildCommand() *cli.Command {
	followFlag := &cli.BoolFlag{Name: flagFollow, Aliases: []string{"f"}, Usage: "stream the build log until the build finishes"}
	yesFlag := &cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}
	return &cli.Command{
		Name:    "build",
		Aliases: []string{"builds"},
		Usage:   "List, follow, start and stop the image builds of a git-backed app",
		Description: "Darkube builds a git-backed app's image itself, pushes it to\n" +
			"registry.hamdocker.ir/<tenant>/<app> and deploys it. These commands are the\n" +
			"console's build tab.",
		Commands: []*cli.Command{
			newBuildListCommand("list", []string{"ls"}),
			{
				Name:      "logs",
				Aliases:   []string{"log"},
				Usage:     "Print a build's log (the latest build by default)",
				ArgsUsage: argRefUsage + " [BUILD]",
				Description: "  darkubectl build logs my-api\n" +
					"  darkubectl build logs my-api 1234 -f\n\n" +
					"Exits non-zero when --follow ends on a failed or canceled build, so it can gate\n" +
					"a script.",
				Flags:  []cli.Flag{followFlag},
				Action: buildLogsAction,
			},
			{
				Name:      "start",
				Usage:     "Build the head of the app's branch and deploy it",
				ArgsUsage: argRefUsage,
				Description: "  darkubectl build start my-api -f\n\n" +
					"The console's \"build and deploy the last commit\" button: Darkube fetches the\n" +
					"branch, builds the image and rolls the app onto it once the build succeeds.",
				Flags:  []cli.Flag{followFlag, yesFlag},
				Action: buildStartAction,
			},
			{
				Name:      "retry",
				Usage:     "Rebuild the commit of an earlier build",
				ArgsUsage: argBuildUsage,
				Flags:     []cli.Flag{followFlag, yesFlag},
				Action:    buildRetryAction,
			},
			{
				Name:      "stop",
				Aliases:   []string{"cancel"},
				Usage:     "Cancel a running build",
				ArgsUsage: argBuildUsage,
				Flags:     []cli.Flag{yesFlag},
				Action:    buildStopAction,
			},
		},
	}
}

// newBuildListCommand is shared by `build list` and `get builds`.
func newBuildListCommand(name string, aliases []string) *cli.Command {
	return &cli.Command{
		Name:      name,
		Aliases:   aliases,
		Usage:     "List a git-backed app's recent builds, newest first",
		ArgsUsage: argRefUsage,
		Flags: []cli.Flag{
			&cli.IntFlag{Name: flagLimit, Value: defaultBuildLimit, Usage: "how many builds to list"},
		},
		Action: buildListAction,
	}
}

func buildListAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	builds, err := c.ListBuilds(ctx, app.ID, cmd.Int(flagLimit))
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, builds); handled {
		return err
	}
	if len(builds) == 0 {
		fmt.Fprintf(os.Stderr, "No builds for app %q.%s\n", app.Name, notGitHint(app))
		return nil
	}
	if format == output.Name {
		for _, b := range builds {
			fmt.Fprintln(os.Stdout, b.ID)
		}
		return nil
	}
	rows := make([][]string, 0, len(builds))
	for _, b := range builds {
		rows = append(rows, []string{
			string(b.ID), dash(b.Status), dash(b.GitBranch), dash(b.ShortCommitHash),
			ageOf(b.CreationTime), buildDuration(b), dash(firstLine(b.CommitMessage)),
		})
	}
	return output.StyledTable(os.Stdout,
		[]string{"ID", colStatus, "BRANCH", "COMMIT", colAge, "DURATION", "MESSAGE"}, rows, output.StateCells(1))
}

func buildLogsAction(ctx context.Context, cmd *cli.Command) error {
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	id := client.BuildID(cmd.Args().Get(1))
	if id == "" {
		latest, err := c.ListBuilds(ctx, app.ID, 1)
		if err != nil {
			return err
		}
		if len(latest) == 0 {
			return fmt.Errorf("%w: %s.%s", errNoBuilds, app.Name, notGitHint(app))
		}
		id = latest[0].ID
	}
	if cmd.Bool(flagFollow) {
		return followBuild(ctx, c, id)
	}
	b, err := c.GetBuild(ctx, id)
	if err != nil {
		return err
	}
	writeBuildLog(os.Stdout, b.Log)
	reportBuildErrors(b)
	return nil
}

func buildStartAction(ctx context.Context, cmd *cli.Command) error {
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	if err := requireGitApp(app); err != nil {
		return err
	}
	// The trigger answers without naming the build it started, so remember the
	// newest one and look for its successor afterwards.
	before, err := c.ListBuilds(ctx, app.ID, 1)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to build the head of app %q's branch and deploy it, in tenant %q.\n", app.Name, c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.BuildLatestCommit(ctx, app.ID); err != nil {
		return err
	}

	b, err := awaitNewBuild(ctx, c, app.ID, before)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "build/%s started for app/%s (%s %s)\n", b.ID, app.Name, dash(b.GitBranch), dash(b.ShortCommitHash))
	if !cmd.Bool(flagFollow) {
		return nil
	}
	return followBuild(ctx, c, b.ID)
}

func buildRetryAction(ctx context.Context, cmd *cli.Command) error {
	c, app, id, err := resolveBuildArgs(ctx, cmd)
	if err != nil {
		return err
	}
	if err := requireGitApp(app); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "About to rebuild build %s of app %q in tenant %q.\n", id, app.Name, c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	b, err := c.RetryBuild(ctx, id)
	if err != nil {
		return err
	}
	if b.ID == "" {
		fmt.Fprintf(os.Stdout, "build/%s retried\n", id)
		return nil
	}
	fmt.Fprintf(os.Stdout, "build/%s started for app/%s\n", b.ID, app.Name)
	if !cmd.Bool(flagFollow) {
		return nil
	}
	return followBuild(ctx, c, b.ID)
}

func buildStopAction(ctx context.Context, cmd *cli.Command) error {
	c, app, id, err := resolveBuildArgs(ctx, cmd)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "About to cancel build %s of app %q in tenant %q.\n", id, app.Name, c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.StopBuild(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "build/%s stopped\n", id)
	return nil
}

// resolveAppArg builds a client and resolves the command's first argument to
// an app.
func resolveAppArg(ctx context.Context, cmd *cli.Command) (*client.Client, *client.App, error) {
	ref := cmd.Args().First()
	if ref == "" {
		return nil, nil, errMissingAppRef
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return nil, nil, err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	return c, app, nil
}

// resolveBuildArgs resolves `APP BUILD`.
func resolveBuildArgs(ctx context.Context, cmd *cli.Command) (*client.Client, *client.App, client.BuildID, error) {
	id := client.BuildID(cmd.Args().Get(1))
	if cmd.Args().First() != "" && id == "" {
		return nil, nil, "", errMissingBuildID
	}
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return nil, nil, "", err
	}
	return c, app, id, nil
}

func requireGitApp(app *client.App) error {
	if app.CreationMethod != client.CreationMethodGitRepoURL {
		return fmt.Errorf("%w: %s was created as %q, so Darkube does not build its image", errNotGitApp, app.Name, dash(app.CreationMethod))
	}
	return nil
}

// notGitHint explains an empty build list when the app is not git-backed.
func notGitHint(app *client.App) string {
	if app.CreationMethod == client.CreationMethodGitRepoURL {
		return ""
	}
	return fmt.Sprintf(" It was created as %q; only git-backed apps are built by Darkube.", dash(app.CreationMethod))
}

// awaitNewBuild polls the build list until a build newer than before appears.
func awaitNewBuild(ctx context.Context, c *client.Client, appID string, before []client.Build) (*client.Build, error) {
	var prev client.BuildID
	if len(before) > 0 {
		prev = before[0].ID
	}
	deadline := time.Now().Add(newBuildGrace)
	for {
		latest, err := c.ListBuilds(ctx, appID, 1)
		if err != nil && !client.IsTransient(err) {
			return nil, err
		}
		if err == nil && len(latest) > 0 && latest[0].ID != prev {
			return &latest[0], nil
		}
		if time.Now().After(deadline) {
			return nil, errBuildNotSeen
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(buildPollInterval):
		}
	}
}

// followBuild prints a build's log as it grows, until the build finishes. The
// API has no stream, so it re-reads the build and prints what is new.
func followBuild(ctx context.Context, c *client.Client, id client.BuildID) error {
	printed := 0
	for {
		b, err := c.GetBuild(ctx, id)
		switch {
		case err == nil:
			if len(b.Log) < printed {
				// The log was replaced rather than appended to; start over.
				printed = 0
			}
			fmt.Fprint(os.Stdout, b.Log[printed:])
			printed = len(b.Log)
			if !b.InProgress() {
				if printed > 0 && !strings.HasSuffix(b.Log, "\n") {
					fmt.Fprintln(os.Stdout)
				}
				fmt.Fprintf(os.Stderr, "build/%s %s after %s\n", id, dash(b.Status), buildDuration(*b))
				reportBuildErrors(b)
				if b.Status != client.BuildSucceeded {
					return fmt.Errorf("%w: build/%s is %s", errBuildFailed, id, dash(b.Status))
				}
				return nil
			}
		case !client.IsTransient(err):
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(buildPollInterval):
		}
	}
}

func writeBuildLog(w io.Writer, log string) {
	if log == "" {
		fmt.Fprintln(os.Stderr, "(the build has no log yet)")
		return
	}
	fmt.Fprint(w, log)
	if !strings.HasSuffix(log, "\n") {
		fmt.Fprintln(w)
	}
}

// reportBuildErrors prints the platform's explanation of a failed build.
func reportBuildErrors(b *client.Build) {
	if msg := strings.TrimSpace(renderValue(b.Errors)); msg != "" && msg != "null" && msg != `""` && msg != "[]" && msg != "{}" {
		fmt.Fprintf(os.Stderr, "build errors: %s\n", msg)
	}
}

// buildDuration is how long a build ran, or has been running.
func buildDuration(b client.Build) string {
	start, err := time.Parse(time.RFC3339, b.CreationTime)
	if err != nil {
		return "-"
	}
	end := time.Now()
	if b.EndTime != "" {
		if end, err = time.Parse(time.RFC3339, b.EndTime); err != nil {
			return "-"
		}
	}
	if end.Before(start) {
		return "-"
	}
	return end.Sub(start).Round(time.Second).String()
}

// firstLine is a commit message's subject, cut to fit a table.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(s); len(r) > maxCommitMessage {
		return string(r[:maxCommitMessage-1]) + "…"
	}
	return s
}
