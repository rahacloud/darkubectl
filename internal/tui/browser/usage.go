package browser

import (
	"fmt"

	"github.com/rahacloud/darkubectl/internal/client"
)

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
	var shown string
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
