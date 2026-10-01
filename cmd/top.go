package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"github.com/urfave/cli/v3"
)

const (
	// topWindow and topStep bound the query behind `top`: long enough that a
	// scrape always lands inside it, and only the latest sample is used.
	topWindow = 5 * time.Minute
	topStep   = time.Minute

	// topParallel caps concurrent queries when `top apps` covers a tenant.
	topParallel = 8
	// topAttempts is how many times one metrics query is tried.
	topAttempts = 3

	percent           = 100
	millicoresPerCore = 1000
)

// podUsage is one pod's latest resource use.
type podUsage struct {
	App       string   `json:"app"                               yaml:"app"`
	Pod       string   `json:"pod"                               yaml:"pod"`
	CPUCores  float64  `json:"cpuCores"                          yaml:"cpuCores"`
	RAMBytes  float64  `json:"ramBytes"                          yaml:"ramBytes"`
	Throttled *float64 `json:"cpuThrottled,omitempty"            yaml:"cpuThrottled,omitempty"`
	RxPerSec  *float64 `json:"networkRxBytesPerSecond,omitempty" yaml:"networkRxBytesPerSecond,omitempty"`
	TxPerSec  *float64 `json:"networkTxBytesPerSecond,omitempty" yaml:"networkTxBytesPerSecond,omitempty"`
}

// appUsage is an app's use summed over its pods.
type appUsage struct {
	App         string  `json:"app"                  yaml:"app"`
	Namespace   string  `json:"namespace"            yaml:"namespace"`
	Pods        int     `json:"pods"                 yaml:"pods"`
	CPUCores    float64 `json:"cpuCores"             yaml:"cpuCores"`
	RAMBytes    float64 `json:"ramBytes"             yaml:"ramBytes"`
	RAMLimit    string  `json:"ramLimit"             yaml:"ramLimit"`
	RAMPercent  *int    `json:"ramPercent,omitempty" yaml:"ramPercent,omitempty"`
	CPURequest  string  `json:"cpuRequest"           yaml:"cpuRequest"`
	Unreachable string  `json:"error,omitempty"      yaml:"error,omitempty"`
}

func newTopCommand() *cli.Command {
	return &cli.Command{
		Name:  "top",
		Usage: "Show CPU and memory use, from the platform's metrics",
		Description: "Reads the same metrics as the console's resource-usage charts and prints the\n" +
			"latest value. Memory is the working set against the app's ram_limit, which is\n" +
			"what an OOM kill is measured against.",
		Commands: []*cli.Command{
			{
				Name:      "app",
				Aliases:   []string{"pods", "pod"},
				Usage:     "Show each pod of an app: CPU, memory, throttling and network",
				ArgsUsage: argRefUsage,
				Action:    topAppAction,
			},
			{
				Name:  "apps",
				Usage: "Show every app in the tenant, busiest first",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: flagNamespace, Usage: "only apps in this namespace (project)"},
					&cli.StringFlag{Name: flagSortBy, Value: sortByMemory, Usage: "sort by memory, cpu or name"},
				},
				Action: topAppsAction,
			},
		},
	}
}

var errBadSortBy = errors.New("invalid --sort-by")

const (
	flagSortBy   = "sort-by"
	sortByMemory = "memory"
	sortByCPU    = "cpu"
	sortByName   = "name"
)

func topAppAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	c, app, err := resolveAppArg(ctx, cmd)
	if err != nil {
		return err
	}
	pods, err := fetchPodUsage(ctx, c, app, true)
	if err != nil {
		return err
	}
	if handled, err := output.Structured(os.Stdout, format, pods); handled {
		return err
	}
	if len(pods) == 0 {
		fmt.Fprintf(os.Stderr, "No metrics for app %q in the last %s; is it running?\n", app.Name, topWindow)
		return nil
	}
	limit, _ := parseBytes(app.RAMLimit)
	rows := make([][]string, 0, len(pods))
	for _, p := range pods {
		rows = append(rows, []string{
			p.Pod, millicores(p.CPUCores), ratio(p.Throttled),
			humanBytes(int64(p.RAMBytes)), ofLimit(p.RAMBytes, limit), rate(p.RxPerSec), rate(p.TxPerSec),
		})
	}
	fmt.Fprintf(os.Stderr, "app/%s: cpu_request %s, ram_limit %s\n", app.Name, dash(app.CPURequest), dash(app.RAMLimit))
	return output.StyledTable(os.Stdout,
		[]string{"POD", colCPU, "THROTTLED", "MEMORY", "MEM/LIMIT", "NET IN", "NET OUT"}, rows, nil)
}

