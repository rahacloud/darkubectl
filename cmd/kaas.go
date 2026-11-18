package cmd

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

// Hamravesh KaaS is the managed Kubernetes product: a whole cluster of the
// tenant's own, as opposed to Darkube's apps on a shared one. Its API lives
// under /podolica (see internal/client/kaas.go). What is wired here is what a
// cluster's operator reaches for from a terminal: which clusters exist, their
// nodes and backups, the API server's source-IP whitelist, and the kubeconfig.

const (
	cmdKaaS          = "kaas"
	cmdCluster       = "cluster"
	argClusterUsage  = "CLUSTER"
	flagForce        = "force"
	kubeconfigPerm   = 0o600
	describeBackups  = 5
	defaultBackups   = 20
	etcdBackupMaxAge = 24 * time.Hour
)

var (
	errMissingCluster  = errors.New("a CLUSTER name is required (see `darkubectl get clusters`)")
	errMissingRanges   = errors.New("at least one IP address or CIDR is required")
	errBadRange        = errors.New("not an IP address or CIDR")
	errNotInWhitelist  = errors.New("not in the whitelist")
	errEmptyWhitelist  = errors.New("refusing to leave the API server whitelist empty")
	errKubeconfigExist = errors.New("file exists; pass --force to overwrite it")
)

// whitelistOp is how a whitelist command derives the new list from the old.
type whitelistOp int

const (
	whitelistAdd whitelistOp = iota
	whitelistRemove
	whitelistSet
)

func newGetClustersCommand() *cli.Command {
	return &cli.Command{
		Name:    "clusters",
		Aliases: []string{cmdCluster, cmdKaaS},
		Usage:   "List the tenant's managed Kubernetes (KaaS) clusters",
		Description: "KaaS clusters are a separate product from Darkube's apps. API-SERVER is the\n" +
			"host a kubeconfig points at, INGRESS-IP is the address the cluster's Traefik\n" +
			"answers on (where a domain's A record goes). -o wide adds the firewall's\n" +
			"public address and the cluster number the platform's names use (c216).",
		Action: getClustersAction,
	}
}

func newDescribeClusterCommand() *cli.Command {
	return &cli.Command{
		Name:          cmdCluster,
		Aliases:       []string{"clusters", cmdKaaS},
		Usage:         "Show a KaaS cluster with its node pools, nodes, API whitelist, version and etcd backups",
		ArgsUsage:     argClusterUsage,
		ShellComplete: completeClusterNames,
		Action:        describeClusterAction,
	}
}

func newKaaSCommand() *cli.Command {
	return &cli.Command{
		Name:  cmdKaaS,
		Usage: "Manage Hamravesh KaaS (managed Kubernetes) clusters",
		Description: "`get clusters` lists them and `describe cluster` shows one; this group holds\n" +
			"the rest:\n\n" +
			"  darkubectl kaas whitelist get talaland-production-c216\n" +
			"  darkubectl kaas whitelist add talaland-production-c216 203.0.113.7\n" +
			"  darkubectl kaas kubeconfig talaland-production-c216 --file ~/.kube/c216.yaml\n" +
			"  darkubectl kaas backups talaland-production-c216",
		Commands: []*cli.Command{
			newKaaSWhitelistCommand(),
			{
				Name:      "kubeconfig",
				Usage:     "Download a cluster's admin kubeconfig (prints a secret unless --file)",
				ArgsUsage: argClusterUsage,
				Description: "The same file as the console's \"Download KubeConfig\": a bearer token with\n" +
					"cluster-admin rights. With --file it is written 0600 and an existing file is\n" +
					"left alone unless --force. The API server only answers addresses on its\n" +
					"whitelist (`kaas whitelist get`), so a kubeconfig is not access on its own.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: flagFile, Aliases: []string{"f"}, Usage: "write it to this file instead of stdout"},
					&cli.BoolFlag{Name: flagForce, Usage: "overwrite --file if it exists"},
				},
				ShellComplete: completeClusterNames,
				Action:        kaasKubeconfigAction,
			},
			{
				Name:      "rotate-kubeconfig",
				Usage:     "Issue a new admin kubeconfig and invalidate every existing copy (unverified)",
				ArgsUsage: argClusterUsage,
				Description: "Everything that uses the current kubeconfig — CI jobs, Argo CD, a teammate's\n" +
					"laptop — loses access the moment this runs. Fetch the new one afterwards\n" +
					"with `kaas kubeconfig`. Wired from the console source and never sent to the\n" +
					"live API, so its answer is not known.",
				Flags:         []cli.Flag{&cli.BoolFlag{Name: flagYes, Aliases: []string{aliasYes}, Usage: usageSkipConfirm}},
				ShellComplete: completeClusterNames,
				Action:        kaasRotateKubeconfigAction,
			},
			{
				Name:      "backups",
				Aliases:   []string{"backup", "etcd-backups"},
				Usage:     "List a cluster's etcd backups, newest first",
				ArgsUsage: argClusterUsage,
				Flags: []cli.Flag{
					&cli.IntFlag{Name: flagLimit, Value: defaultBackups, Usage: "how many to list (0 for all)"},
				},
				ShellComplete: completeClusterNames,
				Action:        kaasBackupsAction,
			},
		},
	}
}

