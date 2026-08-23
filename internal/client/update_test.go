package client_test

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// The v2 list and the detail routes disagree about secret_envs: the list types
// it as a bare vault-path string and carries the names in secret_envs_keys,
// while the detail returns objects with empty values. Both must read back the
// same names, or `get env` shows nothing for half the API.
func TestSecretEnvNamesReadsBothShapes(t *testing.T) {
	t.Parallel()

	detail := map[string]any{
		"secret_envs": []any{
			map[string]any{"name": "DB_PASSWORD", "value": ""},
			map[string]any{"name": "API_KEY", "value": ""},
		},
	}
	list := map[string]any{
		"secret_envs":      "console/data/production/darkube/StatelessApp/abc/secret_envs",
		"secret_envs_keys": []any{map[string]any{"name": "DB_PASSWORD"}, map[string]any{"name": "API_KEY"}},
	}

	for name, app := range map[string]map[string]any{"detail": detail, "list": list} {
		got := client.SecretEnvNames(app)
		if len(got) != 2 || got[0] != "DB_PASSWORD" || got[1] != "API_KEY" {
			t.Errorf("%s: want [DB_PASSWORD API_KEY], got %v", name, got)
		}
	}
}

// A missing or null field must read as empty rather than panic: the API returns
// null for custom_config, ingress_class_name and external_hosts on most apps.
func TestReadersTolerateMissingAndNull(t *testing.T) {
	t.Parallel()

	for name, app := range map[string]map[string]any{
		"absent": {},
		"null":   {"envs": nil, "secret_envs": nil, "external_hosts": nil},
	} {
		if got := client.EnvVars(app); len(got) != 0 {
			t.Errorf("%s: want no envs, got %v", name, got)
		}
		if got := client.SecretEnvNames(app); len(got) != 0 {
			t.Errorf("%s: want no secret names, got %v", name, got)
		}
		if got := client.ExternalHosts(app); len(got) != 0 {
			t.Errorf("%s: want no hosts, got %v", name, got)
		}
	}
}

func TestEnvVarsAndHostsRoundTrip(t *testing.T) {
	t.Parallel()

	app := map[string]any{}
	client.SetEnvVars(app, []client.EnvVar{{Name: "LOG_LEVEL", Value: "debug"}})
	client.SetExternalHosts(app, []string{"api.example.com"})

	envs := client.EnvVars(app)
	if len(envs) != 1 || envs[0].Name != "LOG_LEVEL" || envs[0].Value != "debug" {
		t.Errorf("want LOG_LEVEL=debug, got %v", envs)
	}
	hosts := client.ExternalHosts(app)
	if len(hosts) != 1 || hosts[0] != "api.example.com" {
		t.Errorf("want [api.example.com], got %v", hosts)
	}
}
