package browser

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
	"gopkg.in/yaml.v3"
)

func TestStorageExportPath(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	for _, tc := range []struct{ input, want, format string }{
		{"~/key.YAML", filepath.Join(base, "key.YAML"), ".yaml"},
		{".env", filepath.Join(base, ".env"), ".env"},
		{"key.json", filepath.Join(base, "key.json"), ".json"},
	} {
		path, format, err := storageExportPath(tc.input, base, base)
		if err != nil || path != tc.want || format != tc.format {
			t.Fatalf("path %q: got %q %q, error %v", tc.input, path, format, err)
		}
	}
	for _, input := range []string{"", "key.txt", "key", "bad\x00.json"} {
		if _, _, err := storageExportPath(input, base, base); err == nil {
			t.Fatalf("accepted invalid path %q", input)
		}
	}
}

//nolint:gosec // The fixture contains fake values and reads only files under t.TempDir.
func TestStorageExportEscaping(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := storageCredentialExport{AccessKey: `a'$HOME`, SecretKey: "line\nquote'\\end", Endpoint: "https://example.invalid"}
	yamlPath := filepath.Join(dir, "key.yaml")
	if err := saveStorageCredentials(yamlPath, ".yaml", want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		AccessKey string `yaml:"access_key"` //nolint:tagliatelle // Verify the storage API snake_case export format.
		SecretKey string `yaml:"secret_key"` //nolint:tagliatelle // Verify the storage API snake_case export format.
		Endpoint  string `yaml:"endpoint"`
	}
	if err := yaml.Unmarshal(data, &got); err != nil || got.AccessKey != want.AccessKey ||
		got.SecretKey != want.SecretKey || got.Endpoint != want.Endpoint {
		t.Fatalf("YAML values did not round trip: %v", err)
	}
	envPath := filepath.Join(dir, ".env")
	if err := saveStorageCredentials(envPath, ".env", want); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(),
		"sh", "-c", `. "$1"; printf '%s\0%s\0%s' "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY" "$AWS_ENDPOINT_URL"`,
		"sh", envPath,
	)
	out, err := cmd.Output()
	if err != nil || string(out) != want.AccessKey+"\x00"+want.SecretKey+"\x00"+want.Endpoint {
		t.Fatalf("environment values did not round trip: %v", err)
	}
}

//nolint:gosec // The test reads only files it created below t.TempDir.
func TestSaveStorageCredentials(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := storageCredentialExport{AccessKey: "access", SecretKey: "secret", Endpoint: "https://example.invalid"}
	path := filepath.Join(dir, "key.json")
	if err := saveStorageCredentials(path, ".json", want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got storageCredentialExport
	if err := json.Unmarshal(data, &got); err != nil || got != want {
		t.Fatalf("round trip failed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("incorrect file mode: %v", err)
	}
	if err := saveStorageCredentials(path, ".json", want); err == nil {
		t.Fatal("overwrote existing file")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := saveStorageCredentials(link, ".json", want); err == nil {
		t.Fatal("followed symlink")
	}
	for _, format := range []string{".yaml", ".yml", ".env"} {
		output := filepath.Join(dir, "key"+format)
		if err := saveStorageCredentials(output, format, want); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil || !strings.Contains(string(data), "secret") {
			t.Fatalf("failed to write %s: %v", format, err)
		}
	}
}

func TestStorageSaveModalTypingValidationAndPaste(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	m := &browser{page: pageStorage, width: 120, height: 30, allowSecrets: true,
		storageSecret: &client.StorageKeySecret{AccessKey: "sample-access", SecretKey: "sample-secret"}}
	m.handleKeyPress(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.storageDialog != storageDialogSavePath {
		t.Fatal("save modal did not open")
	}
	invalid := filepath.Join(base, "missing", "credentials.json")
	for _, c := range invalid {
		m.handleKeyPress(tea.KeyPressMsg{Code: c, Text: string(c)})
	}
	if m.storageDraft.text != invalid {
		t.Fatal("typed path was not preserved")
	}
	if !strings.Contains(m.storagePaneStatus(), "directory does not exist") {
		t.Fatal("missing directory not reported")
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != storageDialogSavePath {
		t.Fatal("invalid path advanced")
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	path := filepath.Join(base, "credentials.json")
	m.handlePaste(path)
	if m.storageDraft.text != path {
		t.Fatal("pasted path was not accepted")
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != storageDialogSaveReview {
		t.Fatal("valid path did not advance")
	}
	m.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.storageDialog != "" {
		t.Fatalf("save did not finish: %s", m.storageNotice)
	}
	data, err := os.ReadFile(path) //nolint:gosec // Path is inside the test temporary directory.
	if err != nil || !strings.Contains(string(data), "sample-access") {
		t.Fatal("credential file not saved")
	}
}
