package cmd

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
)

// --- rollout ---

func pod(name string, ready, terminating bool, restarts int) appstate.Pod {
	return appstate.Pod{
		Name: name, Ready: ready, Terminating: terminating,
		Containers: []appstate.Container{{Name: "main", Ready: ready, RestartCount: restarts}},
	}
}

func TestRolloutDone(t *testing.T) {
	t.Parallel()

	old := []string{"api-a"}
	cases := []struct {
		name string
		pods []appstate.Pod
		want bool
	}{
		{"no pods yet", nil, false},
		// The platform reports the app healthy here; the rollout has not begun.
		{"only the old pod", []appstate.Pod{pod("api-a", true, false, 0)}, false},
		{"surge, new not ready", []appstate.Pod{pod("api-a", true, false, 0), pod("api-b", false, false, 0)}, false},
		{"old terminating", []appstate.Pod{pod("api-a", true, true, 0), pod("api-b", true, false, 0)}, false},
		{"replaced and ready", []appstate.Pod{pod("api-b", true, false, 0)}, true},
	}
	for _, tc := range cases {
		if got, summary := rolloutDone(tc.pods, old, false); got != tc.want {
			t.Errorf("%s: rolloutDone = %v (%s), want %v", tc.name, got, summary, tc.want)
		}
	}
}

// A replacement that keeps restarting will never be ready; the summary must
// say so rather than leave the caller to wait out the timeout in silence. The
// old pod's restarts are its own history and are not counted.
func TestRolloutDoneReportsRestartsOfNewPodsOnly(t *testing.T) {
	t.Parallel()

	_, summary := rolloutDone([]appstate.Pod{pod("api-a", true, false, 7), pod("api-b", false, false, 3)},
		[]string{"api-a"}, false)
	if !strings.Contains(summary, "3 restarts") {
		t.Errorf("summary = %q, want the new pod's 3 restarts", summary)
	}
}

func TestRolloutDoneHintsAtAMissingImageOnlyWhenOverdue(t *testing.T) {
	t.Parallel()

	pods := []appstate.Pod{pod("api-a", true, false, 0)}
	if _, s := rolloutDone(pods, []string{"api-a"}, false); strings.Contains(s, "image") {
		t.Errorf("hinted too early: %q", s)
	}
	if _, s := rolloutDone(pods, []string{"api-a"}, true); !strings.Contains(s, "image") {
		t.Errorf("no hint once overdue: %q", s)
	}
}

// --- probes ---

func TestProbeChange(t *testing.T) {
	t.Parallel()

	ready, empty, bad := "/healthz", "", "healthz"

	got, err := probeChange(false, &ready, nil)
	if err != nil || got[keyReadinessPath] != "/healthz" || len(got) != 1 {
		t.Errorf("readiness only: %v, %v", got, err)
	}
	// An explicitly empty value removes that probe; absence leaves it alone.
	if got, err = probeChange(false, nil, &empty); err != nil || got[keyLivenessPath] != "" || len(got) != 1 {
		t.Errorf("remove liveness: %v, %v", got, err)
	}
	if _, err = probeChange(false, &bad, nil); !errors.Is(err, errProbeNotAPath) {
		t.Errorf("relative path: err = %v", err)
	}
	if _, err = probeChange(false, nil, nil); !errors.Is(err, errProbeArgs) {
		t.Errorf("nothing given: err = %v", err)
	}
	if _, err = probeChange(true, &ready, nil); !errors.Is(err, errProbeArgs) {
		t.Errorf("--clear with a path: err = %v", err)
	}
}

// --- resources ---

func TestValidateResources(t *testing.T) {
	t.Parallel()

	for _, ok := range [][3]string{{"2", "", ""}, {"", "1500M", ""}, {"", "", "750m"}} {
		if err := validateResources(ok[0], ok[1], ok[2]); err != nil {
			t.Errorf("validateResources%v = %v", ok, err)
		}
	}
	// The platform's own spellings only: anything else would be stored as
	// given and never read back equal.
	for _, bad := range [][3]string{{"", "", ""}, {"", "1G", ""}, {"", "1500", ""}, {"", "", "0.5"}, {"", "", "500M"}} {
		if err := validateResources(bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("validateResources%v accepted", bad)
		}
	}
}

