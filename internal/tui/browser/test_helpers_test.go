package browser

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
	"strings"
)

func applyAppsResult(m *browser, apps []client.App, err error) {
	meta := requestMeta{tenant: m.tenant, scopeGeneration: m.tenantGeneration, requestID: m.nextRequestID + 1}
	m.nextRequestID = meta.requestID
	m.appsState.begin(meta, m.requestTime())
	m.Update(appsResult{meta: meta, apps: apps, err: err})
}

func summaryHasValue(m *browser, label, value string) bool {
	prefix := ansi.Strip(summaryLabel(label))
	for _, line := range m.summaryPaneLines() {
		line = ansi.Strip(line)
		if strings.HasPrefix(line, prefix) && strings.TrimSpace(strings.TrimPrefix(line, prefix)) == value {
			return true
		}
	}
	return false
}
