package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const (
	flagRegistry  = "registry"
	flagKeep      = "keep"
	flagKeepCount = "keep-count"
	flagKeepDays  = "keep-days"

	argImageUsage = "IMAGE"

	// shortDigest is how much of a sha256 digest a table shows.
	shortDigest = 19
	// maxTagsShown caps the tags a table cell lists.
	maxTagsShown = 3
)

var (
	errMissingImage     = errors.New("an IMAGE argument is required (as listed by `registry images`)")
	errNoSuchRegistry   = errors.New("no such registry")
	errNoSuchImage      = errors.New("no such image")
	errAmbiguousImage   = errors.New("image name is ambiguous")
	errNoSuchTag        = errors.New("no such tag")
	errNoSuchDigest     = errors.New("no such digest")
	errKeepRequired     = errors.New("--keep must be at least 1")
	errKeepChoice       = errors.New("give exactly one of --keep-count or --keep-days")
	errNoSuchGCStrategy = errors.New("no retention rule for that scope")
)

func newRegistryCommand() *cli.Command {
	registryFlag := &cli.StringFlag{Name: flagRegistry, Value: client.DefaultRegistryName, Usage: "registry to act on, by name"}
	yesFlag := &cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}
	return &cli.Command{
		Name:    "registry",
		Aliases: []string{"registries", "reg"},
		Usage:   "Browse and clean up the tenant's container registry",
		Description: "The tenant's space on registry.hamdocker.ir, where Darkube pushes the images it\n" +
			"builds for git-backed apps. Every build adds an image and nothing removes them,\n" +
			"which is what `prune` and `gc` are for.\n\n" +
			"IMAGE is a repository name as `registry images` lists it; a full reference\n" +
			"such as registry.hamdocker.ir/<tenant>/my-api is accepted too.",
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "List the registries the tenant can push to, with their storage use",
				Action:  registryListAction,
			},
			{
				Name:   "images",
				Usage:  "List the image repositories in a registry",
				Flags:  []cli.Flag{registryFlag},
				Action: registryImagesAction,
			},
			{
				Name:      "tags",
				Aliases:   []string{"digests"},
				Usage:     "List an image's manifests and the tags on each, newest first",
				ArgsUsage: argImageUsage,
				Flags:     []cli.Flag{registryFlag},
				Action:    registryTagsAction,
			},
			{
				Name:      "delete",
				Aliases:   []string{"rm"},
				Usage:     "Delete a whole image, one tag, or one manifest",
				ArgsUsage: "IMAGE | IMAGE:TAG | IMAGE@sha256:…",
				Description: "  darkubectl registry delete my-api:old-tag     # untag; the manifest stays\n" +
					"  darkubectl registry delete my-api@sha256:ab…  # the manifest and all its tags\n" +
					"  darkubectl registry delete my-api             # the whole repository",
				Flags:  []cli.Flag{registryFlag, yesFlag},
				Action: registryDeleteAction,
			},
			{
				Name:      "prune",
				Usage:     "Delete all but the newest manifests of an image, sparing any a deployed app runs",
				ArgsUsage: argImageUsage,
				Description: "  darkubectl registry prune my-api --keep 5 --dry-run\n\n" +
					"Keeps the --keep most recently pushed manifests and deletes the rest. A manifest\n" +
					"carrying a tag that an app in the tenant currently runs is never deleted,\n" +
					"however old it is. For a standing rule the platform enforces itself, see `gc`.",
				Flags: []cli.Flag{
					registryFlag, yesFlag,
					&cli.IntFlag{Name: flagKeep, Value: defaultBuildLimit, Usage: "how many of the newest manifests to keep"},
					&cli.BoolFlag{Name: flagDryRun, Usage: "list what would be deleted and exit"},
				},
				Action: registryPruneAction,
			},
			{
				Name:  "password",
				Usage: "Print the registry's password, for docker login (prints a secret)",
				Description: "  darkubectl registry password | docker login registry.hamdocker.ir -u <user> --password-stdin\n\n" +
					"The username is in `registry list`.",
				Flags:  []cli.Flag{registryFlag},
				Action: registryPasswordAction,
			},
			newRegistryGCCommand(registryFlag),
		},
	}
}