func TestResourcesRoundTrip(t *testing.T) {
	t.Parallel()

	plan := &client.Plan{ID: "p-dyn", CostType: costTypeDynamic}
	raw := map[string]any{keyPlan: "p-fixed", keyRAMLimit: "500M", keyCPURequest: "250m"}
	if err := applyResources(raw, plan, true, "700M", "300m"); err != nil {
		t.Fatal(err)
	}
	if raw[keyPlan] != "p-dyn" || raw[keyRAMLimit] != "700M" || raw[keyCPURequest] != "300m" {
		t.Fatalf("applyResources wrote %v", raw)
	}
	// A read nests the plan; resourcesTook has to look inside it.
	read := map[string]any{keyPlan: map[string]any{"id": "p-dyn"}, keyRAMLimit: "700M", keyCPURequest: "300m"}
	if !resourcesTook(read, plan, true, "700M", "300m") {
		t.Error("resourcesTook rejected a matching read")
	}
	read[keyRAMLimit] = "1000M" // what a fixed plan does to the value
	if resourcesTook(read, plan, true, "700M", "300m") {
		t.Error("resourcesTook accepted a discarded memory change")
	}
}

// --- autoscale ---

func TestHPAFromFlags(t *testing.T) {
	t.Parallel()

	if got, err := hpaFromFlags(false, true, 2, 6, 70); err != nil || got != (hpaSpec{true, 2, 6, 70}) {
		t.Errorf("valid bounds: %+v, %v", got, err)
	}
	if got, err := hpaFromFlags(true, false, 0, 0, 80); err != nil || got.Enabled {
		t.Errorf("--disable: %+v, %v", got, err)
	}
	for _, bad := range [][3]int{{0, 3, 80}, {3, 2, 80}, {1, 2, 0}} {
		if _, err := hpaFromFlags(false, true, bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("hpaFromFlags%v accepted", bad)
		}
	}
	if _, err := hpaFromFlags(false, false, 0, 0, 80); !errors.Is(err, errAutoscaleArgs) {
		t.Errorf("no bounds: err = %v", err)
	}
}

// Other chart values in custom_config must survive, and disabling keeps the
// bounds so the next enable has them.
func TestApplyHPAKeepsTheRestOfCustomConfig(t *testing.T) {
	t.Parallel()

	raw := map[string]any{keyCustomConfig: map[string]any{"container": map[string]any{"readinessProbe": "x"}}}
	_ = applyHPA(raw, hpaSpec{Enabled: true, Min: 1, Max: 3, CPUPercent: 70})
	_ = applyHPA(raw, hpaSpec{})

	cfg, _ := raw[keyCustomConfig].(map[string]any)
	if _, ok := cfg["container"]; !ok {
		t.Error("custom_config.container was dropped")
	}
	hpa, _ := cfg[keyHPA].(map[string]any)
	if hpa["enabled"] != false || hpa["maxReplicas"] != 3 {
		t.Errorf("hpa after disable = %v", hpa)
	}
	if raw[keyHPAEnabled] != false {
		t.Error("is_hpa_enabled not cleared")
	}
}

// --- apply / export ---

// liveApp is a read of a docker-image app, shaped the way the API returns it.
func liveApp() map[string]any {
	return map[string]any{
		"name":            "api",
		"creation_method": client.CreationMethodDockerImage,
		"namespace":       map[string]any{"id": float64(7), "name": "prod"},
		keyPlan:           map[string]any{"id": "p1"},
		keyImageRepo:      "registry/api",
		keyImageTag:       "1.0",
		"replicas":        float64(2),
		keyCommand:        "/bin/sh -c",
		keyArgs:           "exec ./api --port 8080",
		"svc": map[string]any{
			"type":            svcTypeLoadBalancer,
			"externalAddress": "x.hsvc.ir",
			"ports": map[string]any{
				"http": map[string]any{"containerPort": float64(8080), "servicePort": float64(80), "protocol": "TCP", "nodePort": float64(30410)},
			},
		},
		keyDisk: map[string]any{
			keyDiskSize: float64(10), "storage_class_name": "rawfile-btrfs", "set_fsgroup": true, "partitions": []any{},
		},
		"envs":           []any{map[string]any{"name": "A", "value": "1"}},
		keyReadinessPath: "/ready",
		keyLivenessPath:  nil,
	}
}

