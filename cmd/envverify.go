package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/wsexec"
)

// An environment change is not done when the API says 202, nor when the pods
// roll: a value consumed at container start is only what the running process
// sees if it is in /proc/1/environ. That is the check this file automates; it
// was first done by hand through `exec` on 2026-11-07, one app at a time, while
// moving plain variables into the vault.
//
// Values are compared by SHA-256 computed inside the pod, so nothing secret
// crosses the terminal or reaches the output.

var errEnvNotLive = errors.New("the running process does not have the environment as written")

// envProbeLine matches the probe's report for one variable. The probe prints it
// through printf arguments, so the shell's echo of the command, which contains
// the format string and not the expanded text, can never match.
var envProbeLine = regexp.MustCompile(`DKENV ([A-Za-z_][A-Za-z0-9_]*) ([0-9a-f]{64}|MISSING|NOHASH)`)

// envProbeCommand builds a POSIX sh command that reports, for each name, the
// SHA-256 of its value in PID 1's environment, MISSING when absent, or NOHASH
// when the image has no sha256sum. The hash covers the value followed by one
// newline, which is what `cut` emits; expectedEnvHash matches that.
func envProbeCommand(names []string) string {
	var b strings.Builder
	b.WriteString("for n in")
	for _, n := range names {
		b.WriteString(" " + shellQuote(n))
	}
	b.WriteString(`; do ` +
		`l=$(tr '\0' '\n' </proc/1/environ | grep -m1 "^$n="); ` +
		`if [ -z "$l" ]; then h=MISSING; ` +
		`elif command -v sha256sum >/dev/null 2>&1; then h=$(printf '%s\n' "$l" | cut -d= -f2- | sha256sum | cut -c1-64); ` +
		`else h=NOHASH; fi; ` +
		`printf 'DK%s %s %s\n' ENV "$n" "$h"; done`)
	return b.String()
}

// expectedEnvHash is the hash the probe reports for a value.
func expectedEnvHash(value string) string {
	sum := sha256.Sum256([]byte(value + "\n"))
	return hex.EncodeToString(sum[:])
}

// parseEnvProbe reads the probe's output into name -> hash/MISSING/NOHASH.
func parseEnvProbe(out string) map[string]string {
	got := map[string]string{}
	for _, m := range envProbeLine.FindAllStringSubmatch(out, -1) {
		got[m[1]] = m[2]
	}
	return got
}

// compareEnvProbe checks a probe result against the values that were written.
// It returns one line per variable and whether every one matched.
func compareEnvProbe(want []client.EnvVar, got map[string]string) ([]string, bool) {
	lines := make([]string, 0, len(want))
	ok := true
	for _, e := range want {
		h, seen := got[e.Name]
		switch {
		case !seen:
			lines = append(lines, e.Name+": no report from the pod")
			ok = false
		case h == "MISSING":
			lines = append(lines, e.Name+": MISSING from the running process")
			ok = false
		case h == "NOHASH":
			lines = append(lines, e.Name+": present (image has no sha256sum, value not compared)")
		case h == expectedEnvHash(e.Value):
			lines = append(lines, e.Name+": present, value matches")
		default:
			lines = append(lines, e.Name+": present but holds a DIFFERENT value")
			ok = false
		}
	}
	return lines, ok
}

// liveEnvCheck is what set env --wait does after a write: wait out the rollout,
// then verify the new pods. Without --wait it does nothing.
type liveEnvCheck func(ctx context.Context, want []client.EnvVar) error

// newLiveEnvCheck snapshots the app's pods before the write, so the rollout
// wait can tell the replacements from the pods they replace.
func newLiveEnvCheck(ctx context.Context, cmd *cli.Command, app *client.App) (liveEnvCheck, error) {
	if !cmd.Bool(flagWait) || cmd.Bool(flagDryRun) {
		return func(context.Context, []client.EnvVar) error { return nil }, nil
	}
	w, err := newPodWatcher(ctx, cmd, app)
	if err != nil {
		return nil, err
	}
	timeout := cmd.Duration(flagTimeout)
	return func(ctx context.Context, want []client.EnvVar) error {
		if err := w.wait(ctx, timeout); err != nil {
			return err
		}
		return verifyLiveEnv(ctx, w, want)
	}, nil
}

// verifyLiveEnv runs the probe in every pod of the app and compares the result
// with want. Call it after the rollout has settled, so every pod is new.
func verifyLiveEnv(ctx context.Context, w *podWatcher, want []client.EnvVar) error {
	if len(want) == 0 {
		return nil
	}
	names := make([]string, len(want))
	for i, e := range want {
		names[i] = e.Name
	}
	pods, _, err := appstate.FetchPods(ctx, w.opts)
	if err != nil {
		return fmt.Errorf("list pods to verify: %w", err)
	}
	if len(pods) == 0 {
		return fmt.Errorf("%w: app/%s has no pods", errEnvNotLive, w.name)
	}
	allOK := true
	for _, p := range pods {
		out, err := probePod(ctx, w.opts, p, names)
		if err != nil {
			return fmt.Errorf("probe pod %s: %w", p.Name, err)
		}
		lines, ok := compareEnvProbe(want, parseEnvProbe(out))
		for _, l := range lines {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", p.Name, l)
		}
		allOK = allOK && ok
	}
	if !allOK {
		return fmt.Errorf("%w: app/%s", errEnvNotLive, w.name)
	}
	fmt.Fprintf(os.Stdout, "app/%s: the running process has every variable as written\n", w.name)
	return nil
}

// probePod opens an exec session on one pod and returns the probe's output.
func probePod(ctx context.Context, opts appstate.Options, p appstate.Pod, names []string) (string, error) {
	container := defaultContainer
	if cn := p.ContainerNames(); len(cn) > 0 && !slices.Contains(cn, defaultContainer) {
		container = cn[0]
	}
	sess, err := wsexec.Dial(ctx, wsexec.Options{
		BaseURL:     opts.BaseURL,
		AccessToken: opts.AccessToken,
		Org:         opts.Org,
		AppID:       opts.AppID,
		PodName:     p.Name,
		Container:   container,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = sess.Close() }()
	var buf strings.Builder
	code, err := runRemoteTo(ctx, sess, envProbeCommand(names), func(b []byte) { buf.Write(b) })
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("probe exited %d", code)
	}
	return buf.String(), nil
}

// liveEnvAction is get env --live: every stored variable, or the named ones,
// against every running pod.
func liveEnvAction(
	ctx context.Context, cmd *cli.Command, c *client.Client, app *client.App, raw map[string]any, only []string,
) error {
	want := client.EnvVars(raw)
	if len(client.SecretEnvNames(raw)) > 0 {
		vault, err := c.SecretEnvs(ctx, app.ID)
		if err != nil {
			return err
		}
		want = append(want, vault...)
	}
	if len(only) > 0 {
		var picked []client.EnvVar
		for _, n := range only {
			i := slices.IndexFunc(want, func(e client.EnvVar) bool { return e.Name == n })
			if i < 0 {
				return fmt.Errorf("%w: %q", client.ErrNoSuchEnv, n)
			}
			picked = append(picked, want[i])
		}
		want = picked
	}
	if len(want) == 0 {
		fmt.Fprintf(os.Stderr, "app %q has no environment variables\n", app.Name)
		return nil
	}
	w, err := newPodWatcher(ctx, cmd, app)
	if err != nil {
		return err
	}
	return verifyLiveEnv(ctx, w, want)
}
