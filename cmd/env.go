package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// flagRemove names an environment variable or domain to drop.
const flagRemove = "remove"

// Env command errors.
var (
	errNoEnvChange  = errors.New("nothing to do: pass NAME=VALUE pairs, or --remove NAME")
	errBadEnvPair   = errors.New(`environment variables are set as NAME=VALUE`)
	errSecretEnvSet = errors.New("that is a secret variable: pass --secret to change it")
	errPlainEnvSet  = errors.New("that is a plain variable: drop --secret to change it")
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
			"the console does, and prints them in the clear.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagShowSecrets, Usage: "read secret values from the vault and print them"},
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
			"  darkubectl set env my-api --secret DB_PASSWORD=s3cret\n\n" +
			"A secret change reads every current secret value from the vault and writes the\n" +
			"whole list back, the way the console does; values are never printed. It is\n" +
			"what makes a secret survive the next deploy, which editing the Kubernetes\n" +
			"Secret directly does not.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: flagRemove, Usage: "environment variable to remove (repeatable)"},
			&cli.BoolFlag{Name: flagSecret, Usage: "the variables are secret environment variables"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: setEnvAction,
	}
}

func setEnvAction(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errMissingAppRef
	}
	ref, pairs := args[0], args[1:]
	removals := cmd.StringSlice(flagRemove)
	if len(pairs) == 0 && len(removals) == 0 {
		return errNoEnvChange
	}

	set, err := parseEnvPairs(pairs)
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

	if cmd.Bool(flagSecret) {
		return setSecretEnv(ctx, cmd, c, app, set, removals)
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

	fmt.Fprintf(os.Stderr, "About to update environment on app %q (%s) in tenant %q: %s\n",
		app.Name, app.ID, c.Org, describeEnvChange(set, removals))
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	_, err = c.UpdateApp(ctx, app.ID, func(raw map[string]any) error {
		return applyEnvChange(raw, set, removals)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s environment updated\n", app.Name)
	return nil
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

func describeEnvChange(set []client.EnvVar, removals []string) string {
	var parts []string
	if len(set) > 0 {
		names := make([]string, 0, len(set))
		for _, e := range set {
			names = append(names, e.Name)
		}
		sort.Strings(names)
		parts = append(parts, "set "+strings.Join(names, ", "))
	}
	if len(removals) > 0 {
		parts = append(parts, "remove "+strings.Join(removals, ", "))
	}
	return strings.Join(parts, "; ")
}

// flagSecret makes set env act on secret variables; flagShowSecrets makes get
// env print their values.
const (
	flagSecret      = "secret"
	flagShowSecrets = "show-secrets"
)

// setSecretEnv changes secret variables: every current value is read from the
// vault, the change is applied to the full list, and the full list is written
// back, then read back to prove it took. Names are shown, values never are.
func setSecretEnv(
	ctx context.Context, cmd *cli.Command, c *client.Client, app *client.App, set []client.EnvVar, removals []string,
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

	fmt.Fprintf(os.Stderr, "About to update SECRET environment on app %q (%s) in tenant %q: %s\n",
		app.Name, app.ID, c.Org, describeEnvChange(set, removals))
	fmt.Fprintf(os.Stderr, "note: like every write, this restarts the app's pods.\n")
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
	for attempt := range verifyAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(verifyInterval):
			}
		}
		got, err := c.SecretEnvs(ctx, app.ID)
		if err != nil {
			if client.IsTransient(err) {
				continue
			}
			return err
		}
		if sameEnvs(got, want) {
			fmt.Fprintf(os.Stdout, "app/%s secret environment updated\n", app.Name)
			return nil
		}
	}
	return fmt.Errorf("%w: the vault still does not hold the secret variables as written", errChangeIgnored)
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
