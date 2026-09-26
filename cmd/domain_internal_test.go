package cmd

import (
	"errors"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestApplyDomainChangeAddsRemovesAndDeduplicates(t *testing.T) {
	t.Parallel()

	app := map[string]any{"external_hosts": []any{"keep.example.com", "drop.example.com"}}

	err := applyDomainChange(app,
		[]string{"new.example.com", "keep.example.com"}, []string{"drop.example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := client.ExternalHosts(app)
	want := []string{"keep.example.com", "new.example.com"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("host %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestApplyDomainChangeRejectsUnknownRemoval(t *testing.T) {
	t.Parallel()

	app := map[string]any{"external_hosts": []any{"a.example.com"}}
	err := applyDomainChange(app, nil, []string{"b.example.com"})
	if !errors.Is(err, client.ErrNoSuchHost) {
		t.Errorf("want ErrNoSuchHost, got %v", err)
	}
}

// The platform defaults new apps to dns01, which never completes on a zone
// Hamravesh does not host: it waits on an _acme-challenge TXT record nobody
// writes, reports nothing, and leaves the site on plain HTTP. These cover the
// decision that avoids that, and the one case where dns01 is genuinely required.
func TestChallengeForCorrectsTheSilentDNS01Default(t *testing.T) {
	t.Parallel()

	got, note, err := challengeFor("", []string{"blog.example.com"}, "dns01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != challengeHTTP01 {
		t.Errorf("want %q, got %q", challengeHTTP01, got)
	}
	if note == "" {
		t.Error("want a note explaining why the default was overridden")
	}
}

func TestChallengeForKeepsDNS01ForWildcards(t *testing.T) {
	t.Parallel()

	// Let's Encrypt issues a wildcard only over dns01 — there is no host to
	// answer an HTTP challenge on, so "correcting" this would break issuance.
	got, note, err := challengeFor("", []string{"*.example.com"}, "http01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != challengeDNS01 {
		t.Errorf("want %q for a wildcard, got %q", challengeDNS01, got)
	}
	if note == "" {
		t.Error("want a note saying why a wildcard keeps dns01")
	}
}

func TestChallengeForLeavesACorrectAppAlone(t *testing.T) {
	t.Parallel()

	got, _, err := challengeFor("", []string{"blog.example.com"}, "http01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("want no change, got %q", got)
	}
}

func TestChallengeForHonoursAnExplicitChoiceAndRejectsNonsense(t *testing.T) {
	t.Parallel()

	got, _, err := challengeFor(challengeDNS01, []string{"blog.example.com"}, "http01")
	if err != nil || got != challengeDNS01 {
		t.Errorf("explicit choice must win: got %q, err %v", got, err)
	}
	if _, _, err := challengeFor("tls-alpn", nil, ""); !errors.Is(err, errBadChallenge) {
		t.Errorf("want errBadChallenge, got %v", err)
	}
}

func TestApplyIngressSettingsOnlyWritesWhatWasAskedFor(t *testing.T) {
	t.Parallel()

	raw := map[string]any{"replicas": 1}
	applyIngressSettings(raw, "", false)
	if len(raw) != 1 {
		t.Errorf("an unset call must touch nothing, got %v", raw)
	}

	applyIngressSettings(raw, challengeHTTP01, true)
	if raw["ssl_challenge_type"] != challengeHTTP01 {
		t.Errorf("challenge not written: %v", raw["ssl_challenge_type"])
	}
	if raw["enable_SSL"] != true {
		t.Error("a challenge is pointless without SSL enabled")
	}
	if raw["redirect_SSL"] != true {
		t.Error("redirect not written")
	}
}
