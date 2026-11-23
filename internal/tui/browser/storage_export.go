package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type storageCredentialExport struct {
	AccessKey string `json:"access_key"` //nolint:tagliatelle // S3 tools expect snake_case credential names.
	SecretKey string `json:"secret_key"` //nolint:tagliatelle // S3 tools expect snake_case credential names.
	Endpoint  string `json:"endpoint,omitempty"`
}

const (
	storageFormatEnv          = ".env"
	storageCredentialFileMode = 0o600
)

var (
	errStorageExportPathRequired         = errors.New("enter a valid destination path")
	errStorageExportHomeMissing          = errors.New("home directory is unavailable")
	errStorageExportTildeInvalid         = errors.New("use ~ or ~/path for your home directory")
	errStorageExportPathResolve          = errors.New("cannot resolve destination path")
	errStorageExportExtension            = errors.New("use a .json, .yaml, .yml or .env file")
	errStorageExportNotRegular           = errors.New("destination must be a new regular file")
	errStorageExportExists               = errors.New("destination already exists; choose another path")
	errStorageExportInspect              = errors.New("cannot inspect destination path")
	errStorageExportDirectory            = errors.New("destination directory does not exist")
	errStorageExportNotWritable          = errors.New("destination directory is not writable")
	errStorageExportUnsupportedFormat    = errors.New("unsupported credential file format")
	errStorageExportEncode               = errors.New("cannot encode credentials")
	errStorageExportIncompleteRemoved    = errors.New("could not finish credential file; incomplete file removed")
	errStorageExportIncompleteNotRemoved = errors.New("could not finish credential file; incomplete file could not be removed")
)

// storageExportPath resolves a user-entered destination without creating it.
func storageExportPath(input, home, workingDir string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" || strings.ContainsRune(input, 0) {
		return "", "", errStorageExportPathRequired
	}
	if input == "~" || strings.HasPrefix(input, "~/") {
		if home == "" {
			return "", "", errStorageExportHomeMissing
		}
		input = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(input, "~"), "/"))
	} else if strings.HasPrefix(input, "~") {
		return "", "", errStorageExportTildeInvalid
	}
	if !filepath.IsAbs(input) {
		input = filepath.Join(workingDir, input)
	}
	path, err := filepath.Abs(input)
	if err != nil {
		return "", "", errStorageExportPathResolve
	}
	format := strings.ToLower(filepath.Ext(path))
	if strings.EqualFold(filepath.Base(path), storageFormatEnv) {
		format = storageFormatEnv
	}
	switch format {
	case ".json", ".yaml", ".yml", storageFormatEnv:
	default:
		return "", "", errStorageExportExtension
	}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", "", errStorageExportNotRegular
		}
		return "", "", errStorageExportExists
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", errStorageExportInspect
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() {
		return "", "", errStorageExportDirectory
	}
	if parent.Mode().Perm()&0o222 == 0 {
		return "", "", errStorageExportNotWritable
	}
	return path, format, nil
}

// saveStorageCredentials creates a new private file and never overwrites an
// existing path. Errors intentionally exclude credential values.
func saveStorageCredentials(path, format string, credentials storageCredentialExport) error {
	var data []byte
	var err error
	switch format {
	case ".json":
		//nolint:gosec // Credentials are explicitly exported only after user confirmation.
		data, err = json.MarshalIndent(credentials, "", "  ")
		data = append(data, '\n')
	case ".yaml", ".yml":
		// JSON string literals are also valid YAML scalars.
		access, err := json.Marshal(credentials.AccessKey)
		if err != nil {
			return errStorageExportEncode
		}
		secret, err := json.Marshal(credentials.SecretKey)
		if err != nil {
			return errStorageExportEncode
		}
		data = []byte("access_key: " + string(access) + "\nsecret_key: " + string(secret) + "\n")
		if credentials.Endpoint != "" {
			endpoint, err := json.Marshal(credentials.Endpoint)
			if err != nil {
				return errStorageExportEncode
			}
			data = append(data, []byte("endpoint: "+string(endpoint)+"\n")...)
		}
	case storageFormatEnv:
		data = []byte("AWS_ACCESS_KEY_ID=" + storageEnvQuote(credentials.AccessKey) +
			"\nAWS_SECRET_ACCESS_KEY=" + storageEnvQuote(credentials.SecretKey) + "\n")
		if credentials.Endpoint != "" {
			data = append(data, []byte("AWS_ENDPOINT_URL="+storageEnvQuote(credentials.Endpoint)+"\n")...)
		}
	default:
		return errStorageExportUnsupportedFormat
	}
	if err != nil {
		return errStorageExportEncode
	}
	//nolint:gosec // The destination was validated and is created exclusively with owner-only permissions.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, storageCredentialFileMode)
	if err != nil {
		return fmt.Errorf("cannot create credential file: %w", err)
	}
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		if removeErr := os.Remove(path); removeErr != nil {
			return errStorageExportIncompleteNotRemoved
		}
		return errStorageExportIncompleteRemoved
	}
	return nil
}

func storageEnvQuote(value string) string {
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`").Replace(value)
	return "\"" + value + "\""
}
