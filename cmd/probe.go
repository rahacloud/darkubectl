package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// A probe can be set in two places, and they are not equivalent.
//
// The top-level string fields readiness_probe_path and liveness_probe_path are
// an HTTP GET on that path against the app's port. They reach the pod —
// confirmed 2026-09-27 on a throwaway nginx, where a readiness path answering
// 404 left the new pod at 0/1 indefinitely — but Darkube renders them with
// timing nobody can change: initialDelaySeconds 5, periodSeconds 1,
// timeoutSeconds 2 and the default failureThreshold of 3, so a liveness probe
// kills a container that is not answering about eight seconds after it starts.
// On 2026-10-27/28 that crash-looped a Django backend and Celery Flower, both of
// which take 15-30 s to come up.
//
// custom_config.container.livenessProbe and .readinessProbe take a full
// Kubernetes probe instead (handler plus initialDelaySeconds, periodSeconds,
// timeoutSeconds, successThreshold, failureThreshold). It is in Darkube's
// custom-config documentation and was confirmed 2026-10-28 with `patch app`:
// `get manifests` showed the probe rendered exactly as written, for both an
// httpGet and an exec (`sh -c "celery … inspect ping"`) probe. custom_config is
// filtered against the chart schema without an error, so the read-back is what
// proves a write took.
//
// An app should carry one definition per kind, so writing either form clears
// the other, and removing a probe removes both.
//
// The same 2026-09-27 experiment, watched on the pod stream, showed the
// rollout is a normal surge: the old pod kept serving the whole time the new
// one sat unready. So a wrong readiness probe stalls the rollout rather than
// taking the app down — until the next restart, when there is no ready pod to
// fall back on. A wrong liveness probe crash-loops the new pod.
//
// probes_http_headers is advertised as writable and is not: every shape tried
// ([{name,value}], [{key,value}], a plain object), alone or alongside a path,
// read back as []. So this command offers no --header.

const (
	flagReadiness        = "readiness"
	flagLiveness         = "liveness"
	flagReadinessExec    = "readiness-exec"
	flagLivenessExec     = "liveness-exec"
	flagProbePort        = "port"
	flagInitialDelay     = "initial-delay"
	flagPeriod           = "period"
	flagSuccessThreshold = "success-threshold"
	flagFailureThreshold = "failure-threshold"

	keyReadinessPath = "readiness_probe_path"
	keyLivenessPath  = "liveness_probe_path"
	keyContainer     = "container"

	// The Kubernetes probe's own field names.
	keyInitialDelaySeconds = "initialDelaySeconds"
	keyPeriodSeconds       = "periodSeconds"
	keyTimeoutSeconds      = "timeoutSeconds"
	keySuccessThreshold    = "successThreshold"
	keyFailureThreshold    = "failureThreshold"
)

var (
	errProbeArgs = errors.New("give --readiness/--liveness (a path; empty removes that probe), " +
		"--readiness-exec/--liveness-exec, or --clear")
	errProbeNotAPath      = errors.New("a probe path must start with /")
	errProbeBothForms     = errors.New("a probe is either an HTTP path or a command, not both")
	errProbeEmptyExec     = errors.New("an exec probe needs a command; remove a probe with --readiness \"\" or --liveness \"\"")
	errProbeNothingToTune = errors.New("the timing and --port flags apply to a probe being set, and every probe named is being removed")
	errProbePortOnExec    = errors.New("--port applies to an HTTP probe, and every probe named is a command")
	errProbeNegative      = errors.New("probe timings and thresholds cannot be negative")
	errProbeWholeSeconds  = errors.New("probe timings are whole seconds")
	errProbeLiveSuccess   = errors.New("a liveness probe's success threshold must be 1 (Kubernetes rejects anything else)")
	errProbeNoPort        = errors.New("cannot tell which port to probe; give --port")
)

// probeKind is readiness or liveness, with the two places each can live.
type probeKind struct {
	name      string // readiness, liveness
	pathKey   string // the top-level path field
	configKey string // the key under custom_config.container
}

var probeKinds = []probeKind{
	{name: flagReadiness, pathKey: keyReadinessPath, configKey: "readinessProbe"},
	{name: flagLiveness, pathKey: keyLivenessPath, configKey: "livenessProbe"},
}