func newRegistryGCCommand(registryFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:    "gc",
		Aliases: []string{"retention"},
		Usage:   "Show or set the retention rules the platform prunes images by",
		Description: "  darkubectl registry gc                          # list the rules\n" +
			"  darkubectl registry gc set --keep-count 20       # every image in the registry\n" +
			"  darkubectl registry gc set my-api --keep-days 30 # one image\n" +
			"  darkubectl registry gc unset my-api\n\n" +
			"A rule on one image overrides the registry-wide rule for that image.",
		Action: registryGCListAction,
		Commands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "Set the retention rule for the registry, or for one image",
				ArgsUsage: "[" + argImageUsage + "]",
				Flags: []cli.Flag{
					registryFlag,
					&cli.IntFlag{Name: flagKeepCount, Usage: "keep this many of the newest images"},
					&cli.IntFlag{Name: flagKeepDays, Usage: "keep images pushed within this many days"},
				},
				Action: registryGCSetAction,
			},
			{
				Name:      "unset",
				Usage:     "Remove the retention rule of the registry, or of one image",
				ArgsUsage: "[" + argImageUsage + "]",
				Flags:     []cli.Flag{registryFlag},
				Action:    registryGCUnsetAction,
			},
		},
	}
}

func registryListAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	regs, err := c.ListRegistries(ctx)
	if err != nil {
		return err
	}
	for i := range regs {
		if regs[i].StorageUsageBytes != nil {
			continue
		}
		// Not every registry reports its usage; a failure here costs one cell.
		if n, err := c.RegistryStorageUsage(ctx, regs[i].ID); err == nil {
			regs[i].StorageUsageBytes = &n
		}
	}
	if handled, err := output.Structured(os.Stdout, format, regs); handled {
		return err
	}
	rows := make([][]string, 0, len(regs))
	for _, r := range regs {
		usage := "-"
		if r.StorageUsageBytes != nil {
			usage = humanBytes(*r.StorageUsageBytes)
		}
		rows = append(rows, []string{r.Name, dash(r.PushPrefix()), dash(r.Username), usage})
	}
	return output.StyledTable(os.Stdout, []string{colName, "PUSH TO", "USERNAME", "USAGE"}, rows, nil)
}

func registryImagesAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, reg, err := resolveRegistry(ctx, cmd)
	if err != nil {
		return err
	}
	repos, err := c.ListRepositories(ctx, reg.ID)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, repos); handled {
		return err
	}
	if format == output.Name {
		for _, r := range repos {
			fmt.Fprintln(os.Stdout, r.Name)
		}
		return nil
	}
	rows := make([][]string, 0, len(repos))
	for _, r := range repos {
		rows = append(rows, []string{r.Name, dash(r.RecentTag), ageOf(r.LastPushed)})
	}
	return output.StyledTable(os.Stdout, []string{colName, "RECENT TAG", "PUSHED"}, rows, nil)
}

func registryTagsAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, reg, repo, err := resolveImageArg(ctx, cmd, cmd.Args().First())
	if err != nil {
		return err
	}
	digests, err := c.ListDigests(ctx, reg.ID, repo)
	if err != nil {
		return err
	}
	sortNewestFirst(digests)
	if handled, err := output.Structured(os.Stdout, format, digests); handled {
		return err
	}
	rows := make([][]string, 0, len(digests))
	for _, d := range digests {
		rows = append(rows, []string{
			cut(d.Digest, shortDigest), dash(tagList(d.Tags)), humanBytes(d.Size), ageOf(d.LastPushed),
		})
	}
	return output.StyledTable(os.Stdout, []string{"DIGEST", "TAGS", "SIZE", "PUSHED"}, rows, nil)
}

func registryDeleteAction(ctx context.Context, cmd *cli.Command) error {
	arg := cmd.Args().First()
	image, digest, _ := strings.Cut(arg, "@")
	var tag string
	if digest == "" {
		image, tag = splitTag(image)
	}
	c, reg, repo, err := resolveImageArg(ctx, cmd, image)
	if err != nil {
		return err
	}
	yes := cmd.Bool(flagYes)

	switch {
	case digest != "":
		d, err := findDigest(ctx, c, reg.ID, repo, digest)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "About to DELETE manifest %s of %s and its tags [%s].\n", d.Digest, repo, strings.Join(d.Tags, ", "))
		if !yes && !confirm() {
			return errAborted
		}
		if err := c.DeleteDigests(ctx, reg.ID, repo, []string{d.Digest}); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s@%s deleted\n", repo, d.Digest)

	case tag != "":
		digests, err := c.ListDigests(ctx, reg.ID, repo)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(digests, func(d client.Digest) bool { return slices.Contains(d.Tags, tag) })
		if i < 0 {
			return fmt.Errorf("%w: %s:%s", errNoSuchTag, repo, tag)
		}
		fmt.Fprintf(os.Stderr, "About to remove tag %s:%s. Its manifest stays.\n", repo, tag)
		if !yes && !confirm() {
			return errAborted
		}
		if err := c.DeleteTags(ctx, reg.ID, repo, digests[i].Digest, []string{tag}); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s:%s untagged\n", repo, tag)

	default:
		fmt.Fprintf(os.Stderr, "About to DELETE image %s with every tag in it, in registry %q. This cannot be undone.\n", repo, reg.Name)
		if !yes && !confirmExact(fmt.Sprintf("Type the image name %q to confirm: ", repo), repo) {
			return errAborted
		}
		if err := c.DeleteRepository(ctx, reg.ID, repo); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s deleted\n", repo)
	}
	return nil
}

