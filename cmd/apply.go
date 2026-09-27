package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// `apply -f` makes an app match a spec file: create it if it is missing, edit
// it in place if it is not. The spec is the one `create app -f` reads and
// `get apps -o spec` writes, so the three form a loop — export what the console
// made, commit it, and apply changes from then on.
//
// It is declarative only as far as the API allows. A field left out of the spec
// is left alone rather than reset, because "absent" in a file someone trimmed by
// hand is far more often an omission than an instruction. Secret envs can only
// be set at creation, domains belong to `set domain`, the namespace an app lives
// in cannot change, and a disk can only grow.

var (
	errApplyNoName      = errors.New("every spec needs a name and a namespace")
	errApplyMoveNS      = errors.New("an app cannot move between namespaces; delete and recreate it instead")
	errApplyImageOnGit  = errors.New("the spec sets image, but this app is built from git")
	errApplyGitOnImage  = errors.New("the spec sets git, but this app runs a prebuilt image")
	errApplyNoSpecs     = errors.New("the file holds no app specs")
	errApplyAmbiguousNS = errors.New("more than one app has this name in this namespace")
	errNoAppInNamespace = errors.New("no app of this name in this namespace")
)

func newApplyCommand() *cli.Command {
	return &cli.Command{
		Name:      "apply",
		Usage:     "Create or update apps to match a spec file",
		ArgsUsage: " ",
		Description: "  darkubectl apply -f my-api.yaml\n" +
			"  darkubectl get apps -n acme --namespace prod -o spec > prod.yaml   # export\n" +
			"  darkubectl apply -f prod.yaml --dry-run\n\n" +
			"The file is the `create app -f` spec, one app per YAML document. An app that\n" +
			"does not exist is created; one that does is changed in place with the diff shown\n" +
			"first. Beyond `create`, a spec can carry autoscale {min, max, cpuPercent}, probes\n" +
			"{readiness, liveness}, and memory/cpu for a dynamic plan.\n\n" +
			"Fields left out are left alone, not reset. Not applied to an existing app, with a\n" +
			"warning: secretEnvs (settable only at creation). Domains are not part of a spec;\n" +
			"use `set domain`. A disk can grow but not shrink, and an app cannot change\n" +
			"namespace. Every change is read back, so a field the platform silently drops is\n" +
			"reported as an error rather than passed off as applied.",
		Flags: append(mutationFlags(),
			&cli.StringFlag{Name: flagFile, Aliases: []string{"f"}, Required: true, Usage: "spec file (- for stdin)"},
		),
		Action: applyAction,
	}
}

func applyAction(ctx context.Context, cmd *cli.Command) error {
	specs, err := loadAppSpecs(cmd.String(flagFile))
	if err != nil {
		return err
	}
	c, _, err := buildClient(ctx, cmd)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := applyOne(ctx, cmd, c, spec); err != nil {
			return fmt.Errorf("app/%s: %w", spec.Name, err)
		}
	}
	return nil
}

// loadAppSpecs reads every YAML document in the file.
func loadAppSpecs(path string) ([]appSpec, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path) //nolint:gosec // the user's own spec file
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	dec := yaml.NewDecoder(r)
	var out []appSpec
	for {
		var s appSpec
		err := dec.Decode(&s)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if s.Name == "" && s.Namespace == "" {
			continue // an empty document, e.g. a trailing ---
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errApplyNoSpecs
	}
	return out, nil
}

func applyOne(ctx context.Context, cmd *cli.Command, c *client.Client, spec appSpec) error {
	if err := validateApplySpec(spec); err != nil {
		return err
	}
	nsID, err := resolveNamespaceID(ctx, c, spec.Namespace)
	if err != nil {
		return err
	}
	var plan *client.Plan
	if spec.Plan != "" {
		if plan, err = findPlan(ctx, c, spec.Plan); err != nil {
			return err
		}
	}

	app, err := findAppIn(ctx, c, spec.Name, nsID)
	if errors.Is(err, errNoAppInNamespace) {
		return createMissing(ctx, cmd, c, spec, plan, nsID)
	}
	if err != nil {
		return err
	}
	return updateFromSpec(ctx, cmd, c, app, spec, plan, nsID, false)
}

