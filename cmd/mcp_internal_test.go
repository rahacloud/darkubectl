package cmd

import (
	"strings"
	"testing"
)

func TestMCPClassify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args string
		want mcpAccess
	}{
		{"get apps", accessRead},
		{"get apps -o json", accessRead},
		{"-o json get apps", accessRead},
		{"-n acme get apps --namespace web", accessRead},
		{"--org=acme get apps", accessRead},
		{"get env my-api", accessRead},
		{"get env my-api --show-secrets", accessSecret},
		{"get deploy-token my-api", accessSecret},
		{"registry password", accessSecret},
		{"api GET /api/v1/darkube/plans/", accessSecret},
		{"api options /api/v1/darkube/apps/", accessSecret},
		{"api PUT /api/v1/darkube/apps/x/ -d {}", accessWrite},
		{"get pods my-api", accessRead},
		{"get pod my-api -w", accessBlocked},
		{"logs application my-api --follow", accessBlocked},
		{"logs app my-api -f", accessBlocked},
		{"logs app my-api --tail 50 -p", accessRead},
		{"describe app my-api -i", accessBlocked},
		{"rollout status my-api", accessRead},
		{"rollout restart my-api", accessWrite},
		{"delete app my-api --yes", accessWrite},
		{"delete app my-api --help", accessRead},
		{"delete", accessRead},
		{"help", accessRead},
		{"set env my-api A=b", accessWrite},
		{"exec app my-api -- ls -l", accessWrite},
		{"terminal my-api", accessBlocked},
		{"tunnel connect", accessBlocked},
		{"config use-tenant acme", accessBlocked},
		{"config view", accessRead},
		{"--token k get apps", accessBlocked},
		{"get apps --config /etc/x", accessBlocked},
		{"get orphans", accessRead},
		{"get orphans --kubectl /bin/sh", accessWrite},
		{"registry gc", accessRead},
		{"registry gc set --keep-count 3", accessWrite},
		{"mcp", accessBlocked},
	}
	root := NewApp()
	for _, tc := range cases {
		inv, err := resolveMCPInvocation(root, strings.Fields(tc.args))
		if err != nil {
			t.Errorf("%q: %v", tc.args, err)
			continue
		}
		if got, _ := inv.classify(); got != tc.want {
			t.Errorf("%q (path %q) = %s, want %s", tc.args, inv.key(), got, tc.want)
		}
	}
}

// A policy key that no longer names a command would silently turn a read
// back into a refused write, or worse, leave a blocked command unlisted.
func TestMCPPolicyKeysAreCommands(t *testing.T) {
	t.Parallel()

	root := NewApp()
	for key := range mcpPolicy {
		if key == "completion" { // added by urfave/cli at run time
			continue
		}
		inv, err := resolveMCPInvocation(root, strings.Fields(key))
		if err != nil {
			t.Errorf("%q: %v", key, err)
			continue
		}
		if inv.key() != key || len(inv.args) != 0 {
			t.Errorf("policy key %q resolves to %q with args %q", key, inv.key(), inv.args)
		}
	}
	for key := range mcpBlockedFlags {
		if inv, _ := resolveMCPInvocation(root, strings.Fields(key)); inv.key() != key {
			t.Errorf("blocked-flag key %q resolves to %q", key, inv.key())
		}
	}
}

func TestMCPResolveRejects(t *testing.T) {
	t.Parallel()

	for _, args := range []string{"get apps --no-such-flag", "-- get apps"} {
		if _, err := resolveMCPInvocation(NewApp(), strings.Fields(args)); err == nil {
			t.Errorf("%q: want an error", args)
		}
	}
}

func TestMCPCheck(t *testing.T) {
	t.Parallel()

	ro := &mcpServer{}
	if err := ro.check([]string{"get", "apps"}); err != nil {
		t.Errorf("read on a read-only server: %v", err)
	}
	if err := ro.check([]string{"delete", "app", "x"}); err == nil || !strings.Contains(err.Error(), "--allow-writes") {
		t.Errorf("write on a read-only server: err = %v, want one naming --allow-writes", err)
	}
	if err := ro.check([]string{"get", "deploy-token", "x"}); err == nil || !strings.Contains(err.Error(), "--allow-secrets") {
		t.Errorf("secret on a read-only server: err = %v, want one naming --allow-secrets", err)
	}
	rw := &mcpServer{allowWrites: true, allowSecrets: true}
	if err := rw.check([]string{"delete", "app", "x", "--yes"}); err != nil {
		t.Errorf("write on a read-write server: %v", err)
	}
	if err := rw.check([]string{"terminal", "x"}); err == nil {
		t.Error("terminal must stay refused even with --allow-writes")
	}
	if err := rw.check(nil); err == nil {
		t.Error("empty args must be refused")
	}
}

// Every typed tool must build a command line that the real command tree
// accepts, lands on the command the tool names, and is classified as the tool
// claims: a read tool that turned into a write would be refused on every
// read-only server, and a write tool that classified as a read would bypass
// --allow-writes.
func TestMCPToolSpecsResolve(t *testing.T) {
	t.Parallel()

	root := NewApp()
	seen := map[string]bool{progName: true}
	for _, spec := range mcpToolSpecs() {
		if seen[spec.name] {
			t.Errorf("duplicate tool name %q", spec.name)
		}
		seen[spec.name] = true

		// Fill every parameter, so each flag name is checked against the tree.
		in := map[string]any{}
		for _, p := range spec.params {
			switch p.kind {
			case mcpBoolean:
				in[p.key()] = true
			case mcpInteger:
				in[p.key()] = float64(2)
			case mcpArray:
				in[p.key()] = []any{"A=b"}
			default:
				in[p.key()] = "x"
			}
		}
		argv, _, err := spec.argv(in)
		if err != nil {
			t.Errorf("%s: %v", spec.name, err)
			continue
		}
		inv, err := resolveMCPInvocation(root, argv)
		if err != nil {
			t.Errorf("%s: %q: %v", spec.name, argv, err)
			continue
		}
		want := accessRead
		if spec.write {
			want = accessWrite
		}
		if got, why := inv.classify(); got != want {
			t.Errorf("%s: %q classifies as %s (%s), want %s", spec.name, argv, got, why, want)
		}
	}
}

func TestMCPToolArgv(t *testing.T) {
	t.Parallel()

	spec := mcpToolSpec{
		command: "set env",
		write:   true,
		params: []mcpParam{
			pApp(),
			{name: "set", kind: mcpArray},
			{name: "remove", kind: mcpArray, flag: "remove"},
			{name: "tail", kind: mcpInteger, flag: "tail", def: 5},
			{name: "secret", kind: mcpBoolean, flag: "secret"},
		},
	}
	argv, tenant, err := spec.argv(map[string]any{
		"app": "api", "set": []any{"A=1", "B=2"}, "remove": []any{"C"}, "secret": false, "tenant": "acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(argv, " "), "set env api A=1 B=2 --remove=C --tail=5 --yes"; got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
	if tenant != "acme" {
		t.Errorf("tenant = %q, want acme", tenant)
	}

	for name, in := range map[string]map[string]any{
		"missing required": {},
		"unknown key":      {"app": "api", "bogus": 1},
		"wrong type":       {"app": "api", "tail": "five"},
		"fractional int":   {"app": "api", "tail": 1.5},
		"dash positional":  {"app": "--token=x"},
	} {
		if _, _, err := spec.argv(in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
