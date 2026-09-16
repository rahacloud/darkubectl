package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	chclient "github.com/jpillora/chisel/client"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

// A tunnel is a chisel server (https://github.com/jpillora/chisel) running as an
// ordinary Darkube app, reached over its own ingress, forwarding TCP to anything
// the namespace can resolve. It exists because the alternatives are worse:
//
//   - `kubectl port-forward` needs cluster credentials. On Hamravesh that means
//     an OIDC exec plugin and a browser login, and the RBAC a Darkube user gets
//     is frequently read-only or absent — so the person who can deploy the app
//     often cannot reach it.
//   - `set svc-type LoadBalancer` works and needs nothing extra, but it puts the
//     port on the public internet with only the app's own auth in front. For a
//     database that is a poor trade.
//
// A tunnel needs neither: it rides the HTTP ingress that already exists, and it
// is reachable only by someone holding the credential minted at `tunnel up`.
//
// The client half is chisel's own Go package, linked in rather than executed.
// It used to shell out to a `chisel` binary on PATH, on the same reasoning as
// internal/kube shelling out to kubectl, and that reasoning does not survive the
// use case: the person who needs `tunnel connect` is usually a developer at a
// client company who has been handed a hostname and a credential. Telling them
// to fetch a second binary from GitHub releases -- frequently blocked from Iran,
// which is where every user of this tool is -- turns a one-line instruction into
// a support thread. Linking it in costs a few MB and a dependency tree to track;
// --chisel-binary still runs an external one for anybody who wants that.
//
// Two platform details shape the implementation. The chisel image is built FROM
// scratch, so there is no shell in it and `command` must name the binary
// directly — which is fine, because `command` is split on whitespace while
// `args` is not (see cmd/appargs.go), so the whole invocation goes in `command`.
// And secret envs are write-only: the API never reads a value back, so the
// credential is stored in the 0600 config file at creation or it is lost.

const (
	// The Docker Hub tags carry no "v" — `jpillora/chisel:v1.12.1` is a manifest
	// unknown, which surfaces as a Pending pod with an empty log and nothing in
	// the API naming the tag as the cause. Keep this in step with the module
	// version in go.mod: client and server negotiate a protocol version, and a
	// mismatch is refused at the handshake.
	chiselImage = "jpillora/chisel:1.12.1"
	// chiselBinaryPath is where the binary sits INSIDE that image, and it moves:
	// 1.10 shipped /app/chisel, 1.12 is ko-built and ships /ko-app/chisel with
	// WorkingDir /app. The image has an ENTRYPOINT, but it cannot be used —
	// `args` is never split, so the flags would arrive as one argv element — so
	// the path is named here and has to be rechecked whenever the tag moves.
	// Getting it wrong is a CreateContainerError, which the API reports as a
	// Pending pod with no logs at all.
	chiselBinaryPath = "/ko-app/chisel"
	// chiselPort is both the container port and the service port. The ingress
	// routes to the service port, and keeping them equal is the shape every
	// working app in the wild uses.
	chiselPort = 8080
	// chiselKeepalive keeps the websocket alive through proxies that drop idle
	// connections. Traefik's default idle timeout is well under a working day.
	chiselKeepalive = 25 * time.Second
	// chiselMaxRetryInterval caps the backoff between reconnects. chisel's own
	// default is five minutes, which is a long time to stare at a dead forward.
	chiselMaxRetryInterval = 30 * time.Second

	defaultTunnelName = "darkube-tunnel"
	defaultTunnelPlan = "1"
	tunnelUser        = "tunnel"
	// tunnelSecretBytes is the entropy in the generated password.
	tunnelSecretBytes = 24

	// flagNamespace is declared in get.go and reused here.
	flagName      = "name"
	flagPlan      = "plan"
	flagHost      = "host"
	flagAuth      = "auth"
	flagSubdomain = "subdomain"
	flagBinary    = "chisel-binary"

	// envTunnelAuth supplies the credential without touching the config file.
	envTunnelAuth = "DARKUBE_TUNNEL_AUTH"
)

var (
	errChiselMissing = errors.New(
		"the --chisel-binary given was not found on PATH; drop the flag to use the client " +
			"built into darkubectl, which needs nothing installed")
	errNoNamespace  = errors.New("--namespace is required")
	errNoForwards   = errors.New("at least one forward is required, e.g. 1433:mssql-dev.talaland-dev.svc:1433")
	errNoTunnelAuth = errors.New(
		"no stored credential for this tunnel: pass --auth user:pass, set $DARKUBE_TUNNEL_AUTH, " +
			"or re-run `darkubectl tunnel up` to mint a new one")
	errNoTunnelHost = errors.New(
		"this tunnel app has no hostname yet: give it one with `set subdomain` or `set domain --add`")
	errBadForward = errors.New("a forward is LOCALPORT:REMOTEHOST:REMOTEPORT")
)

