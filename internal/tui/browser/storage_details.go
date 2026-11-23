package browser

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) storageDetailLines(_ int) []string {
	if m.storageDialog != "" && !m.storageIsModal() {
		return m.storageDialogLines()
	}
	if m.storageSecret != nil {
		return m.storageCredentialLines()
	}
	var lines []string
	s := m.selectedStorage()
	if s == nil {
		lines = []string{paint("Select a datacenter", browserMuted, false)}
	} else if bucket := m.selectedBucket(); bucket != nil {
		lines = append(lines, paint(storageIconBucket+" BUCKET", browserAccent, true),
			summaryField("Datacenter", safeText(s.Name)), summaryField("Name", safeText(bucket.Name)),
			summaryField("Status", displayValue(bucket.Status)),
			summaryField("Access", map[bool]string{true: "Public", false: "Private"}[bucket.IsPublic]),
			summaryField("Size", storageSize(bucket.Size)), summaryField("Created", formatLocalTime(bucket.CreateTime)))
		lines = appendStorageKeyDetails(lines, s.Keys, bucket.Name)
	} else if key := m.selectedKey(); key != nil {
		lines = append(lines, paint(storageIconKey+" KEY", browserAccent, true),
			summaryField("Datacenter", safeText(s.Name)), summaryField("Name", safeText(key.Name)),
			summaryField("Access", storagePermissions(key.Actions)), summaryField("Buckets", storageList(key.Buckets)),
			paint("R read · W write · L list", browserMuted, false),
			"", paint("Credentials are only available after creation or rotation.", browserMuted, false))
	} else {
		lines = append(lines, paint(storageIconDatacenter+" DATACENTER", browserAccent, true),
			summaryField("Name", safeText(s.Name)), summaryField("Class", displayValue(s.StorageClass)),
			summaryField("Endpoint", storageEndpointText(s.Endpoint())), summaryField("Usage", storageUsage(*s)),
			summaryField("Traffic plan", storagePlanAllowance(s.PlanTrafficGB)),
			summaryField("Buckets", strconv.Itoa(len(s.Buckets))), summaryField("Keys", strconv.Itoa(len(s.Keys))))
		switch {
		case m.storagePlansState.active():
			lines = append(lines, paint("Refreshing plan allowances…", browserMuted, false))
		case m.storagePlansState.err != nil:
			lines = append(lines, paint("Plan lookup failed: "+safeText(m.storagePlansState.err.Error()), browserNumber, false))
		case s.PlanDiskGB == nil && s.PlanTrafficGB == nil:
			lines = append(lines, paint("No assigned billing plan reported", browserMuted, false))
		}
		lines = m.appendStorageTrafficDetails(lines, s.ID)
		lines = appendStorageKeyDetails(lines, s.Keys, "")
	}
	if m.storageState.err != nil && len(m.storageRows) > 0 {
		lines = append(lines, "", "Refresh failed; showing stale data", safeText(m.storageState.err.Error()))
	}
	if strings.Contains(strings.ToLower(m.storageNotice), "failed") || strings.Contains(m.storageNotice, "may have succeeded") {
		lines = append(lines, "", paint(safeText(m.storageNotice), browserNumber, false))
	}
	return lines
}

func (m *browser) appendStorageTrafficDetails(lines []string, storageID string) []string {
	lines = append(lines, "", paint(storageIconTraffic+" TRAFFIC RATE", browserAccent, true))
	if m.storageTrafficID != storageID {
		return append(lines, paint("Traffic data unavailable", browserMuted, false))
	}
	if m.storageTrafficState.active() {
		lines = append(lines, paint("Refreshing traffic…", browserMuted, false))
		if len(m.storageTraffic) == 0 {
			return lines
		}
	}
	if m.storageTrafficState.err != nil {
		lines = append(lines, paint("Traffic lookup failed: "+safeText(m.storageTrafficState.err.Error()), browserNumber, false))
		if len(m.storageTraffic) == 0 {
			return lines
		}
		lines = append(lines, paint("Showing last known rates", browserMuted, false))
	} else if len(m.storageTraffic) == 0 {
		return append(lines, paint("No samples reported in the last hour", browserMuted, false))
	}
	for _, sample := range m.storageTraffic {
		lines = append(lines, safeText(sample.Domain), summaryField("Rate", formatBytes(sample.BytesPerSecond)+"/s"),
			summaryField("Observed", formatLocalWallTime(sample.ObservedAt, "Jan 2, 3:04 PM MST")))
	}
	return lines
}