func newKaaSWhitelistCommand() *cli.Command {
	write := func(name, usage string, op whitelistOp, aliases ...string) *cli.Command {
		return &cli.Command{
			Name:          name,
			Aliases:       aliases,
			Usage:         usage,
			ArgsUsage:     argClusterUsage + " IP|CIDR...",
			Flags:         mutationFlags(),
			ShellComplete: completeClusterNames,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return whitelistAction(ctx, cmd, op)
			},
		}
	}
	return &cli.Command{
		Name:    "whitelist",
		Aliases: []string{"allowlist", "api-whitelist"},
		Usage:   "Read or change which addresses may reach a cluster's API server",
		Description: "The platform stores the whole list and a write replaces it, so add and remove\n" +
			"read the list, change it and send every entry back, then read it again to\n" +
			"confirm. A bare IP is taken as a /32 (/128 for IPv6). An add of an address\n" +
			"already present writes nothing, and a change that would empty the list is\n" +
			"refused.",
		Commands: []*cli.Command{
			{
				Name:          "get",
				Aliases:       []string{cmdList, "show"},
				Usage:         "Print the source ranges allowed to reach the API server",
				ArgsUsage:     argClusterUsage,
				ShellComplete: completeClusterNames,
				Action:        whitelistGetAction,
			},
			write("add", "Allow more addresses to reach the API server", whitelistAdd),
			write("remove", "Stop allowing addresses to reach the API server", whitelistRemove, "rm", "delete"),
			write("set", "Replace the whole whitelist with the given addresses", whitelistSet),
		},
	}
}

func getClustersAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	clusters, err := c.ListClusters(ctx)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, clusters); handled {
		return err
	}
	if format == output.Name {
		for _, cl := range clusters {
			fmt.Fprintln(os.Stdout, cl.Name)
		}
		return nil
	}
	if len(clusters) == 0 {
		fmt.Fprintln(os.Stderr, "no KaaS clusters in this tenant")
		return nil
	}
	header := []string{colName, colStatus, "VERSION", "PROVIDER", "DATACENTER", colSize, "API-SERVER", "INGRESS-IP"}
	if format == output.Wide {
		header = append(header, "FIREWALL-IP", "NUMBER")
	}
	rows := make([][]string, 0, len(clusters))
	for _, cl := range clusters {
		dc := cl.Datacenter.Name
		if cl.Datacenter.Zone != "" {
			dc += "/" + cl.Datacenter.Zone
		}
		row := []string{
			cl.Name, dash(cl.Status), dash(cl.Version), dash(cl.Provider), dash(dc), dash(cl.Size),
			dash(cl.APIServerURL), dash(cl.TraefikServiceIP),
		}
		if format == output.Wide {
			row = append(row, dash(cl.FirewallPublicIP), "c"+strconv.Itoa(cl.UniqueID))
		}
		rows = append(rows, row)
	}
	return output.StyledTable(os.Stdout, header, rows, output.StatusCells(1))
}