func newTunnelCommand() *cli.Command {
	return &cli.Command{
		Name:  "tunnel",
		Usage: "Reach in-cluster services through a chisel tunnel",
		Description: "Runs a chisel server as an app and forwards local ports through it, so a\n" +
			"ClusterIP service can be reached from a laptop without exposing it publicly and\n" +
			"without cluster credentials.\n\n" +
			"  darkubectl tunnel up --namespace talaland-dev --subdomain tld-tunnel\n" +
			"  darkubectl tunnel connect 1433:mssql-dev.talaland-dev.svc:1433\n" +
			"  darkubectl tunnel down\n\n" +
			"Neither side needs anything installed: the chisel server runs as an app and the\n" +
			"client is linked into this binary. `connect --host … --auth …` needs no Darkube\n" +
			"account either, so a tunnel can be handed to someone outside the tenant.",
		Commands: []*cli.Command{
			newTunnelUpCommand(),
			newTunnelConnectCommand(),
			newTunnelDownCommand(),
		},
	}
}

// ---------------------------------------------------------------- tunnel up

func newTunnelUpCommand() *cli.Command {
	return &cli.Command{
		Name:  "up",
		Usage: "Create the chisel server app that backs the tunnel",
		Description: "Mints a credential, creates the app, and gives it a hostname. The credential\n" +
			"is printed once and saved to the config file — the API stores secret envs\n" +
			"write-only, so it cannot be recovered afterwards.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagNamespace, Usage: "namespace (project) to create the tunnel in", Required: true},
			&cli.StringFlag{Name: flagName, Value: defaultTunnelName, Usage: "app name for the tunnel server"},
			&cli.StringFlag{Name: flagPlan, Value: defaultTunnelPlan, Usage: "plan for the tunnel app"},
			&cli.StringFlag{Name: flagSubdomain, Usage: "platform subdomain label, e.g. `tld-tunnel` for tld-tunnel.darkube.app"},
			&cli.StringFlag{Name: flagHost, Usage: "a domain you own and have CNAMEd at the cluster"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: tunnelUpAction,
	}
}

func tunnelUpAction(ctx context.Context, cmd *cli.Command) error {
	namespace := cmd.String(flagNamespace)
	if namespace == "" {
		return errNoNamespace
	}
	name := cmd.String(flagName)
	subdomain, host := cmd.String(flagSubdomain), cmd.String(flagHost)

	c, cfg, err := buildClient(ctx, cmd)
	if err != nil {
		return err
	}
	nsID, err := resolveNamespaceID(ctx, c, namespace)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(ctx, c, cmd.String(flagPlan))
	if err != nil {
		return err
	}
	orgID, err := c.OrganizationID(ctx)
	if err != nil {
		return err
	}
	auth, err := generateTunnelAuth()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to create tunnel app %q in tenant %q: namespace=%s image=%s\n",
		name, c.Org, namespace, chiselImage)
	if subdomain == "" && host == "" {
		fmt.Fprintf(os.Stderr,
			"warning: no --subdomain or --host given, so the tunnel will have no ingress and\n"+
				"         nothing can connect to it. Add one later with `set subdomain`.\n")
	}
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	repo, tag := splitImage(chiselImage)
	if _, err := c.CreateApp(ctx, client.CreateAppInput{
		Name:           name,
		NamespaceID:    nsID,
		OrganizationID: orgID,
		PlanID:         planID,
		ImageRepo:      repo,
		ImageTag:       tag,
		Command:        chiselServerCommand(),
		Replicas:       1,
		SvcType:        "ClusterIP",
		Ports: map[string]client.Port{
			"main": {ContainerPort: chiselPort, ServicePort: chiselPort, Protocol: "TCP"},
		},
		SecretEnvs: []client.EnvVar{{Name: "AUTH", Value: auth}},
	}); err != nil {
		return explainCreateError(ctx, c, appSpec{Name: name, Namespace: namespace}, err)
	}
	fmt.Fprintf(os.Stdout, "app/%s created\n", name)

	// Store before attaching the hostname: if the domain call fails, the
	// credential is still the only unrecoverable part of what just happened.
	cfg.SetTunnelAuth(tunnelKey(c.Org, name), auth)
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save the tunnel credential (%v).\n"+
			"         Keep it yourself — the API will not give it back: %s\n", err, auth)
	}

	if err := attachTunnelHost(ctx, c, name, subdomain, host); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\ncredential: %s   (saved to the config file; the API cannot return it again)\n", auth)
	fmt.Fprintf(os.Stdout, "next:       darkubectl tunnel connect --name %s LOCALPORT:REMOTEHOST:REMOTEPORT\n", name)
	// The line worth copying: it names the hostname and the credential, so it
	// works for someone with neither a login nor this tool's config file. The
	// hostname is read back rather than assembled, because the base domain is the
	// cluster's and not ours to guess.
	if h := tunnelHostAfterCreate(ctx, c, name, host); h != "" {
		fmt.Fprintf(os.Stdout, "hand over:  darkubectl tunnel connect --host %s --auth %s LOCALPORT:REMOTEHOST:REMOTEPORT\n", h, auth)
		fmt.Fprintf(os.Stderr, "            (that form makes no API call, so the other side needs no Darkube account)\n")
	} else if subdomain != "" {
		fmt.Fprintf(os.Stderr, "            (the hostname is not readable yet; `get domains %s` prints it once it is)\n", name)
	}
	fmt.Fprintf(os.Stderr,
		"\nnote: the hostname and its certificate take a few minutes. `darkubectl wait app %s\n"+
			"      --for ready` waits for the app; the ingress may lag behind it.\n", name)
	return nil
}