func appendStorageKeyDetails(lines []string, keys []client.StorageKey, bucketName string) []string {
	lines = append(lines, "", paint(storageIconKey+" KEY ACCESS", browserAccent, true),
		paint("R read · W write · L list", browserMuted, false))
	count := 0
	for _, key := range keys {
		if bucketName != "" && !slices.Contains(key.Buckets, bucketName) {
			continue
		}
		count++
		lines = append(lines, "", paint(storageIconKey+" ", browserBranch, false)+safeText(key.Name),
			paint("  Access   ", browserMuted, false)+paint(storagePermissions(key.Actions), browserValue, false))
		if bucketName == "" {
			lines = append(lines, paint("  Buckets  ", browserMuted, false)+storageList(key.Buckets))
		}
	}
	if count == 0 {
		lines = append(lines, paint("No keys with access", browserMuted, false))
	}
	return lines
}

func storagePermissions(actions []string) string {
	var result strings.Builder
	for _, permission := range []struct{ action, short string }{
		{client.StorageRead, "R"}, {client.StorageWrite, "W"}, {client.StorageList, "L"},
	} {
		if slices.Contains(actions, permission.action) {
			result.WriteString(permission.short)
		}
	}
	if result.Len() == 0 {
		return "—"
	}
	return result.String()
}

func storageInputPermissions(value string) string {
	actions, ok := parseStoragePermissions(value)
	if !ok {
		return safeText(value)
	}
	return storagePermissions(actions)
}

func parseStoragePermissions(value string) ([]string, bool) {
	var actions []string
	for _, item := range splitStorageNames(value) {
		var letters string
		switch strings.ToLower(item) {
		case "read":
			letters = "R"
		case "write":
			letters = "W"
		case "list":
			letters = "L"
		default:
			letters = strings.ToUpper(strings.TrimSpace(item))
		}
		for _, letter := range letters {
			var action string
			switch letter {
			case 'R':
				action = client.StorageRead
			case 'W':
				action = client.StorageWrite
			case 'L':
				action = client.StorageList
			default:
				return nil, false
			}
			if !slices.Contains(actions, action) {
				actions = append(actions, action)
			}
		}
	}
	return actions, true
}

func storageEndpointText(s string) string {
	if s == "" {
		return "(not configured)"
	}
	return safeText(s)
}

func storageSize(s string) string {
	value, ok := storageBytes(s)
	if !ok {
		return "—"
	}
	return formatBytes(value)
}

func storageBytes(s string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return value, err == nil && value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

func storageUsage(storage client.Storage) string {
	var total float64
	allowance := storagePlanAllowance(storage.PlanDiskGB)
	for _, bucket := range storage.Buckets {
		value, ok := storageBytes(bucket.Size)
		if !ok {
			return "— / " + allowance
		}
		total += value
	}
	return formatBytes(total) + " / " + allowance
}

func storagePlanAllowance(value *float64) string {
	if value == nil || *value < 0 || math.IsInf(*value, 0) || math.IsNaN(*value) {
		return "—"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64) + " GB plan"
}

func storageTreeSummary(storage client.Storage) string {
	return storageTreeUsage(storage) + " · " + storageBucketCount(storage)
}

func storageBucketCount(storage client.Storage) string {
	label := "buckets"
	if len(storage.Buckets) == 1 {
		label = "bucket"
	}
	return fmt.Sprintf("%d %s", len(storage.Buckets), label)
}

func storageTreeUsage(storage client.Storage) string {
	usage := storageUsage(storage)
	if storagePlanAllowance(storage.PlanDiskGB) == "—" {
		return strings.TrimSuffix(usage, " / —")
	}
	return strings.TrimSuffix(usage, " plan")
}

func storageList(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	return safeText(strings.Join(values, ", "))
}

func splitStorageNames(value string) []string {
	var out []string
	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" && !containsStorageValue(out, part) {
			out = append(out, part)
		}
	}
	return out
}

func containsStorageValue(values []string, value string) bool {
	return slices.Contains(values, value)
}
