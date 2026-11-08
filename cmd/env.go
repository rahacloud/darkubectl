package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// flagRemove names an environment variable or domain to drop.
const flagRemove = "remove"

// Env command errors.
var (
	errNoEnvChange          = errors.New("nothing to do: pass NAME=VALUE pairs, or --remove NAME")
	errBadEnvPair           = errors.New(`environment variables are set as NAME=VALUE`)
	errSecretEnvSet         = errors.New("that is a secret variable: pass --secret to change it")
	errPlainEnvSet          = errors.New("that is a plain variable: drop --secret to change it")
	errFromPlainNeedsSecret = errors.New("--from-plain moves variables into the vault: pass --secret with it")
	errNotPlain             = errors.New("not a plain variable on this app, so there is nothing to move")
)

func newGetEnvCommand() *cli.Command {
	return &cli.Command{
		Name:      "env",
		Aliases:   []string{"envs"},
		Usage:     "List an app's environment variables",
		ArgsUsage: argRefUsage,
		Description: "Plain variables are shown with their values. Secret variables are listed by\n" +
			"name only, unless --show-secrets: their values live in a vault, and the app\n" +
			"read returns them blank. --show-secrets fetches them from the vault the way\n" +
			"the console does, and prints them in the clear.\n\n" +
			"--live [NAME ...] compares what the platform stores (plain values, and secret\n" +
			"values from the vault) with /proc/1/environ in every running pod, by SHA-256\n" +
			"computed in the pod, and prints no values. It catches a pod still running an\n" +
			"old value after a Secret change, and a hand-patched Secret the next deploy\n" +
			"will revert. Exits non-zero on any mismatch.\n\n" +
			"  darkubectl get env my-api --live\n" +
			"  darkubectl get env my-api --live ConnectionStrings__DefaultConnection",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagShowSecrets, Usage: "read secret values from the vault and print them"},
			&cli.BoolFlag{Name: flagLive, Usage: "compare stored values with the running pods' environment (names after the app narrow it)"},
		},
		Action: getEnvAction,
	}
}

func getEnvAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}
	if cmd.Bool(flagLive) {
		return liveEnvAction(ctx, cmd, c, app, raw, cmd.Args().Tail())
	}

	envs := client.EnvVars(raw)
	secrets := client.SecretEnvNames(raw)
	report := envReport{Envs: envs, SecretEnvs: secrets}

	var values map[string]string
	if cmd.Bool(flagShowSecrets) {
		vault, err := c.SecretEnvs(ctx, app.ID)
		if err != nil {
			return err
		}
		values = make(map[string]string, len(vault))
		for _, e := range vault {
			values[e.Name] = e.Value
		}
		report.SecretValues = vault
	}

	if handled, err := output.Structured(os.Stdout, format, report); handled {
		return err
	}
	if format == output.Name {
		for _, e := range envs {
			fmt.Fprintln(os.Stdout, e.Name)
		}
		for _, s := range secrets {
			fmt.Fprintln(os.Stdout, s)
		}
		return nil
	}

	rows := make([][]string, 0, len(envs)+len(secrets))
	for _, e := range envs {
		rows = append(rows, []string{e.Name, "plain", e.Value})
	}
	for _, s := range secrets {
		value := "<hidden; --show-secrets>"
		if v, ok := values[s]; ok {
			value = v
		}
		rows = append(rows, []string{s, "secret", value})
	}
	if len(rows) == 0 {
		fmt.Fprintf(os.Stderr, "app %q has no environment variables\n", app.Name)
		return nil
	}
	return output.StyledTable(os.Stdout, []string{colName, "KIND", "VALUE"}, rows, nil)
}

// envReport is the -o json|yaml shape of `get env`.
type envReport struct {
	Envs       []client.EnvVar `json:"envs"       yaml:"envs"`
	SecretEnvs []string        `json:"secretEnvs" yaml:"secretEnvs"`
	// SecretValues is filled only with --show-secrets.
	SecretValues []client.EnvVar `json:"secretValues,omitempty" yaml:"secretValues,omitempty"`
}

