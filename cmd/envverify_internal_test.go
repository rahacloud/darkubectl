package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The shell echoes the probe command back before running it; that echo must
// not read as a report, or a probe that never ran would look like a pass.
func TestEnvProbeEchoDoesNotMatch(t *testing.T) {
	t.Parallel()
	cmd := envProbeCommand([]string{"A", "DB_URL"})
	if got := parseEnvProbe(cmd); len(got) != 0 {
		t.Fatalf("the command text itself parsed as a report: %v", got)
	}
}

// Run the probe against a real sh with a fake environ, so the hash the pod
// computes and the hash expectedEnvHash computes are proven to agree.
func TestEnvProbeHashMatchesLocalHash(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("no sha256sum")
	}
	value := "Server=x,1433;Database=W;User Id=sa;password=not-real$=x!%; T=1"
	script := strings.ReplaceAll(envProbeCommand([]string{"CONN", "ABSENT"}), "/proc/1/environ", "/proc/$$/environ")
	c := exec.CommandContext(t.Context(), "sh", "-c", script) //nolint:gosec // the script is envProbeCommand over fixed names
	c.Env = []string{"CONN=" + value, "PATH=" + os.Getenv("PATH")}
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := parseEnvProbe(string(out))
	lines, ok := compareEnvProbe([]client.EnvVar{{Name: "CONN", Value: value}}, got)
	if !ok {
		t.Fatalf("probe disagreed with local hash: %v (%s)", lines, out)
	}
	if got["ABSENT"] != "MISSING" {
		t.Fatalf("absent variable reported as %q", got["ABSENT"])
	}
	if _, ok := compareEnvProbe([]client.EnvVar{{Name: "CONN", Value: value + "x"}}, got); ok {
		t.Fatal("a different value compared equal")
	}
}

func TestParseMovePairs(t *testing.T) {
	t.Parallel()
	set, bare, err := parseMovePairs([]string{"A", "B=x=y"})
	if err != nil || len(bare) != 1 || bare[0] != "A" || len(set) != 1 || set[0].Value != "x=y" {
		t.Fatalf("got set=%v bare=%v err=%v", set, bare, err)
	}
	if _, _, err := parseMovePairs([]string{"=v"}); err == nil {
		t.Fatal("empty name accepted")
	}
}

func TestReadEnvFile(t *testing.T) {
	t.Parallel()
	f := t.TempDir() + "/env"
	if err := os.WriteFile(f, []byte("# c\n\nA=1\r\nB=x=$y'\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readEnvFile(f)
	if err != nil || len(got) != 2 || got[0] != "A=1" || got[1] != "B=x=$y'\"" {
		t.Fatalf("got %q, %v", got, err)
	}
}