// attachTunnelHost gives the freshly created app an ingress, by whichever of the
// two quite different hostname mechanisms the caller asked for.
func attachTunnelHost(ctx context.Context, c *client.Client, name, subdomain, host string) error {
	if subdomain == "" && host == "" {
		return nil
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.UpdateApp(ctx, app.ID, func(a map[string]any) error {
		if subdomain != "" {
			a["custom_subdomain_addr"] = subdomain
		}
		if host != "" {
			a["external_hosts"] = append(client.ExternalHosts(a), host)
		}
		a["enable_SSL"] = true
		return nil
	})
	return err
}

// tunnelHostAfterCreate reads the hostname back off the freshly created app. A
// custom --host is already known; a platform subdomain becomes a hostname the
// cluster decides, and it can lag the create by a moment, so a miss here is
// normal and not an error.
func tunnelHostAfterCreate(ctx context.Context, c *client.Client, name, host string) string {
	if host != "" {
		return host
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return ""
	}
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return ""
	}
	return tunnelHost(raw)
}

// ------------------------------------------------------------ tunnel connect

func newTunnelConnectCommand() *cli.Command {
	return &cli.Command{
		Name:      "connect",
		Usage:     "Forward local ports through the tunnel (runs the chisel client)",
		ArgsUsage: "LOCALPORT:REMOTEHOST:REMOTEPORT [more…]",
		Description: "  darkubectl tunnel connect 1433:mssql-dev.talaland-dev.svc:1433\n" +
			"  darkubectl tunnel connect 1433:mssql-dev.talaland-dev.svc:1433 5432:postgres-dev.talaland-dev.svc:5432\n" +
			"  darkubectl tunnel connect --host tld-tunnel.darkube.app --auth tunnel:… 27017:mongodb-stage.talaland-stage.svc:27017\n\n" +
			"REMOTEHOST is resolved inside the cluster, so it is the in-cluster address —\n" +
			"`get app <name>` reports it as svc.internalAddress. Runs in the foreground until\n" +
			"interrupted.\n\n" +
			"The chisel client is built in; nothing has to be installed. With --host and\n" +
			"--auth this makes no API call at all, so it works for someone who has the\n" +
			"hostname and the credential but no Darkube account — which is the normal case\n" +
			"for a developer at a client company. Without --host the tunnel app is looked up\n" +
			"by name, which needs a login and a tenant.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagName, Value: defaultTunnelName, Usage: "app name of the tunnel server"},
			&cli.StringFlag{Name: flagHost, Usage: "tunnel `hostname` (skips the API lookup, and with it the need to log in)"},
			&cli.StringFlag{Name: flagAuth, Usage: "user:pass (defaults to the stored credential or $" + envTunnelAuth + ")"},
			&cli.StringFlag{Name: flagBinary, Usage: "run this external chisel `binary` instead of the built-in client"},
		},
		Action: tunnelConnectAction,
	}
}

