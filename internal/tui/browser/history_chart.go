package browser

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

const historyPodColorCount uint32 = 10
const historyColorAlphaOpaque = 255

const (
	historyAxisMidpointDivisor = 2
	historyPercentageScale     = 100
	historyRaspberryGreen      = 51
	historyLeafGreen           = 192
	historyVioletGreen         = 94
	historyRedGreen            = 49
	historyLimeGreen           = 201
	historyFNVPrime            = 16777619
	historyPodColorValue130    = 130
	historyPodColorValue132    = 132
	historyPodColorValue214    = 214
	historyPodColorValue224    = 224
	historyPodColorValue64     = 64
	historyPodColorBlue108     = 108
	historyPodColorGreen87     = 87
	historyPodColorBlue247     = 247
	historyPodColorRed49       = 49
	historyPodColorBlue30      = 30
	historyColorBlueA1         = 34
	historyColorBlueA2         = 139
	historyColorBlueA3         = 230
	historyColorOrangeR        = 240
	historyColorOrangeG        = 140
	historyColorOrangeB        = 0
	historyColorCyanR          = 21
	historyColorCyanG          = 170
	historyColorCyanB          = 191
	historyColorPinkG          = 101
	historyColorPinkB          = 149
	historyColorAmberR         = 250
	historyColorAmberG         = 176
	historyColorAmberB         = 5
	historyXAxisValue16        = 16
	historyXAxisValue2         = 2
	historyXAxisValue36        = 36
	historyXAxisValue8         = 8
	usageCapacityValue1000     = 1000
	usageCapacityValue1e6      = 1e6
	usageCapacityValue30       = 30
)

const historyPlotHeight = 10

func maxHistoryScale(series []client.Series, reference *float64) float64 {
	maximum := 0.0
	for _, item := range series {
		for _, sample := range item.Values {
			if sample.Value >= 0 && !math.IsNaN(sample.Value) && !math.IsInf(sample.Value, 0) {
				maximum = math.Max(maximum, sample.Value)
			}
		}
	}
	if reference != nil && *reference > maximum {
		maximum = *reference
	}
	return maximum
}

func historyXAxis(start, end time.Time, width, prefixWidth int) []string {
	if width < 1 {
		return nil
	}
	format := "15:04"
	if end.Sub(start) >= 24*time.Hour {
		switch {
		case width >= historyXAxisValue36:
			format = "01/02 15:04"
		case width >= historyXAxisValue16:
			format = "01/02"
		default:
			format = "15h"
		}
	} else if width < historyXAxisValue8 {
		format = "15"
	}
	labels := []string{formatLocalWallTime(start, format), formatLocalWallTime(start.Add(end.Sub(start)/historyAxisMidpointDivisor),
		format), formatLocalWallTime(end, format)}
	showMiddle := width >= 3*ansi.StringWidth(labels[0])+historyXAxisValue2
	axis := make([]rune, width)
	for i := range axis {
		axis[i] = '─'
	}
	axis[0] = '└'
	axis[min(width-1, width/historyXAxisValue2)] = '┴'
	axis[width-1] = '┘'
	labelLine := make([]rune, width)
	for i := range labelLine {
		labelLine[i] = ' '
	}
	occupied := make([]bool, width)
	place := func(label string, startAt int) {
		runes := []rune(label)
		if startAt < 0 || startAt+len(runes) > width {
			return
		}
		for i := range runes {
			if occupied[startAt+i] {
				return
			}
		}
		for i, r := range runes {
			labelLine[startAt+i], occupied[startAt+i] = r, true
		}
	}
	place(labels[0], 0)
	place(labels[2], width-ansi.StringWidth(labels[2]))
	if showMiddle {
		place(labels[1], width/2-ansi.StringWidth(labels[1])/2)
	}
	prefix := strings.Repeat(" ", prefixWidth)
	return []string{paint(prefix+string(axis), browserBorder, false), paint(prefix+string(labelLine), browserMuted, false)}
}

type historyMetric struct {
	name, icon, capacityLabel string
	series                    func(*UsageHistory) []client.Series
	failed                    func(UsageMetricFailures) bool
	format                    func(float64) string
	capacity                  func(client.App) *float64
}

