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
	flagGitRepo     = "git-repo"
	defaultImageTag = "latest"
	uuidLength      = 36
)

var errIncompleteSpec = errors.New(
	"incomplete app spec: name, namespace, plan and one of image or git.repoUrl are required")

// appSpec is the friendly Docker-image app definition shared by all three input
// modes (flags, --file YAML, interactive).
type appSpec struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"` // project name or id
	Plan      string `yaml:"plan"`      // plan name, code name, or id
	Image     string `yaml:"image"`     // repo:tag
	Replicas  int    `yaml:"replicas"`

	// Command is split on whitespace by the platform; Args is not. Both accept
	// either a string or a list. See appargs.go — that asymmetry is the sharpest
	// edge in this API and costs a crash loop to rediscover.
	Command shellWords `yaml:"command"` // entrypoint override
	Args    shellWords `yaml:"args"`

	// The fields below are nested, so they are --file/-f only, where the shape
	// is expressible. Env and domains can also be changed later with
	// `set env` / `set domain`. See the example in `create app --help`.
	SvcType    string                 `yaml:"svcType"`
	Ports      map[string]client.Port `yaml:"ports"`
	Disk       *client.Disk           `yaml:"disk"`
	Envs       []client.EnvVar        `yaml:"envs"`
	SecretEnvs []client.EnvVar        `yaml:"secretEnvs"`

	// Git is set for apps Darkube builds from a repository rather than pulling a
	// prebuilt image. See gitSpec.
	Git *gitSpec `yaml:"git"`
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
		Usage:     "Create an app from a Docker image or a git repository",
		ArgsUsage: "[NAME]",
		Description: "Provide the app three ways: command-line flags, a YAML spec (--file),\n" +
			"or interactively (--interactive, or run with no flags on a terminal).\n\n" +
			"An app is sourced either from a prebuilt image (--image) or from a repository\n" +
			"Darkube builds for you (--git-repo), never both. The git form is what the console\n" +
			"calls creation_method git_repo_url; Darkube builds the image, pushes it to the\n" +
			"tenant registry and fills in image_repo/image_tag itself.\n\n" +
			"On --command and --args, which are not symmetric:\n" +
			"  command is SPLIT on whitespace   -> [\"/bin/sh\", \"-c\"]\n" +
			"  args    is NOT split, ever       -> [\"<the whole string>\"]\n" +
			"So a flag belongs in command, not args. `--command /bin/sh --args \"-c echo hi\"`\n" +
			"hands the container one argument \"-c echo hi\" and crash-loops with\n" +
			"`/bin/sh: illegal option -`; `--command \"/bin/sh -c\" --args \"<script>\"` works.\n" +
			"Both fields accept a YAML list in a spec file.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFile, Aliases: []string{"f"}, Usage: "create from a YAML spec file (- for stdin)"},
			&cli.StringFlag{Name: "namespace", Aliases: []string{"ns"}, Usage: "namespace (project) name or id"},
			&cli.StringFlag{Name: "plan", Usage: "plan name, code name, or id"},
			&cli.StringFlag{Name: "image", Usage: "docker image (repo:tag); omit when building from git"},
			&cli.IntFlag{Name: flagReplicas, Value: 1, Usage: "replica count"},
			&cli.StringFlag{Name: "command", Usage: "container command (entrypoint override); SPLIT on whitespace"},
			&cli.StringFlag{Name: "args", Usage: "container args; passed as ONE argument, never split"},
			&cli.StringFlag{Name: flagGitRepo, Usage: "build from this git repository instead of an image"},
			&cli.StringFlag{Name: "git-branch", Usage: "branch to build (default main)"},
			&cli.StringFlag{Name: "git-dockerfile", Usage: "Dockerfile path within the repo (default ./Dockerfile)"},
			&cli.StringFlag{Name: "git-context", Usage: "docker build context within the repo (default .)"},
			&cli.StringFlag{Name: "git-provider", Usage: "Github or Gitlab (default: inferred from the URL)"},
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
	if err := spec.validate(); err != nil {
		return err
	}
	// Warnings, not errors: each describes a shape that is legal but almost
	// certainly not what was meant. They go to stderr before the confirmation
	// prompt so there is still a chance to answer no.
	for _, w := range entrypointWarnings(spec.Command.String(), spec.Args.String()) {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
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
	orgID, err := c.OrganizationID(ctx)
	if err != nil {
		return err
	}
	var repo, tag, source string
	if spec.Git != nil {
		source = "git=" + spec.Git.describe()
	} else {
		repo, tag = splitImage(spec.Image)
		source = fmt.Sprintf("image=%s:%s", repo, tag)
	}

	fmt.Fprintf(os.Stderr, "About to create app %q in tenant %q: namespace=%s plan=%s %s replicas=%d\n",
		spec.Name, c.Org, spec.Namespace, spec.Plan, source, spec.Replicas)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	_, err = c.CreateApp(ctx, client.CreateAppInput{
		Name:           spec.Name,
		NamespaceID:    nsID,
		OrganizationID: orgID,
		PlanID:         planID,
		ImageRepo:      repo,
		ImageTag:       tag,
		Command:        spec.Command.String(),
		Args:           spec.Args.String(),
		Replicas:       spec.Replicas,
		SvcType:        spec.SvcType,
		Ports:          spec.Ports,
		Disk:           spec.Disk,
		Envs:           spec.Envs,
		SecretEnvs:     spec.SecretEnvs,
		Git:            spec.Git.toClient(),
	})
	if err != nil {
		return explainCreateError(ctx, c, spec, err)
	}
	fmt.Fprintf(os.Stdout, "app/%s created\n", spec.Name)
	return nil
}

