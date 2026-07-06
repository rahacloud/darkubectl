package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rahacloud/darkubectl/internal/auth"
	"github.com/rahacloud/darkubectl/internal/client"
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
	errNoPod       = errors.New("could not determine a pod; pass --pod (live pods are not exposed over REST)")
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

// accessToken mints a fresh Console access token from the stored refresh token.
func accessToken(ctx context.Context, cmd *cli.Command, cfg *config.Config) (string, error) {
	if cfg.RefreshToken == "" {
		return "", errNotLoggedIn
	}
	a := auth.New(resolveBaseURL(cmd, cfg))
	defer func() { _ = a.Close() }()
	return a.Refresh(ctx, cfg.RefreshToken)
}

// selectPod resolves the target pod and container (in that order) for an app.
func selectPod(ctx context.Context, c *client.Client, appID string, cmd *cli.Command) (string, string, error) {
	container := cmd.String(flagContainer)
	if container == "" {
		container = defaultContainer
	}
	if p := cmd.String(flagPod); p != "" {
		return p, container, nil
	}
	pods, err := c.ListPods(ctx, appID)
	if err != nil {
		return "", "", err
	}
	switch len(pods) {
	case 1:
		return pods[0].Name, container, nil
	case 0:
		return "", "", errNoPod
	default:
		names := make([]string, len(pods))
		for i, p := range pods {
			names[i] = p.Name
		}
		return "", "", fmt.Errorf("%w: choose one of: %s", errNoPod, strings.Join(names, ", "))
	}
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
	c, cfg, err := buildClient(cmd)
	if err != nil {
		return nil, err
	}
	app, err := c.ResolveApp(ctx, nameOrID)
	if err != nil {
		return nil, err
	}
	pod, container, err := selectPod(ctx, c, app.ID, cmd)
	if err != nil {
		return nil, err
	}
	access, err := accessToken(ctx, cmd, cfg)
	if err != nil {
		return nil, err
	}
	sess, err := wsexec.Dial(ctx, wsexec.Options{
		BaseURL:     resolveBaseURL(cmd, cfg),
		AccessToken: access,
		Org:         resolveOrg(cmd, cfg),
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
