package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"sort"
	"strings"
	"time"
)

// UsageHistory holds the API's per-pod time series for a single query window.
type UsageHistory struct {
	Start, End                  time.Time
	Step                        time.Duration
	CPU, CPUThrottle, RAM, Disk []client.Series
	NetworkReceive, NetworkSend []client.Series
	Failed                      UsageMetricFailures
}

type usageRange struct {
	label        string
	window, step time.Duration
}

var usageRanges = []usageRange{
	{label: "1h", window: time.Hour, step: time.Minute},
	{label: "6h", window: 6 * time.Hour, step: 5 * time.Minute},
	{label: "24h", window: 24 * time.Hour, step: 20 * time.Minute},
}

func (m *browser) loadUsageHistory() tea.Cmd {
	id := m.selectedDetailTarget()
	if id == "" || m.historyState.active() {
		return nil
	}
	if m.usageHistory == nil {
		m.historyState.unavailable(nil)
		return nil
	}
	window := usageRanges[m.historyRange]
	tenant := m.tenant
	meta, ctx := m.startRequest(requestUsageHistory, tenant, id, m.selectionGeneration)
	m.historyState.begin(meta, m.requestTime())
	getHistory := m.usageHistory
	return func() tea.Msg {
		history, err := getHistory(ctx, tenant, id, window.window, window.step)
		return usageHistoryResult{meta: meta, history: history, err: err}
	}
}

func (m *browser) historyLines() []string {
	window := usageRanges[m.historyRange]
	app := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == app.ID {
		app = *m.detailApp
	}
	metrics := availableHistoryMetrics(app)
	if m.historyMetric >= len(metrics) {
		m.historyMetric = 0
	}
	metric := metrics[m.historyMetric]
	_, width, _ := m.paneWidths()
	width = max(16, width-2)
	podName := m.historyPod
	if podName == "" {
		podName = "All pods"
	}
	controls := "[w] " + window.label + " · [m] " + metric.name + " · [p] "
	controls += ansi.Truncate(safeText(podName), max(0, width-ansi.StringWidth(controls)), "…")
	lines := []string{"", paint(controls, output.ColorMuted, false)}
	switch m.historyState.status {
	case requestIdle, requestLoading:
		return append(lines, m.loadingMessage("Loading history…"))
	case requestUnavailable:
		return append(lines, paint("History unavailable for this connection.", output.ColorMuted, false))
	case requestFailed:
		return append(lines, paint("History unavailable · r retry", output.ColorNumber, false))
	case requestEmpty:
		return append(lines, paint("No samples in this range.", output.ColorMuted, false))
	case requestRefreshing:
		lines = append(lines, m.loadingMessage("Refreshing history…"))
	case requestStale:
		lines = append(lines, paint("History stale · r retry", output.ColorNumber, false))
	case requestPartial:
		lines = append(lines, paint("Some metrics unavailable.", output.ColorNumber, false))
	}
	if m.history == nil {
		return lines
	}
	lines = append(lines, "")
	if metric.failed(m.history.Failed) {
		return append(lines, paint(metric.icon+" "+metric.name+" history unavailable", output.ColorMuted, false))
	}
	series := filterHistoryPod(metric.series(m.history), m.historyPod)
	capacity := metric.capacity(app)
	labelWidth := ansi.StringWidth(metric.format(maxHistoryScale(series, capacity)))
	labelWidth = min(12, max(5, labelWidth))
	axisWidth := labelWidth + 3
	plotWidth := max(4, width-axisWidth)
	chart, peak, ok := historyPlot(series, m.history.Start, m.history.End, m.history.Step, plotWidth, capacity)
	if !ok {
		return append(lines, paint(metric.icon+" "+metric.name+" · no samples", output.ColorMuted, false))
	}
	lines = append(lines, paint(metric.icon+" "+metric.name+" · peak "+metric.format(peak), output.ColorKey, true))
	if capacity != nil {
		capacityName := metric.capacityLabel
		if capacityName != "" {
			capacityName = strings.ToUpper(capacityName[:1]) + capacityName[1:]
		}
		lines = append(lines, paint("━━ "+capacityName+" · "+metric.format(*capacity), output.ColorMatchBG, false))
	}
	axisScale := maxHistoryScale(series, capacity)
	for row, plot := range chart {
		value := ""
		switch row {
		case 0:
			value = metric.format(axisScale)
		case historyPlotHeight / 2:
			value = metric.format(axisScale / 2)
		case historyPlotHeight - 1:
			value = metric.format(0)
		}
		prefix := strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(value))) + value + " │ "
		lines = append(lines, paint(prefix, output.ColorMuted, false)+plot)
	}
	lines = append(lines, historyXAxis(m.history.Start, m.history.End, plotWidth, axisWidth)...)
	lines = append(lines, "")
	for _, item := range series {
		if item.Pod == "" {
			continue
		}
		lines = append(lines, paint("● "+safeText(item.Pod), historyPodColor(item.Pod), false))
	}
	return lines
}

func availableHistoryMetrics(app client.App) []historyMetric {
	metrics := make([]historyMetric, 0, len(historyMetrics))
	for _, metric := range historyMetrics {
		if metric.name == "Disk Usage" && (app.Disk == nil || app.Disk.SizeInGi <= 0) {
			continue
		}
		metrics = append(metrics, metric)
	}
	return metrics
}

func (m *browser) usageViewport(lines []string, height int) []string {
	if len(lines) == 0 || height < 1 {
		return nil
	}
	room := max(0, height-1)
	m.usageScroll = min(m.usageScroll, max(0, len(lines)-1-room))
	end := min(len(lines), 1+m.usageScroll+room)
	return append([]string{lines[0]}, lines[1+m.usageScroll:end]...)
}

func historyPods(history *UsageHistory) []string {
	if history == nil {
		return nil
	}
	unique := make(map[string]struct{})
	for _, group := range [][]client.Series{history.CPU, history.CPUThrottle, history.RAM, history.Disk, history.NetworkReceive, history.NetworkSend} {
		for _, series := range group {
			if series.Pod != "" {
				unique[series.Pod] = struct{}{}
			}
		}
	}
	pods := make([]string, 0, len(unique))
	for pod := range unique {
		pods = append(pods, pod)
	}
	sort.Strings(pods)
	return pods
}

func (m *browser) syncHistoryPod() {
	if !m.historyPodChosen {
		m.historyPod = ""
		m.historyPodChosen = true
		return
	}
	pods := historyPods(m.history)
	if m.historyPod != "" {
		for _, pod := range pods {
			if pod == m.historyPod {
				return
			}
		}
		m.historyPod = ""
	}
}

func (m *browser) nextHistoryPod() {
	pods := historyPods(m.history)
	if len(pods) == 0 {
		return
	}
	m.historyPodChosen = true
	if m.historyPod == "" {
		m.historyPod = pods[0]
		return
	}
	for i, pod := range pods {
		if pod == m.historyPod && i+1 < len(pods) {
			m.historyPod = pods[i+1]
			return
		}
	}
	m.historyPod = ""
}

func filterHistoryPod(series []client.Series, pod string) []client.Series {
	if pod == "" {
		return series
	}
	var selected []client.Series
	for _, item := range series {
		if item.Pod == pod {
			selected = append(selected, item)
		}
	}
	return selected
}