// createMissing creates the app a spec describes, then applies with a PUT what
// the POST ignores. That second step was part of what the user approved, so it
// is not asked about again.
func createMissing(
	ctx context.Context, cmd *cli.Command, c *client.Client, spec appSpec, plan *client.Plan, nsID int,
) error {
	if !spec.complete() {
		return errIncompleteSpec
	}
	if (spec.Memory != "" || spec.CPU != "") && (plan == nil || plan.CostType != costTypeDynamic) {
		return fmt.Errorf("%w (the app would be %s)", errFixedPlan, planLabel(namedPlan(ctx, c, plan)))
	}
	if cmd.Bool(flagDryRun) {
		fmt.Fprintf(os.Stdout, "app/%s: dry run, no app of this name in namespace %s, so it would be created\n",
			spec.Name, spec.Namespace)
		return nil
	}
	if err := createFromSpec(ctx, cmd, c, spec); err != nil {
		return err
	}
	if !spec.hasPostCreateFields() {
		return nil
	}
	app, err := findAppIn(ctx, c, spec.Name, nsID)
	if err != nil {
		return fmt.Errorf("created, but could not find it again to apply autoscale/probes/size: %w", err)
	}
	return updateFromSpec(ctx, cmd, c, app, spec, plan, nsID, true)
}