// probeDef is one probe to write. Without full it is the path-only form, the
// top-level field; with it, a Kubernetes probe in custom_config. It is also the
// spec-file form: a bare string is a path, a mapping is a full probe. Zero
// timings are left out, which leaves them to Kubernetes' defaults.
type probeDef struct {
	Path                string `yaml:"path,omitempty"`
	Exec                string `yaml:"exec,omitempty"`
	Port                int    `yaml:"port,omitempty"` // 0: the app's own port
	InitialDelaySeconds int    `yaml:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int    `yaml:"periodSeconds,omitempty"`
	TimeoutSeconds      int    `yaml:"timeoutSeconds,omitempty"`
	SuccessThreshold    int    `yaml:"successThreshold,omitempty"`
	FailureThreshold    int    `yaml:"failureThreshold,omitempty"`

	full bool
}

// probeFields lets the YAML methods decode and encode the struct without
// recursing into themselves.
type probeFields probeDef

// UnmarshalYAML reads a bare path as the path-only form and a mapping as a
// full probe, so `probes: {readiness: /healthz}` keeps meaning what it did.
func (p *probeDef) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*p = probeDef{}
		return node.Decode(&p.Path)
	case yaml.MappingNode:
		var f probeFields
		if err := node.Decode(&f); err != nil {
			return err
		}
		*p = probeDef(f)
		p.full = true
		return nil
	default:
		return fmt.Errorf("a probe is a path or a mapping, got %v", node.Kind)
	}
}

// MarshalYAML writes the form it was read as.
func (p probeDef) MarshalYAML() (any, error) {
	if !p.full {
		return p.Path, nil
	}
	return probeFields(p), nil
}

// IsZero reports an absent probe, so `omitempty` leaves it out of a spec.
func (p probeDef) IsZero() bool { return !p.full && p.Path == "" }

// probeOptions are the flags that make a probe full, applied to every probe
// the command sets.
type probeOptions struct {
	port   int
	timing probeDef
	// set is whether any of them was given.
	set bool
}

func newSetProbeCommand() *cli.Command {
	return &cli.Command{
		Name:      "probe",
		Aliases:   []string{"probes"},
		Usage:     "Set the readiness and liveness probes of an app",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl set probe my-api --readiness /healthz --liveness /livez\n" +
			"  darkubectl set probe my-api --liveness /healthz --initial-delay 60s --period 15s \\\n" +
			"      --timeout 5s --failure-threshold 4\n" +
			"  darkubectl set probe flower --liveness /healthcheck --port 5555 --initial-delay 60s\n" +
			"  darkubectl set probe worker --initial-delay 2m --period 1m --timeout 20s \\\n" +
			"      --liveness-exec 'celery -b \"redis://$REDIS_HOST:6379\" inspect ping -d \"celery@$HOSTNAME\"'\n" +
			"  darkubectl set probe my-api --liveness \"\"      # remove the liveness probe\n" +
			"  darkubectl set probe my-api --clear\n\n" +
			"With only a path, the probe is an HTTP GET on that path against the app's port\n" +
			"(any 2xx or 3xx passes), and Darkube fixes its timing: 5s initial delay, every\n" +
			"1s, 2s timeout, 3 failures. A liveness probe like that kills a container that is\n" +
			"not answering about 8 seconds after it starts, so an app that takes longer to\n" +
			"boot (Django, Celery, a JVM) crash-loops forever.\n\n" +
			"Any of --initial-delay, --period, --timeout, --success-threshold,\n" +
			"--failure-threshold or --port, or an exec probe, writes a full Kubernetes probe\n" +
			"into custom_config.container instead, applied to every probe the command sets;\n" +
			"what is not given takes Kubernetes' defaults. --port defaults to the app's port\n" +
			"when it has one (or one named http). An exec probe runs its command with sh -c\n" +
			"and passes on exit 0. Setting either form clears the other, and removing a\n" +
			"probe removes both. Changing a probe rolls the pods.\n\n" +
			"Test the probe first (`darkubectl exec app my-api -- wget -qO- localhost/healthz`).\n" +
			"A wrong readiness probe stalls the rollout — the old pod keeps serving, the new\n" +
			"one never becomes ready — and a wrong liveness probe crash-loops the new pod.\n" +
			"`darkubectl rollout status` shows which.\n\n" +
			"Probe HTTP headers are not settable: the API accepts probes_http_headers and\n" +
			"stores nothing.",
		Flags: append(mutationFlags(),
			&cli.StringFlag{Name: flagReadiness, Usage: "readiness probe path, e.g. /healthz (empty removes the probe)"},
			&cli.StringFlag{Name: flagLiveness, Usage: "liveness probe path (empty removes the probe)"},
			&cli.StringFlag{Name: flagReadinessExec, Usage: "readiness probe command, run with sh -c"},
			&cli.StringFlag{Name: flagLivenessExec, Usage: "liveness probe command, run with sh -c"},
			&cli.IntFlag{Name: flagProbePort, Usage: "port the HTTP probe calls (default: the app's port)"},
			&cli.DurationFlag{Name: flagInitialDelay, Usage: "wait this long after the container starts before probing"},
			&cli.DurationFlag{Name: flagPeriod, Usage: "probe this often"},
			&cli.DurationFlag{Name: flagTimeout, Usage: "a probe that takes longer than this fails"},
			&cli.IntFlag{Name: flagSuccessThreshold, Usage: "passes in a row to count as up (readiness only)"},
			&cli.IntFlag{Name: flagFailureThreshold, Usage: "failures in a row before the pod is restarted or marked unready"},
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
	opts, err := probeOptionsFromFlags(cmd)
	if err != nil {
		return err
	}
	want, err := probeChange(cmd.Bool(flagClear),
		map[string]*string{flagReadiness: optionalString(cmd, flagReadiness), flagLiveness: optionalString(cmd, flagLiveness)},
		map[string]*string{flagReadiness: optionalString(cmd, flagReadinessExec), flagLiveness: optionalString(cmd, flagLivenessExec)},
		opts)
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
		what:  "change the probes of",
		apply: func(raw map[string]any) error { return applyProbes(raw, want) },
		took:  func(raw map[string]any) bool { return probesTook(raw, want) },
		notes: []string{"a wrong probe stalls the rollout; follow it with `darkubectl rollout status " + app.Name + "`"},
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

// probeOptionsFromFlags reads the flags that make a probe full.
func probeOptionsFromFlags(cmd *cli.Command) (probeOptions, error) {
	var o probeOptions
	for _, name := range []string{flagProbePort, flagInitialDelay, flagPeriod, flagTimeout, flagSuccessThreshold, flagFailureThreshold} {
		o.set = o.set || cmd.IsSet(name)
	}
	o.port = cmd.Int(flagProbePort)
	o.timing.SuccessThreshold = cmd.Int(flagSuccessThreshold)
	o.timing.FailureThreshold = cmd.Int(flagFailureThreshold)
	for _, d := range []struct {
		flag string
		into *int
	}{
		{flagInitialDelay, &o.timing.InitialDelaySeconds},
		{flagPeriod, &o.timing.PeriodSeconds},
		{flagTimeout, &o.timing.TimeoutSeconds},
	} {
		s, err := wholeSeconds(cmd.Duration(d.flag))
		if err != nil {
			return probeOptions{}, fmt.Errorf("--%s: %w", d.flag, err)
		}
		*d.into = s
	}
	return o, nil
}

// wholeSeconds converts a duration flag to the seconds a probe is written in.
func wholeSeconds(d time.Duration) (int, error) {
	if d < 0 {
		return 0, errProbeNegative
	}
	if d%time.Second != 0 {
		return 0, fmt.Errorf("%w, got %s", errProbeWholeSeconds, d)
	}
	return int(d / time.Second), nil
}

// probeChange turns the flags into the probes to write, keyed by kind; a nil
// entry removes that kind, a missing one leaves it alone.
func probeChange(clearAll bool, paths, execs map[string]*string, opts probeOptions) (map[string]*probeDef, error) {
	given := false
	for _, k := range probeKinds {
		given = given || paths[k.name] != nil || execs[k.name] != nil
	}
	if clearAll {
		if given || opts.set {
			return nil, errProbeArgs
		}
		return map[string]*probeDef{flagReadiness: nil, flagLiveness: nil}, nil
	}
	if !given {
		return nil, errProbeArgs
	}

	out := map[string]*probeDef{}
	setting, httpSet := false, false
	for _, k := range probeKinds {
		p, named, err := probeFromFlags(k, paths[k.name], execs[k.name], opts)
		if err != nil {
			return nil, err
		}
		if !named {
			continue
		}
		out[k.name] = p
		setting = setting || p != nil
		httpSet = httpSet || (p != nil && p.Exec == "")
	}
	if opts.set && !setting {
		return nil, errProbeNothingToTune
	}
	if opts.port != 0 && !httpSet {
		return nil, errProbePortOnExec
	}
	return out, nil
}

// probeFromFlags reads and validates one kind's flags: given is false when
// neither was set, and a nil probe with given true removes it.
func probeFromFlags(k probeKind, path, cmdline *string, opts probeOptions) (*probeDef, bool, error) {
	switch {
	case path != nil && cmdline != nil:
		return nil, false, fmt.Errorf("%w: --%s and --%s-exec", errProbeBothForms, k.name, k.name)
	case cmdline != nil:
		if strings.TrimSpace(*cmdline) == "" {
			return nil, false, errProbeEmptyExec
		}
		p := opts.timing
		p.Exec, p.full = *cmdline, true
		return &p, true, p.validate(k)
	case path != nil:
		trimmed := strings.TrimSpace(*path)
		if trimmed == "" {
			return nil, true, nil
		}
		p := probeDef{Path: trimmed}
		if opts.set {
			p = opts.timing
			p.Path, p.Port, p.full = trimmed, opts.port, true
		}
		return &p, true, p.validate(k)
	default:
		return nil, false, nil
	}
}

// validate checks what Kubernetes or the chart would otherwise reject on the
// PUT, or the pod at rollout.
func (p probeDef) validate(k probeKind) error {
	if p.full && p.Exec != "" && p.Path != "" {
		return fmt.Errorf("%w: the %s probe", errProbeBothForms, k.name)
	}
	if p.Exec == "" && !strings.HasPrefix(p.Path, "/") {
		return fmt.Errorf("%w: %q", errProbeNotAPath, p.Path)
	}
	for _, n := range []int{p.Port, p.InitialDelaySeconds, p.PeriodSeconds, p.TimeoutSeconds, p.SuccessThreshold, p.FailureThreshold} {
		if n < 0 {
			return errProbeNegative
		}
	}
	if k.name == flagLiveness && p.SuccessThreshold > 1 {
		return errProbeLiveSuccess
	}
	return nil
}

// applyProbes writes the probes onto a normalized app object. Each kind ends up
// in exactly one place: the path field or custom_config, never both.
func applyProbes(raw map[string]any, want map[string]*probeDef) error {
	for _, k := range probeKinds {
		p, ok := want[k.name]
		switch {
		case !ok:
			continue
		case p == nil:
			setProbePath(raw, k.pathKey, "")
			removeConfigProbe(raw, k.configKey)
		case !p.full:
			raw[k.pathKey] = p.Path
			removeConfigProbe(raw, k.configKey)
		default:
			probe, err := p.render(raw)
			if err != nil {
				return err
			}
			setProbePath(raw, k.pathKey, "")
			setConfigProbe(raw, k.configKey, probe)
		}
	}
	return nil
}

// probesTook checks a fresh read for the probes. A full probe is compared on
// the keys written, so a default the platform might fill in is not mistaken
// for a dropped write.
func probesTook(raw map[string]any, want map[string]*probeDef) bool {
	for _, k := range probeKinds {
		p, ok := want[k.name]
		if !ok {
			continue
		}
		path, _ := raw[k.pathKey].(string)
		stored := configProbe(raw, k.configKey)
		switch {
		case p == nil:
			if path != "" || stored != nil {
				return false
			}
		case !p.full:
			if path != p.Path || stored != nil {
				return false
			}
		default:
			probe, err := p.render(raw)
			if err != nil || path != "" || !jsonContains(stored, probe) {
				return false
			}
		}
	}
	return true
}

// render builds the Kubernetes probe for custom_config, reading the port off
// the app when none was given.
func (p probeDef) render(raw map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if p.Exec != "" {
		out["exec"] = map[string]any{keyCommand: []any{"sh", "-c", p.Exec}}
	} else {
		port := p.Port
		if port == 0 {
			var err error
			if port, err = appPort(raw); err != nil {
				return nil, err
			}
		}
		out["httpGet"] = map[string]any{"path": p.Path, "port": port}
	}
	for key, n := range map[string]int{
		keyInitialDelaySeconds: p.InitialDelaySeconds,
		keyPeriodSeconds:       p.PeriodSeconds,
		keyTimeoutSeconds:      p.TimeoutSeconds,
		keySuccessThreshold:    p.SuccessThreshold,
		keyFailureThreshold:    p.FailureThreshold,
	} {
		if n != 0 {
			out[key] = n
		}
	}
	return out, nil
}

// appPort is the container port the path-only form would probe: the app's only
// port, or the one named http.
func appPort(raw map[string]any) (int, error) {
	svc, _ := raw["svc"].(map[string]any)
	ports, _ := svc["ports"].(map[string]any)
	pick := func(v any) int {
		m, _ := v.(map[string]any)
		if n, ok := m["containerPort"].(int); ok {
			return n
		}
		return jsonInt(m["containerPort"])
	}
	if len(ports) == 1 {
		for _, v := range ports {
			if n := pick(v); n > 0 {
				return n, nil
			}
		}
	}
	if n := pick(ports["http"]); n > 0 {
		return n, nil
	}
	return 0, fmt.Errorf("%w (the app exposes %d ports, none named http)", errProbeNoPort, len(ports))
}

// configProbe returns custom_config.container.<key>, or nil.
func configProbe(raw map[string]any, key string) map[string]any {
	cfg, _ := raw[keyCustomConfig].(map[string]any)
	container, _ := cfg[keyContainer].(map[string]any)
	probe, _ := container[key].(map[string]any)
	return probe
}

// setConfigProbe replaces custom_config.container.<key> whole — switching from
// httpGet to exec must not leave both handlers — and keeps every other key, so
// `autoscale`'s hpa and the other probe survive.
func setConfigProbe(raw map[string]any, key string, probe map[string]any) {
	cfg, _ := raw[keyCustomConfig].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
	}
	container, _ := cfg[keyContainer].(map[string]any)
	if container == nil {
		container = map[string]any{}
	}
	container[key] = probe
	cfg[keyContainer] = container
	raw[keyCustomConfig] = cfg
}

// removeConfigProbe deletes custom_config.container.<key>, and container with
// it once empty. An app that never had one is left untouched rather than given
// empty objects that would show as a change.
func removeConfigProbe(raw map[string]any, key string) {
	cfg, _ := raw[keyCustomConfig].(map[string]any)
	container, _ := cfg[keyContainer].(map[string]any)
	if _, ok := container[key]; !ok {
		return
	}
	delete(container, key)
	if len(container) == 0 {
		delete(cfg, keyContainer)
	}
}

// jsonContains reports whether got holds every key of want with an equal
// value, comparing leaves as rendered JSON so a written int matches the
// float64 a read decodes.
func jsonContains(got, want any) bool {
	wm, ok := want.(map[string]any)
	if !ok {
		return renderValue(got) == renderValue(want)
	}
	gm, ok := got.(map[string]any)
	if !ok {
		return false
	}
	for k, v := range wm {
		if !jsonContains(gm[k], v) {
			return false
		}
	}
	return true
}

// probeFromConfig reads a stored custom_config probe back into the spec form.
// It reports false for a probe the form cannot carry — tcpSocket, grpc,
// headers, a named port, a command not run through sh -c — so an export never
// writes a spec that would change the probe when applied.
func probeFromConfig(m map[string]any) (probeDef, bool) {
	p := probeDef{full: true}
	for key, v := range m {
		switch key {
		case "httpGet":
			h, _ := v.(map[string]any)
			path, _ := h["path"].(string)
			port := jsonInt(h["port"])
			if len(h) != 2 || path == "" || port == 0 {
				return probeDef{}, false
			}
			p.Path, p.Port = path, port
		case "exec":
			e, _ := v.(map[string]any)
			argv, _ := e[keyCommand].([]any)
			if len(e) != 1 || len(argv) != 3 || argv[0] != "sh" || argv[1] != "-c" {
				return probeDef{}, false
			}
			p.Exec, _ = argv[2].(string)
		case keyInitialDelaySeconds:
			p.InitialDelaySeconds = jsonInt(v)
		case keyPeriodSeconds:
			p.PeriodSeconds = jsonInt(v)
		case keyTimeoutSeconds:
			p.TimeoutSeconds = jsonInt(v)
		case keySuccessThreshold:
			p.SuccessThreshold = jsonInt(v)
		case keyFailureThreshold:
			p.FailureThreshold = jsonInt(v)
		default:
			return probeDef{}, false
		}
	}
	if (p.Path == "") == (p.Exec == "") {
		return probeDef{}, false
	}
	return p, true
}
