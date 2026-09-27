package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

// An app's size is its plan. Established 2026-09-27 on a throwaway app:
//
//   - `plan` changes in place with a PUT of the new plan's id, even though
//     OPTIONS marks the field read-only, and ram_limit/cpu_request follow it.
//   - On a fixed plan, ram_limit and cpu_request are derived: a PUT that sets
//     them is accepted and discarded.
//   - On a dynamic plan (cost_type "dynamic", billed per core and per GiB)
//     they are the app's own, and a PUT of 700M/300m stuck.
//
// So --memory and --cpu are refused up front unless the plan the app will be on
// is dynamic, rather than sent to be silently dropped.

const (
	flagMemory = "memory"
	flagCPU    = "cpu"

	keyPlan       = "plan"
	keyRAMLimit   = "ram_limit"
	keyCPURequest = "cpu_request"

	costTypeDynamic = "dynamic"
)

var (
	errResourcesArgs = errors.New("give --plan, --memory or --cpu")
	errFixedPlan     = errors.New("memory and CPU are fixed by the plan")

	// The platform's own spellings, as every app reads back: "500M", "250m".
	memoryPattern = regexp.MustCompile(`^[1-9][0-9]*M$`)
	cpuPattern    = regexp.MustCompile(`^[1-9][0-9]*m$`)
)

func newSetResourcesCommand() *cli.Command {
	return &cli.Command{
		Name:      "resources",
		Aliases:   []string{"plan", "resource"},
		Usage:     "Change an app's plan, or its memory and CPU on a dynamic plan",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl set resources my-api --plan 2\n" +
			"  darkubectl set resources my-api --plan dynamic --memory 1500M --cpu 750m\n" +
			"  darkubectl set resources my-api --memory 2000M      # already on a dynamic plan\n\n" +
			"A fixed plan decides memory and CPU itself, so --memory/--cpu need a dynamic plan\n" +
			"(`darkubectl get plans --all` lists them) and are refused otherwise: the API would\n" +
			"accept them and discard them. Units are the platform's own — M for memory, m\n" +
			"(millicores) for CPU.\n\n" +
			"The app is billed at the new plan from now on, and the pods roll.",
		Flags: append(mutationFlags(),
			&cli.StringFlag{Name: flagPlan, Usage: "plan name, code name or id (see `get plans`)"},
			&cli.StringFlag{Name: flagMemory, Usage: "memory limit, e.g. 1500M (dynamic plans only)"},
			&cli.StringFlag{Name: flagCPU, Usage: "CPU request in millicores, e.g. 750m (dynamic plans only)"},
		),
		Action: setResourcesAction,
	}
}

func setResourcesAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	planRef := strings.TrimSpace(cmd.String(flagPlan))
	memory, cpu := strings.TrimSpace(cmd.String(flagMemory)), strings.TrimSpace(cmd.String(flagCPU))
	if err := validateResources(planRef, memory, cpu); err != nil {
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

	// The plan the app will be on decides whether memory/cpu can be set.
	var plan *client.Plan
	if planRef != "" {
		if plan, err = findPlan(ctx, c, planRef); err != nil {
			return err
		}
	} else if app.Plan != nil {
		plan = app.Plan
	}
	if (memory != "" || cpu != "") && (plan == nil || plan.CostType != costTypeDynamic) {
		return fmt.Errorf("%w (the app would be %s): pick a dynamic one with --plan, e.g. --plan dynamic",
			errFixedPlan, planLabel(namedPlan(ctx, c, plan)))
	}

	changed, err := runAppChange(ctx, cmd, c, app, appChange{
		what:  "resize",
		apply: func(raw map[string]any) error { return applyResources(raw, plan, planRef != "", memory, cpu) },
		took:  func(raw map[string]any) bool { return resourcesTook(raw, plan, planRef != "", memory, cpu) },
		notes: []string{"the app is billed at the new size from now on, and its pods roll"},
	})
	if err != nil || !changed {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s resized\n", app.Name)
	return nil
}

// validateResources checks the flags before anything is read.
func validateResources(plan, memory, cpu string) error {
	if plan == "" && memory == "" && cpu == "" {
		return errResourcesArgs
	}
	if memory != "" && !memoryPattern.MatchString(memory) {
		return fmt.Errorf("--%s %q: write it the way the platform does, in megabytes, e.g. 1500M", flagMemory, memory)
	}
	if cpu != "" && !cpuPattern.MatchString(cpu) {
		return fmt.Errorf("--%s %q: write it the way the platform does, in millicores, e.g. 750m", flagCPU, cpu)
	}
	return nil
}

// applyResources writes the plan and sizes onto a normalized app object, where
// plan is a bare id.
func applyResources(raw map[string]any, plan *client.Plan, setPlan bool, memory, cpu string) error {
	if setPlan && plan != nil {
		raw[keyPlan] = plan.ID
	}
	if memory != "" {
		raw[keyRAMLimit] = memory
	}
	if cpu != "" {
		raw[keyCPURequest] = cpu
	}
	return nil
}

// resourcesTook checks a fresh read, where plan is the nested object.
func resourcesTook(raw map[string]any, plan *client.Plan, setPlan bool, memory, cpu string) bool {
	if setPlan && plan != nil {
		nested, _ := raw[keyPlan].(map[string]any)
		if id, _ := nested["id"].(string); id != plan.ID {
			return false
		}
	}
	if got, _ := raw[keyRAMLimit].(string); memory != "" && got != memory {
		return false
	}
	if got, _ := raw[keyCPURequest].(string); cpu != "" && got != cpu {
		return false
	}
	return true
}

// findPlan resolves a plan by id, code name or name, returning the whole plan
// so its cost type is known.
func findPlan(ctx context.Context, c *client.Client, ref string) (*client.Plan, error) {
	plans, err := c.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	for i := range plans {
		if plans[i].ID == ref || plans[i].CodeName == ref || plans[i].Name == ref {
			return &plans[i], nil
		}
	}
	return nil, fmt.Errorf("no plan matching %q; `darkubectl get plans --all` lists them", ref)
}

// namedPlan fills in a plan's names from the catalogue. An app's own plan comes
// off the list route with only its id and sizing, which makes for an error
// message that names nothing.
func namedPlan(ctx context.Context, c *client.Client, p *client.Plan) *client.Plan {
	if p == nil || p.CodeName != "" || p.Name != "" {
		return p
	}
	if named, err := findPlan(ctx, c, p.ID); err == nil {
		return named
	}
	return p
}

// planLabel names a plan the way `get plans` does.
func planLabel(p *client.Plan) string {
	if p == nil {
		return "on an unknown plan"
	}
	return fmt.Sprintf("on plan %q", planRef(*p))
}
