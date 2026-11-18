package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestHistoryChartSumsPodsAndPreservesMissingSteps(t *testing.T) {
	start := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	series := []client.Series{
		{Values: []client.Sample{{Time: start, Value: 1}, {Time: start.Add(2 * time.Minute), Value: 2}}},
		{Values: []client.Sample{{Time: start, Value: 3}}},
	}
	chart, peak, ok := historyChart(series, start, start.Add(2*time.Minute), time.Minute, 3)
	if !ok || peak != 4 || chart != "█ ▄" {
		t.Fatalf("chart = %q, peak = %v, ok = %t", chart, peak, ok)
	}
}

func TestHistoryPlotShowsReferenceAndSampleGaps(t *testing.T) {
	start := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	reference := 2.0
	series := []client.Series{{Pod: "a", Values: []client.Sample{{Time: start, Value: 1}, {Time: start.Add(2 * time.Minute), Value: 3}}}}
	rows, peak, ok := historyPlot(series, start, start.Add(2*time.Minute), time.Minute, 3, &reference)
	joined := strings.Join(rows, "")
	if !ok || peak != 3 || len(rows) != 8 || !strings.Contains(joined, "●") || !strings.Contains(joined, "─") {
		t.Fatalf("plot=%q peak=%v ok=%t", rows, peak, ok)
	}
}

func TestUsageCapacityAndColors(t *testing.T) {
	app := client.App{CPURequest: "750m", RAMLimit: "512Mi", Disk: &client.Disk{SizeInGi: 2}}
	for _, tc := range []struct {
		resource string
		want     float64
	}{{"cpu", .75}, {"ram", 512 * (1 << 20)}, {"disk", 2 * (1 << 30)}} {
		got := usageCapacity(app, tc.resource)
		if got == nil || *got != tc.want {
			t.Errorf("%s capacity = %v, want %v", tc.resource, got, tc.want)
		}
	}
}

func TestHistoryPodSelection(t *testing.T) {
	m := &browser{history: &UsageHistory{CPU: []client.Series{{Pod: "b"}, {Pod: "a"}}}}
	m.syncHistoryPod()
	if m.historyPod != "" {
		t.Fatalf("initial pod filter = %q, want all pods", m.historyPod)
	}
	m.nextHistoryPod()
	if m.historyPod != "a" {
		t.Fatalf("next pod = %q", m.historyPod)
	}
	m.nextHistoryPod()
	if m.historyPod != "b" {
		t.Fatalf("next pod = %q", m.historyPod)
	}
	m.nextHistoryPod()
	if m.historyPod != "" {
		t.Fatalf("pod cycle should return to all pods")
	}
	m.syncHistoryPod()
	if m.historyPod != "" {
		t.Fatal("refresh should preserve aggregate selection")
	}
}

func TestDiskMetricIsConditionalOnConfiguredDisk(t *testing.T) {
	if len(availableHistoryMetrics(client.App{})) != 5 {
		t.Fatal("disk metric should be hidden without disk")
	}
	metrics := availableHistoryMetrics(client.App{Disk: &client.Disk{SizeInGi: 1}})
	if len(metrics) != 6 || metrics[3].name != "Disk Usage" {
		t.Fatalf("metrics with disk = %#v", metrics)
	}
}

func TestHistoryRangeChangeRejectsPreviousResponse(t *testing.T) {
	m := &browser{
		tenant: "acme", detail: true, activeAppView: appViewUsage,
		apps: []client.App{{ID: "a", Name: "app"}},
		usageHistory: func(_ context.Context, _, _ string, window, step time.Duration) (*UsageHistory, error) {
			return &UsageHistory{Step: step, End: time.Unix(int64(window.Seconds()), 0)}, nil
		},
	}
	old := m.loadUsageHistory()
	_, next := m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	newResult := next().(usageHistoryResult)
	m.Update(newResult)
	m.Update(old().(usageHistoryResult))
	if m.history == nil || m.history.Step != 5*time.Minute || m.historyRange != 1 {
		t.Fatalf("old range replaced current history: %+v", m.history)
	}
}
