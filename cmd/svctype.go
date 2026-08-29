package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"
)

// `patch app -p '{"svc": …}'` can also change the service type, but it is the
// wrong tool for it: the patch is a shallow merge into the top-level object, so
// a caller who sends only `{"svc":{"type":"LoadBalancer"}}` replaces the whole
// svc object and silently drops `ports` — and an app with no ports exposes
// nothing and gets no Service at all. Getting it right by hand means restating
// every port, which is exactly the sort of edge this command removes.
//
// The read-only members of svc (`internalAddress`, `externalIP`) are left as
// they were read. UpdateApp does not strip them and the write serializer
// ignores them, so preserving them keeps the diff to the one field that moved.

const cmdSvcType = "svc-type"

// The service types the platform accepts.
const (
	svcTypeClusterIP    = "ClusterIP"
	svcTypeLoadBalancer = "LoadBalancer"
	svcTypeNodePort     = "NodePort"
)

// svcTypes maps a lowercased argument onto its canonical spelling.
var svcTypes = map[string]string{
	strings.ToLower(svcTypeClusterIP):    svcTypeClusterIP,
	strings.ToLower(svcTypeLoadBalancer): svcTypeLoadBalancer,
	strings.ToLower(svcTypeNodePort):     svcTypeNodePort,
}

var (
	errSvcTypeArgs = errors.New("usage: darkubectl set svc-type NAME|ID ClusterIP|LoadBalancer|NodePort")
	errNoSvc       = errors.New("this app has no svc block, so there is no service type to set")
	errNoSvcPorts  = errors.New(
		"this app declares no ports, so a Service would expose nothing: set ports at creation first")
)

// errUnknownSvcType names what was passed and what is allowed.
func errUnknownSvcType(got string) error {
	return fmt.Errorf("%w: %q is not one of ClusterIP, LoadBalancer, NodePort", errSvcTypeArgs, got)
}

func newSetSvcTypeCommand() *cli.Command {
	return &cli.Command{
		Name:      cmdSvcType,
		Aliases:   []string{"svctype", "service-type"},
		Usage:     "Set the app's Kubernetes Service type",
		ArgsUsage: "NAME|ID ClusterIP|LoadBalancer|NodePort",
		Description: "  darkubectl set svc-type postgres-dev LoadBalancer\n" +
			"  darkubectl set svc-type postgres-dev ClusterIP --dry-run\n\n" +
			"Changes svc.type in place and leaves svc.ports alone. Prefer this over\n" +
			"`patch app -p '{\"svc\":…}'`, whose shallow merge replaces the whole svc object\n" +
			"and drops the ports with it, and over deleting and recreating the app, which is\n" +
			"what strands Helm releases.\n\n" +
			"LoadBalancer asks the cluster for a public address. Whatever the app answers on\n" +
			"is then reachable from the internet with no authentication in front of it, so it\n" +
			"is the wrong default for a database.\n\n" +
			"The address is reported afterwards. Note that the port it answers on is the\n" +
			"allocated nodePort, NOT the servicePort — Hamravesh fronts these with a shared\n" +
			"gateway, so a LoadBalancer app on servicePort 5432 is reached on, say, :30410.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagDryRun, Usage: "show what would change and exit without writing"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: setSvcTypeAction,
	}
}

func setSvcTypeAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errMissingAppRef
	}
	want, err := canonicalSvcType(cmd.Args().Get(1))
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

	apply := func(raw map[string]any) error { return applySvcType(raw, want) }

	if cmd.Bool(flagDryRun) {
		before, after, prepErr := c.PrepareAppUpdate(ctx, app.ID, apply)
		if prepErr != nil {
			return prepErr
		}
		fmt.Fprintf(os.Stdout, "app/%s: dry run, nothing was sent\n", app.Name)
		writeDiff(os.Stdout, diffApps(before, after))
		return nil
	}

	from, err := currentSvcType(ctx, c.GetApp, app.ID)
	if err != nil {
		return err
	}
	if from == want {
		fmt.Fprintf(os.Stdout, "app/%s service type is already %s\n", app.Name, want)
		return nil
	}

	fmt.Fprintf(os.Stderr, "About to change the service type of app %q (%s) in tenant %q: %s -> %s\n",
		app.Name, app.ID, c.Org, dash(from), want)
	if want == svcTypeLoadBalancer {
		fmt.Fprintf(os.Stderr,
			"warning: this asks the cluster for a public address. Every port the app declares\n"+
				"         becomes reachable from the internet, authenticated only by whatever the\n"+
				"         app itself asks for.\n")
	}
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	if _, err := c.UpdateApp(ctx, app.ID, apply); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s service type set to %s\n", app.Name, want)
	if want == svcTypeLoadBalancer {
		reportExposure(ctx, c.GetApp, app.ID)
	}
	return nil
}

