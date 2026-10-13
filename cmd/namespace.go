package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/urfave/cli/v3"
)

const (
	flagCluster  = "cluster"
	aliasNS      = "ns"
	aliasProject = "project"
)

var (
	errNamespaceName     = errors.New("a namespace NAME is required")
	errNoSuchNamespace   = errors.New("no such namespace in this tenant")
	errNamespaceNotEmpty = errors.New("namespace is not empty")
	errNamespacePrefix   = errors.New("namespace name must start with the tenant slug")
)

func newCreateNamespaceCommand() *cli.Command {
	return &cli.Command{
		Name:      flagNamespace,
		Aliases:   []string{aliasNS, aliasProject},
		Usage:     "Create a namespace (project) in the current tenant",
		ArgsUsage: colName,
		Description: "  darkubectl create namespace paradisehub-production --cluster hamravesh-c11\n" +
			"  darkubectl create namespace staging --cluster 46\n\n" +
			"--cluster takes a cluster name or its numeric id. The name is what\n" +
			"`get namespaces` prints, so it is the one you will have to hand; the id is\n" +
			"only visible in JSON.\n\n" +
			"THE NAME MUST START WITH THE TENANT SLUG. A tenant `acme` can create\n" +
			"`acme-production`; it cannot create `production`, and the API rejects it with\n" +
			"400 `نام انتخاب شده درست نیست` -- 'the chosen name is not correct' -- which\n" +
			"names no rule and is identical for a bad charset, a bad length or this. The\n" +
			"prefix is checked here so the error says which.\n\n" +
			"This needs a Console JWT — the namespace surface rejects an account Api-key —\n" +
			"so it works from a `login` session and not from a token alone.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagCluster, Usage: "cluster name or numeric id", Required: true},
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: createNamespaceAction,
	}
}

func newDeleteNamespaceCommand() *cli.Command {
	return &cli.Command{
		Name:      flagNamespace,
		Aliases:   []string{aliasNS, aliasProject},
		Usage:     "Delete an empty namespace (project)",
		ArgsUsage: "NAME|ID",
		Description: "  darkubectl delete namespace paradisehub-product-prod\n\n" +
			"Refuses while the namespace still holds apps. The API itself does not check,\n" +
			"and deleting a populated project takes its workloads with it — so the check\n" +
			"is here rather than nowhere.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm},
		},
		Action: deleteNamespaceAction,
	}
}

func deleteNamespaceAction(ctx context.Context, cmd *cli.Command) error {
	ref := cmd.Args().First()
	if ref == "" {
		return errNamespaceName
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	known, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}

	var target *client.Namespace
	for i, n := range known {
		if n.Name == ref || strconv.Itoa(n.ID) == ref {
			target = &known[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: %q", errNoSuchNamespace, ref)
	}

	apps, err := c.ListApps(ctx)
	if err != nil {
		return err
	}
	var held int
	for _, a := range apps {
		if a.Namespace.ID == target.ID {
			held++
		}
	}
	if held > 0 {
		return fmt.Errorf("%w: %q still holds %d app(s); delete them first",
			errNamespaceNotEmpty, target.Name, held)
	}
	// Managed databases are a separate product and never in the app list, but
	// they live in a namespace all the same: an "empty" namespace can still
	// hold a production database.
	dbs, err := c.ListDatabases(ctx)
	if err != nil {
		return fmt.Errorf("checking the namespace for managed databases: %w", err)
	}
	var dbNames []string
	for _, d := range dbs {
		if d.NamespaceID == target.ID {
			dbNames = append(dbNames, d.Name)
		}
	}
	if len(dbNames) > 0 {
		return fmt.Errorf("%w: %q still holds managed database(s) %s; delete them first",
			errNamespaceNotEmpty, target.Name, strings.Join(dbNames, ", "))
	}

	fmt.Fprintf(os.Stderr, "About to delete empty namespace %q (id %d) in tenant %q\n",
		target.Name, target.ID, c.Org)
	// Claims outlive the apps that made them, so the disks of deleted apps can
	// still be here, and their data with them. Name them before the prompt.
	if pvcs, err := c.ListPVCs(ctx, target.Name, target.Cluster.ID); err == nil && len(pvcs) > 0 {
		fmt.Fprintf(os.Stderr, "warning: it still holds %d disk(s), probably left behind by deleted apps:\n", len(pvcs))
		for _, p := range pvcs {
			fmt.Fprintf(os.Stderr, "  %s (%s)\n", p.Name, p.Size)
		}
	}
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}
	if err := c.DeleteNamespace(ctx, target.ID); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "namespace/%s deleted\n", target.Name)
	return nil
}

func createNamespaceAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errNamespaceName
	}

	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}

	known, err := c.Namespaces(ctx)
	if err != nil {
		return err
	}
	clusterID, err := client.ResolveCluster(cmd.String(flagCluster), known)
	if err != nil {
		return err
	}

	// The API requires the tenant slug as a prefix and will not say so: every
	// rejection is the same opaque 400. Checked here, before the round trip,
	// because the message it returns cannot be acted on.
	if !strings.HasPrefix(name, c.Org+"-") && name != c.Org {
		return fmt.Errorf("%w: %q must start with the tenant slug — try %q",
			errNamespacePrefix, name, c.Org+"-"+strings.TrimPrefix(name, c.Org))
	}

	for _, n := range known {
		if n.Name == name && n.Cluster.ID == clusterID {
			return fmt.Errorf("namespace %q already exists on that cluster (id %d)", name, n.ID)
		}
	}

	fmt.Fprintf(os.Stderr, "About to create namespace %q on cluster %d in tenant %q\n", name, clusterID, c.Org)
	if !cmd.Bool(flagYes) && !confirm() {
		return errAborted
	}

	ns, err := c.CreateNamespace(ctx, name, clusterID)
	if err != nil {
		return err
	}
	if ns.ID == 0 {
		// The create answered with nothing readable; the namespace still exists.
		fmt.Fprintf(os.Stdout, "namespace/%s created\n", name)
		return nil
	}
	fmt.Fprintf(os.Stdout, "namespace/%s created (id %d)\n", ns.Name, ns.ID)
	return nil
}