// clusterDetail is `describe cluster -o json`: the cluster as the API sends it
// plus everything the table view shows under it.
type clusterDetail struct {
	Cluster     map[string]any              `json:"cluster"               yaml:"cluster"`
	NodePools   []client.NodePool           `json:"nodePools,omitempty"   yaml:"nodePools,omitempty"`
	Nodes       []client.ClusterNode        `json:"nodes,omitempty"       yaml:"nodes,omitempty"`
	Whitelist   *client.APIServerWhitelist  `json:"whitelist,omitempty"   yaml:"whitelist,omitempty"`
	Update      *client.ClusterUpdateStatus `json:"update,omitempty"      yaml:"update,omitempty"`
	EtcdBackups []client.EtcdBackup         `json:"etcdBackups,omitempty" yaml:"etcdBackups,omitempty"`
}

func describeClusterAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingCluster
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	raw, err := c.GetClusterRaw(ctx, name)
	if err != nil {
		return clusterErr(err, name)
	}
	if format == output.Name {
		fmt.Fprintln(os.Stdout, name)
		return nil
	}

	// The rest is context. A failing read loses its section, not the command.
	d := clusterDetail{Cluster: raw}
	warn := func(what string, err error) {
		fmt.Fprintf(os.Stderr, "warning: could not read the %s: %v\n", what, err)
	}
	if d.NodePools, err = c.NodePools(ctx, name); err != nil {
		warn("node pools", err)
	}
	if d.Nodes, err = c.ClusterNodes(ctx, name); err != nil {
		warn("nodes", err)
	}
	if d.Whitelist, err = c.APIServerWhitelist(ctx, name); err != nil {
		warn("API server whitelist", err)
	}
	if d.Update, err = c.ClusterUpdateStatus(ctx, name); err != nil {
		warn("version status", err)
	}
	if d.EtcdBackups, err = c.EtcdBackups(ctx, name); err != nil {
		warn("etcd backups", err)
	}
	if handled, err := output.Structured(os.Stdout, format, d); handled {
		return err
	}
	return renderClusterDetail(os.Stdout, name, d)
}

func renderClusterDetail(w io.Writer, name string, d clusterDetail) error {
	// The brand block is the platform's own white-labelling, the same on
	// every cluster, and only pushes the fields that matter off the screen.
	shown := make(map[string]any, len(d.Cluster))
	for k, v := range d.Cluster {
		if k != "brand" {
			shown[k] = v
		}
	}
	if err := output.RenderDescribe(w, "cluster/"+name, output.FlattenObject(shown)); err != nil {
		return err
	}
	sections := []func(io.Writer, clusterDetail) error{
		renderNodePools, renderClusterNodes, renderWhitelistSection, renderVersionSection, renderBackupSection,
	}
	for _, render := range sections {
		if err := render(w, d); err != nil {
			return err
		}
	}
	return nil
}

func renderNodePools(w io.Writer, d clusterDetail) error {
	if len(d.NodePools) == 0 {
		return nil
	}
	if err := clusterSection(w, "node pools"); err != nil {
		return err
	}
	rows := make([][]string, 0, len(d.NodePools))
	for _, p := range d.NodePools {
		rows = append(rows, []string{
			p.Name, p.TypeName + "/" + p.TypeSize, num(p.CPU), num(p.RAM) + "G",
			num(p.OSDisk) + "G", num(p.DataDisk) + "G", yesNo(p.HasTaint),
		})
	}
	return output.StyledTable(w, []string{colName, colType, colCPU, "RAM", "OS-DISK", "DATA-DISK", "TAINTED"}, rows, nil)
}

func renderClusterNodes(w io.Writer, d clusterDetail) error {
	if len(d.Nodes) == 0 {
		return nil
	}
	if err := clusterSection(w, "nodes"); err != nil {
		return err
	}
	rows := make([][]string, 0, len(d.Nodes))
	for _, n := range d.Nodes {
		rows = append(rows, []string{n.Name, n.Pool, dash(n.Status), dash(n.IP), strconv.Itoa(n.CPU)})
	}
	return output.StyledTable(w, []string{colName, "POOL", colStatus, "IP", colCPU}, rows, nodeStatusCells)
}

// clusterSection starts a section of `describe cluster` after a blank line.
func clusterSection(w io.Writer, title string) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return output.PrintSectionHeader(w, title)
}