func tunnelConnectAction(ctx context.Context, cmd *cli.Command) error {
	forwards := cmd.Args().Slice()
	if len(forwards) == 0 {
		return errNoForwards
	}
	for _, f := range forwards {
		if err := validateForward(f); err != nil {
			return err
		}
	}

	host, auth, err := resolveConnectTarget(ctx, cmd)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "tunnelling through %s — press ctrl-c to stop\n", chiselServerURL(host))
	for _, f := range forwards {
		local, _, _ := strings.Cut(f, ":")
		fmt.Fprintf(os.Stderr, "  localhost:%s\n", local)
	}

	if bin := cmd.String(flagBinary); bin != "" {
		return runChiselBinary(ctx, bin, host, auth, forwards)
	}
	return runChiselClient(ctx, host, auth, forwards)
}

// resolveConnectTarget answers which hostname to dial and with which credential.
//
// The --host path deliberately touches no API: given a hostname and a credential
// this command is self-contained, which is what lets a tunnel be handed to
// somebody outside the tenant. The lookup path is the convenience for whoever
// ran `tunnel up`, whose config already holds both.
func resolveConnectTarget(ctx context.Context, cmd *cli.Command) (string, string, error) {
	if host := trimHost(cmd.String(flagHost)); host != "" {
		auth := cmd.String(flagAuth)
		if auth == "" {
			auth = os.Getenv(envTunnelAuth)
		}
		if auth == "" {
			return "", "", errNoTunnelAuth
		}
		return host, auth, nil
	}

	name := cmd.String(flagName)
	c, cfg, err := buildClient(ctx, cmd)
	if err != nil {
		return "", "", err
	}
	auth := resolveTunnelAuth(cmd, cfg, c.Org, name)
	if auth == "" {
		return "", "", errNoTunnelAuth
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return "", "", err
	}
	raw, err := c.GetApp(ctx, app.ID)
	if err != nil {
		return "", "", err
	}
	host := tunnelHost(raw)
	if host == "" {
		return "", "", errNoTunnelHost
	}
	return host, auth, nil
}

// trimHost accepts what someone actually pastes: a bare hostname, a host:port,
// or the whole URL they were sent, trailing slash and all. The scheme is kept
// when it is given, because it is not always https — a tunnel reached through a
// LoadBalancer answers plain HTTP on a nodePort, and chisel tunnels are
// encrypted by their own SSH layer either way, so http there is not a downgrade
// of the forwarded traffic.
func trimHost(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), "/")
}

// chiselServerURL gives the address a scheme if the caller did not.
func chiselServerURL(host string) string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "https://" + host
}

// runChiselClient runs chisel's client in this process. Start returns once the
// forwards are listening; Wait blocks until the connection loop gives up, which
// for a client with no retry limit means until the process is interrupted.
func runChiselClient(ctx context.Context, host, auth string, forwards []string) error {
	cl, err := chclient.NewClient(&chclient.Config{
		Server:           chiselServerURL(host),
		Auth:             auth,
		KeepAlive:        chiselKeepalive,
		MaxRetryInterval: chiselMaxRetryInterval,
		Remotes:          forwards,
	})
	if err != nil {
		return fmt.Errorf("chisel client: %w", err)
	}
	// chisel's own logging is the only sign the connection came up or dropped.
	cl.Info = true
	if err := cl.Start(ctx); err != nil {
		return err
	}
	return cl.Wait()
}

// runChiselBinary is the escape hatch: --chisel-binary runs an external client
// instead of the linked-in one, for a different version or a patched build.
func runChiselBinary(ctx context.Context, name, host, auth string, forwards []string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return errChiselMissing
	}
	// Hand over stdio: chisel logs to stderr and this runs until interrupted.
	// The binary is the --chisel-binary value resolved through exec.LookPath, and
	// the arguments are validated forwards, so this is the intended indirection.
	//nolint:gosec // G204: the binary is an explicit, PATH-resolved flag
	run := exec.CommandContext(ctx, bin, chiselClientArgs(host, auth, forwards)...)
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	return run.Run()
}

// chiselClientArgs builds the client invocation. Kept separate from the exec so
// the credential handling is testable without running anything.
func chiselClientArgs(host, auth string, forwards []string) []string {
	const fixedArgs = 6
	argv := make([]string, 0, fixedArgs+len(forwards))
	argv = append(argv, "client", "--auth", auth, "--keepalive", chiselKeepalive.String(), chiselServerURL(host))
	return append(argv, forwards...)
}