// updateFromSpec edits an existing app to match the spec.
func updateFromSpec(
	ctx context.Context, cmd *cli.Command, c *client.Client, app *client.App,
	spec appSpec, plan *client.Plan, nsID int, confirmed bool,
) error {
	target := plan
	if target == nil {
		target = app.Plan
	}
	if (spec.Memory != "" || spec.CPU != "") && (target == nil || target.CostType != costTypeDynamic) {
		return fmt.Errorf("%w (the app would be %s)", errFixedPlan, planLabel(namedPlan(ctx, c, target)))
	}

	var notes []string
	if len(spec.SecretEnvs) > 0 {
		notes = append(notes, "secretEnvs are not applied to an existing app: they can only be set at creation")
	}
	if spec.Autoscale != nil && spec.Replicas != nil {
		notes = append(notes, "replicas ignored: autoscale owns the replica count")
	}

	changed, err := runAppChange(ctx, cmd, c, app, appChange{
		what:       "apply the spec to",
		apply:      func(raw map[string]any) error { return applySpecTo(raw, spec, plan, nsID) },
		verifyDiff: true,
		confirmed:  confirmed,
		notes:      notes,
	})
	if err != nil || !changed {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s configured\n", app.Name)
	return nil
}

// validateApplySpec checks what can be checked without the API.
func validateApplySpec(spec appSpec) error {
	if spec.Name == "" || spec.Namespace == "" {
		return errApplyNoName
	}
	if spec.Git != nil && spec.Image != "" {
		return errGitAndImage
	}
	if err := validateArgs(spec.Args); err != nil {
		return err
	}
	if spec.Memory != "" || spec.CPU != "" {
		if err := validateResources("", spec.Memory, spec.CPU); err != nil {
			return err
		}
	}
	if a := spec.Autoscale; a != nil {
		if _, err := hpaFromFlags(false, true, a.Min, a.Max, a.cpuPercent()); err != nil {
			return err
		}
	}
	if p := spec.Probes; p != nil {
		if _, err := probeChange(false, &p.Readiness, &p.Liveness); err != nil {
			return err
		}
	}
	for _, w := range entrypointWarnings(spec.Command.String(), spec.Args.String()) {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", spec.Name, w)
	}
	return nil
}

// applySpecTo writes every field the spec sets onto a normalized app object.
func applySpecTo(raw map[string]any, spec appSpec, plan *client.Plan, nsID int) error {
	if ns := jsonInt(raw["namespace"]); ns != 0 && ns != nsID {
		return errApplyMoveNS
	}
	isGit, _ := raw["creation_method"].(string)

	if spec.Image != "" {
		if isGit == client.CreationMethodGitRepoURL {
			return errApplyImageOnGit
		}
		repo, tag := splitImage(spec.Image)
		_ = applyImage(raw, repo, tag)
	}
	if spec.Git != nil {
		if isGit != client.CreationMethodGitRepoURL {
			return errApplyGitOnImage
		}
		applyGit(raw, spec.Git)
	}

	if err := applyResources(raw, plan, plan != nil, spec.Memory, spec.CPU); err != nil {
		return err
	}
	if spec.Replicas != nil && spec.Autoscale == nil {
		raw["replicas"] = *spec.Replicas
	}
	if !spec.Command.IsZero() {
		raw[keyCommand] = spec.Command.String()
	}
	if !spec.Args.IsZero() {
		raw[keyArgs] = spec.Args.String()
	}
	if err := applySvc(raw, spec.SvcType, spec.Ports); err != nil {
		return err
	}
	if spec.Disk != nil && spec.Disk.SizeInGi > 0 {
		if err := applyDiskSize(raw, spec.Disk.SizeInGi); err != nil {
			return err
		}
	}
	if spec.Envs != nil {
		client.SetEnvVars(raw, spec.Envs)
	}
	if a := spec.Autoscale; a != nil {
		_ = applyHPA(raw, hpaSpec{Enabled: true, Min: a.Min, Max: a.Max, CPUPercent: a.cpuPercent()})
	}
	if p := spec.Probes; p != nil {
		setProbePath(raw, keyReadinessPath, p.Readiness)
		setProbePath(raw, keyLivenessPath, p.Liveness)
	}
	return nil
}

// setProbePath writes a probe path unless doing so would only turn an unset
// (null) field into an empty one, which means the same and would show as a
// change on every apply.
func setProbePath(raw map[string]any, key, path string) {
	current, _ := raw[key].(string)
	if path == "" && current == "" {
		return
	}
	raw[key] = path
}

// applySvc sets the service type and replaces the port list, keeping each
// surviving port's allocated nodePort so a LoadBalancer app's public port does
// not move under its clients.
func applySvc(raw map[string]any, svcType string, ports map[string]client.Port) error {
	if svcType == "" && ports == nil {
		return nil
	}
	svc, _ := raw["svc"].(map[string]any)
	if svc == nil {
		svc = map[string]any{}
	}
	if svcType != "" {
		canonical, err := canonicalSvcType(svcType)
		if err != nil {
			return err
		}
		svc["type"] = canonical
	}
	if ports != nil {
		existing, _ := svc["ports"].(map[string]any)
		svc["ports"] = mergePorts(existing, ports)
	}
	raw["svc"] = svc
	return nil
}

// mergePorts builds the new port map, carrying over the nodePort of each port
// that keeps its name.
func mergePorts(existing map[string]any, ports map[string]client.Port) map[string]any {
	next := make(map[string]any, len(ports))
	for name, p := range ports {
		entry := map[string]any{}
		if old, ok := existing[name].(map[string]any); ok && old["nodePort"] != nil {
			entry["nodePort"] = old["nodePort"]
		}
		servicePort := p.ServicePort
		if servicePort == 0 {
			servicePort = p.ContainerPort
		}
		protocol := p.Protocol
		if protocol == "" {
			protocol = "TCP"
		}
		entry["containerPort"] = p.ContainerPort
		entry["servicePort"] = servicePort
		entry["protocol"] = protocol
		next[name] = entry
	}
	return next
}

// applyGit writes the build settings the spec names; empty ones are left alone.
func applyGit(raw map[string]any, g *gitSpec) {
	set := func(key, v string) {
		if v != "" {
			raw[key] = v
		}
	}
	set("git_repo_url", g.RepoURL)
	set("git_branch_name", g.Branch)
	set("git_provider_type", g.Provider)
	set("git_build_dockerfile", g.Dockerfile)
	set("git_build_context", g.Context)
	set("git_build_workdir", g.Workdir)
	set("builder", g.Builder)
	set("build_method", g.BuildMethod)
	if g.Autodeploy != nil {
		raw["autodeploy_on_git_push"] = *g.Autodeploy
	}
}

// findAppIn finds an app by name within one namespace, or errNoAppInNamespace.
// Names are unique per namespace, not per tenant, so ResolveApp — which is
// tenant-wide — is not the right lookup here.
func findAppIn(ctx context.Context, c *client.Client, name string, nsID int) (*client.App, error) {
	apps, err := c.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	var found []client.App
	for _, a := range apps {
		if a.Name == name && a.Namespace.ID == nsID {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 0:
		return nil, errNoAppInNamespace
	case 1:
		return &found[0], nil
	default:
		return nil, errApplyAmbiguousNS
	}
}

// hasPostCreateFields reports whether the spec sets anything the POST ignores.
func (s appSpec) hasPostCreateFields() bool {
	return s.Autoscale != nil || s.Probes != nil || s.Memory != "" || s.CPU != ""
}

// cpuPercent is the target, defaulting as `autoscale app` does.
func (a autoscaleSpec) cpuPercent() int {
	if a.CPUPercent == 0 {
		return defaultCPUPercent
	}
	return a.CPUPercent
}