// explainCreateError writes guidance for the API's two name-collision codes to
// stderr and returns the original error. Both codes arrive with a Persian
// `detail` saying only "an app with this name exists, change the name or the
// namespace", which is actively misleading when no such app exists.
func explainCreateError(ctx context.Context, c *client.Client, spec appSpec, err error) error {
	switch client.ErrorCode(err) {
	case client.CodeDuplicateReleaseAndNamespace:
		fmt.Fprintf(os.Stderr,
			"\nAn app named %q already exists in this namespace. Pick another name, or delete it first.\n",
			spec.Name)

	case client.CodeTerminatingApp:
		fmt.Fprintf(os.Stderr,
			"\nAn app named %q is still being deleted. Deletion is asynchronous — wait and retry.\n"+
				"`darkubectl wait app %s --for deleted` blocks until it is gone.\n",
			spec.Name, spec.Name)

	case client.CodeGithubAuth, client.CodeGitlabAuth:
		// The Persian detail says only "unauthorized access; install the Darkube
		// app in your account's Integration section". Worth restating, because
		// nothing about the command is wrong: building from a repository has an
		// account-level prerequisite that pulling an image does not.
		fmt.Fprintf(os.Stderr,
			"\nDarkube cannot read that repository. Building from git requires connecting the git\n"+
				"provider to your Hamravesh account first: install the Darkube app under the\n"+
				"Integration section of your GitHub/GitLab account and grant it access to this\n"+
				"repository. The app spec itself was accepted — only the authorization is missing.\n\n"+
				"Creating from a prebuilt image (--image) has no such prerequisite.\n")

	case client.CodeSameHelmReleaseName:
		// Distinguish a real name clash from an orphaned Helm release: if no app
		// of this name exists, the record is gone but the release is not.
		if _, resolveErr := c.ResolveApp(ctx, spec.Name); resolveErr == nil {
			fmt.Fprintf(os.Stderr,
				"\nAn app named %q already exists in this tenant. Pick another name, or delete it first.\n",
				spec.Name)
			break
		}
		fmt.Fprintf(os.Stderr,
			"\nNo app named %q exists, so its Helm release has outlived the app record — a known\n"+
				"consequence of deleting an app. The name cannot be reused until that release is gone,\n"+
				"and the API exposes no way to remove it: there is no force/overwrite flag on create\n"+
				"and no release endpoint. Clear it from the Darkube console, or ask Hamravesh support\n"+
				"to drop the orphaned release, then retry.\n\n"+
				"To avoid this: prefer editing an app in place (`set env`, `set domain`, `scale`,\n"+
				"`patch`) over delete-and-recreate, which is what strands the release.\n",
			spec.Name)
	}
	return err
}

// complete reports whether the spec names everything the API requires. An app
// is sourced either from a prebuilt image or from a repository Darkube builds,
// so exactly one of those must be present.
func (s appSpec) complete() bool {
	hasSource := s.Image != "" || (s.Git != nil && s.Git.RepoURL != "")
	return s.Name != "" && s.Namespace != "" && s.Plan != "" && hasSource
}

// validate rejects the spec shapes the API cannot express, before sending them.
func (s appSpec) validate() error {
	if err := validateArgs(s.Args); err != nil {
		return err
	}
	if s.Git == nil {
		return nil
	}
	if s.Image != "" {
		return errGitAndImage
	}
	return s.Git.validate()
}

// gatherAppSpec builds the spec from --file, interactive prompts, or flags.
func gatherAppSpec(cmd *cli.Command) (appSpec, error) {
	if f := cmd.String(flagFile); f != "" {
		return loadAppSpecFile(f)
	}

	flagsGiven := cmd.Args().First() != "" || cmd.String("image") != "" || cmd.String(flagGitRepo) != ""
	if cmd.Bool(flagInteractive) || (!flagsGiven && term.IsTerminal(int(os.Stdin.Fd()))) {
		return promptAppSpec()
	}

	return appSpec{
		Name:      cmd.Args().First(),
		Namespace: cmd.String("namespace"),
		Plan:      cmd.String("plan"),
		Image:     cmd.String("image"),
		Replicas:  cmd.Int(flagReplicas),
		Command:   newShellWords(cmd.String("command")),
		Args:      newShellWords(cmd.String("args")),
		Git:       gitSpecFromFlags(cmd),
	}, nil
}

// gitSpecFromFlags builds the git block from --git-* flags, or nil if none were
// given.
func gitSpecFromFlags(cmd *cli.Command) *gitSpec {
	repo := cmd.String(flagGitRepo)
	if repo == "" {
		return nil
	}
	provider := cmd.String("git-provider")
	if provider == "" {
		provider = guessProvider(repo)
	}
	return &gitSpec{
		RepoURL:    repo,
		Branch:     cmd.String("git-branch"),
		Provider:   provider,
		Dockerfile: cmd.String("git-dockerfile"),
		Context:    cmd.String("git-context"),
	}
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
	command, err := prompt("Command (entrypoint override, blank for image default): ")
	if err != nil {
		return s, err
	}
	s.Command = newShellWords(command)
	if s.Replicas, err = promptInt("Replicas [1]: ", 1); err != nil {
		return s, err
	}
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

// resolveNamespaceID maps a namespace name or numeric id to its id.
func resolveNamespaceID(ctx context.Context, c *client.Client, nameOrID string) (int, error) {
	if id, err := strconv.Atoi(nameOrID); err == nil {
		return id, nil
	}
	// Namespaces, not NamespacesFromApps: creating the *first* app in a new
	// project is precisely the case the apps-derived list cannot serve.
	namespaces, err := c.Namespaces(ctx)
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
