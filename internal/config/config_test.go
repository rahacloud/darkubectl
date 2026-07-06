package config_test

import (
	"path/filepath"
	"testing"

	"github.com/rahacloud/darkubectl/internal/config"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load (fresh): %v", err)
	}
	c.Token = "tok"
	c.CurrentTenant = "talaland"
	c.AddTenant("talaland")
	c.AddTenant("rahacloud")
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load (reread): %v", err)
	}
	if got.Token != "tok" || got.CurrentTenant != "talaland" || len(got.Tenants) != 2 {
		t.Errorf("round-trip = %+v, want tok/talaland/2 tenants", got)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("DARKUBE_TOKEN", "envtok")
	t.Setenv("DARKUBE_ORG", "envorg")

	c, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Token != "envtok" {
		t.Errorf("Token = %q, want envtok (from DARKUBE_TOKEN)", c.Token)
	}
	if c.CurrentTenant != "envorg" {
		t.Errorf("CurrentTenant = %q, want envorg (from DARKUBE_ORG)", c.CurrentTenant)
	}
}

func TestAddTenantDedup(t *testing.T) {
	t.Parallel()

	var c config.Config
	c.AddTenant("a")
	c.AddTenant("a")
	c.AddTenant("b")
	if len(c.Tenants) != 2 {
		t.Errorf("Tenants = %v, want 2 unique", c.Tenants)
	}
}