func topAppsAction(ctx context.Context, cmd *cli.Command) error {
	format, err := outputFormat(cmd)
	if err != nil {
		return err
	}
	sortBy := cmd.String(flagSortBy)
	if !slices.Contains([]string{sortByMemory, sortByCPU, sortByName}, sortBy) {
		return fmt.Errorf("%w %q (want: memory, cpu or name)", errBadSortBy, sortBy)
	}
	c, err := newClient(ctx, cmd)
	if err != nil {
		return err
	}
	apps, err := c.ListApps(ctx)
	if err != nil {
		return err
	}
	if ns := cmd.String(flagNamespace); ns != "" {
		apps = slices.DeleteFunc(apps, func(a client.App) bool { return a.Namespace.Name != ns })
	}

	usage := make([]appUsage, len(apps))
	sem := make(chan struct{}, topParallel)
	var wg sync.WaitGroup
	for i := range apps {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			usage[i] = summarize(ctx, c, &apps[i])
		})
	}
	wg.Wait()
	sortUsage(usage, sortBy)

	if handled, err := output.Structured(os.Stdout, format, usage); handled {
		return err
	}
	header := []string{colName, colNamespace, "PODS", colCPU, "MEMORY", "MEM/LIMIT"}
	failed := slices.ContainsFunc(usage, func(u appUsage) bool { return u.Unreachable != "" })
	if failed {
		header = append(header, "NOTE")
	}
	rows := make([][]string, 0, len(usage))
	for _, u := range usage {
		if u.Unreachable != "" {
			rows = append(rows, []string{u.App, u.Namespace, "-", "-", "-", "-", u.Unreachable})
			continue
		}
		pct := "-"
		if u.RAMPercent != nil {
			pct = strconv.Itoa(*u.RAMPercent) + "%"
		}
		row := []string{u.App, u.Namespace, strconv.Itoa(u.Pods), millicores(u.CPUCores), humanBytes(int64(u.RAMBytes)), pct}
		if failed {
			row = append(row, "")
		}
		rows = append(rows, row)
	}
	return output.StyledTable(os.Stdout, header, rows, nil)
}

// summarize sums an app's pods. A failed query is reported in the row rather
// than failing the whole table: one app without metrics is no reason to hide
// the rest.
func summarize(ctx context.Context, c *client.Client, app *client.App) appUsage {
	u := appUsage{App: app.Name, Namespace: app.Namespace.Name, RAMLimit: app.RAMLimit, CPURequest: app.CPURequest}
	pods, err := fetchPodUsage(ctx, c, app, false)
	if err != nil {
		u.Unreachable = "metrics unavailable"
		return u
	}
	u.Pods = len(pods)
	for _, p := range pods {
		u.CPUCores += p.CPUCores
		u.RAMBytes += p.RAMBytes
	}
	if limit, ok := parseBytes(app.RAMLimit); ok && u.Pods > 0 {
		// The limit is per pod, so the fair comparison is the busiest pod.
		var peak float64
		for _, p := range pods {
			peak = max(peak, p.RAMBytes)
		}
		pct := int(math.Round(peak / limit * percent))
		u.RAMPercent = &pct
	}
	return u
}

// fetchPodUsage queries an app's metrics and folds them into one row per pod.
// detail adds throttling and network, which `top apps` does not show.
func fetchPodUsage(ctx context.Context, c *client.Client, app *client.App, detail bool) ([]podUsage, error) {
	types := []string{client.InsightCPU, client.InsightRAM}
	if detail {
		types = append(types, client.InsightCPUThrottling, client.InsightNetworkReceive, client.InsightNetworkSend)
	}
	end := time.Now()
	byPod := map[string]*podUsage{}
	for _, t := range types {
		series, err := insightWithRetry(ctx, c, client.InsightQuery{
			AppID: app.ID, Type: t, Pod: client.AllPods, Start: end.Add(-topWindow), End: end, Step: topStep,
		})
		if err != nil {
			return nil, err
		}
		for _, s := range series {
			latest, ok := s.Latest()
			if !ok {
				continue
			}
			name := cmp.Or(s.Pod, "-")
			p := byPod[name]
			if p == nil {
				p = &podUsage{App: app.Name, Pod: name}
				byPod[name] = p
			}
			v := latest.Value
			switch t {
			case client.InsightCPU:
				p.CPUCores += v
			case client.InsightRAM:
				p.RAMBytes += v
			case client.InsightCPUThrottling:
				p.Throttled = &v
			case client.InsightNetworkReceive:
				p.RxPerSec = &v
			case client.InsightNetworkSend:
				p.TxPerSec = &v
			}
		}
	}
	out := make([]podUsage, 0, len(byPod))
	for _, p := range byPod {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b podUsage) int { return strings.Compare(a.Pod, b.Pod) })
	return out, nil
}

// insightWithRetry retries a metrics query that failed transiently. Across a
// whole tenant's worth of queries one usually does, and a blank row for an app
// that is fine is worse than a second of waiting.
func insightWithRetry(ctx context.Context, c *client.Client, q client.InsightQuery) ([]client.Series, error) {
	var err error
	for attempt := range topAttempts {
		var series []client.Series
		if series, err = c.Insight(ctx, q); err == nil || !client.IsTransient(err) {
			return series, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return nil, err
}

func sortUsage(u []appUsage, by string) {
	slices.SortStableFunc(u, func(a, b appUsage) int {
		switch by {
		case sortByCPU:
			if c := cmp.Compare(b.CPUCores, a.CPUCores); c != 0 {
				return c
			}
		case sortByMemory:
			if c := cmp.Compare(b.RAMBytes, a.RAMBytes); c != 0 {
				return c
			}
		}
		return strings.Compare(a.App, b.App)
	})
}

// parseBytes reads a memory quantity as the API writes it ("500M", "1G") or as
// Kubernetes does ("512Mi"). The API's plain suffixes are decimal.
func parseBytes(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   float64
	}{
		{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40},
		{"k", 1e3}, {"K", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12},
	}
	for _, u := range units {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseFloat(n, 64)
			return v * u.mult, err == nil && v > 0
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && v > 0
}

func millicores(cores float64) string {
	return strconv.Itoa(int(math.Round(cores*millicoresPerCore))) + "m"
}

func ofLimit(used, limit float64) string {
	if limit <= 0 {
		return "-"
	}
	return strconv.Itoa(int(math.Round(used/limit*percent))) + "%"
}

func ratio(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.Itoa(int(math.Round(*v*percent))) + "%"
}

func rate(v *float64) string {
	if v == nil {
		return "-"
	}
	return humanBytes(int64(*v)) + "/s"
}
