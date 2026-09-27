package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

// Autoscaling is a HorizontalPodAutoscaler the app's chart renders from
// custom_config.hpa, with is_hpa_enabled mirroring hpa.enabled. Set by PUT —
// confirmed 2026-08-23 — and not at POST, where custom_config is ignored for a
// docker-image app. custom_config is also filtered against the chart's schema
// without an error, so on a chart that has no hpa block the write is dropped;
// the read-back is what says so.

const (
	flagMin        = "min"
	flagMax        = "max"
	flagCPUPercent = "cpu-percent"
	flagDisable    = "disable"

	keyCustomConfig = "custom_config"
	keyHPA          = "hpa"
	keyHPAEnabled   = "is_hpa_enabled"

	defaultCPUPercent = 80
	maxCPUPercent     = 1000
)

var (
	errAutoscaleBounds = errors.New("need 1 <= --min <= --max")
	errAutoscaleCPU    = fmt.Errorf("--%s must be between 1 and %d", flagCPUPercent, maxCPUPercent)
	errAutoscaleArgs   = fmt.Errorf("give --%s and --%s, or --%s", flagMin, flagMax, flagDisable)
)

// hpaSpec is what custom_config.hpa holds, with the chart's own key names.
type hpaSpec struct {
	Enabled    bool
	Min        int
	Max        int
	CPUPercent int
}

func newAutoscaleCommand() *cli.Command {
	return &cli.Command{
		Name:  "autoscale",
		Usage: "Scale an app on CPU with a HorizontalPodAutoscaler",
		Commands: []*cli.Command{
			{
				Name:      cmdApp,
				Aliases:   []string{aliasApp},
				Usage:     "Turn autoscaling on or off for an app",
				ArgsUsage: argRefUsage,
				Description: "  darkubectl autoscale app my-api --min 2 --max 6 --cpu-percent 70\n" +
					"  darkubectl autoscale app my-api --disable\n\n" +
					"The percentage is of the pod's CPU request, which the plan sets. While\n" +
					"autoscaling is on, the autoscaler owns the replica count and `scale app`\n" +
					"is overridden on its next evaluation.\n\n" +
					"--disable leaves the replica count wherever the autoscaler last put it.",
				Flags: append(mutationFlags(),
					&cli.IntFlag{Name: flagMin, Usage: "fewest replicas"},
					&cli.IntFlag{Name: flagMax, Usage: "most replicas"},
					&cli.IntFlag{Name: flagCPUPercent, Value: defaultCPUPercent, Usage: "target average CPU, as a percentage of the request"},
					&cli.BoolFlag{Name: flagDisable, Usage: "turn autoscaling off"},
				),
				Action: autoscaleAppAction,
			},
		},
	}
}

func autoscaleAppAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	want, err := hpaFromFlags(cmd.Bool(flagDisable), cmd.IsSet(flagMin) || cmd.IsSet(flagMax),
		cmd.Int(flagMin), cmd.Int(flagMax), cmd.Int(flagCPUPercent))
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

	what := "turn on autoscaling for"
	if !want.Enabled {
		what = "turn off autoscaling for"
	}
	changed, err := runAppChange(ctx, cmd, c, app, appChange{
		what:  what,
		apply: func(raw map[string]any) error { return applyHPA(raw, want) },
		took:  func(raw map[string]any) bool { return hpaTook(raw, want) },
	})
	if err != nil || !changed {
		return err
	}
	if want.Enabled {
		fmt.Fprintf(os.Stdout, "app/%s autoscaled: %d-%d replicas at %d%% CPU\n", app.Name, want.Min, want.Max, want.CPUPercent)
	} else {
		fmt.Fprintf(os.Stdout, "app/%s autoscaling off\n", app.Name)
	}
	return nil
}

// hpaFromFlags validates the flags into the spec to write.
func hpaFromFlags(disable, bounds bool, minReplicas, maxReplicas, cpu int) (hpaSpec, error) {
	if disable {
		if bounds {
			return hpaSpec{}, errAutoscaleArgs
		}
		return hpaSpec{}, nil
	}
	if !bounds {
		return hpaSpec{}, errAutoscaleArgs
	}
	if minReplicas < 1 || maxReplicas < minReplicas {
		return hpaSpec{}, errAutoscaleBounds
	}
	if cpu < 1 || cpu > maxCPUPercent {
		return hpaSpec{}, errAutoscaleCPU
	}
	return hpaSpec{Enabled: true, Min: minReplicas, Max: maxReplicas, CPUPercent: cpu}, nil
}

// applyHPA writes the spec into custom_config.hpa, keeping any other keys the
// chart put in either object. Disabling touches only `enabled`, so the bounds
// survive for the next time it is turned on.
func applyHPA(raw map[string]any, want hpaSpec) error {
	cfg, _ := raw[keyCustomConfig].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
	}
	hpa, _ := cfg[keyHPA].(map[string]any)
	if hpa == nil {
		hpa = map[string]any{}
	}
	hpa["enabled"] = want.Enabled
	if want.Enabled {
		hpa["minReplicas"] = want.Min
		hpa["maxReplicas"] = want.Max
		hpa["targetCPUUtilizationPercentage"] = want.CPUPercent
	}
	cfg[keyHPA] = hpa
	raw[keyCustomConfig] = cfg
	raw[keyHPAEnabled] = want.Enabled
	return nil
}

// hpaTook checks a fresh read for the spec.
func hpaTook(raw map[string]any, want hpaSpec) bool {
	cfg, _ := raw[keyCustomConfig].(map[string]any)
	hpa, _ := cfg[keyHPA].(map[string]any)
	if enabled, _ := hpa["enabled"].(bool); enabled != want.Enabled {
		return false
	}
	if flag, _ := raw[keyHPAEnabled].(bool); flag != want.Enabled {
		return false
	}
	if !want.Enabled {
		return true
	}
	return jsonInt(hpa["minReplicas"]) == want.Min &&
		jsonInt(hpa["maxReplicas"]) == want.Max &&
		jsonInt(hpa["targetCPUUtilizationPercentage"]) == want.CPUPercent
}
