package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"time"
)

// tlsProbeTimeout bounds one handshake; a host that does not answer in this
// long is reported unreachable.
const tlsProbeTimeout = 10 * time.Second

// servedCert is the certificate a host actually presents on 443.
type servedCert struct {
	Expires *time.Time `json:"expires,omitempty" yaml:"expires,omitempty"`
	Issuer  string     `json:"issuer,omitempty"  yaml:"issuer,omitempty"`
	// Trusted says the chain verifies against the system roots for this name.
	Trusted bool `json:"trusted" yaml:"trusted"`
	// Problem is why the certificate is untrusted, or why there is none.
	Problem string `json:"problem,omitempty" yaml:"problem,omitempty"`
}

// probeServedCert fetches the certificate host serves and checks it. The
// handshake skips verification so that a bad certificate can still be
// described; verification is then done by hand.
func probeServedCert(ctx context.Context, host string) servedCert {
	ctx, cancel := context.WithTimeout(ctx, tlsProbeTimeout)
	defer cancel()

	d := tls.Dialer{
		NetDialer: &net.Dialer{},
		// Verification is done below, against the same name, so that an
		// untrusted certificate is reported rather than refused.
		Config: &tls.Config{ServerName: host, InsecureSkipVerify: true}, //nolint:gosec // verified by hand below
	}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return servedCert{Problem: "unreachable: " + err.Error()}
	}
	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return servedCert{Problem: "not a TLS connection"}
	}
	chain := tlsConn.ConnectionState().PeerCertificates
	if len(chain) == 0 {
		return servedCert{Problem: "no certificate presented"}
	}
	leaf := chain[0]
	out := servedCert{Expires: &leaf.NotAfter, Issuer: leaf.Issuer.CommonName}

	inter := x509.NewCertPool()
	for _, c := range chain[1:] {
		inter.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: inter}); err != nil {
		out.Problem = err.Error()
		return out
	}
	out.Trusted = true
	return out
}

// servedLabel describes a served certificate for a table cell.
func servedLabel(s *servedCert, now time.Time) string {
	switch {
	case s == nil:
		return "-"
	case s.Expires == nil:
		return s.Problem
	case !s.Trusted:
		return "untrusted (" + s.Problem + ")"
	default:
		return certLabel(s.Expires, now) + ", " + s.Issuer
	}
}
