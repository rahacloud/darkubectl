package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

const (
	flagFile        = "file"
	defaultImageTag = "latest"
	uuidLength      = 36
)

var errIncompleteSpec = errors.New("incomplete app spec: name, namespace, plan and image are required")

// appSpec is the friendly Docker-image app definition shared by all three input
// modes (flags, --file YAML, interactive).
type appSpec struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"` // project name or id
	Plan      string `yaml:"plan"`      // plan name, code name, or id
	Image     string `yaml:"image"`     // repo:tag
	Port      int    `yaml:"port"`
	Replicas  int    `yaml:"replicas"`
	EnableSSL bool   `yaml:"ssl"`
	Command   string `yaml:"command"` // entrypoint override
	Args      string `yaml:"args"`
}

func newCreateCommand() *cli.Command {
	return &cli.Command{
		Name:     "create",
		Usage:    "Create resources",
		Commands: []*cli.Command{newCreateAppCommand()},
	}
}

func newCreateAppCommand() *cli.Command {
	return &cli.Command{
		Name:      cmdApp,
		Aliases:   []string{aliasApp},
		Usage:     "Create an app from a Docker image",
		ArgsUsage: "[NAME]",
		Description: "Provide the app three ways: command-line flags, a YAML spec (--file),\n" +
			"or interactively (--interactive, or run with no flags on a terminal).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFile, Aliases: []string{"f"}, Usage: "create from a YAML spec file (- for stdin)"},
			&cli.StringFlag{Name: "namespace", Aliases: []string{"ns"}, Usage: "namespace (project) name or id"},
			&cli.StringFlag{Name: "plan", Usage: "plan name, code name, or id"},
			&cli.StringFlag{Name: "image", Usage: "docker image (repo:tag)"},
			&cli.IntFlag{Name: "port", Usage: "container port"},
			&cli.IntFlag{Name: flagReplicas, Value: 1, Usage: "replica count"},
			&cli.StringFlag{Name: "command", Usage: "container command (entrypoint override)"},
			&cli.StringFlag{Name: "args", Usage: "container args"},
			&cli.BoolFlag{Name: "enable-ssl", Usage: "enable SSL"},
			&cli.BoolFlag{Name: flagInteractive, Aliases: []string{"i"}, Usage: "prompt for each field"},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: createAppAction,
	}
}

func createAppAction(ctx context.Context, cmd *cli.Command) error {
	spec, err := gatherAppSpec(cmd)
	if err != nil {
		return err
	}
	if !spec.complete() {
		return errIncompleteSpec
	}

	c, _, err := buildClient(ctx, cmd)
	if err != nil {
		return err
	}

	nsID, err := resolveNamespaceID(ctx, c, spec.Namespace)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(ctx, c, spec.Plan)
	if err != nil {
		return err
	}
	repo, tag := splitImage(spec.Image)

	fmt.Fprintf(os.Stderr, "About to create app %q in tenant %q: namespace=%s plan=%s image=%s:%s replicas=%d\n",
		spec.Name, c.Org, spec.Namespace, spec.Plan, repo, tag, spec.Replicas)
	if !cmd.Bool(flagYes) && !confirm("Proceed?") {
		return errAborted
	}

	_, err = c.CreateApp(ctx, client.CreateAppInput{
		Name:        spec.Name,
		NamespaceID: nsID,
		PlanID:      planID,
		ImageRepo:   repo,
		ImageTag:    tag,
		Port:        spec.Port,
		Replicas:    spec.Replicas,
		EnableSSL:   spec.EnableSSL,
		Command:     spec.Command,
		Args:        spec.Args,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "app/%s created\n", spec.Name)
	return nil
}

func (s appSpec) complete() bool {
	return s.Name != "" && s.Namespace != "" && s.Plan != "" && s.Image != ""
}

// gatherAppSpec builds the spec from --file, interactive prompts, or flags.
func gatherAppSpec(cmd *cli.Command) (appSpec, error) {
	if f := cmd.String(flagFile); f != "" {
		return loadAppSpecFile(f)
	}

	flagsGiven := cmd.Args().First() != "" || cmd.String("image") != ""
	if cmd.Bool(flagInteractive) || (!flagsGiven && term.IsTerminal(int(os.Stdin.Fd()))) {
		return promptAppSpec()
	}

	return appSpec{
		Name:      cmd.Args().First(),
		Namespace: cmd.String("namespace"),
		Plan:      cmd.String("plan"),
		Image:     cmd.String("image"),
		Port:      cmd.Int("port"),
		Replicas:  cmd.Int(flagReplicas),
		EnableSSL: cmd.Bool("enable-ssl"),
		Command:   cmd.String("command"),
		Args:      cmd.String("args"),
	}, nil
}

func loadAppSpecFile(path string) (appSpec, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path) //nolint:gosec // path is a user-provided spec file
	}
	if err != nil {
		return appSpec{}, fmt.Errorf("read spec %s: %w", path, err)
	}
	var s appSpec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return appSpec{}, fmt.Errorf("parse spec %s: %w", path, err)
	}
	return s, nil
}

func promptAppSpec() (appSpec, error) {
	var s appSpec
	var err error
	if s.Name, err = prompt("App name: "); err != nil {
		return s, err
	}
	if s.Namespace, err = prompt("Namespace (project) name or id: "); err != nil {
		return s, err
	}
	if s.Plan, err = prompt("Plan (name, code name, or id): "); err != nil {
		return s, err
	}
	if s.Image, err = prompt("Docker image (repo:tag): "); err != nil {
		return s, err
	}
	if s.Command, err = prompt("Command (entrypoint override, blank for image default): "); err != nil {
		return s, err
	}
	if s.Port, err = promptInt("Container port (blank for none): ", 0); err != nil {
		return s, err
	}
	if s.Replicas, err = promptInt("Replicas [1]: ", 1); err != nil {
		return s, err
	}
	ssl, err := prompt("Enable SSL? [y/N]: ")
	if err != nil {
		return s, err
	}
	s.EnableSSL = isYes(ssl)
	return s, nil
}

func promptInt(label string, def int) (int, error) {
	raw, err := prompt(label)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", raw, err)
	}
	return n, nil
}

func isYes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// resolveNamespaceID maps a namespace name or numeric id to its id.
func resolveNamespaceID(ctx context.Context, c *client.Client, nameOrID string) (int, error) {
	if id, err := strconv.Atoi(nameOrID); err == nil {
		return id, nil
	}
	namespaces, err := c.NamespacesFromApps(ctx)
	if err != nil {
		return 0, err
	}
	for _, n := range namespaces {
		if n.Name == nameOrID {
			return n.ID, nil
		}
	}
	return 0, fmt.Errorf("no namespace named %q in tenant %q", nameOrID, c.Org)
}

// resolvePlanID maps a plan id, code name, or name to its id.
func resolvePlanID(ctx context.Context, c *client.Client, ref string) (string, error) {
	if len(ref) == uuidLength && strings.Count(ref, "-") == 4 {
		return ref, nil // already an id
	}
	plans, err := c.ListPlans(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range plans {
		if p.CodeName == ref || p.Name == ref || p.ID == ref {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no plan matching %q", ref)
}

// splitImage splits "repo:tag" into repo and tag, defaulting the tag to "latest".
func splitImage(image string) (string, string) {
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash {
		return image[:colon], image[colon+1:]
	}
	return image, defaultImageTag
}
