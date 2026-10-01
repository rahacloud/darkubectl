package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"
)

const flagKind = "kind"

// kindLine finds a document's top-level kind.
var kindLine = regexp.MustCompile(`(?m)^kind:\s*(\S+)`)

func newGetManifestsCommand() *cli.Command {
	return &cli.Command{
		Name:      "manifests",
		Aliases:   []string{"manifest", "yaml"},
		Usage:     "Print the Kubernetes objects the platform renders for an app",
		ArgsUsage: argRefUsage,
		Description: "  darkubectl get manifests my-api\n" +
			"  darkubectl get manifests my-api --kind Ingress\n\n" +
			"The Helm output of the app's chart: what the cluster is actually given, which\n" +
			"is the place to check how a setting lands (probes, resources, ingress\n" +
			"annotations). Secret values are not included.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagKind, Usage: "only objects of this kind (Deployment, Service, Ingress, …)"},
		},
		Action: getManifestsAction,
	}
}

func getManifestsAction(ctx context.Context, cmd *cli.Command) error {
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	yaml, err := c.AppManifests(ctx, app.ID)
	if err != nil {
		return err
	}
	docs := manifestDocs(yaml, cmd.String(flagKind))
	if len(docs) == 0 {
		fmt.Fprintf(os.Stderr, "no objects of kind %q in app/%s\n", cmd.String(flagKind), app.Name)
		return nil
	}
	fmt.Fprintln(os.Stdout, strings.Join(docs, "\n---\n"))
	return nil
}

// manifestDocs splits multi-document YAML, drops documents that render no
// object (a template that produced only its "# Source:" comment), and keeps
// those of kind when it is set.
func manifestDocs(yaml, kind string) []string {
	var out []string
	for doc := range strings.SplitSeq(yaml, "\n---") {
		doc = strings.Trim(strings.TrimPrefix(doc, "---"), "\n")
		m := kindLine.FindStringSubmatch(doc)
		if m == nil {
			continue
		}
		if kind != "" && !strings.EqualFold(m[1], kind) {
			continue
		}
		out = append(out, doc)
	}
	return out
}
