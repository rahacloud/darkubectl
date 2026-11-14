package kube_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/kube"
)

// sharedKubectl is the one fake kubectl script, written by TestMain before any
// test runs. Each test links to it rather than writing its own: a parallel test
// that forks while another still has a fresh executable open for writing hands
// that write descriptor to its child until exec, and running the executable in
// that window fails with "text file busy" (golang/go#22315).
var sharedKubectl string

// The script finds its data next to the name it was run by, which is the
// test's own symlink: it records its arguments to args.txt, prints stdout,
// writes stderr, and exits with the code in the code file.
const kubectlScript = `#!/bin/sh
d=$(dirname "$0")
printf '%s\n' "$@" > "$d/args.txt"
cat "$d/stdout"
cat "$d/stderr" >&2
exit "$(cat "$d/code")"
`

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("", "fake-kubectl-")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(dir) //nolint:errcheck // best-effort cleanup of a temp dir
		sharedKubectl = filepath.Join(dir, "kubectl")
		if err := os.WriteFile(sharedKubectl, []byte(kubectlScript), 0o700); err != nil { //nolint:gosec // an executable test fixture
			panic(err)
		}
	}
	return m.Run()
}

// fakeKubectl gives the test its own kubectl: a symlink to the shared script in
// a fresh directory holding what it should print and exit with. It returns the
// symlink and the file the arguments are recorded in.
func fakeKubectl(t *testing.T, stdout, stderr string, code int) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"stdout": stdout, "stderr": stderr, "code": strconv.Itoa(code)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "kubectl")
	if err := os.Symlink(sharedKubectl, bin); err != nil {
		t.Fatal(err)
	}
	return bin, filepath.Join(dir, "args.txt")
}

func TestDeploymentsParsesAndPassesFlags(t *testing.T) {
	t.Parallel()

	bin, argsFile := fakeKubectl(t, `{"items":[
		{"metadata":{"name":"api","namespace":"prod","labels":{"`+kube.AppIDLabel+`":"id-1"}}},
		{"metadata":{"name":"other","namespace":"prod"}}]}`, "", 0)

	got, err := kube.Deployments(context.Background(), kube.Options{
		Binary: bin, Kubeconfig: "/k/config", Context: "c11", Namespace: "prod",
	})
	if err != nil {
		t.Fatalf("Deployments: %v", err)
	}
	want := []kube.Workload{
		{Name: "api", Namespace: "prod", AppID: "id-1"},
		{Name: "other", Namespace: "prod"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("workload %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	raw, err := os.ReadFile(argsFile) //nolint:gosec // the test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(string(raw))
	wantArgs := []string{"get", "deployments", "-o", "json", "-n", "prod", "--kubeconfig", "/k/config", "--context", "c11"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestDeploymentsAllNamespaces(t *testing.T) {
	t.Parallel()

	bin, argsFile := fakeKubectl(t, `{"items":[]}`, "", 0)
	if _, err := kube.Deployments(context.Background(), kube.Options{Binary: bin}); err != nil {
		t.Fatalf("Deployments: %v", err)
	}
	raw, err := os.ReadFile(argsFile) //nolint:gosec // the test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "--all-namespaces") {
		t.Errorf("args %q lack --all-namespaces", raw)
	}
}

func TestDeploymentsMissingBinary(t *testing.T) {
	t.Parallel()

	_, err := kube.Deployments(context.Background(), kube.Options{Binary: filepath.Join(t.TempDir(), "no-kubectl")})
	if !errors.Is(err, kube.ErrKubectlMissing) {
		t.Fatalf("err = %v, want ErrKubectlMissing", err)
	}
}

func TestDeploymentsReportsKubectlStderr(t *testing.T) {
	t.Parallel()

	bin, _ := fakeKubectl(t, "", "error: You must be logged in to the server (Unauthorized)", 1)
	_, err := kube.Deployments(context.Background(), kube.Options{Binary: bin})
	if !errors.Is(err, kube.ErrKubectl) {
		t.Fatalf("err = %v, want ErrKubectl", err)
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("err = %v, want kubectl's stderr in it", err)
	}
}

func TestDeploymentsRejectsMalformedOutput(t *testing.T) {
	t.Parallel()

	bin, _ := fakeKubectl(t, "this is not json", "", 0)
	if _, err := kube.Deployments(context.Background(), kube.Options{Binary: bin}); err == nil {
		t.Fatal("Deployments accepted output that is not JSON")
	}
}
