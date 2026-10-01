package client

import (
	"context"
	"net/url"
	"time"
)

// Domain checks the console's Hosts tab runs (console-remote, confirmed
// 2026-09-30):
//
//	GET  apps/<uuid>/certificate_status/  {"<host>": {"expires_at": <RFC 3339>|null}}
//	POST apps/check_dns_records/ {hosts}  {"<host>": true|false}
//
// expires_at is the certificate the platform issued for the host, not what the
// host serves: a domain fronted by something else keeps a stale or null
// expiry here while its live certificate is fine. A null means none issued.
//
// apps/check_subdomain/ also exists, and answers {"is_valid": true} for any
// label at all, "bad_label!" included, so it is not used.
const dnsCheckPathV1 = "/api/v1/darkube/apps/check_dns_records/"

// CertificateStatus returns, per host of an app, when the platform-issued
// certificate expires. A nil time means the platform has issued none.
func (c *Client) CertificateStatus(ctx context.Context, appID string) (map[string]*time.Time, error) {
	var raw map[string]struct {
		ExpiresAt *string `json:"expires_at"`
	}
	if err := c.getJSON(ctx, appsPathV1+url.PathEscape(appID)+"/certificate_status/", nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]*time.Time, len(raw))
	for host, s := range raw {
		if s.ExpiresAt == nil {
			out[host] = nil
			continue
		}
		t, err := time.Parse(time.RFC3339, *s.ExpiresAt)
		if err != nil {
			out[host] = nil
			continue
		}
		out[host] = &t
	}
	return out, nil
}

// CheckDNS reports, per host, whether its DNS points at the platform.
func (c *Client) CheckDNS(ctx context.Context, hosts []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(hosts) == 0 {
		return out, nil
	}
	if err := c.postJSON(ctx, dnsCheckPathV1, map[string]any{"hosts": hosts}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
