package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

func TestParseEnvPairsKeepsEqualsInValue(t *testing.T) {
	t.Parallel()

	got, err := parseEnvPairs([]string{"DSN=host=db port=5432 sslmode=off", "EMPTY="})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].Value != "host=db port=5432 sslmode=off" {
		t.Errorf("value truncated at the second '=': %q", got[0].Value)
	}
	if got[1].Name != "EMPTY" || got[1].Value != "" {
		t.Errorf("want EMPTY set to the empty string, got %+v", got[1])
	}
}

func TestParseEnvPairsRejectsBareWord(t *testing.T) {
	t.Parallel()

	if _, err := parseEnvPairs([]string{"NOEQUALS"}); err == nil {
		t.Error("want an error for an argument with no '='")
	}
	if _, err := parseEnvPairs([]string{"=novalue"}); err == nil {
		t.Error("want an error for an empty name")
	}
}

// Setting must be a merge, not a replace: an app's untouched variables have to
// survive, because the write path PUTs the whole object back.
func TestApplyEnvChangeMergesRatherThanReplaces(t *testing.T) {
	t.Parallel()

	app := map[string]any{"envs": []any{
		map[string]any{"name": "KEEP", "value": "as-is"},
		map[string]any{"name": "LOG_LEVEL", "value": "info"},
	}}

	err := applyEnvChange(app,
		[]client.EnvVar{{Name: "LOG_LEVEL", Value: "debug"}, {Name: "ADDED", Value: "new"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := map[string]string{}
	for _, e := range client.EnvVars(app) {
		got[e.Name] = e.Value
	}
	want := map[string]string{"KEEP": "as-is", "LOG_LEVEL": "debug", "ADDED": "new"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: want %q, got %q", k, v, got[k])
		}
	}
}

func TestApplyEnvChangeRemoves(t *testing.T) {
	t.Parallel()

	app := map[string]any{"envs": []any{
		map[string]any{"name": "GONE", "value": "x"},
		map[string]any{"name": "STAYS", "value": "y"},
	}}
	if err := applyEnvChange(app, nil, []string{"GONE"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	envs := client.EnvVars(app)
	if len(envs) != 1 || envs[0].Name != "STAYS" {
		t.Errorf("want only STAYS, got %v", envs)
	}
}

// Removing something that is not there is a typo, not a no-op: silently
// succeeding would report "environment updated" while changing nothing.
func TestApplyEnvChangeRejectsUnknownRemoval(t *testing.T) {
	t.Parallel()

	app := map[string]any{"envs": []any{map[string]any{"name": "REAL", "value": "1"}}}
	err := applyEnvChange(app, nil, []string{"TYPO"})
	if !errors.Is(err, client.ErrNoSuchEnv) {
		t.Errorf("want ErrNoSuchEnv, got %v", err)
	}
}

// A secret variable's value is vault-backed and write-only, so a plain `set env`
// on that name would silently create a shadowing plain variable instead.
func TestApplyEnvChangeRefusesToShadowASecret(t *testing.T) {
	t.Parallel()

	app := map[string]any{
		"envs":        []any{},
		"secret_envs": []any{map[string]any{"name": "DB_PASSWORD", "value": ""}},
	}
	err := applyEnvChange(app, []client.EnvVar{{Name: "DB_PASSWORD", Value: "hunter2"}}, nil)
	if err == nil {
		t.Fatal("want an error when setting a name that is already a secret")
	}
}

func TestMergeEnvs(t *testing.T) {
	t.Parallel()
	base := []client.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}}

	got, err := mergeEnvs(base, []client.EnvVar{{Name: "A", Value: "9"}, {Name: "C", Value: "3"}}, []string{"B"})
	if err != nil {
		t.Fatal(err)
	}
	want := []client.EnvVar{{Name: "A", Value: "9"}, {Name: "C", Value: "3"}}
	if !sameEnvs(got, want) || got[0].Name != "A" {
		t.Fatalf("got %v, want %v with order kept", got, want)
	}
	if base[0].Value != "1" {
		t.Fatal("mergeEnvs modified its input")
	}

	if _, err := mergeEnvs(base, nil, []string{"NOPE"}); !errors.Is(err, client.ErrNoSuchEnv) {
		t.Fatalf("removing a missing name: got %v, want ErrNoSuchEnv", err)
	}
}

func TestSameEnvs(t *testing.T) {
	t.Parallel()
	a := []client.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}}
	if !sameEnvs(a, []client.EnvVar{{Name: "B", Value: "2"}, {Name: "A", Value: "1"}}) {
		t.Error("order should not matter")
	}
	if sameEnvs(a, []client.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "3"}}) {
		t.Error("a different value must not compare equal")
	}
	if sameEnvs(a, a[:1]) {
		t.Error("a missing entry must not compare equal")
	}
}

func TestReadAPIBody(t *testing.T) {
	t.Parallel()
	if b, err := readAPIBody(""); err != nil || b != nil {
		t.Fatalf("empty: got %q, %v", b, err)
	}
	if b, err := readAPIBody(`{"a":1}`); err != nil || string(b) != `{"a":1}` {
		t.Fatalf("literal: got %q, %v", b, err)
	}
	f := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(f, []byte(`{"b":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := readAPIBody("@" + f); err != nil || string(b) != `{"b":2}` {
		t.Fatalf("@file: got %q, %v", b, err)
	}
}