// nodeStatusCells colors a node's STATUS. StateColor reads "NotReady" as
// healthy, because it contains "ready".
func nodeStatusCells(col int, value string) color.Color {
	if col != 2 { //nolint:mnd // the STATUS column
		return nil
	}
	if value == "Ready" {
		return output.ColorSuccess
	}
	return output.ColorDanger
}

func renderWhitelistSection(w io.Writer, d clusterDetail) error {
	if d.Whitelist == nil {
		return nil
	}
	if err := clusterSection(w,
		fmt.Sprintf("API server whitelist (synced %s ago)", ageOf(d.Whitelist.SyncedAt))); err != nil {
		return err
	}
	if len(d.Whitelist.SourceRanges) == 0 {
		_, err := fmt.Fprintln(w, "(empty)")
		return err
	}
	_, err := fmt.Fprintln(w, strings.Join(d.Whitelist.SourceRanges, "\n"))
	return err
}

func renderVersionSection(w io.Writer, d clusterDetail) error {
	if d.Update == nil {
		return nil
	}
	if err := clusterSection(w, "version"); err != nil {
		return err
	}
	state := "the latest offered"
	if !d.Update.IsLatest {
		state = "an update is available"
	}
	_, err := fmt.Fprintf(w, "%s (%s)\n", dash(d.Update.CurrentVersion), state)
	return err
}

func renderBackupSection(w io.Writer, d clusterDetail) error {
	if d.EtcdBackups == nil {
		return nil
	}
	if err := clusterSection(w, fmt.Sprintf("etcd backups (%d kept)", len(d.EtcdBackups))); err != nil {
		return err
	}
	if note := backupFreshness(d.EtcdBackups, time.Now()); note != "" {
		if _, err := fmt.Fprintln(w, note); err != nil {
			return err
		}
	}
	return backupTable(w, d.EtcdBackups[:min(describeBackups, len(d.EtcdBackups))])
}

// backupFreshness warns when the newest successful etcd backup is older than a
// day. The schedule is every six hours, so a day without one means backups
// have stopped, and a list full of old successes does not show that by itself.
func backupFreshness(backups []client.EtcdBackup, now time.Time) string {
	var newest time.Time
	for _, b := range backups {
		if b.Status != "success" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, b.Date); err == nil && t.After(newest) {
			newest = t
		}
	}
	switch {
	case newest.IsZero():
		return "warning: no successful etcd backup is listed"
	case now.Sub(newest) > etcdBackupMaxAge:
		return fmt.Sprintf("warning: the newest successful etcd backup is %d days old (%s)",
			int(now.Sub(newest).Hours()/hoursPerDay), newest.Format(time.DateTime))
	default:
		return ""
	}
}

func backupTable(w io.Writer, backups []client.EtcdBackup) error {
	rows := make([][]string, 0, len(backups))
	for _, b := range backups {
		date := b.Date
		if t, err := time.Parse(time.RFC3339, b.Date); err == nil {
			date = t.UTC().Format(time.DateTime)
		}
		rows = append(rows, []string{date, ageOf(b.Date), dash(b.Status), dash(b.Size)})
	}
	return output.StyledTable(w, []string{"DATE (UTC)", colAge, colStatus, colSize}, rows, output.StatusCells(2)) //nolint:mnd // STATUS
}

func kaasBackupsAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingCluster
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	backups, err := c.EtcdBackups(ctx, name)
	if err != nil {
		return clusterErr(err, name)
	}
	// Judged on the whole list, before --limit cuts it.
	note := backupFreshness(backups, time.Now())
	if n := cmd.Int(flagLimit); n > 0 && n < len(backups) {
		backups = backups[:n]
	}
	if handled, err := output.Structured(os.Stdout, format, backups); handled {
		return err
	}
	if note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	if len(backups) == 0 {
		return nil
	}
	return backupTable(os.Stdout, backups)
}

func whitelistGetAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingCluster
	}
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	wl, err := c.APIServerWhitelist(ctx, name)
	if err != nil {
		return clusterErr(err, name)
	}
	if handled, err := output.Structured(os.Stdout, format, wl); handled {
		return err
	}
	for _, r := range wl.SourceRanges {
		fmt.Fprintln(os.Stdout, r)
	}
	if format != output.Name {
		fmt.Fprintf(os.Stderr, "%d entries, synced to the cluster %s ago\n", len(wl.SourceRanges), ageOf(wl.SyncedAt))
	}
	return nil
}

