package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// Probes are two top-level string fields, readiness_probe_path and
// liveness_probe_path: an HTTP GET on that path against the app's port. They
// reach the pod — confirmed 2026-09-27 on a throwaway nginx, where a readiness
// path answering 404 left the new pod at 0/1 indefinitely.
//
// The same experiment, watched on the pod stream, showed the rollout is a
// normal surge: the old pod kept serving the whole time the new one sat
// unready. So a wrong readiness path stalls the rollout rather than taking the
// app down — until the next restart, when there is no ready pod to fall back
// on. A wrong liveness path crash-loops the new pod.
//
// probes_http_headers is advertised as writable and is not: every shape tried
// ([{name,value}], [{key,value}], a plain object), alone or alongside a path,
// read back as []. So this command offers no --header.

const (
	flagReadiness = "readiness"
	flagLiveness  = "liveness"

	keyReadinessPath = "readiness_probe_path"
	keyLivenessPath  = "liveness_probe_path"
)

var (
	errProbeArgs     = errors.New("give --readiness and/or --liveness (an empty value removes that probe), or --clear")
	errProbeNotAPath = errors.New("a probe path must start with /")
)

func newSetProbeCommand() *cli.Command {
	return &cli.Command{
		Name:      "probe",
		Aliases:   []string{"probes"},
		Usage:     "Set the HTTP readiness and liveness probe paths of an app",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl set probe my-api --readiness /healthz --liveness /livez\n" +
			"  darkubectl set probe my-api --liveness \"\"      # remove the liveness probe\n" +
			"  darkubectl set probe my-api --clear\n\n" +
			"Each probe is an HTTP GET on that path against the app's port; any 2xx or 3xx\n" +
			"passes. Changing a probe rolls the pods.\n\n" +
			"Test the path first (`darkubectl exec app my-api -- wget -qO- localhost/healthz`).\n" +
			"A wrong readiness path stalls the rollout — the old pod keeps serving, the new\n" +
			"one never becomes ready — and a wrong liveness path crash-loops the new pod.\n" +
			"`darkubectl rollout status` shows which.\n\n" +
			"Probe HTTP headers are not settable: the API accepts probes_http_headers and\n" +
			"stores nothing.",
		Flags: append(mutationFlags(),
			&cli.StringFlag{Name: flagReadiness, Usage: "readiness probe path, e.g. /healthz (empty removes it)"},
			&cli.StringFlag{Name: flagLiveness, Usage: "liveness probe path (empty removes it)"},
			&cli.BoolFlag{Name: flagClear, Usage: "remove both probes"},
		),
		Action: setProbeAction,
	}
}

func setProbeAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	want, err := probeChange(cmd.Bool(flagClear),
		optionalString(cmd, flagReadiness), optionalString(cmd, flagLiveness))
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

	changed, err := runAppChange(ctx, cmd, c, app, appChange{
		what: "change the probes of",
		apply: func(raw map[string]any) error {
			for k, v := range want {
				raw[k] = v
			}
			return nil
		},
		took: func(raw map[string]any) bool {
			for k, v := range want {
				if got, _ := raw[k].(string); got != v {
					return false
				}
			}
			return true
		},
		notes: []string{"a wrong path stalls the rollout; follow it with `darkubectl rollout status " + app.Name + "`"},
	})
	if err != nil || !changed {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s probes updated\n", app.Name)
	return nil
}

// optionalString returns a flag's value, or nil when it was not given — so an
// explicitly empty value ("remove it") is distinguishable from absence.
func optionalString(cmd *cli.Command, name string) *string {
	if !cmd.IsSet(name) {
		return nil
	}
	v := cmd.String(name)
	return &v
}

// probeChange turns the flags into the fields to write.
func probeChange(clearAll bool, readiness, liveness *string) (map[string]string, error) {
	if clearAll {
		if readiness != nil || liveness != nil {
			return nil, errProbeArgs
		}
		return map[string]string{keyReadinessPath: "", keyLivenessPath: ""}, nil
	}
	if readiness == nil && liveness == nil {
		return nil, errProbeArgs
	}
	out := map[string]string{}
	for key, v := range map[string]*string{keyReadinessPath: readiness, keyLivenessPath: liveness} {
		if v == nil {
			continue
		}
		path := strings.TrimSpace(*v)
		if path != "" && !strings.HasPrefix(path, "/") {
			return nil, fmt.Errorf("%w: %q", errProbeNotAPath, path)
		}
		out[key] = path
	}
	return out, nil
}