// The central property of export and apply: applying the spec an app exports
// changes nothing. Verified against 79 live apps in talaland on 2026-09-27;
// this pins it without the network.
func TestSpecRoundTripIsANoOp(t *testing.T) {
	t.Parallel()

	plans := []client.Plan{{ID: "p1", CodeName: "2", CostType: "fixed"}}
	namespaces := []client.Namespace{{ID: 7, Name: "prod"}}
	spec, _ := specFromApp(liveApp(), plans, namespaces)

	if spec.Namespace != "prod" || spec.Plan != "2" || spec.Image != "registry/api:1.0" {
		t.Fatalf("exported %+v", spec)
	}

	raw := liveApp()
	client.NormalizeForPut(raw)
	before := renderValue(raw)
	plan := &plans[0]
	if err := applySpecTo(raw, spec, plan, 7); err != nil {
		t.Fatal(err)
	}
	if changes := diffApps(decodeRendered(t, before), raw); len(changes) != 0 {
		t.Errorf("applying the exported spec changed %v", changes)
	}
}

func decodeRendered(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Two projects can share a name on different clusters; the export must then
// name the namespace by id, or the spec would resolve to the wrong one.
func TestNamespaceRefFallsBackToIDWhenNamesCollide(t *testing.T) {
	t.Parallel()

	raw := map[string]any{"namespace": map[string]any{"id": float64(156357), "name": "rahacloud"}}
	shared := []client.Namespace{{ID: 156357, Name: "rahacloud"}, {ID: 156356, Name: "rahacloud"}}
	if got := namespaceRef(raw, shared); got != "156357" {
		t.Errorf("namespaceRef = %q, want the id", got)
	}
	if got := namespaceRef(raw, shared[:1]); got != "rahacloud" {
		t.Errorf("namespaceRef = %q, want the name", got)
	}
}

// Rewriting the port list must keep an allocated nodePort, or a LoadBalancer
// app's public port would move under its clients on every apply.
func TestApplySvcKeepsNodePorts(t *testing.T) {
	t.Parallel()

	raw := liveApp()
	err := applySvc(raw, "", map[string]client.Port{
		"http":    {ContainerPort: 8080, ServicePort: 80},
		"metrics": {ContainerPort: 9090},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := raw["svc"].(map[string]any)
	ports, _ := svc["ports"].(map[string]any)
	http, _ := ports["http"].(map[string]any)
	if got := http["nodePort"]; got != float64(30410) {
		t.Errorf("http nodePort = %v, want it kept", got)
	}
	metrics, _ := ports["metrics"].(map[string]any)
	if metrics["servicePort"] != 9090 || metrics["protocol"] != "TCP" {
		t.Errorf("metrics defaults = %v", metrics)
	}
}

func TestApplySpecRefusesWhatTheAPICannotDo(t *testing.T) {
	t.Parallel()

	shrink := appSpec{Name: "api", Namespace: "prod", Disk: &client.Disk{SizeInGi: 5}}
	if err := applySpecTo(normalized(liveApp()), shrink, nil, 7); err == nil {
		t.Error("accepted a disk shrink")
	}
	git := appSpec{Name: "api", Namespace: "prod", Git: &gitSpec{Branch: "main"}}
	if err := applySpecTo(normalized(liveApp()), git, nil, 7); !errors.Is(err, errApplyGitOnImage) {
		t.Errorf("git on an image app: err = %v", err)
	}
}

func normalized(raw map[string]any) map[string]any {
	client.NormalizeForPut(raw)
	return raw
}

// An unset probe reads back as null; writing "" over it would show as a change
// on every apply of a spec that has no such probe.
func TestSetProbePathLeavesNullAlone(t *testing.T) {
	t.Parallel()

	raw := map[string]any{keyLivenessPath: nil}
	setProbePath(raw, keyLivenessPath, "")
	if raw[keyLivenessPath] != nil {
		t.Errorf("null became %q", raw[keyLivenessPath])
	}
}

func TestLoadAppSpecsReadsEveryDocument(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "apps.yaml")
	body := "name: a\nnamespace: prod\n---\nname: b\nnamespace: prod\nreplicas: 0\n---\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	specs, err := loadAppSpecs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].Replicas != nil || specs[1].Replicas == nil || *specs[1].Replicas != 0 {
		t.Errorf("specs = %+v", specs)
	}
	// Leaving replicas out creates one replica, not zero.
	if specs[0].replicas() != 1 {
		t.Errorf("default replicas = %d", specs[0].replicas())
	}
}

// Verification compares only svc's writable members: the platform adds
// addresses and nodePorts, which must not read as a dropped write.
func TestSameAfterWriteIgnoresPlatformFilledSvcFields(t *testing.T) {
	t.Parallel()

	written := map[string]any{"type": "LoadBalancer", "ports": map[string]any{
		"http": map[string]any{"containerPort": 80, "servicePort": 80, "protocol": "TCP"},
	}}
	read := map[string]any{"type": "LoadBalancer", "externalIP": "1.2.3.4", "ports": map[string]any{
		"http": map[string]any{"containerPort": float64(80), "servicePort": float64(80), "protocol": "TCP", "nodePort": float64(31000)},
	}}
	if !sameAfterWrite("svc", read, written) {
		t.Error("platform-filled svc fields read as a mismatch")
	}
}

// --- cp ---

func TestParseRemoteRef(t *testing.T) {
	t.Parallel()

	cases := map[string]remoteRef{
		"my-app:/etc/x": {app: "my-app", path: "/etc/x"},
		"my-app:rel":    {app: "my-app", path: "rel"},
	}
	for in, want := range cases {
		if got, ok := parseRemoteRef(in); !ok || got != want {
			t.Errorf("parseRemoteRef(%q) = %+v, %v", in, got, ok)
		}
	}
	for _, local := range []string{"./a:b", "/tmp/a:b", "plain", `C:\x`, "C:/x", "app:"} {
		if _, ok := parseRemoteRef(local); ok {
			t.Errorf("parseRemoteRef(%q) took a local path for remote", local)
		}
	}
}

// The payload arrives after the shell's echo of the command, wrapped at 76 and
// with CRLF line ends from the PTY.
func TestCapturedPayloadDecodes(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte{0, 1, 2, 250, 251}, 40)
	enc := base64.StdEncoding.EncodeToString(data)
	var wrapped strings.Builder
	for i := 0; i < len(enc); i += 76 {
		wrapped.WriteString(enc[i:min(i+76, len(enc))] + "\r\n")
	}
	stream := "/ # printf '__DK_BEGIN_%s__\\n' 'abc'; base64 < f\r\n__DK_BEGIN_abc__\r\n" + wrapped.String()

	payload, ok := afterBeginMarker([]byte(stream), "abc")
	if !ok {
		t.Fatal("marker not found")
	}
	got, err := decodeTerminalBase64(payload)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("decoded %d bytes, err %v", len(got), err)
	}
	// The echoed command line holds the marker's pieces but never the marker.
	if _, ok := afterBeginMarker([]byte("printf '__DK_BEGIN_%s__\\n' 'abc'"), "abc"); ok {
		t.Error("matched the echo of the command")
	}
}