func whitelistAction(ctx context.Context, cmd *cli.Command, op whitelistOp) error {
	if cmd.Args().Len() == 0 {
		return errMissingCluster
	}
	args := cmd.Args().Slice()
	if len(args) < 2 { //nolint:mnd // CLUSTER and at least one range
		return errMissingRanges
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	return changeWhitelist(ctx, c, args[0], op, args[1:], cmd.Bool(flagDryRun), cmd.Bool(flagYes))
}

// changeWhitelist is the read-modify-write behind add, remove and set: read
// the list, derive the new one, show the difference, write the whole list and
// read it back. Nothing is sent when nothing would change, so an add of an
// address already present is a safe no-op.
func changeWhitelist(
	ctx context.Context, c *client.Client, cluster string, op whitelistOp, args []string, dryRun, yes bool,
) error {
	cur, err := c.APIServerWhitelist(ctx, cluster)
	if err != nil {
		return clusterErr(err, cluster)
	}
	next, added, removed, err := planWhitelist(cur.SourceRanges, op, args)
	if err != nil {
		return err
	}
	if len(added) == 0 && len(removed) == 0 {
		fmt.Fprintf(os.Stderr, "cluster/%s: the API server whitelist already reads that way; nothing to change\n", cluster)
		return nil
	}

	fmt.Fprintf(os.Stderr, "API server whitelist of cluster/%s:\n", cluster)
	for _, r := range added {
		fmt.Fprintf(os.Stderr, "  + %s\n", r)
	}
	for _, r := range removed {
		fmt.Fprintf(os.Stderr, "  - %s\n", r)
	}
	fmt.Fprintf(os.Stderr, "%d entries now, %d after.\n", len(cur.SourceRanges), len(next))
	if len(removed) > 0 {
		fmt.Fprintln(os.Stderr, "A removed address loses kubectl access to this cluster at once, "+
			"including the one this machine uses if it is among them.")
	}
	if dryRun {
		return nil
	}
	if !yes && !confirm() {
		return errAborted
	}
	if _, err := c.SetAPIServerWhitelist(ctx, cluster, next); err != nil {
		return err
	}

	ok, err := retryReadBack(ctx, func(ctx context.Context) (bool, error) {
		wl, err := c.APIServerWhitelist(ctx, cluster)
		if err != nil {
			return false, err
		}
		return sameRanges(wl.SourceRanges, next), nil
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: the whitelist of %s read %s later does not match what was sent",
			errChangeIgnored, cluster, verifyInterval*(verifyAttempts-1))
	}
	fmt.Fprintf(os.Stdout, "cluster/%s API server whitelist updated (%d entries)\n", cluster, len(next))
	return nil
}

// planWhitelist derives the list to send. Entries already on the list are kept
// as stored and in their order; new ones go at the end. It refuses an empty
// result: whether an empty list shuts every address out or lets every address
// in has not been tried, and neither is something to find out by accident.
func planWhitelist(current []string, op whitelistOp, args []string) ([]string, []string, []string, error) {
	want := make([]string, 0, len(args))
	for _, a := range args {
		r, err := normalizeRange(a)
		if err != nil {
			return nil, nil, nil, err
		}
		if !slices.Contains(want, r) {
			want = append(want, r)
		}
	}
	// Compare on the normalized form so "1.2.3.4" matches a stored "1.2.3.4/32".
	key := func(s string) string {
		if r, err := normalizeRange(s); err == nil {
			return r
		}
		return s
	}
	has := func(list []string, r string) bool {
		return slices.ContainsFunc(list, func(s string) bool { return key(s) == r })
	}

	var next []string
	switch op {
	case whitelistAdd:
		next = slices.Clone(current)
		for _, r := range want {
			if !has(current, r) {
				next = append(next, r)
			}
		}
	case whitelistRemove:
		var missing []string
		for _, r := range want {
			if !has(current, r) {
				missing = append(missing, r)
			}
		}
		if len(missing) > 0 {
			return nil, nil, nil, fmt.Errorf("%w: %s", errNotInWhitelist, strings.Join(missing, ", "))
		}
		next = slices.DeleteFunc(slices.Clone(current), func(s string) bool { return slices.Contains(want, key(s)) })
	case whitelistSet:
		next = want
	}

	var added, removed []string
	for _, r := range next {
		if !has(current, key(r)) {
			added = append(added, r)
		}
	}
	for _, r := range current {
		if !has(next, key(r)) {
			removed = append(removed, r)
		}
	}
	if len(next) == 0 && (len(added) > 0 || len(removed) > 0) {
		return nil, nil, nil, errEmptyWhitelist
	}
	return next, added, removed, nil
}

// normalizeRange turns an address or CIDR into the canonical CIDR the platform
// stores: a bare address becomes a single-host prefix, and a prefix with host
// bits set is refused rather than silently widened or narrowed.
func normalizeRange(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return "", fmt.Errorf("%w: %q", errBadRange, s)
		}
		if m := p.Masked(); m != p {
			return "", fmt.Errorf("%w: %q has host bits set; the network is %s", errBadRange, s, m)
		}
		return p.String(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return "", fmt.Errorf("%w: %q", errBadRange, s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()).String(), nil
}

// sameRanges compares two lists as sets of normalized ranges.
func sameRanges(a, b []string) bool {
	norm := func(list []string) []string {
		out := make([]string, 0, len(list))
		for _, s := range list {
			if r, err := normalizeRange(s); err == nil {
				s = r
			}
			out = append(out, s)
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(norm(a), norm(b))
}

func kaasKubeconfigAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingCluster
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	data, err := c.Kubeconfig(ctx, name)
	if err != nil {
		return clusterErr(err, name)
	}
	dest := cmd.String(flagFile)
	if dest == "" || dest == stdinArg {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := writeKubeconfig(dest, data, cmd.Bool(flagForce)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "kubeconfig for cluster/%s written to %s (a cluster-admin credential; keep it private)\n", name, dest)
	return nil
}

// writeKubeconfig writes a credential 0600, and refuses to replace a file
// unless told to: a kubeconfig path is often the one already in use.
func writeKubeconfig(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, kubeconfigPerm) //nolint:gosec // the user names the file
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s: %w", path, errKubeconfigExist)
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// O_TRUNC keeps an existing file's mode; a credential must not stay readable.
	return os.Chmod(path, kubeconfigPerm)
}

func kaasRotateKubeconfigAction(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Args().First()
	if name == "" {
		return errMissingCluster
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	if _, err := c.GetClusterRaw(ctx, name); err != nil {
		return clusterErr(err, name)
	}
	fmt.Fprintf(os.Stderr, "About to rotate the admin kubeconfig of cluster/%s. Every existing copy stops working:\n"+
		"CI jobs, Argo CD or anything else holding it lose access until they are given the new one.\n", name)
	if !cmd.Bool(flagYes) && !confirmExact(fmt.Sprintf("Type the cluster name %q to confirm: ", name), name) {
		return errAborted
	}
	if err := c.RotateKubeconfig(ctx, name); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "cluster/%s kubeconfig rotated; fetch the new one with `darkubectl kaas kubeconfig %s --file FILE`\n",
		name, name)
	return nil
}

// clusterErr names the cluster on a 404, which the API answers with a bare
// "An unexpected error occurred" that says nothing about what was missing.
func clusterErr(err error, name string) error {
	if client.IsNotFound(err) {
		return fmt.Errorf("no KaaS cluster %q in this tenant (see `darkubectl get clusters`): %w", name, err)
	}
	return err
}

// num renders a size the API sends as a float (8.0, 0.125) without trailing zeros.
func num(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// completeClusterNames offers the tenant's cluster names for the first
// argument, as completeAppNames does for apps.
func completeClusterNames(ctx context.Context, cmd *cli.Command) {
	if cmd.NArg() > 0 || lastArgIsFlag() {
		cli.DefaultCompleteWithFlags(ctx, cmd)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, completionTimeout)
	defer cancel()

	c, err := newClient(ctx, cmd)
	if err != nil {
		return
	}
	clusters, err := c.ListClusters(ctx)
	if err != nil {
		return
	}
	for _, cl := range clusters {
		fmt.Fprintln(os.Stdout, cl.Name)
	}
}