func registryPruneAction(ctx context.Context, cmd *cli.Command) error {
	keep := cmd.Int(flagKeep)
	if keep < 1 {
		return errKeepRequired
	}
	c, reg, repo, err := resolveImageArg(ctx, cmd, cmd.Args().First())
	if err != nil {
		return err
	}
	digests, err := c.ListDigests(ctx, reg.ID, repo)
	if err != nil {
		return err
	}
	apps, err := c.ListApps(ctx)
	if err != nil {
		return err
	}
	doomed, spared := pruneCandidates(digests, keep, deployedTags(apps, repo))
	for _, d := range spared {
		fmt.Fprintf(os.Stderr, "keeping %s [%s]: a deployed app runs it\n", cut(d.Digest, shortDigest), tagList(d.Tags))
	}
	if len(doomed) == 0 {
		fmt.Fprintf(os.Stdout, "nothing to prune: %s has %d manifests\n", repo, len(digests))
		return nil
	}

	var freed int64
	shas := make([]string, 0, len(doomed))
	for _, d := range doomed {
		freed += d.Size
		shas = append(shas, d.Digest)
		fmt.Fprintf(os.Stderr, "  delete %s  %-8s %s  [%s]\n",
			cut(d.Digest, shortDigest), humanBytes(d.Size), ageOf(d.LastPushed), tagList(d.Tags))
	}
	fmt.Fprintf(os.Stderr, "%d of %d manifests of %s would be deleted (%s at most; layers shared with kept images stay).\n",
		len(doomed), len(digests), repo, humanBytes(freed))
	if cmd.Bool(flagDryRun) {
		return nil
	}
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.DeleteDigests(ctx, reg.ID, repo, shas); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s pruned: %d manifests deleted, %d kept\n", repo, len(doomed), len(digests)-len(doomed))
	return nil
}

func registryPasswordAction(ctx context.Context, cmd *cli.Command) error {
	c, reg, err := resolveRegistry(ctx, cmd)
	if err != nil {
		return err
	}
	pw, err := c.RegistryPassword(ctx, reg.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, pw)
	return nil
}

func registryGCListAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	rules, err := c.ListGCStrategies(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, rules); handled {
		return err
	}
	if len(rules) == 0 {
		fmt.Fprintln(os.Stderr, "No retention rules: images are kept until deleted.")
		return nil
	}
	rows := make([][]string, 0, len(rules))
	for _, r := range rules {
		rows = append(rows, []string{string(r.ID), gcScope(r), gcKeep(r)})
	}
	return output.StyledTable(os.Stdout, []string{"ID", "SCOPE", "KEEP"}, rows, nil)
}

func registryGCSetAction(ctx context.Context, cmd *cli.Command) error {
	count, days := cmd.Int(flagKeepCount), cmd.Int(flagKeepDays)
	if (count > 0) == (days > 0) {
		return errKeepChoice
	}
	body := map[string]any{"keep_type": client.GCKeepCount, "keep_count": count, "keep_duration": nil}
	if days > 0 {
		body = map[string]any{"keep_type": client.GCKeepDuration, "keep_count": nil, "keep_duration": strconv.Itoa(days) + " 00:00:00"}
	}

	c, reg, err := resolveRegistry(ctx, cmd)
	if err != nil {
		return err
	}
	scope, imageRepo, err := gcTarget(ctx, c, reg, cmd.Args().First())
	if err != nil {
		return err
	}
	rules, err := c.ListGCStrategies(ctx)
	if err != nil {
		return err
	}
	if existing := findGCStrategy(rules, reg, imageRepo); existing != nil {
		if err := c.UpdateGCStrategy(ctx, existing.ID, body); err != nil {
			return err
		}
	} else {
		body["destination_type"] = client.GCDestinationRegistry
		body["registry"] = string(reg.ID)
		body["image_repo"] = nil
		if imageRepo != "" {
			body["destination_type"] = client.GCDestinationImageRepo
			body["registry"] = ""
			body["image_repo"] = imageRepo
		}
		if err := c.CreateGCStrategy(ctx, body); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stdout, "retention for %s set\n", scope)
	return nil
}

