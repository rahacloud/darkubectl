package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/auth"
	"github.com/rahacloud/darkubectl/internal/config"
	"github.com/rahacloud/darkubectl/internal/wsexec"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

const (
	defaultContainer = "main"
	flagPod          = "pod"
	flagContainer    = "container"
	flagDebug        = "debug"
	ioBufferSize     = 32 * 1024
)

// Errors shared by the exec/terminal/login commands.
var (
	errNotLoggedIn = errors.New("not logged in: run `darkubectl login` first " +
		"(the terminal needs a JWT; the Api-key cannot open it)")
	errNoPod = errors.New("no running pods found for this app; " +
		"pass --pod, or run `darkubectl get pods <app>` to list them")
	errNoCommand   = errors.New("a command is required after `--`")
	errNotATTY     = errors.New("this command requires an interactive terminal")
	errInputClosed = errors.New("input closed")
)

// podFlags are shared by the exec and terminal commands.
func podFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: flagPod, Usage: "pod name (default: auto-detect from app state)"},
		&cli.StringFlag{Name: flagContainer, Aliases: []string{"c"}, Value: defaultContainer, Usage: "container name"},
		&cli.BoolFlag{Name: flagDebug, Usage: "log every websocket frame to stderr (protocol discovery)"},
	}
}

// accessToken obtains a Console access token for the exec websocket, trying, in
// order of precedence:
//
//  1. an access token used verbatim ($DARKUBE_ACCESS_TOKEN) — no refresh call;
//  2. a refresh token ($DARKUBE_REFRESH_TOKEN, then the stored one) minted into
//     an access token.
//
// A refresh token is as powerful as an interactive login, so supplying one (via
// `darkubectl login --refresh-token`, the env var, or config) is a full
// alternative to entering email/password/TOTP.
func accessToken(ctx context.Context, cmd *cli.Command, cfg *config.Config) (string, error) {
	if at := os.Getenv("DARKUBE_ACCESS_TOKEN"); at != "" {
		return at, nil
	}
	refresh := cfg.RefreshToken // already includes $DARKUBE_REFRESH_TOKEN via koanf
	if refresh == "" {
		return "", errNotLoggedIn
	}
	a := auth.New(resolveBaseURL(cmd, cfg))
	defer func() { _ = a.Close() }()
	return a.Refresh(ctx, refresh)
}

// selectPod resolves the target pod and container (in that order) for an app,
// using --pod if given, otherwise the app-state websocket.
func selectPod(ctx context.Context, cmd *cli.Command, opts appstate.Options) (string, string, error) {
	if p := cmd.String(flagPod); p != "" {
		return p, containerFlag(cmd), nil
	}
	pods, _, err := appstate.FetchPods(ctx, opts)
	if err != nil {
		return "", "", err
	}
	switch len(pods) {
	case 1:
		return pods[0].Name, pickContainer(cmd, pods[0]), nil
	case 0:
		return "", "", errNoPod
	default:
		names := make([]string, len(pods))
		for i, p := range pods {
			names[i] = p.Name
		}
		return "", "", fmt.Errorf("%w: choose one with --pod: %s", errNoPod, strings.Join(names, ", "))
	}
}

func containerFlag(cmd *cli.Command) string {
	if c := cmd.String(flagContainer); c != "" {
		return c
	}
	return defaultContainer
}

// pickContainer honors an explicit --container, else prefers "main" among the
// pod's containers, else the pod's first container.
func pickContainer(cmd *cli.Command, pod appstate.Pod) string {
	if cmd.IsSet(flagContainer) {
		return containerFlag(cmd)
	}
	if len(pod.Containers) > 0 {
		if slices.Contains(pod.Containers, defaultContainer) {
			return defaultContainer
		}
		return pod.Containers[0]
	}
	return defaultContainer
}

// execTarget is a dialed exec session plus display metadata.
type execTarget struct {
	sess      *wsexec.Session
	appName   string
	pod       string
	container string
}

// dialExec resolves the app, selects a pod/container, mints a Console access
// token, and opens the exec websocket.
func dialExec(ctx context.Context, cmd *cli.Command, nameOrID string) (*execTarget, error) {
	c, cfg, err := buildClient(ctx, cmd)
	if err != nil {
		return nil, err
	}
	app, err := c.ResolveApp(ctx, nameOrID)
	if err != nil {
		return nil, err
	}
	access, err := accessToken(ctx, cmd, cfg)
	if err != nil {
		return nil, err
	}
	baseURL, org := resolveBaseURL(cmd, cfg), resolveOrg(cmd, cfg)

	pod, container, err := selectPod(ctx, cmd, appstate.Options{
		BaseURL:     baseURL,
		AccessToken: access,
		Org:         org,
		AppID:       app.ID,
		Debug:       cmd.Bool(flagDebug),
	})
	if err != nil {
		return nil, err
	}
	sess, err := wsexec.Dial(ctx, wsexec.Options{
		BaseURL:     baseURL,
		AccessToken: access,
		Org:         org,
		AppID:       app.ID,
		PodName:     pod,
		Container:   container,
		Debug:       cmd.Bool(flagDebug),
	})
	if err != nil {
		return nil, err
	}
	return &execTarget{sess: sess, appName: app.Name, pod: pod, container: container}, nil
}

// prompt writes a label to stderr and reads a trimmed line from stdin.
func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	line, ok := readLine()
	if !ok {
		return "", errInputClosed
	}
	return strings.TrimSpace(line), nil
}

// promptPassword reads a line from stdin without echoing it.
func promptPassword(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
