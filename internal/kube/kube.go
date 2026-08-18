// Package kube reads Darkube-managed workloads out of a Kubernetes cluster and
// compares them with what the Darkube API believes exists.
//
// It shells out to kubectl rather than linking client-go. That keeps a ~40MB
// dependency tree out of a small CLI, and — more usefully — it inherits whatever
// kubeconfig, context and OIDC login already work for the operator, including
// the exec-plugin credentials that Hamravesh clusters use.
//
// The comparison exists because the two sides can disagree, and the disagreement
// is otherwise invisible from either one alone. Deleting a Darkube app removes it
// from the API immediately but tears its Helm release down separately, and the
// release can be left behind indefinitely — holding the name, still running, and
// still labelled with the id of an app that no longer exists. Recreating under
// that name then fails with SameHelmReleaseNameExists forever. `get apps` cannot
// show it, because from the API's point of view there is nothing to show.
package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// AppIDLabel is the label every Darkube-managed workload carries. It records the
// id of the app that created the workload, and it outlives that app: the label is
// still on an orphaned release, so its presence is not evidence the app exists.
const AppIDLabel = "darkube.hamravesh.com/app-id"

// DefaultBinary is the kubectl executable looked up on PATH.
const DefaultBinary = "kubectl"

// Sentinel errors, comparable with errors.Is.
var (
	// ErrKubectlMissing means the kubectl binary was not on PATH.
	ErrKubectlMissing = errors.New("kubectl not found on PATH")

	// ErrKubectl means kubectl ran and failed; its stderr is wrapped in.
	ErrKubectl = errors.New("kubectl failed")
)

// Workload is one Deployment in the cluster and the Darkube app id it claims.
type Workload struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	AppID     string `json:"appId"`
}

// AppRef is the little this package needs to know about a Darkube app, so that
// it does not import the API client and can be tested without one.
type AppRef struct {
	ID        string
	Name      string
	Namespace string
}

// Kind classifies a disagreement between the Darkube app list and the cluster.
type Kind string

// The two ways the two sides can disagree.
const (
	// Orphaned is a workload running in the cluster whose Darkube app is gone.
	// This is the one that costs time: the release still holds the name, so the
	// app cannot be recreated, and nothing in the Darkube UI hints at it.
	Orphaned Kind = "orphaned"

	// NoWorkload is a Darkube app with nothing in the cluster carrying its id —
	// a create that never landed, or a workload deleted out from under it.
	NoWorkload Kind = "no-workload"
)

// Divergence is one app the two sides disagree about.
type Divergence struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	AppID     string `json:"appId"`
	Kind      Kind   `json:"kind"`
}

// Options selects which cluster and namespace to read.
type Options struct {
	// Binary is the kubectl executable; empty means DefaultBinary.
	Binary string
	// Kubeconfig is passed as --kubeconfig; empty leaves kubectl's own resolution alone.
	Kubeconfig string
	// Context is passed as --context; empty uses the current context.
	Context string
	// Namespace limits the read to one namespace; empty reads all of them.
	Namespace string
}

// binary returns the kubectl executable to run.
func (o Options) binary() string {
	if o.Binary == "" {
		return DefaultBinary
	}
	return o.Binary
}

// args builds the kubectl argument vector for listing deployments as JSON.
func (o Options) args() []string {
	args := []string{"get", "deployments", "-o", "json"}
	if o.Namespace == "" {
		args = append(args, "--all-namespaces")
	} else {
		args = append(args, "-n", o.Namespace)
	}
	if o.Kubeconfig != "" {
		args = append(args, "--kubeconfig", o.Kubeconfig)
	}
	if o.Context != "" {
		args = append(args, "--context", o.Context)
	}
	return args
}

// deploymentList is the slice of `kubectl get deployments -o json` we read.
type deploymentList struct {
	Items []struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
	} `json:"items"`
}

// Deployments lists the Deployments visible in the selected cluster and
// namespace, including ones with no Darkube label — the caller decides what to
// do with those, and Diff ignores them.
func Deployments(ctx context.Context, o Options) ([]Workload, error) {
	bin, err := exec.LookPath(o.binary())
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrKubectlMissing, o.binary())
	}

	// The binary and every flag value below come from the operator's own command
	// line, not from anything the API returned.
	cmd := exec.CommandContext(ctx, bin, o.args()...) //nolint:gosec // operator-supplied binary and flags
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrKubectl, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("running kubectl: %w", err)
	}

	var list deploymentList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parsing kubectl output: %w", err)
	}

	workloads := make([]Workload, 0, len(list.Items))
	for _, it := range list.Items {
		workloads = append(workloads, Workload{
			Name:      it.Metadata.Name,
			Namespace: it.Metadata.Namespace,
			AppID:     it.Metadata.Labels[AppIDLabel],
		})
	}
	return workloads, nil
}

// Diff reports where the Darkube app list and the cluster disagree.
//
// Only namespaces that appear in workloads are considered. A tenant's apps are
// spread over several clusters and one kubeconfig context reaches one of them, so
// comparing against the whole app list would report every app on every other
// cluster as missing. Restricting to namespaces the cluster actually returned
// keeps the answer to what was really looked at — which does mean an app in a
// namespace holding no Deployments at all is out of scope.
//
// Workloads with no Darkube label are skipped: they were not created by Darkube
// and are nobody's orphan.
func Diff(apps []AppRef, workloads []Workload) []Divergence {
	knownApps := make(map[string]struct{}, len(apps))
	for _, a := range apps {
		knownApps[a.ID] = struct{}{}
	}

	inCluster := make(map[string]struct{}, len(workloads))
	namespaces := make(map[string]struct{})
	for _, w := range workloads {
		namespaces[w.Namespace] = struct{}{}
		if w.AppID != "" {
			inCluster[w.AppID] = struct{}{}
		}
	}

	var found []Divergence
	for _, w := range workloads {
		if w.AppID == "" {
			continue
		}
		if _, ok := knownApps[w.AppID]; !ok {
			found = append(found, Divergence{Name: w.Name, Namespace: w.Namespace, AppID: w.AppID, Kind: Orphaned})
		}
	}
	for _, a := range apps {
		if _, seen := namespaces[a.Namespace]; !seen {
			continue
		}
		if _, ok := inCluster[a.ID]; !ok {
			found = append(found, Divergence{Name: a.Name, Namespace: a.Namespace, AppID: a.ID, Kind: NoWorkload})
		}
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].Namespace != found[j].Namespace {
			return found[i].Namespace < found[j].Namespace
		}
		if found[i].Kind != found[j].Kind {
			return found[i].Kind < found[j].Kind
		}
		return found[i].Name < found[j].Name
	})
	return found
}