func newSetEnvCommand() *cli.Command {
	return &cli.Command{
		Name:      "env",
		Aliases:   []string{"envs"},
		Usage:     "Set or remove an app's environment variables",
		ArgsUsage: argRefUsage + " NAME=VALUE [NAME=VALUE ...]",
		Description: "Applies a read-modify-write to the app: only the named variables change and\n" +
			"everything else is preserved. Without --secret, secret variables are untouched;\n" +
			"with it, the NAME=VALUE pairs and --remove names are secret variables instead.\n\n" +
			"  darkubectl set env my-api LOG_LEVEL=debug PORT=8080\n" +
			"  darkubectl set env my-api --remove LOG_LEVEL\n" +
			"  darkubectl set env my-api --secret DB_PASSWORD=s3cret\n" +
			"  darkubectl set env my-api --secret --from-plain DB_PASSWORD [DB_URL=new]\n\n" +
			"--from-plain moves plain variables into the vault in one write: a bare NAME\n" +
			"keeps its current value, NAME=VALUE replaces it, and the plain copy is dropped\n" +
			"in the same PUT, so the app never starts without the variable.\n\n" +
			"--env-file reads NAME=VALUE lines (blank lines and # comments skipped) and adds\n" +
			"them to the arguments, so a password never appears on a command line or in\n" +
			"shell history. It works with every mode above.\n\n" +
			"--wait follows the rollout and then reads /proc/1/environ in every new pod,\n" +
			"comparing each set variable by SHA-256 computed in the pod, so it proves the\n" +
			"running process has the value without printing it. Exits non-zero if not.\n\n" +
			"A secret change reads every current secret value from the vault and writes the\n" +
			"whole list back, the way the console does; values are never printed. It is\n" +
			"what makes a secret survive the next deploy, which editing the Kubernetes\n" +
			"Secret directly does not.\n\n" +
			"Like every write, a change restarts the app's pods, so a run that would change\n" +
			"nothing sends nothing; that keeps the command safe to repeat from a script.",
		Flags: append([]cli.Flag{
			&cli.StringSliceFlag{Name: flagRemove, Usage: "environment variable to remove (repeatable)"},
			&cli.BoolFlag{Name: flagSecret, Usage: "the variables are secret environment variables"},
			&cli.BoolFlag{Name: flagFromPlain, Usage: "with --secret: move these plain variables into the vault (a bare NAME keeps its value)"},
			&cli.StringFlag{Name: flagEnvFile, Usage: "read NAME=VALUE lines from this file as well, so values stay off the command line"},
		}, append(mutationFlags(), rolloutWaitFlags()...)...),
		Action: setEnvAction,
	}
}

func setEnvAction(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errMissingAppRef
	}
	ref := args[0]
	pairs, err := withEnvFile(cmd, args[1:])
	if err != nil {
		return err
	}
	removals := cmd.StringSlice(flagRemove)
	if len(pairs) == 0 && len(removals) == 0 {
		return errNoEnvChange
	}

	fromPlain := cmd.Bool(flagFromPlain)
	if fromPlain && !cmd.Bool(flagSecret) {
		return errFromPlainNeedsSecret
	}
	var set []client.EnvVar
	var bare []string
	if fromPlain {
		set, bare, err = parseMovePairs(pairs)
	} else {
		set, err = parseEnvPairs(pairs)
	}
	if err != nil {
		return err
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, ref)
	if err != nil {
		return err
	}

	check, err := newLiveEnvCheck(ctx, cmd, app)
	if err != nil {
		return err
	}
	if fromPlain {
		return moveEnvToSecret(ctx, cmd, c, app, set, bare, check)
	}
	if cmd.Bool(flagSecret) {
		return setSecretEnv(ctx, cmd, c, app, set, removals, check)
	}

	// The PUT path strips secret_envs before the change is applied, so the guard
	// in applyEnvChange never sees a secret name; check against a fresh read. Without
	// this, setting a plain variable that shadows a secret reached the platform
	// and failed there as a 500 from the Helm upgrade.
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}
	for _, name := range client.SecretEnvNames(raw) {
		if slices.ContainsFunc(set, func(e client.EnvVar) bool { return e.Name == name }) || slices.Contains(removals, name) {
			return fmt.Errorf("%w: %q", errSecretEnvSet, name)
		}
	}

	wrote, err := runAppChange(ctx, cmd, c, app, appChange{
		what:       "update the environment of",
		apply:      func(raw map[string]any) error { return applyEnvChange(raw, set, removals) },
		verifyDiff: true,
	})
	if err != nil || !wrote {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s environment updated\n", app.Name)
	return check(ctx, set)
}

