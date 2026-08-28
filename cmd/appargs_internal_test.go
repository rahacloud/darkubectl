package cmd

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestShellWordsAcceptsScalarAndSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		yaml   string
		want   string
		isList bool
	}{
		{name: "scalar", yaml: `command: /bin/sh -c`, want: "/bin/sh -c"},
		{name: "sequence", yaml: "command:\n  - /bin/sh\n  - -c", want: "/bin/sh -c", isList: true},
		{name: "single item sequence", yaml: "command:\n  - /bin/sh", want: "/bin/sh", isList: true},
		{name: "absent", yaml: `other: x`, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var spec struct {
				Command shellWords `yaml:"command"`
			}
			if err := yaml.Unmarshal([]byte(tc.yaml), &spec); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := spec.Command.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
			if spec.Command.IsList != tc.isList {
				t.Errorf("IsList = %v, want %v", spec.Command.IsList, tc.isList)
			}
		})
	}
}

func TestValidateArgsRejectsMultiItemList(t *testing.T) {
	t.Parallel()

	// A list says "these are separate arguments", which the platform cannot
	// express: args arrives as exactly one argv entry.
	var spec struct {
		Args shellWords `yaml:"args"`
	}
	if err := yaml.Unmarshal([]byte("args:\n  - -c\n  - echo hi"), &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := validateArgs(spec.Args); !errors.Is(err, errArgsMultiList) {
		t.Errorf("validateArgs = %v, want errArgsMultiList", err)
	}
}

func TestValidateArgsAllowsSingleValues(t *testing.T) {
	t.Parallel()

	for _, doc := range []string{`args: onething`, "args:\n  - onething", `other: x`} {
		var spec struct {
			Args shellWords `yaml:"args"`
		}
		if err := yaml.Unmarshal([]byte(doc), &spec); err != nil {
			t.Fatalf("unmarshal %q: %v", doc, err)
		}
		if err := validateArgs(spec.Args); err != nil {
			t.Errorf("validateArgs(%q) = %v, want nil", doc, err)
		}
	}
}

func TestEntrypointWarningsFlagsWhitespaceInArgs(t *testing.T) {
	t.Parallel()

	// The exact shape that crash-looped a real deploy with `/bin/sh: illegal
	// option -`: the flag was in args, where nothing splits it.
	got := entrypointWarnings("/bin/sh", "-c echo hello")
	if len(got) == 0 {
		t.Fatal("expected a warning for whitespace in args, got none")
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "ONE argument") {
		t.Errorf("warning should explain args is not split; got:\n%s", joined)
	}
}

func TestEntrypointWarningsQuietForTheWorkingShape(t *testing.T) {
	t.Parallel()

	// The form that actually runs: the flag lives in command (which is split),
	// and the script carries no whitespace.
	if got := entrypointWarnings("/bin/sh -c", "echo$IFS'hi';exec$IFS/bin/true"); len(got) != 0 {
		t.Errorf("expected no warnings for the known-good shape, got %v", got)
	}
}

func TestEntrypointWarningsFlagsShellWithoutDashC(t *testing.T) {
	t.Parallel()

	// A shell with no -c reads stdin, finds none and exits 0 — which looks like a
	// container that started cleanly and then vanished.
	got := entrypointWarnings("/bin/sh", "script")
	if len(got) != 1 || !strings.Contains(got[0], "-c") {
		t.Errorf("expected a -c warning, got %v", got)
	}
}

func TestEntrypointWarningsIgnoresNonShellCommands(t *testing.T) {
	t.Parallel()

	if got := entrypointWarnings("/bin/blackbox_exporter", "--config.file=/x"); len(got) != 0 {
		t.Errorf("expected no warnings, got %v", got)
	}
}

func TestIsShell(t *testing.T) {
	t.Parallel()

	shells := []string{"sh", "/bin/sh", "/bin/sh -c", "bash", "/usr/bin/env", "busybox"}
	want := map[string]bool{
		"sh": true, "/bin/sh": true, "/bin/sh -c": true,
		"bash": true, "/usr/bin/env": false, "busybox": true,
	}
	for _, s := range shells {
		if got := isShell(s); got != want[s] {
			t.Errorf("isShell(%q) = %v, want %v", s, got, want[s])
		}
	}
}
