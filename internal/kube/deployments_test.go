package kube_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/kube"
)

// fakeKubectl writes a shell script standing in for kubectl. It records its
// arguments to args.txt next to itself, prints stdout, writes stderr, and exits
// with code.
func fakeKubectl(t *testing.T, stdout, stderr string, code int) (bin, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	for name, body := range map[string]string{"stdout": stdout, "stderr": stderr} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + argsFile + "\"\n" +
		`cat "` + filepath.Join(dir, "stdout") + "\"\n" +
		`cat "` + filepath.Join(dir, "stderr") + "\" >&2\n" +
		"exit " + string(rune('0'+code)) + "\n"
	bin = filepath.Join(dir, "kubectl")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}
	return bin, argsFile
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
