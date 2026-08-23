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
	errNoEnvChange  = errors.New("nothing to do: pass NAME=VALUE pairs, or --remove NAME")
	errBadEnvPair   = errors.New(`environment variables are set as NAME=VALUE`)
	errSecretEnvSet = errors.New("secret environment variables cannot be set through the API: their values are " +
		"vault-backed and write-only, so only the console can change them")
)

func newGetEnvCommand() *cli.Command {
	return &cli.Command{
		Name:      "env",
		Aliases:   []string{"envs"},
		Usage:     "List an app's environment variables",
		ArgsUsage: argRefUsage,
		Description: "Plain variables are shown with their values. Secret variables are listed by\n" +
			"name only with an empty value: the API stores them in a vault and never\n" +
			"returns them, so there is nothing to reveal and no --show-secrets flag.",
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

	if handled, err := output.Structured(os.Stdout, format, envReport{Envs: envs, SecretEnvs: secrets}); handled {
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
		rows = append(rows, []string{s, "secret", "<vault-backed, not readable>"})
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
}

func newSetEnvCommand() *cli.Command {
	return &cli.Command{
		Name:      "env",
		Aliases:   []string{"envs"},
		Usage:     "Set or remove an app's environment variables",
		ArgsUsage: argRefUsage + " NAME=VALUE [NAME=VALUE ...]",
		Description: "Applies a read-modify-write to the app: only the named variables change and\n" +
			"everything else is preserved. Secret variables are untouched.\n\n" +
			"  darkubectl set env my-api LOG_LEVEL=debug PORT=8080\n" +
			"  darkubectl set env my-api --remove LOG_LEVEL",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: flagRemove, Usage: "environment variable to remove (repeatable)"},
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
