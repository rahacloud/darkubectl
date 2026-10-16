package client_test

import (
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// TestStorageEndpoint pins how the S3 endpoint is chosen. It matters because
// the address cannot be derived from the tenant slug -- the platform name
// carries a random suffix, as in paradisehub-product-hhhwz.hs3.ir -- so this
// selection is the only way a caller learns where to point an S3 client.
func TestStorageEndpoint(t *testing.T) {
	t.Parallel()

	platform := client.StorageDomain{URL: "paradisehub-product-hhhwz.hs3.ir"}
	custom := client.StorageDomain{URL: "media.example.ir", Custom: true}

	for name, tc := range map[string]struct {
		domains []client.StorageDomain
		want    string
	}{
		"none":            {nil, ""},
		"platform only":   {[]client.StorageDomain{platform}, "https://paradisehub-product-hhhwz.hs3.ir"},
		"custom only":     {[]client.StorageDomain{custom}, "https://media.example.ir"},
		"custom wins":     {[]client.StorageDomain{platform, custom}, "https://media.example.ir"},
		"custom wins rev": {[]client.StorageDomain{custom, platform}, "https://media.example.ir"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (client.Storage{Domains: tc.domains}).Endpoint(); got != tc.want {
				t.Errorf("Endpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}
