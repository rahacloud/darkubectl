package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/kube"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// Flag names for the cluster-side half of `get orphans`.
const (
	flagKubeconfig  = "kubeconfig"
	flagKubeContext = "context"
	flagKubectl     = "kubectl"
)

// errNoDeployToken is returned when an app carries no trigger deploy token.
var errNoDeployToken = errors.New("app has no deploy token")

// getOrphansAction compares the tenant's app list against the Deployments in a
// cluster and reports where the two disagree.
//
// The orphan direction is the one worth running: a deleted app can leave its
// Helm release behind, still running and still holding the name, and nothing in
// the API can show it because the app it belonged to is gone. The first symptom
// is otherwise a create failing with SameHelmReleaseNameExists, which arrives
// long after the fact and points at the wrong thing.
func getOrphansAction(ctx context.Context, cmd *cli.Command) error {
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}

	apps, err := c.ListApps(ctx)
	if err != nil {
		return err
	}
	workloads, err := kube.Deployments(ctx, kube.Options{
		Binary:     cmd.String(flagKubectl),
		Kubeconfig: cmd.String(flagKubeconfig),
		Context:    cmd.String(flagKubeContext),
		Namespace:  cmd.String(flagNamespace),
	})
	if err != nil {
		return err
	}

	found := kube.Diff(appRefs(apps), workloads)

	if handled, err := output.Structured(os.Stdout, format, found); handled {
		return err
	}
	if len(found) == 0 {
		fmt.Fprintf(os.Stderr, "no divergence: %d workload(s) checked against %d app(s) in tenant %q\n",
			len(workloads), len(apps), c.Org)
		return nil
	}
	if format == output.Name {
		for _, d := range found {
			fmt.Fprintln(os.Stdout, d.Name)
		}
		return nil
	}
	return printDivergenceTable(found)
}

// appRefs narrows the API's apps to what the comparison needs.
func appRefs(apps []client.App) []kube.AppRef {
	refs := make([]kube.AppRef, 0, len(apps))
	for _, a := range apps {
		refs = append(refs, kube.AppRef{ID: a.ID, Name: a.Name, Namespace: a.Namespace.Name})
	}
	return refs
}

func printDivergenceTable(found []kube.Divergence) error {
	header := []string{colName, colNamespace, "KIND", "APP-ID"}
	rows := make([][]string, 0, len(found))
	for _, d := range found {
		rows = append(rows, []string{d.Name, d.Namespace, string(d.Kind), d.AppID})
	}
	if err := output.StyledTable(os.Stdout, header, rows, nil); err != nil {
		return err
	}
	explainDivergence(found)
	return nil
}

// explainDivergence writes guidance to stderr, so it stays out of a piped table.
func explainDivergence(found []kube.Divergence) {
	var orphans, missing int
	for _, d := range found {
		switch d.Kind {
		case kube.Orphaned:
			orphans++
		case kube.NoWorkload:
			missing++
		}
	}
	if orphans > 0 {
		fmt.Fprintf(os.Stderr, "\n%d orphaned: running in the cluster, but no such app in Darkube. "+
			"The Helm release still holds the name, so `create app` under it fails with "+
			"SameHelmReleaseNameExists; clearing it needs the console or Hamravesh support.\n", orphans)
	}
	if missing > 0 {
		fmt.Fprintf(os.Stderr, "\n%d no-workload: the app exists in Darkube but nothing in the cluster "+
			"carries its id — a create that never landed, or a workload deleted underneath it.\n", missing)
	}
}

// getDeployTokenAction prints the CI credentials for one app.
func getDeployTokenAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingAppRef
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}

	app, err := c.ResolveApp(ctx, name)
	if err != nil {
		return err
	}
	token, err := c.DeployToken(ctx, app.ID)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("%w: %s", errNoDeployToken, app.Name)
	}

	cred := deployCredentials{
		App:         app.Name,
		Namespace:   app.Namespace.Name,
		AppID:       app.ID,
		DeployToken: token,
	}
	if handled, err := output.Structured(os.Stdout, format, cred); handled {
		return err
	}
	// -o name gives the bare token, for `--token "$(darkubectl get deploy-token X -o name)"`.
	if format == output.Name {
		fmt.Fprintln(os.Stdout, cred.DeployToken)
		return nil
	}
	return output.StyledTable(os.Stdout,
		[]string{colName, colNamespace, "APP-ID", "DEPLOY-TOKEN"},
		[][]string{{cred.App, cred.Namespace, cred.AppID, cred.DeployToken}},
		nil)
}

// deployCredentials is the pair a CI pipeline needs: `darkube deploy --app-id
// <AppID> --token <DeployToken>`.
type deployCredentials struct {
	App         string `json:"app"`
	Namespace   string `json:"namespace"`
	AppID       string `json:"appId"`
	DeployToken string `json:"deployToken"`
}
