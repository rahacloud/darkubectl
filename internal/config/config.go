// Package config loads and persists darkubectl's kubeconfig-style settings.
//
// The config file lives at $DARKUBE_CONFIG or ~/.darkube/config.yaml and models
// "tenants" (Hamravesh organizations) as switchable contexts, in the same spirit
// as kubectl contexts. A single account API key authenticates across all tenants;
// the active tenant is sent as the X-Organization header on every request.
//
// Loading is layered with koanf: the YAML file first, then DARKUBE_* environment
// variables on top (env wins). Persisting is a plain YAML marshal, since koanf is
// a read/merge library and does not itself write files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	kyaml "github.com/knadh/koanf/parsers/yaml"
	kenv "github.com/knadh/koanf/providers/env/v2"
	kfile "github.com/knadh/koanf/providers/file"
	kstructs "github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// envPrefix is the environment-variable prefix koanf strips when layering env
// vars onto the file config, e.g. DARKUBE_TOKEN -> token.
const envPrefix = "DARKUBE_"

// File permissions for the config directory and file. The file holds an API key,
// so it is owner-read/write only.
const (
	dirPerm  = 0o700
	filePerm = 0o600
)

// ErrNoPath is returned by Save when the config has no backing file path.
var ErrNoPath = errors.New("config has no path")

// Config is the on-disk configuration.
type Config struct {
	// Token is the Hamravesh account API key, sent as "Authorization: Api-key <token>".
	Token string `koanf:"token"`
	// CurrentTenant is the active organization slug (X-Organization).
	CurrentTenant string `koanf:"current-tenant"`
	// Tenants is the list of known organization slugs.
	Tenants []string `koanf:"tenants"`
	// BaseURL overrides the API host (defaults to the public Hamravesh API).
	BaseURL string `koanf:"base-url"`
	// RefreshToken is a Console JWT refresh token (from `darkubectl login`), used
	// to mint short-lived access tokens for the terminal/exec websocket. The
	// account API key cannot open the terminal, so this is a separate credential.
	RefreshToken string `koanf:"refresh-token"`

	path string
}

// DefaultPath returns the config path, honoring $DARKUBE_CONFIG then ~/.darkube/config.yaml.
func DefaultPath() (string, error) {
	if p := os.Getenv("DARKUBE_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".darkube", "config.yaml"), nil
}

// envTransform maps DARKUBE_* variable names to config keys. A couple of aliases
// are friendlier than the raw key: DARKUBE_ORG -> current-tenant, DARKUBE_BASE_URL -> base-url.
func envTransform(key, value string) (string, any) {
	k := strings.ToLower(strings.TrimPrefix(key, envPrefix))
	switch k {
	case "org":
		k = "current-tenant"
	case "base_url":
		k = "base-url"
	default:
		k = strings.ReplaceAll(k, "_", "-")
	}
	return k, value
}

// Load reads the config at path (file optional) and layers DARKUBE_* env vars on top.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if _, err := os.Stat(path); err == nil {
		if err := k.Load(kfile.Provider(path), kyaml.Parser()); err != nil {
			return nil, fmt.Errorf("load config %s: %w", path, err)
		}
	}

	envProvider := kenv.Provider(".", kenv.Opt{Prefix: envPrefix, TransformFunc: envTransform})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("load environment config: %w", err)
	}

	c := &Config{path: path}
	if err := k.Unmarshal("", c); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	c.path = path
	return c, nil
}

// Save writes the config back to disk as YAML, creating the parent directory if needed.
func (c *Config) Save() error {
	if c.path == "" {
		return ErrNoPath
	}
	if err := os.MkdirAll(filepath.Dir(c.path), dirPerm); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	k := koanf.New(".")
	if err := k.Load(kstructs.Provider(*c, "koanf"), nil); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data, err := k.Marshal(kyaml.Parser())
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(c.path, data, filePerm); err != nil {
		return fmt.Errorf("write config %s: %w", c.path, err)
	}
	return nil
}

// Path returns the file path backing this config.
func (c *Config) Path() string { return c.path }

// AddTenant records a tenant slug if not already present.
func (c *Config) AddTenant(name string) {
	if slices.Contains(c.Tenants, name) {
		return
	}
	c.Tenants = append(c.Tenants, name)
}