// applyEnvChange merges the requested additions and removals into the app's
// existing plain environment, leaving order stable for untouched entries.
func applyEnvChange(raw map[string]any, set []client.EnvVar, removals []string) error {
	if secrets := client.SecretEnvNames(raw); len(secrets) > 0 {
		for _, e := range set {
			if slices.Contains(secrets, e.Name) {
				return fmt.Errorf("%w: %q is a secret variable", errSecretEnvSet, e.Name)
			}
		}
	}

	envs := client.EnvVars(raw)
	for _, want := range set {
		replaced := false
		for i := range envs {
			if envs[i].Name == want.Name {
				envs[i].Value = want.Value
				replaced = true
				break
			}
		}
		if !replaced {
			envs = append(envs, want)
		}
	}

	for _, name := range removals {
		idx := slices.IndexFunc(envs, func(e client.EnvVar) bool { return e.Name == name })
		if idx < 0 {
			return fmt.Errorf("%w: %q", client.ErrNoSuchEnv, name)
		}
		envs = slices.Delete(envs, idx, idx+1)
	}

	client.SetEnvVars(raw, envs)
	return nil
}

// parseEnvPairs turns NAME=VALUE arguments into env vars. A value may contain
// "=", so only the first one separates.
func parseEnvPairs(pairs []string) ([]client.EnvVar, error) {
	out := make([]client.EnvVar, 0, len(pairs))
	for _, p := range pairs {
		name, value, found := strings.Cut(p, "=")
		if !found || name == "" {
			return nil, fmt.Errorf("%w, got %q", errBadEnvPair, p)
		}
		out = append(out, client.EnvVar{Name: name, Value: value})
	}
	return out, nil
}

// flagSecret makes set env act on secret variables; flagShowSecrets makes get
// env print their values.
const (
	flagSecret      = "secret"
	flagShowSecrets = "show-secrets"
	flagFromPlain   = "from-plain"
	flagLive        = "live"
	flagEnvFile     = "env-file"
)

// withEnvFile appends the lines of --env-file, if given, to the arguments.
func withEnvFile(cmd *cli.Command, pairs []string) ([]string, error) {
	f := cmd.String(flagEnvFile)
	if f == "" {
		return pairs, nil
	}
	more, err := readEnvFile(f)
	if err != nil {
		return nil, err
	}
	return append(pairs, more...), nil
}

