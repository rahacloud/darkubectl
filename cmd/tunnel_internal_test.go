package cmd

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// The image is FROM scratch and `args` is never split, so the entire invocation
// has to be one whitespace-separated `command`. Getting this wrong is a crash
// loop with nothing in the API response pointing at the cause.
func TestChiselServerCommandIsASingleSplittableCommand(t *testing.T) {
	t.Parallel()

	got := chiselServerCommand()
	fields := strings.Fields(got)
	if fields[0] != "/app/chisel" {
		t.Errorf("argv[0] = %q, want the absolute binary path (the image has no shell)", fields[0])
	}
	if !slices.Contains(fields, "server") {
		t.Errorf("command = %q, want the server subcommand", got)
	}
	if !slices.Contains(fields, "8080") {
		t.Errorf("command = %q, want the listen port to match chiselPort", got)
	}
	// entrypointWarnings is what warns about this shape at creation time; the
	// command must survive it cleanly.
	if w := entrypointWarnings(got, ""); len(w) != 0 {
		t.Errorf("entrypointWarnings(%q) = %v, want none", got, w)
	}
}

func TestChiselClientArgs(t *testing.T) {
	t.Parallel()

	got := chiselClientArgs("tld-tunnel.darkube.app", "tunnel:secret", []string{
		"1433:mssql-dev.talaland-dev.svc:1433",
		"5432:postgres-dev.talaland-dev.svc:5432",
	})

	if got[0] != "client" {
		t.Errorf("argv[0] = %q, want client", got[0])
	}
	// The server URL must precede the forwards, or chisel reads it as one.
	url := slices.Index(got, "https://tld-tunnel.darkube.app")
	first := slices.Index(got, "1433:mssql-dev.talaland-dev.svc:1433")
	if url == -1 || first == -1 || url > first {
		t.Fatalf("argv = %v, want the server URL before the forwards", got)
	}
	if got[len(got)-1] != "5432:postgres-dev.talaland-dev.svc:5432" {
		t.Errorf("argv = %v, want every forward passed through in order", got)
	}
	if !slices.Contains(got, "tunnel:secret") {
		t.Errorf("argv = %v, want the credential passed to --auth", got)
	}
}

func TestGenerateTunnelAuthIsUniqueAndWellFormed(t *testing.T) {
	t.Parallel()

	first, err := generateTunnelAuth()
	if err != nil {
		t.Fatalf("generateTunnelAuth returned %v", err)
	}
	user, pass, ok := strings.Cut(first, ":")
	if !ok || user != tunnelUser || len(pass) != tunnelSecretBytes*2 {
		t.Fatalf("auth = %q, want %s:<%d hex chars>", first, tunnelUser, tunnelSecretBytes*2)
	}
	second, err := generateTunnelAuth()
	if err != nil {
		t.Fatalf("generateTunnelAuth returned %v", err)
	}
	if first == second {
		t.Error("two generated credentials were identical")
	}
}

func TestValidateForward(t *testing.T) {
	t.Parallel()

	if err := validateForward("1433:mssql-dev.talaland-dev.svc:1433"); err != nil {
		t.Errorf("valid forward rejected: %v", err)
	}

	for name, spec := range map[string]string{
		"two parts":    "1433:1433",
		"four parts":   "1433:host:1433:extra",
		"bad local":    "sql:mssql-dev.talaland-dev.svc:1433",
		"bad remote":   "1433:mssql-dev.talaland-dev.svc:sql",
		"port zero":    "0:mssql-dev.talaland-dev.svc:1433",
		"port too big": "1433:mssql-dev.talaland-dev.svc:70000",
		"empty host":   "1433::1433",
	} {
		if err := validateForward(spec); !errors.Is(err, errBadForward) {
			t.Errorf("%s (%q): err = %v, want errBadForward", name, spec, err)
		}
	}
}

// Forwarding to localhost resolves inside the tunnel pod, whose filesystem is
// empty — it connects and then refuses everything, which reads as a broken
// tunnel rather than a wrong address.
func TestValidateForwardRejectsLoopbackRemote(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{"1433:localhost:1433", "1433:127.0.0.1:1433"} {
		err := validateForward(spec)
		if !errors.Is(err, errBadForward) {
			t.Fatalf("%q: err = %v, want errBadForward", spec, err)
		}
		if !strings.Contains(err.Error(), "in-cluster address") {
			t.Errorf("%q: error should explain the direction; got %v", spec, err)
		}
	}
}

func TestTunnelHostPrefersPlatformSubdomain(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"custom_domain_address": "tld-tunnel.darkube.app",
		"external_hosts":        []any{"tunnel.example.com"},
	}
	if got := tunnelHost(raw); got != "tld-tunnel.darkube.app" {
		t.Errorf("tunnelHost = %q, want the platform subdomain", got)
	}
}

func TestTunnelHostFallsBackToExternalHost(t *testing.T) {
	t.Parallel()

	raw := map[string]any{"external_hosts": []any{"tunnel.example.com"}}
	if got := tunnelHost(raw); got != "tunnel.example.com" {
		t.Errorf("tunnelHost = %q, want the custom domain", got)
	}
	if got := tunnelHost(map[string]any{}); got != "" {
		t.Errorf("tunnelHost with no hostname = %q, want empty", got)
	}
}

func TestTunnelKeyIsTenantScoped(t *testing.T) {
	t.Parallel()

	// App names are unique only within a tenant, so the same tunnel name in two
	// tenants must not share one stored credential.
	if tunnelKey("talaland", defaultTunnelName) == tunnelKey("rahacloud", defaultTunnelName) {
		t.Error("tunnel keys collide across tenants")
	}
}

type stubAuthStore map[string]string

func (s stubAuthStore) TunnelAuth(key string) string { return s[key] }

func TestResolveTunnelAuthFallsBackToConfig(t *testing.T) {
	t.Parallel()

	store := stubAuthStore{tunnelKey("talaland", "darkube-tunnel"): "tunnel:stored"}
	cmd := newTunnelConnectCommand()
	if got := resolveTunnelAuth(cmd, store, "talaland", "darkube-tunnel"); got != "tunnel:stored" {
		t.Errorf("resolveTunnelAuth = %q, want the stored credential", got)
	}
	if got := resolveTunnelAuth(cmd, store, "talaland", "other"); got != "" {
		t.Errorf("resolveTunnelAuth for an unknown tunnel = %q, want empty", got)
	}
}

func TestResolveTunnelAuthPrefersEnvOverConfig(t *testing.T) {
	// Not parallel: it sets an environment variable.
	t.Setenv(envTunnelAuth, "tunnel:from-env")

	store := stubAuthStore{tunnelKey("talaland", "darkube-tunnel"): "tunnel:stored"}
	got := resolveTunnelAuth(newTunnelConnectCommand(), store, "talaland", "darkube-tunnel")
	if got != "tunnel:from-env" {
		t.Errorf("resolveTunnelAuth = %q, want the environment to win", got)
	}
}