// reportExposure prints where a freshly exposed app can now be reached.
//
// The public port is the allocated nodePort, not the servicePort: Hamravesh
// fronts LoadBalancer services with a shared gateway, so `<app-id>.hsvc.ir` on
// the nodePort is the endpoint that answers while the servicePort refuses.
// Confirmed against two apps on hamravesh-c11 on 2026-08-29 — worth printing,
// because every instinct says to dial the servicePort.
func reportExposure(
	ctx context.Context, get func(context.Context, string) (map[string]any, error), id string,
) {
	raw, err := get(ctx, id)
	if err != nil {
		return
	}
	host, endpoints := exposureOf(raw)
	if host == "" || len(endpoints) == 0 {
		fmt.Fprintf(os.Stderr,
			"note: no address has been allocated yet. Re-read it in a minute with\n"+
				"      `darkubectl describe app <name>` and look at svc.externalAddress.\n")
		return
	}
	fmt.Fprintf(os.Stderr, "\nreachable at:\n")
	for _, e := range endpoints {
		fmt.Fprintf(os.Stderr, "  %s:%d   (%s, container port %d)\n", host, e.nodePort, e.name, e.containerPort)
	}
	fmt.Fprintf(os.Stderr,
		"note: that is the nodePort, not the service port. The gateway is shared, so the\n"+
			"      service port itself refuses connections.\n")
}

// endpoint is one published port of an exposed app.
type endpoint struct {
	name          string
	nodePort      int
	containerPort int
}

// exposureOf reads the public hostname and the published ports off an app.
func exposureOf(raw map[string]any) (string, []endpoint) {
	svc, ok := raw["svc"].(map[string]any)
	if !ok {
		return "", nil
	}
	host, _ := svc["externalAddress"].(string)
	if host == "" {
		host, _ = svc["externalIP"].(string)
	}
	ports, ok := svc["ports"].(map[string]any)
	if !ok {
		return host, nil
	}

	out := make([]endpoint, 0, len(ports))
	for name, v := range ports {
		p, ok := v.(map[string]any)
		if !ok {
			continue
		}
		node, container := jsonInt(p["nodePort"]), jsonInt(p["containerPort"])
		if node == 0 {
			continue
		}
		out = append(out, endpoint{name: name, nodePort: node, containerPort: container})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return host, out
}

// jsonInt reads a number that decoded as float64, 0 if it is anything else.
func jsonInt(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

// canonicalSvcType maps a case-insensitive argument onto the spelling the API
// expects, so `loadbalancer` and `LoadBalancer` both work.
func canonicalSvcType(arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return "", errSvcTypeArgs
	}
	canonical, ok := svcTypes[strings.ToLower(strings.TrimSpace(arg))]
	if !ok {
		return "", errUnknownSvcType(arg)
	}
	return canonical, nil
}

// applySvcType sets svc.type on a normalized app object, leaving every other
// member of svc — ports above all — as it was.
func applySvcType(raw map[string]any, want string) error {
	svc, ok := raw["svc"].(map[string]any)
	if !ok {
		return errNoSvc
	}
	ports, ok := svc["ports"].(map[string]any)
	if !ok || len(ports) == 0 {
		return errNoSvcPorts
	}
	svc["type"] = want
	return nil
}

// currentSvcType reads the type the app has now, for the confirmation line.
// It takes the getter as an argument so the caller's client is not a dependency
// of the test.
func currentSvcType(
	ctx context.Context, get func(context.Context, string) (map[string]any, error), id string,
) (string, error) {
	raw, err := get(ctx, id)
	if err != nil {
		return "", err
	}
	return svcTypeOf(raw), nil
}

// svcTypeOf reads svc.type out of a raw app object, empty if absent.
func svcTypeOf(raw map[string]any) string {
	svc, ok := raw["svc"].(map[string]any)
	if !ok {
		return ""
	}
	t, _ := svc["type"].(string)
	return t
}
