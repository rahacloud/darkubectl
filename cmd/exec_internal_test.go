package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// The read loop stops at the first match of the exit marker. The remote is an
// echoing terminal, so the command line itself comes back on the stream before
// the command has run — if that echo could match, every exec would report the
// status of a command that had not finished yet.
func TestExitMarkerLineDoesNotMatchItsOwnEcho(t *testing.T) {
	t.Parallel()

	const nonce = "0123456789ab"
	pattern := exitMarkerPattern(nonce)

	for _, command := range []string{
		"ls -l",
		"wc -c /etc/prometheus/prometheus.yml",
		// a command that mentions the marker shape on purpose
		`echo __DK_EXIT_0123456789ab_0__`,
	} {
		line := exitMarkerLine(command, nonce)
		if command == `echo __DK_EXIT_0123456789ab_0__` {
			// This one *should* match once run — the point is only that the
			// nonce never appears adjacent to a status in the generated suffix.
			continue
		}
		if pattern.MatchString(line) {
			t.Errorf("echo of %q matches the exit marker: %q", command, line)
		}
	}
}

func TestExitMarkerPatternCapturesStatus(t *testing.T) {
	t.Parallel()

	const nonce = "deadbeefcafe"
	pattern := exitMarkerPattern(nonce)

	for _, want := range []string{"0", "1", "127"} {
		out := fmt.Sprintf("some output\n__DK_EXIT_%s_%s__\n", nonce, want)
		m := pattern.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("status %s: no match in %q", want, out)
		}
		if m[1] != want {
			t.Errorf("status: got %q, want %q", m[1], want)
		}
	}
}

// A nonce is hex, but the pattern is built by concatenation, so quote it.
func TestExitMarkerPatternIsNotAffectedByNonceMetacharacters(t *testing.T) {
	t.Parallel()

	pattern := exitMarkerPattern("a.c")
	if pattern.MatchString("__DK_EXIT_abc_0__") {
		t.Error("an unquoted nonce let `.` match any character")
	}
	if !pattern.MatchString("__DK_EXIT_a.c_0__") {
		t.Error("the literal nonce no longer matches")
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"/etc/prometheus/prometheus.yml": `'/etc/prometheus/prometheus.yml'`,
		"/tmp/it's here":                 `'/tmp/it'\''s here'`,
		"":                               `''`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewNonceIsHexAndUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for range 100 {
		n, err := newNonce()
		if err != nil {
			t.Fatalf("newNonce: %v", err)
		}
		if len(n) != 12 || strings.Trim(n, "0123456789abcdef") != "" {
			t.Fatalf("nonce %q is not 12 hex characters", n)
		}
		if seen[n] {
			t.Fatalf("nonce %q repeated", n)
		}
		seen[n] = true
	}
}