func registryGCUnsetAction(ctx context.Context, cmd *cli.Command) error {
	c, reg, err := resolveRegistry(ctx, cmd)
	if err != nil {
		return err
	}
	scope, imageRepo, err := gcTarget(ctx, c, reg, cmd.Args().First())
	if err != nil {
		return err
	}
	rules, err := c.ListGCStrategies(ctx)
	if err != nil {
		return err
	}
	existing := findGCStrategy(rules, reg, imageRepo)
	if existing == nil {
		return fmt.Errorf("%w: %s", errNoSuchGCStrategy, scope)
	}
	if err := c.DeleteGCStrategy(ctx, existing.ID); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "retention for %s removed\n", scope)
	return nil
}

// resolveRegistry picks the registry named by --registry. With a single
// registry in the tenant, the name does not have to match.
func resolveRegistry(ctx context.Context, cmd *cli.Command) (*client.Client, *client.Registry, error) {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return nil, nil, err
	}
	regs, err := c.ListRegistries(ctx)
	if err != nil {
		return nil, nil, err
	}
	name := cmd.String(flagRegistry)
	if i := slices.IndexFunc(regs, func(r client.Registry) bool { return r.Name == name || string(r.ID) == name }); i >= 0 {
		return c, &regs[i], nil
	}
	if len(regs) == 1 && !cmd.IsSet(flagRegistry) {
		return c, &regs[0], nil
	}
	names := make([]string, 0, len(regs))
	for _, r := range regs {
		names = append(names, r.Name)
	}
	return nil, nil, fmt.Errorf("%w %q (have: %s)", errNoSuchRegistry, name, dash(strings.Join(names, ", ")))
}

// resolveImageArg resolves the registry and an IMAGE argument to the
// repository name the registry uses.
func resolveImageArg(ctx context.Context, cmd *cli.Command, image string) (*client.Client, *client.Registry, string, error) {
	if image == "" {
		return nil, nil, "", errMissingImage
	}
	c, reg, err := resolveRegistry(ctx, cmd)
	if err != nil {
		return nil, nil, "", err
	}
	repos, err := c.ListRepositories(ctx, reg.ID)
	if err != nil {
		return nil, nil, "", err
	}
	repo, err := matchRepository(repos, reg.PushPrefix(), image)
	if err != nil {
		return nil, nil, "", err
	}
	return c, reg, repo, nil
}

// matchRepository finds the repository an IMAGE argument names. It takes the
// name as listed, a full reference under the registry's push prefix, or a
// trailing path such as the app name.
func matchRepository(repos []client.Repository, prefix, image string) (string, error) {
	image = strings.TrimPrefix(image, prefix+"/")
	var suffixHits []string
	for _, r := range repos {
		name := strings.TrimPrefix(r.Name, prefix+"/")
		if r.Name == image || name == image {
			return r.Name, nil
		}
		if strings.HasSuffix(r.Name, "/"+image) {
			suffixHits = append(suffixHits, r.Name)
		}
	}
	switch len(suffixHits) {
	case 1:
		return suffixHits[0], nil
	case 0:
		return "", fmt.Errorf("%w %q; see `darkubectl registry images`", errNoSuchImage, image)
	default:
		return "", fmt.Errorf("%w %q: matches %s", errAmbiguousImage, image, strings.Join(suffixHits, ", "))
	}
}

// splitTag splits IMAGE:TAG, minding that a registry host may carry a port.
func splitTag(ref string) (string, string) {
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i+1:], "/") {
		return ref, ""
	}
	return ref[:i], ref[i+1:]
}

