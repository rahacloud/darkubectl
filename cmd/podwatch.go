package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/output"
)

const (
	flagWatch = "watch"

	// podDeleted is the STATUS printed for a pod that has left the stream.
	podDeleted = "deleted"

	// watchColumnGap separates columns in watch output.
	watchColumnGap = 3
)

// watchPods prints the app's pods and then a line for each change, the way
// `kubectl get pods -w` does. Every frame carries the whole pod list, so a
// change is found by comparing each pod's row with the last one printed for it.
// AGE is left out of that comparison, or every frame would reprint every pod.
func watchPods(ctx context.Context, opts appstate.Options, format output.Format) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if format == output.JSON || format == output.YAML {
		enc := json.NewEncoder(os.Stdout)
		return appstate.WatchPods(ctx, opts, func(pods []appstate.Pod) error {
			if pods == nil {
				pods = []appstate.Pod{}
			}
			return enc.Encode(pods)
		})
	}

	wide := format == output.Wide
	w := &podWatchPrinter{out: os.Stdout, wide: wide, last: map[string]string{}, missing: map[string]int{}, gone: map[string]bool{}}
	return appstate.WatchPods(ctx, opts, func(pods []appstate.Pod) error {
		w.frame(pods)
		return nil
	})
}

// podWatchPrinter remembers what it last printed per pod.
//
// The server sends stale frames out of order: a pod already gone can reappear
// for one frame as plainly "running", and a live pod can be missing from one.
// Seen 2026-09-27 during a restart. So a pod is reported deleted only once it
// has been absent from two frames running, and a deleted pod is never
// resurrected — pod names are not reused.
type podWatchPrinter struct {
	out     io.Writer
	wide    bool
	widths  []int
	last    map[string]string
	order   []string
	missing map[string]int
	gone    map[string]bool
}

// absentFramesToDelete is how many consecutive frames a pod must be missing
// from before it is reported deleted.
const absentFramesToDelete = 2

func (w *podWatchPrinter) frame(pods []appstate.Pod) {
	if w.widths == nil {
		w.widths = watchWidths(podHeader(w.wide), pods, w.wide)
		w.print(podHeader(w.wide))
	}

	seen := map[string]bool{}
	for _, p := range pods {
		if w.gone[p.Name] {
			continue
		}
		seen[p.Name] = true
		delete(w.missing, p.Name)
		row := podRow(p, w.wide)
		key := rowKey(row)
		prev, known := w.last[p.Name]
		if known && prev == key {
			continue
		}
		if !known {
			w.order = append(w.order, p.Name)
		}
		w.last[p.Name] = key
		w.print(row)
	}
	// A pod that has left the stream is gone; say so once.
	for _, name := range slices.Clone(w.order) {
		if seen[name] {
			continue
		}
		if w.missing[name]++; w.missing[name] < absentFramesToDelete {
			continue
		}
		row := make([]string, len(podHeader(w.wide)))
		for i := range row {
			row[i] = "-"
		}
		row[0], row[2] = name, podDeleted
		w.print(row)
		delete(w.last, name)
		delete(w.missing, name)
		w.gone[name] = true
		w.order = slices.DeleteFunc(w.order, func(n string) bool { return n == name })
	}
}

// print writes one row padded to the widths fixed by the first frame. Watch
// output cannot be aligned after the fact the way a table is, since each line
// is written as the change happens.
func (w *podWatchPrinter) print(row []string) {
	var b strings.Builder
	for i, cell := range row {
		if i == len(row)-1 {
			b.WriteString(cell)
			break
		}
		fmt.Fprintf(&b, "%-*s", w.widths[i], cell)
	}
	fmt.Fprintln(w.out, b.String())
}

// rowKey is a row without its AGE cell, for change detection.
func rowKey(row []string) string {
	const ageCol = 4
	key := slices.Clone(row)
	if len(key) > ageCol {
		key[ageCol] = ""
	}
	return strings.Join(key, "\x00")
}

// watchWidths sizes each column from the header and the first frame, with room
// for a pod name a little longer than any seen so far.
func watchWidths(header []string, pods []appstate.Pod, wide bool) []int {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, p := range pods {
		for i, cell := range podRow(p, wide) {
			widths[i] = max(widths[i], len(cell))
		}
	}
	// Replacement pods keep the name's shape; statuses do not, so leave room.
	const statusCol, statusRoom = 2, len("terminating")
	widths[statusCol] = max(widths[statusCol], statusRoom)
	for i := range widths {
		widths[i] += watchColumnGap
	}
	return widths
}
