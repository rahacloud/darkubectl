package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rahacloud/darkubectl/internal/client"
)

// Both answers are keyed by host. A null expiry means no certificate has been
// issued, and must stay distinguishable from a parsed one.
func TestCertificateStatusAndCheckDNS(t *testing.T) {
	t.Parallel()

	var dnsBody map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/darkube/apps/app-1/certificate_status/":
			_, _ = w.Write([]byte(`{"a.example":{"expires_at":"2026-09-07T06:37:06+00:00"},"b.example":{"expires_at":null}}`))
		case "/api/v1/darkube/apps/check_dns_records/":
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &dnsBody)
			_, _ = w.Write([]byte(`{"a.example":true,"b.example":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := client.New(srv.URL, client.APIKey("k"), "acme")
	certs, err := c.CertificateStatus(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("CertificateStatus: %v", err)
	}
	if got := certs["a.example"]; got == nil || got.UTC().Format("2006-01-02") != "2026-09-07" {
		t.Errorf("a.example = %v", got)
	}
	if got, ok := certs["b.example"]; !ok || got != nil {
		t.Errorf("b.example = %v, %v; want a nil entry", got, ok)
	}

	dns, err := c.CheckDNS(context.Background(), []string{"a.example", "b.example"})
	if err != nil {
		t.Fatalf("CheckDNS: %v", err)
	}
	if !dns["a.example"] || dns["b.example"] {
		t.Errorf("dns = %v", dns)
	}
	if len(dnsBody["hosts"]) != 2 {
		t.Errorf("request body = %v", dnsBody)
	}
}