// findDigest resolves a digest, accepting any unique prefix of it.
func findDigest(ctx context.Context, c *client.Client, registryID client.FlexID, repo, prefix string) (*client.Digest, error) {
	digests, err := c.ListDigests(ctx, registryID, repo)
	if err != nil {
		return nil, err
	}
	var hits []client.Digest
	for _, d := range digests {
		if strings.HasPrefix(d.Digest, prefix) || strings.HasPrefix(strings.TrimPrefix(d.Digest, "sha256:"), prefix) {
			hits = append(hits, d)
		}
	}
	if len(hits) != 1 {
		return nil, fmt.Errorf("%w: %s@%s matches %d manifests", errNoSuchDigest, repo, prefix, len(hits))
	}
	return &hits[0], nil
}

// deployedTags is the set of tags of repo that some app currently runs.
func deployedTags(apps []client.App, repo string) map[string]bool {
	tags := map[string]bool{}
	for _, a := range apps {
		if a.ImageTag == "" {
			continue
		}
		if a.ImageRepo == repo || strings.HasSuffix(a.ImageRepo, "/"+repo) {
			tags[a.ImageTag] = true
		}
	}
	return tags
}

// pruneCandidates splits digests into those to delete and those spared only
// because a deployed app runs them. The keep newest are kept outright.
func pruneCandidates(digests []client.Digest, keep int, deployed map[string]bool) ([]client.Digest, []client.Digest) {
	sorted := slices.Clone(digests)
	sortNewestFirst(sorted)
	var doomed, spared []client.Digest
	for i, d := range sorted {
		if i < keep {
			continue
		}
		if slices.ContainsFunc(d.Tags, func(t string) bool { return deployed[t] }) {
			spared = append(spared, d)
			continue
		}
		doomed = append(doomed, d)
	}
	return doomed, spared
}

func sortNewestFirst(digests []client.Digest) {
	slices.SortStableFunc(digests, func(a, b client.Digest) int {
		return parseTime(b.LastPushed).Compare(parseTime(a.LastPushed))
	})
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// gcTarget describes the scope of a retention rule: the registry, or one
// image in it named by the full repository reference the platform keys on.
func gcTarget(ctx context.Context, c *client.Client, reg *client.Registry, image string) (string, string, error) {
	if image == "" {
		return "registry " + reg.Name, "", nil
	}
	repos, err := c.ListRepositories(ctx, reg.ID)
	if err != nil {
		return "", "", err
	}
	repo, err := matchRepository(repos, reg.PushPrefix(), image)
	if err != nil {
		return "", "", err
	}
	full := repo
	if !strings.HasPrefix(repo, reg.PushPrefix()+"/") {
		full = reg.PushPrefix() + "/" + strings.TrimPrefix(repo, "/")
	}
	return "image " + repo, full, nil
}

func findGCStrategy(rules []client.GCStrategy, reg *client.Registry, imageRepo string) *client.GCStrategy {
	for i, r := range rules {
		if imageRepo == "" && r.DestinationType == client.GCDestinationRegistry && r.Registry != nil && r.Registry.ID == reg.ID {
			return &rules[i]
		}
		if imageRepo != "" && r.DestinationType == client.GCDestinationImageRepo && r.ImageRepo == imageRepo {
			return &rules[i]
		}
	}
	return nil
}

func gcScope(r client.GCStrategy) string {
	if r.DestinationType == client.GCDestinationImageRepo {
		return "image " + r.ImageRepo
	}
	if r.Registry == nil {
		return "registry"
	}
	return "registry " + cmp.Or(r.Registry.Name, string(r.Registry.ID))
}

func gcKeep(r client.GCStrategy) string {
	switch {
	case r.KeepType == client.GCKeepDuration && r.KeepDuration != nil:
		days, _, _ := strings.Cut(*r.KeepDuration, " ")
		return "pushed in the last " + days + "d"
	case r.KeepCount != nil:
		return "newest " + strconv.Itoa(*r.KeepCount)
	default:
		return "-"
	}
}

// tagList renders a manifest's tags for a table: the sha256__<digest> tags
// some pushes add restate the digest and are left out, and a long list is cut
// to the first few.
func tagList(tags []string) string {
	shown := slices.DeleteFunc(slices.Clone(tags), func(t string) bool { return strings.HasPrefix(t, "sha256__") })
	if len(shown) <= maxTagsShown {
		return strings.Join(shown, ",")
	}
	return strings.Join(shown[:maxTagsShown], ",") + fmt.Sprintf(",+%d", len(shown)-maxTagsShown)
}

// humanBytes renders a byte count in binary units.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// cut shortens s to n runes.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
