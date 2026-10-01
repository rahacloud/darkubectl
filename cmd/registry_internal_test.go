package cmd

import (
	"errors"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestMatchRepository(t *testing.T) {
	t.Parallel()

	const prefix = "registry.hamdocker.ir/acme"
	repos := []client.Repository{{Name: "acme/my-api"}, {Name: "acme/worker"}, {Name: "other/worker"}, {Name: "web"}}
	cases := map[string]string{
		"acme/my-api":                       "acme/my-api",
		"my-api":                            "acme/my-api",
		"registry.hamdocker.ir/acme/my-api": "acme/my-api",
		"web":                               "web",
	}
	for in, want := range cases {
		got, err := matchRepository(repos, prefix, in)
		if err != nil || got != want {
			t.Errorf("matchRepository(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := matchRepository(repos, prefix, "worker"); !errors.Is(err, errAmbiguousImage) {
		t.Errorf("worker: err = %v, want ambiguous", err)
	}
	if _, err := matchRepository(repos, prefix, "nope"); !errors.Is(err, errNoSuchImage) {
		t.Errorf("nope: err = %v, want no such image", err)
	}
}

func TestSplitTag(t *testing.T) {
	t.Parallel()

	cases := [][3]string{
		{"my-api:v1", "my-api", "v1"},
		{"my-api", "my-api", ""},
		{"host:5000/acme/my-api", "host:5000/acme/my-api", ""},
		{"host:5000/acme/my-api:abc-12", "host:5000/acme/my-api", "abc-12"},
	}
	for _, c := range cases {
		if img, tag := splitTag(c[0]); img != c[1] || tag != c[2] {
			t.Errorf("splitTag(%q) = %q, %q; want %q, %q", c[0], img, tag, c[1], c[2])
		}
	}
}

// Prune keeps the newest --keep manifests and never deletes one an app runs,
// however old it is.
func TestPruneCandidatesSparesDeployedTags(t *testing.T) {
	t.Parallel()

	digests := []client.Digest{
		{Digest: "sha256:old", Tags: []string{"a1"}, LastPushed: "2026-09-01T00:00:00Z"},
		{Digest: "sha256:new", Tags: []string{"a4"}, LastPushed: "2026-09-04T00:00:00Z"},
		{Digest: "sha256:live", Tags: []string{"a2"}, LastPushed: "2026-09-02T00:00:00Z"},
		{Digest: "sha256:mid", Tags: []string{"a3"}, LastPushed: "2026-09-03T00:00:00Z"},
	}
	apps := []client.App{
		{ImageRepo: "registry.hamdocker.ir/acme/my-api", ImageTag: "a2"},
		{ImageRepo: "registry.hamdocker.ir/acme/other", ImageTag: "a1"},
	}
	doomed, spared := pruneCandidates(digests, 1, deployedTags(apps, "acme/my-api"))

	if len(spared) != 1 || spared[0].Digest != "sha256:live" {
		t.Errorf("spared = %+v", spared)
	}
	if len(doomed) != 2 || doomed[0].Digest != "sha256:mid" || doomed[1].Digest != "sha256:old" {
		t.Errorf("doomed = %+v", doomed)
	}
	// The input order is left alone.
	if digests[0].Digest != "sha256:old" {
		t.Error("pruneCandidates reordered its input")
	}
}

func TestHumanBytes(t *testing.T) {
	t.Parallel()

	cases := map[int64]string{0: "0B", 1023: "1023B", 1024: "1.0KiB", 1536: "1.5KiB", 5 << 30: "5.0GiB"}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestGCKeep(t *testing.T) {
	t.Parallel()

	n, d := 20, "30 00:00:00"
	if got := gcKeep(client.GCStrategy{KeepType: client.GCKeepCount, KeepCount: &n}); got != "newest 20" {
		t.Errorf("count: %q", got)
	}
	if got := gcKeep(client.GCStrategy{KeepType: client.GCKeepDuration, KeepDuration: &d}); got != "pushed in the last 30d" {
		t.Errorf("duration: %q", got)
	}
}
