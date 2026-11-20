package browser

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestDomainChallengeRecordsDisplayAndSelection(t *testing.T) {
	t.Parallel()
	m := &browser{width: 120, height: 30, detail: true, apps: []client.App{{ID: "app-1"}}, domainConfig: DomainConfig{
		Hosts: []string{"api.example.com"}, Challenge: "dns01",
		ChallengeRecords: map[string]string{"_acme-challenge.z.example": "z.target", "_acme-challenge.a.example": "a.target"},
	}}
	m.domainCursor = 2
	lines := ansi.Strip(strings.Join(m.domainsPaneLines(), "\n"))
	if !strings.Contains(lines, "› CNAME") || !strings.Contains(lines, "a.target") || !strings.Contains(lines, "z.target") ||
		!strings.Contains(lines, "_acme-challen…") || len(m.domainRecordNames()) != 2 ||
		m.domainRecordNames()[0] != "_acme-challenge.a.example" {
		t.Fatalf("unexpected records: %s", lines)
	}
	if m.domainCursorLine <= 0 {
		t.Fatalf("selected record has no scroll position: %d", m.domainCursorLine)
	}
}