func TestTarRoundTrip(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), "hello")
	mustWrite(t, filepath.Join(src, "sub", "b.txt"), "nested")

	archive, err := tarDirectory(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := untarInto(archive, dst); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"a.txt": "hello", "sub/b.txt": "nested"} {
		got, err := os.ReadFile(filepath.Join(dst, rel)) //nolint:gosec // a path the test itself built
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", rel, got, err)
		}
	}
}

// The archive comes from a pod; an entry climbing out of the destination is
// refused, not written.
func TestUntarRefusesEscapes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "../escaped", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()

	dir := filepath.Join(t.TempDir(), "out")
	if err := untarInto(buf.Bytes(), dir); !errors.Is(err, errTarEscape) {
		t.Errorf("err = %v, want errTarEscape", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped")); err == nil {
		t.Error("the escaping entry was written")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- get pods -w ---

// The server sends stale frames: a live pod missing from one, a dead pod back
// for one. Neither may print as a deletion or a resurrection.
func TestPodWatchPrinterRidesOutStaleFrames(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	w := &podWatchPrinter{last: map[string]string{}, missing: map[string]int{}, gone: map[string]bool{}}
	w.out = &out

	a, b := pod("api-a", true, false, 0), pod("api-b", true, false, 0)
	w.frame([]appstate.Pod{a, b})
	w.frame([]appstate.Pod{a}) // stale: b missing once
	w.frame([]appstate.Pod{a, b})
	if strings.Contains(out.String(), podDeleted) {
		t.Fatalf("one missing frame printed a deletion:\n%s", out.String())
	}

	w.frame([]appstate.Pod{b})
	w.frame([]appstate.Pod{b})    // a gone for two frames: deleted
	w.frame([]appstate.Pod{a, b}) // stale: a back from the dead
	if n := strings.Count(out.String(), "api-a"); n != 2 {
		t.Errorf("api-a printed %d times, want its first row and its deletion:\n%s", n, out.String())
	}
}
