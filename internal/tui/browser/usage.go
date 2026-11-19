package browser

import (
	"fmt"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"strings"
)

func (m *browser) usagePaneLines() []string {
	lines := []string{}
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{paint("No app selected. Move to an app to view its resource usage.", output.ColorMuted, false)}
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	switch m.usageState.status {
	case requestLoading:
		lines = append(lines, paint("Loading current resource usage…", output.ColorMuted, false))
	case requestRefreshing:
		lines = append(lines, paint("Refreshing current resource usage…", output.ColorMuted, false))
	case requestStale:
		lines = append(lines, paint("Refresh failed · showing the last successful samples", output.ColorNumber, false), staleRequestTime("Usage", m.usageState))
	case requestFailed:
		lines = append(lines, paint("Resource usage is unavailable. Press r to retry.", output.ColorNumber, false))
	case requestUnavailable:
		lines = append(lines, paint("Resource usage is unavailable for this connection.", output.ColorMuted, false))
	case requestEmpty:
		lines = append(lines, paint("No resource samples in the last 5 minutes.", output.ColorMuted, false))
	default:
		if m.usageState.lastSuccessAt.IsZero() {
			lines = append(lines, paint("No usage snapshot yet. Press r to load it.", output.ColorMuted, false))
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
		lines = append(lines, "", paint("Last usage response · "+when, output.ColorMuted, false))
	}
	lines = append(lines, paint("Window · latest samples from the last 5 minutes", output.ColorMuted, false))
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
		return paint(icon+" "+label+" ", output.ColorMuted, true) + paint(shown, output.ColorBranch, false)
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
