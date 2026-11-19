package browser

import (
	"fmt"
	"github.com/rahacloud/darkubectl/internal/client"
	"strings"
)

func (m *browser) usagePaneLines() []string {
	lines := []string{}
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{summaryField("App", "not selected")}
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	switch m.usageState.status {
	case requestLoading:
		lines = append(lines, summaryField("Usage", "loading"))
	case requestRefreshing:
		lines = append(lines, summaryField("Usage", "refreshing"))
	case requestStale:
		lines = append(lines, summaryField("Refresh", paint("failed · last samples shown", browserNumber, false)), staleRequestTime("Usage", m.usageState))
	case requestFailed:
		lines = append(lines, summaryField("Usage", paint("unavailable", browserNumber, false)))
	case requestUnavailable:
		lines = append(lines, summaryField("Usage", "unavailable for this connection"))
	case requestEmpty:
		lines = append(lines, summaryField("Samples", "none · last 5 minutes"))
	default:
		if m.usageState.lastSuccessAt.IsZero() {
			lines = append(lines, summaryField("Usage", "no snapshot yet"))
		}
	}
	usage := m.detailUsage
	if m.usageAppID != a.ID {
		usage = nil
	}
	lines = append(lines,
		usageField(" CPU", usageValue(usage, func(u *AppUsage) *float64 { return u.CPUCores }), resourceLimit(a, "cpu"), formatCores, usage != nil && usage.Failed.CPU),
		usageField(" RAM", usageValue(usage, func(u *AppUsage) *float64 { return u.RAMBytes }), resourceLimit(a, "ram"), formatBytes, usage != nil && usage.Failed.RAM))
	if a.Disk != nil && a.Disk.SizeInGi > 0 {
		lines = append(lines, usageField(" Disk", usageValue(usage, func(u *AppUsage) *float64 { return u.DiskBytes }), resourceLimit(a, "disk"), formatBytes, usage != nil && usage.Failed.Disk))
	}
	lines = append(lines, "")
	lines = append(lines, m.historyLines()...)
	if !m.usageState.lastSuccessAt.IsZero() {
		when := m.usageState.lastSuccessAt.Local().Format("Jan 2, 3:04 PM MST")
		lines = append(lines, "", summaryField("Updated", when))
	}
	lines = append(lines, summaryField("Usage window", "last 5 minutes"))
	return lines
}

func resourceLimit(app client.App, resource string) string {
	switch resource {
	case "cpu":
		if app.Plan != nil && app.Plan.Detail.CPURequest > 0 {
			return fmt.Sprintf("%dm", app.Plan.Detail.CPURequest)
		}
		return displayValue(app.CPURequest)
	case "ram":
		if app.Plan != nil && app.Plan.Detail.RAMLimit > 0 {
			return fmt.Sprintf("%dM", app.Plan.Detail.RAMLimit)
		}
		return displayValue(app.RAMLimit)
	case "disk":
		if app.Disk != nil && app.Disk.SizeInGi > 0 {
			return fmt.Sprintf("%d GiB", app.Disk.SizeInGi)
		}
	}
	return "-"
}

func usageField(label string, value *float64, limit string, format func(float64) string, failed bool) string {
	icon := map[string]string{"CPU": "", "RAM": "", "Disk": ""}[label]
	field := func(shown string) string {
		return paint(icon+" "+label+" ", browserMuted, true) + paint(shown, browserValue, false)
	}
	if value == nil && !failed {
		return field("-")
	}
	return field(resourceUsageText(value, limit, format, failed))
}

func resourceUsageText(value *float64, limit string, format func(float64) string, failed bool) string {
	shown := ""
	switch {
	case failed && value == nil:
		shown = "Unavailable"
	case value == nil:
		shown = displayValue(limit)
	default:
		shown = format(*value)
		if failed {
			shown += " · stale"
		}
	}
	if value != nil && limit != "-" {
		shown += " / " + limit
	} else if failed && limit != "-" {
		shown += " / " + limit
	}
	return shown
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func usageValue(usage *AppUsage, selectValue func(*AppUsage) *float64) *float64 {
	if usage == nil {
		return nil
	}
	return selectValue(usage)
}

func usageRatio(used *float64, total string, format func(float64) string) string {
	if used == nil {
		return displayValue(total)
	}
	usedText := format(*used)
	if strings.TrimSpace(total) == "" || total == "-" {
		return usedText
	}
	return usedText + " / " + displayValue(total)
}