// readEnvFile returns the NAME=VALUE lines of a file, skipping blank lines and
// lines starting with #. Values are taken verbatim after the first "=": no
// quoting or expansion, so a password with $ or quotes survives as written.
func readEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own argument
	if err != nil {
		return nil, fmt.Errorf("--%s: %w", flagEnvFile, err)
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

// parseMovePairs is parseEnvPairs for --from-plain, where a bare NAME is allowed
// and means "keep the value the plain variable has now".
func parseMovePairs(pairs []string) ([]client.EnvVar, []string, error) {
	var set []client.EnvVar
	var bare []string
	for _, p := range pairs {
		if name, value, found := strings.Cut(p, "="); found {
			if name == "" {
				return nil, nil, fmt.Errorf("%w, got %q", errBadEnvPair, p)
			}
			set = append(set, client.EnvVar{Name: name, Value: value})
		} else if p != "" {
			bare = append(bare, p)
		} else {
			return nil, nil, fmt.Errorf("%w, got %q", errBadEnvPair, p)
		}
	}
	return set, bare, nil
}

// moveEnvToSecret takes plain variables off the app and puts them in the vault
// in a single PUT, then reads both back: the vault must hold the new list and
// the plain environment must no longer carry any of the moved names.
func moveEnvToSecret(
	ctx context.Context, cmd *cli.Command, c *client.Client, app *client.App, set []client.EnvVar, bare []string,
	check liveEnvCheck,
) error {
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}
	plain := client.EnvVars(raw)
	value := func(name string) (string, bool) {
		for _, e := range plain {
			if e.Name == name {
				return e.Value, true
			}
		}
		return "", false
	}
	moved := make([]string, 0, len(set)+len(bare))
	for _, name := range bare {
		v, ok := value(name)
		if !ok {
			return fmt.Errorf("%w: %q", errNotPlain, name)
		}
		set = append(set, client.EnvVar{Name: name, Value: v})
	}
	for _, e := range set {
		if _, ok := value(e.Name); !ok {
			return fmt.Errorf("%w: %q", errNotPlain, e.Name)
		}
		moved = append(moved, e.Name)
	}

	current, err := c.SecretEnvs(ctx, app.ID)
	if err != nil {
		return err
	}
	merged, err := mergeEnvs(current, set, nil)
	if err != nil {
		return err
	}
	if cmd.Bool(flagDryRun) {
		fmt.Fprintf(os.Stdout, "app/%s: dry run, nothing was sent\n", app.Name)
		fmt.Fprintf(os.Stdout, "  plain -> secret: %s\n", strings.Join(moved, ", "))
		fmt.Fprintf(os.Stdout, "  secret environment: %s\n", describeSecretChange(current, merged))
		return nil
	}

	fmt.Fprintf(os.Stderr, "About to move %s from plain to SECRET environment on app %q (%s) in tenant %q: %s\n",
		strings.Join(moved, ", "), app.Name, app.ID, c.Org, describeSecretChange(current, merged))
	fmt.Fprintf(os.Stderr, "note: like every write, this restarts the app's pods.\n")
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	dropPlain := func(app map[string]any) error {
		envs := client.EnvVars(app)
		envs = slices.DeleteFunc(envs, func(e client.EnvVar) bool { return slices.Contains(moved, e.Name) })
		client.SetEnvVars(app, envs)
		return nil
	}
	want, err := c.UpdateSecretEnvs(ctx, app.ID, func(secrets []client.EnvVar) ([]client.EnvVar, error) {
		return mergeEnvs(secrets, set, nil)
	}, dropPlain)
	if err != nil {
		return err
	}

	ok, err := retryReadBack(ctx, func(ctx context.Context) (bool, error) {
		got, err := c.SecretEnvs(ctx, app.ID)
		if err != nil || !sameEnvs(got, want) {
			return false, err
		}
		raw, err := c.GetApp(ctx, app.ID)
		if err != nil {
			return false, err
		}
		return !slices.ContainsFunc(client.EnvVars(raw), func(e client.EnvVar) bool { return slices.Contains(moved, e.Name) }), nil
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: the vault or the plain environment does not show the move as written", errChangeIgnored)
	}
	fmt.Fprintf(os.Stdout, "app/%s: %s moved to the secret environment\n", app.Name, strings.Join(moved, ", "))
	return check(ctx, set)
}

// setSecretEnv changes secret variables: every current value is read from the
// vault, the change is applied to the full list, and the full list is written
// back, then read back to prove it took. Names are shown, values never are.
func setSecretEnv(
	ctx context.Context, cmd *cli.Command, c *client.Client, app *client.App, set []client.EnvVar, removals []string,
	check liveEnvCheck,
) error {
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return err
	}
	for _, e := range client.EnvVars(raw) {
		if slices.ContainsFunc(set, func(s client.EnvVar) bool { return s.Name == e.Name }) || slices.Contains(removals, e.Name) {
			return fmt.Errorf("%w: %q", errPlainEnvSet, e.Name)
		}
	}

	current, err := c.SecretEnvs(ctx, app.ID)
	if err != nil {
		return err
	}
	merged, err := mergeEnvs(current, set, removals)
	if err != nil {
		return err
	}
	if cmd.Bool(flagDryRun) {
		fmt.Fprintf(os.Stdout, "app/%s: dry run, nothing was sent\n", app.Name)
		fmt.Fprintf(os.Stdout, "  secret environment: %s\n", describeSecretChange(current, merged))
		return nil
	}
	if sameEnvs(current, merged) {
		fmt.Fprintf(os.Stdout, "app/%s unchanged: it already matches\n", app.Name)
		return nil
	}

	fmt.Fprintf(os.Stderr, "About to update SECRET environment on app %q (%s) in tenant %q: %s\n",
		app.Name, app.ID, c.Org, describeSecretChange(current, merged))
	fmt.Fprintf(os.Stderr, "note: the app is restarted afterwards so its pods pick the change up.\n")
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	want, err := c.UpdateSecretEnvs(ctx, app.ID, func(secrets []client.EnvVar) ([]client.EnvVar, error) {
		return mergeEnvs(secrets, set, removals)
	})
	if err != nil {
		return err
	}

	// A 202 from this API does not prove the write was stored; read the vault back.
	ok, err := retryReadBack(ctx, func(ctx context.Context) (bool, error) {
		got, err := c.SecretEnvs(ctx, app.ID)
		return err == nil && sameEnvs(got, want), err
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: the vault still does not hold the secret variables as written", errChangeIgnored)
	}
	fmt.Fprintf(os.Stdout, "app/%s secret environment updated\n", app.Name)

	// A PUT that changes only secret_envs re-renders the app's Secret but does
	// not roll its pods (found 2026-11-07: the vault took a new connection
	// string, the Secret followed, and the running pod kept the old value with
	// no rollout at all). A value read through secretKeyRef only reaches the
	// process at start, so restart the app the way the console's restart button
	// does; without this the change silently waits for the next deploy.
	if err := c.RestartApp(ctx, app.ID); err != nil {
		return fmt.Errorf("secret stored, but restarting the app to apply it failed: %w", err)
	}
	fmt.Fprintf(os.Stdout, "app/%s restarted so its pods pick the change up\n", app.Name)
	return check(ctx, set)
}

// describeSecretChange names the secret variables a change adds, alters and
// removes, without their values.
func describeSecretChange(before, after []client.EnvVar) string {
	old := make(map[string]string, len(before))
	for _, e := range before {
		old[e.Name] = e.Value
	}
	var added, changed []string
	for _, e := range after {
		v, ok := old[e.Name]
		switch {
		case !ok:
			added = append(added, e.Name)
		case v != e.Value:
			changed = append(changed, e.Name)
		}
		delete(old, e.Name)
	}
	removed := make([]string, 0, len(old))
	for name := range old {
		removed = append(removed, name)
	}
	sort.Strings(removed)
	var parts []string
	for _, p := range []struct {
		verb  string
		names []string
	}{{"add", added}, {"change", changed}, {"remove", removed}} {
		if len(p.names) > 0 {
			parts = append(parts, p.verb+" "+strings.Join(p.names, ", "))
		}
	}
	if len(parts) == 0 {
		return "no change"
	}
	return strings.Join(parts, "; ")
}

// mergeEnvs applies additions and removals to a variable list, keeping the order
// of untouched entries.
func mergeEnvs(envs, set []client.EnvVar, removals []string) ([]client.EnvVar, error) {
	out := slices.Clone(envs)
	for _, want := range set {
		if i := slices.IndexFunc(out, func(e client.EnvVar) bool { return e.Name == want.Name }); i >= 0 {
			out[i].Value = want.Value
		} else {
			out = append(out, want)
		}
	}
	for _, name := range removals {
		i := slices.IndexFunc(out, func(e client.EnvVar) bool { return e.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("%w: %q", client.ErrNoSuchEnv, name)
		}
		out = slices.Delete(out, i, i+1)
	}
	return out, nil
}

// sameEnvs reports whether two variable lists hold the same names and values,
// in any order.
func sameEnvs(a, b []client.EnvVar) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]string, len(a))
	for _, e := range a {
		m[e.Name] = e.Value
	}
	for _, e := range b {
		if v, ok := m[e.Name]; !ok || v != e.Value {
			return false
		}
	}
	return true
}
