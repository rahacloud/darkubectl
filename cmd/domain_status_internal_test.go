package cmd

import (
	"testing"
	"time"
)

func TestDomainStatusLabels(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	future := now.Add(43 * 24 * time.Hour)
	if got := certLabel(&future, now); got != "43d left" {
		t.Errorf("certLabel(future) = %q", got)
	}
	if got := certLabel(nil, now); got != "none" {
		t.Errorf("certLabel(nil) = %q", got)
	}
	yes, no := true, false
	if dnsLabel(&yes) != "ok" || dnsLabel(&no) != "not pointed here" || dnsLabel(nil) != "-" {
		t.Error("dnsLabel")
	}
	if got := servedLabel(&servedCert{Expires: &future, Issuer: "YE2", Trusted: true}, now); got != "43d left, YE2" {
		t.Errorf("servedLabel(trusted) = %q", got)
	}
	bad := &servedCert{Expires: &future, Problem: "x509: certificate is valid for a, not b"}
	if got := servedLabel(bad, now); got != "untrusted (x509: certificate is valid for a, not b)" {
		t.Errorf("servedLabel(untrusted) = %q", got)
	}
	if got := servedLabel(&servedCert{Problem: "unreachable: timeout"}, now); got != "unreachable: timeout" {
		t.Errorf("servedLabel(unreachable) = %q", got)
	}
}