var historyMetrics = []historyMetric{
	{"CPU Usage", "", "request", func(h *UsageHistory) []client.Series { return h.CPU },
		func(f UsageMetricFailures) bool { return f.CPU }, formatCores, func(a client.App) *float64 { return usageCapacity(a, "cpu") }},
	{"CPU Throttle", "", "", func(h *UsageHistory) []client.Series { return h.CPUThrottle },
		func(f UsageMetricFailures) bool { return f.CPUThrottle }, func(v float64) string {
			return fmt.Sprintf("%.1f%%",
				v*historyPercentageScale)
		}, func(client.App) *float64 { return nil }},
	{"RAM Usage", "", "limit", func(h *UsageHistory) []client.Series { return h.RAM },
		func(f UsageMetricFailures) bool { return f.RAM }, formatBytes, func(a client.App) *float64 { return usageCapacity(a, "ram") }},
	{"Disk Usage", "", "capacity", func(h *UsageHistory) []client.Series { return h.Disk },
		func(f UsageMetricFailures) bool { return f.Disk }, formatBytes, func(a client.App) *float64 { return usageCapacity(a, "disk") }},
	{"Network Receive", "↓", "", func(h *UsageHistory) []client.Series { return h.NetworkReceive },
		func(f UsageMetricFailures) bool { return f.NetworkReceive }, func(v float64) string { return formatBytes(v) + "/s" },
		func(client.App) *float64 { return nil }},
	{"Network Send", "↑", "", func(h *UsageHistory) []client.Series { return h.NetworkSend },
		func(f UsageMetricFailures) bool { return f.NetworkSend }, func(v float64) string { return formatBytes(v) + "/s" },
		func(client.App) *float64 { return nil }},
}

func historyPodColor(pod string) color.Color {
	colors := []color.Color{
		color.RGBA{R: historyPodColorValue214, G: historyRaspberryGreen, B: historyPodColorBlue108, A: historyColorAlphaOpaque},
		color.RGBA{R: historyColorBlueA1, G: historyColorBlueA2, B: historyColorBlueA3, A: historyColorAlphaOpaque},
		color.RGBA{R: historyPodColorValue64, G: historyLeafGreen, B: historyPodColorGreen87, A: historyColorAlphaOpaque},
		color.RGBA{R: historyColorOrangeR, G: historyColorOrangeG, B: historyColorOrangeB, A: historyColorAlphaOpaque},
		color.RGBA{R: historyPodColorValue132, G: historyVioletGreen, B: historyPodColorBlue247, A: historyColorAlphaOpaque},
		color.RGBA{R: historyColorCyanR, G: historyColorCyanG, B: historyColorCyanB, A: historyColorAlphaOpaque},
		color.RGBA{R: historyPodColorValue224, G: historyRedGreen, B: historyPodColorRed49, A: historyColorAlphaOpaque},
		color.RGBA{R: historyColorOrangeR, G: historyColorPinkG, B: historyColorPinkB, A: historyColorAlphaOpaque},
		color.RGBA{R: historyPodColorValue130, G: historyLimeGreen, B: historyPodColorBlue30, A: historyColorAlphaOpaque},
		color.RGBA{R: historyColorAmberR, G: historyColorAmberG, B: historyColorAmberB, A: historyColorAlphaOpaque},
	}
	var hash uint32 = 2166136261
	for _, b := range []byte(pod) {
		hash = (hash ^ uint32(b)) * historyFNVPrime
	}
	return colors[hash%historyPodColorCount]
}

