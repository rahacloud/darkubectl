package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/rahacloud/darkubectl/internal/client"
	"gopkg.in/yaml.v3"
)

// `get apps -o spec` writes apps out in the shape `create app -f` and `apply -f`
// read, so an app made in the console can be brought under version control
// without retyping it. What a spec cannot carry is left out loudly rather than
// quietly: secret values (the API never returns them), domains (`set domain`
// owns their certificate logic), and managed services, which are not created
// from a spec at all.

const (
	outputSpec = "spec"
	specIndent = 2
)

// specNotes collects what an export had to leave out, reported on stderr.
type specNotes []string

// exportSpecs fetches each app's full object and renders it as a spec document.
func exportSpecs(ctx context.Context, c *client.Client, apps []client.App) error {
	plans, err := c.ListPlans(ctx)
	if err != nil {
		return err
	}
	namespaces, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}

	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(specIndent)
	defer func() { _ = enc.Close() }()

	for _, a := range apps {
		if a.IsManagedService() {
			fmt.Fprintf(os.Stderr, "# skipped %s: a %s managed service is not created from a spec\n",
				a.Name, a.CreationMethod)
			continue
		}
		raw, err := c.GetApp(ctx, a.ID)
		if err != nil {
			return err
		}
		spec, notes := specFromApp(raw, plans, namespaces)
		for _, n := range notes {
			fmt.Fprintf(os.Stderr, "# %s: %s\n", a.Name, n)
		}
		if err := enc.Encode(spec); err != nil {
			return err
		}
	}
	return nil
}

// specFromApp turns a raw app object into the spec that would recreate it.
func specFromApp(raw map[string]any, plans []client.Plan, namespaces []client.Namespace) (appSpec, specNotes) {
	var notes specNotes
	str := func(k string) string { v, _ := raw[k].(string); return v }

	spec := appSpec{
		Name:      str("name"),
		Namespace: namespaceRef(raw, namespaces),
	}
	replicas := jsonInt(raw["replicas"])
	spec.Replicas = &replicas

	if plan, ok := planOf(raw, plans); ok {
		spec.Plan = planRef(plan)
		if plan.CostType == costTypeDynamic {
			spec.Memory, spec.CPU = str(keyRAMLimit), str(keyCPURequest)
		}
	}

	if str("creation_method") == client.CreationMethodGitRepoURL {
		spec.Git = gitFromApp(raw)
	} else if repo := str(keyImageRepo); repo != "" {
		spec.Image = repo + ":" + str(keyImageTag)
	}

	if cmd := str(keyCommand); cmd != "" {
		spec.Command = newShellWords(cmd)
	}
	if args := str(keyArgs); args != "" {
		spec.Args = newShellWords(args)
	}

	if svc, ok := raw["svc"].(map[string]any); ok {
		if t, _ := svc["type"].(string); t != "" && t != svcTypeClusterIP {
			spec.SvcType = t
		}
		spec.Ports = portsFromSvc(svc)
	}
	spec.Disk = diskFromApp(raw)
	spec.Envs = client.EnvVars(raw)

	if names := client.SecretEnvNames(raw); len(names) > 0 {
		notes = append(notes, fmt.Sprintf("secret envs %v left out: their values are never returned by the API", names))
	}
	if hosts := client.ExternalHosts(raw); len(hosts) > 0 {
		notes = append(notes, fmt.Sprintf("domains %v left out: manage them with `set domain`", hosts))
	}

	spec.Autoscale = autoscaleFromApp(raw)
	if r, l := str(keyReadinessPath), str(keyLivenessPath); r != "" || l != "" {
		spec.Probes = &probesSpec{Readiness: r, Liveness: l}
	}
	if spec.Autoscale != nil {
		// The autoscaler owns the count; pinning today's number in the spec
		// would fight it on every apply.
		spec.Replicas = nil
	}
	return spec, notes
}

// namespaceRef names the app's namespace, by name when that is unambiguous in
// the tenant and by id when two projects share it (on different clusters).
func namespaceRef(raw map[string]any, namespaces []client.Namespace) string {
	ns, _ := raw["namespace"].(map[string]any)
	name, _ := ns["name"].(string)
	id := jsonInt(ns["id"])
	same := 0
	for _, n := range namespaces {
		if n.Name == name {
			same++
		}
	}
	if same == 1 || id == 0 {
		return name
	}
	return strconv.Itoa(id)
}

// planOf finds the app's plan in the catalogue.
func planOf(raw map[string]any, plans []client.Plan) (client.Plan, bool) {
	nested, _ := raw[keyPlan].(map[string]any)
	id, _ := nested["id"].(string)
	for _, p := range plans {
		if p.ID == id {
			return p, true
		}
	}
	return client.Plan{}, false
}

func gitFromApp(raw map[string]any) *gitSpec {
	str := func(k string) string { v, _ := raw[k].(string); return v }
	g := &gitSpec{
		RepoURL:     str("git_repo_url"),
		Branch:      str("git_branch_name"),
		Provider:    str("git_provider_type"),
		Dockerfile:  str("git_build_dockerfile"),
		Context:     str("git_build_context"),
		Workdir:     str("git_build_workdir"),
		Builder:     str("builder"),
		BuildMethod: str("build_method"),
	}
	if v, ok := raw["autodeploy_on_git_push"].(bool); ok {
		g.Autodeploy = &v
	}
	return g
}

func portsFromSvc(svc map[string]any) map[string]client.Port {
	ports, _ := svc["ports"].(map[string]any)
	if len(ports) == 0 {
		return nil
	}
	out := make(map[string]client.Port, len(ports))
	names := make([]string, 0, len(ports))
	for name := range ports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p, _ := ports[name].(map[string]any)
		proto, _ := p["protocol"].(string)
		out[name] = client.Port{
			ContainerPort: jsonInt(p["containerPort"]),
			ServicePort:   jsonInt(p["servicePort"]),
			Protocol:      proto,
		}
	}
	return out
}

func diskFromApp(raw map[string]any) *client.Disk {
	disk, _ := raw[keyDisk].(map[string]any)
	if len(disk) == 0 {
		return nil
	}
	var d client.Disk
	d.SizeInGi = jsonInt(disk[keyDiskSize])
	d.StorageClassName, _ = disk["storage_class_name"].(string)
	d.SetFSGroup, _ = disk["set_fsgroup"].(bool)
	parts, _ := disk["partitions"].([]any)
	for _, item := range parts {
		p, _ := item.(map[string]any)
		name, _ := p["display_name"].(string)
		mount, _ := p["mount_path"].(string)
		sub, _ := p["sub_path"].(string)
		d.Partitions = append(d.Partitions, client.Partition{DisplayName: name, MountPath: mount, SubPath: sub})
	}
	return &d
}

func autoscaleFromApp(raw map[string]any) *autoscaleSpec {
	cfg, _ := raw[keyCustomConfig].(map[string]any)
	hpa, _ := cfg[keyHPA].(map[string]any)
	if enabled, _ := hpa["enabled"].(bool); !enabled {
		return nil
	}
	return &autoscaleSpec{
		Min:        jsonInt(hpa["minReplicas"]),
		Max:        jsonInt(hpa["maxReplicas"]),
		CPUPercent: jsonInt(hpa["targetCPUUtilizationPercentage"]),
	}
}