// resolveTunnelAuth picks the credential: --auth, then the environment, then the
// config entry written by `tunnel up`.
func resolveTunnelAuth(cmd *cli.Command, cfgAuth tunnelAuthStore, org, name string) string {
	if v := cmd.String(flagAuth); v != "" {
		return v
	}
	if v := os.Getenv(envTunnelAuth); v != "" {
		return v
	}
	return cfgAuth.TunnelAuth(tunnelKey(org, name))
}

// tunnelAuthStore is the slice of *config.Config this file reads, so the
// resolution order can be tested without a config file on disk.
type tunnelAuthStore interface {
	TunnelAuth(key string) string
}

// --------------------------------------------------------------- tunnel down

func newTunnelDownCommand() *cli.Command {
	return &cli.Command{
		Name:  "down",
		Usage: "Delete the tunnel server app",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagName, Value: defaultTunnelName, Usage: "app name of the tunnel server"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: tunnelDownAction,
	}
}

func tunnelDownAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.String(flagName)
	c, cfg, err := buildClient(ctx, cmd)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "About to delete tunnel app %q (%s) in tenant %q.\n", app.Name, app.ID, c.Org)
	fmt.Fprintf(os.Stderr,
		"note: deletion is asynchronous and can leave the Helm release behind holding the\n"+
			"      name, which would make `tunnel up` fail with SameHelmReleaseNameExists.\n"+
			"      `get orphans` finds that state.\n")
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	if err := c.DeleteApp(ctx, app.ID); err != nil {
		return err
	}
	cfg.ForgetTunnel(tunnelKey(c.Org, name))
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: app deleted but the stored credential could not be removed: %v\n", err)
	}
	fmt.Fprintf(os.Stdout, "app/%s deleted\n", app.Name)
	return nil
}

// ------------------------------------------------------------------ helpers

// chiselServerCommand is the whole server invocation, which goes in `command`
// rather than `args` because the platform splits the first on whitespace and
// passes the second through as a single argv element. The image is FROM scratch,
// so there is no shell to fall back on either way.
func chiselServerCommand() string {
	return fmt.Sprintf("%s server --port %d --keepalive %s", chiselBinaryPath, chiselPort, chiselKeepalive)
}

// tunnelKey namespaces a stored credential by tenant, since app names are only
// unique within one.
func tunnelKey(org, name string) string { return org + "/" + name }

// generateTunnelAuth mints a user:pass credential for the chisel server.
func generateTunnelAuth() (string, error) {
	buf := make([]byte, tunnelSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate tunnel credential: %w", err)
	}
	return tunnelUser + ":" + hex.EncodeToString(buf), nil
}

// tunnelHost picks the hostname to dial: the platform subdomain if the app has
// one, otherwise the first custom domain.
func tunnelHost(raw map[string]any) string {
	if h := rawString(raw, "custom_domain_address"); h != "" {
		return h
	}
	if hosts := client.ExternalHosts(raw); len(hosts) > 0 {
		return hosts[0]
	}
	return ""
}

// validateForward checks the shape chisel expects, because a typo here surfaces
// as a tunnel that connects and then silently refuses every connection.
func validateForward(spec string) error {
	// LOCALPORT:REMOTEHOST:REMOTEPORT — chisel accepts richer forms, but this is
	// the only one that reaches a ClusterIP service, so anything else is a typo.
	const forwardParts = 3

	parts := strings.Split(spec, ":")
	if len(parts) != forwardParts {
		return fmt.Errorf("%w: %q", errBadForward, spec)
	}
	local, host, remote := parts[0], parts[1], parts[2]
	if err := checkPort(local, spec); err != nil {
		return err
	}
	if err := checkPort(remote, spec); err != nil {
		return err
	}
	if host == "" {
		return fmt.Errorf("%w: %q has an empty remote host", errBadForward, spec)
	}
	if host == "localhost" || host == "127.0.0.1" {
		// Resolved in the tunnel pod, where it means the chisel container
		// itself — which is empty. Almost always a misread of the direction.
		return fmt.Errorf(
			"%w: %q forwards to the tunnel pod's own loopback, not yours. "+
				"Use the in-cluster address of the service, e.g. mssql-dev.talaland-dev.svc",
			errBadForward, spec)
	}
	return nil
}

func checkPort(p, spec string) error {
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%w: %q has an invalid port %q", errBadForward, spec, p)
	}
	return nil
}