func usageCapacity(app client.App, resource string) *float64 {
	var value float64
	switch resource {
	case "cpu":
		if parsed, valid := parseUsageQuantity(app.CPURequest, true); valid {
			value = parsed
		}
		if value <= 0 && app.Plan != nil {
			value = float64(app.Plan.Detail.CPURequest) / usageCapacityValue1000
		}
	case "ram":
		if parsed, valid := parseUsageQuantity(app.RAMLimit, false); valid {
			value = parsed
		}
		if value <= 0 && app.Plan != nil {
			value = float64(app.Plan.Detail.RAMLimit) * usageCapacityValue1e6
		}
	case "disk":
		if app.Disk != nil {
			value = float64(app.Disk.SizeInGi) * (1 << usageCapacityValue30)
		}
	}
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func parseUsageQuantity(raw string, cpu bool) (float64, bool) {
	s := strings.TrimSpace(raw)
	multiplier := 1.0
	if cpu {
		if number, ok := strings.CutSuffix(s, "m"); ok {
			s, multiplier = number, .001
		}
	} else {
		for _, unit := range []struct {
			suffix     string
			multiplier float64
		}{{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"k", 1e3}, {"K", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12}} {
			if number, ok := strings.CutSuffix(s, unit.suffix); ok {
				s, multiplier = number, unit.multiplier
				break
			}
		}
	}
	value, err := strconv.ParseFloat(s, 64)
	value *= multiplier
	return value, err == nil && value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

// historyPlot scales to the higher of the sampled peak and reference value.
// Each column shows the highest reported value in its time bucket. Empty
// buckets stay blank except for the reference line.

func historyPlot(series []client.Series, start, end time.Time, step time.Duration, width int, reference *float64) ([]string,
	float64, bool) {
	if step <= 0 || !end.After(start) || width < 1 {
		return nil, 0, false
	}
	count := int(end.Sub(start)/step) + 1
	if count < 1 || count > 10000 {
		return nil, 0, false
	}
	if len(series) == 0 {
		return nil, 0, false
	}
	series = sortedHistorySeries(series)
	groups := min(count, max(1, width/len(series)))
	values, present, peak, found := bucketHistorySamples(series, start, end, step, count, groups)
	if !found {
		return nil, 0, false
	}
	scale := historyScale(peak, reference)
	rows, colors := newHistoryPlotGrid(width)
	interpolateHistoryGaps(values, present, len(series), groups)
	drawHistoryBars(rows, colors, series, values, present, scale, width, groups)
	drawHistoryReference(rows, colors, reference, scale)
	return renderHistoryPlot(rows, colors), peak, true
}

func sortedHistorySeries(series []client.Series) []client.Series {
	series = append([]client.Series(nil), series...)
	sort.Slice(series, func(i, j int) bool { return series[i].Pod < series[j].Pod })
	return series
}

func bucketHistorySamples(series []client.Series, start, end time.Time, step time.Duration, count, groups int) ([][]float64,
	[][]bool, float64, bool) {
	values := make([][]float64, len(series))
	present := make([][]bool, len(series))
	peak, found := 0.0, false
	for podIndex, pod := range series {
		values[podIndex] = make([]float64, groups)
		present[podIndex] = make([]bool, groups)
		for _, sample := range pod.Values {
			if invalidHistorySample(sample.Value) || sample.Time.Before(start) || sample.Time.After(end) {
				continue
			}
			index := int(sample.Time.Sub(start) / step)
			group := min(groups-1, index*groups/count)
			values[podIndex][group] = math.Max(values[podIndex][group], sample.Value)
			present[podIndex][group] = true
			peak, found = math.Max(peak, sample.Value), true
		}
	}
	return values, present, peak, found
}

func invalidHistorySample(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value < 0
}

func historyScale(peak float64, reference *float64) float64 {
	if reference != nil && *reference > peak {
		return *reference
	}
	return peak
}

func newHistoryPlotGrid(width int) ([][]rune, [][]color.Color) {
	rows := make([][]rune, historyPlotHeight)
	colors := make([][]color.Color, historyPlotHeight)
	for row := range rows {
		rows[row] = make([]rune, width)
		colors[row] = make([]color.Color, width)
		for col := range rows[row] {
			rows[row][col] = ' '
		}
	}
	return rows, colors
}

func interpolateHistoryGaps(values [][]float64, present [][]bool, pods, groups int) {
	for podIndex := range pods {
		previous := -1
		for group := range groups {
			if !present[podIndex][group] {
				continue
			}
			for gap := previous + 1; previous >= 0 && group-previous > 1 && gap < group; gap++ {
				fraction := float64(gap-previous) / float64(group-previous)
				values[podIndex][gap] = values[podIndex][previous] + (values[podIndex][group]-values[podIndex][previous])*fraction
				present[podIndex][gap] = true
			}
			previous = group
		}
	}
}

func drawHistoryBars(rows [][]rune, colors [][]color.Color, series []client.Series, values [][]float64, present [][]bool,
	scale float64, width, groups int) {
	height := len(rows)
	for group := range groups {
		left, right := group*width/groups, (group+1)*width/groups
		span := max(1, right-left)
		for podIndex, pod := range series {
			if !present[podIndex][group] {
				continue
			}
			barLeft := left + podIndex*span/len(series)
			barRight := left + (podIndex+1)*span/len(series)
			if barRight <= barLeft {
				barRight = barLeft + 1
			}
			barRight = min(width, barRight)
			level := 0
			if scale > 0 {
				level = int(math.Round(values[podIndex][group] / scale * float64(height-1)))
			}
			top := height - 1 - level
			for col := barLeft; col < barRight; col++ {
				for row := top; row < height; row++ {
					rows[row][col] = '█'
					colors[row][col] = historyPodColor(pod.Pod)
				}
			}
		}
	}
}

func drawHistoryReference(rows [][]rune, colors [][]color.Color, reference *float64, scale float64) {
	if reference != nil && *reference > 0 && scale > 0 {
		height := len(rows)
		line := height - 1 - int(math.Round(*reference/scale*float64(height-1)))
		for col := range rows[line] {
			if rows[line][col] == '█' {
				rows[line][col] = '┼'
			} else {
				rows[line][col] = '─'
			}
			colors[line][col] = browserMatchBG
		}
	}
}

func renderHistoryPlot(rows [][]rune, colors [][]color.Color) []string {
	out := make([]string, len(rows))
	for row := range rows {
		var b strings.Builder
		for col, cell := range rows[row] {
			if cell == ' ' {
				b.WriteRune(cell)
				continue
			}
			if colors[row][col] == nil {
				b.WriteString(paint(string(cell), browserMatchBG, false))
			} else {
				b.WriteString(paint(string(cell), colors[row][col], false))
			}
		}
		out[row] = b.String()
	}
	return out
}

// historyChart sums values reported at the same timestamp and plots gaps as
// spaces. The chart uses a zero baseline and the peak in this query window.

func historyChart(series []client.Series, start, end time.Time, step time.Duration, width int) (string, float64, bool) {
	count, ok := historyChartCount(start, end, step, width)
	if !ok {
		return "", 0, false
	}
	values, present, peak, found := aggregateHistorySamples(series, start, end, step, count)
	if !found {
		return "", 0, false
	}
	width = min(width, count)
	blocks := []rune("▁▂▃▄▅▆▇█")
	var chart strings.Builder
	for col := range width {
		first, last := col*count/width, (col+1)*count/width
		var maxValue float64
		hasValue := false
		for i := first; i < last; i++ {
			if present[i] {
				hasValue = true
				maxValue = math.Max(maxValue, values[i])
			}
		}
		if !hasValue {
			chart.WriteByte(' ')
			continue
		}
		level := 0
		if peak > 0 {
			level = min(len(blocks)-1, int(maxValue/peak*float64(len(blocks)-1)))
		}
		chart.WriteRune(blocks[level])
	}
	return chart.String(), peak, true
}

func historyChartCount(start, end time.Time, step time.Duration, width int) (int, bool) {
	if step <= 0 || !end.After(start) || width < 1 {
		return 0, false
	}
	count := int(end.Sub(start)/step) + 1
	return count, count > 0 && count <= 10000
}

func aggregateHistorySamples(series []client.Series, start, end time.Time, step time.Duration, count int) ([]float64, []bool,
	float64, bool) {
	values := make([]float64, count)
	present := make([]bool, count)
	for _, pod := range series {
		for _, sample := range pod.Values {
			if sample.Time.Before(start) || sample.Time.After(end) || math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) || sample.Value < 0 {
				continue
			}
			i := int(sample.Time.Sub(start) / step)
			values[i] += sample.Value
			present[i] = true
		}
	}
	var peak float64
	found := false
	for i, value := range values {
		if !present[i] {
			continue
		}
		found = true
		peak = math.Max(peak, value)
	}
	return values, present, peak, found
}
