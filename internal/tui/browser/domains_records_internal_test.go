package browser

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestDomainChallengeRecordsDisplayAndSelection(t *testing.T) {
	t.Parallel()
	m := &browser{detail: true, apps: []client.App{{ID: "app-1"}}, domainConfig: DomainConfig{
		Hosts: []string{"api.example.com"}, Challenge: "dns01",
		ChallengeRecords: map[string]string{"_acme-challenge.z.example": "z.target", "_acme-challenge.a.example": "a.target"},
	}}
	m.domainCursor = 2
	lines := ansi.Strip(strings.Join(m.domainsPaneLines(), "\n"))
	if !strings.Contains(lines, "› _acme-challenge.a.example\n    CNAME → a.target") || !strings.Contains(lines,
		"_acme-challenge.z.example\n    CNAME → z.target") {
		t.Fatalf("unexpected records: %s", lines)
	}
	if m.domainCursorLine <= 0 {
		t.Fatalf("selected record has no scroll position: %d", m.domainCursorLine)
	}
}
