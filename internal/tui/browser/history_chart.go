package browser

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
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
		case width >= 36:
			format = "01/02 15:04"
		case width >= 16:
			format = "01/02"
		default:
			format = "15h"
		}
	} else if width < 8 {
		format = "15"
	}
	labels := []string{start.Local().Format(format), start.Add(end.Sub(start) / 2).Local().Format(format), end.Local().Format(format)}
	showMiddle := width >= 3*ansi.StringWidth(labels[0])+2
	axis := make([]rune, width)
	for i := range axis {
		axis[i] = '─'
	}
	axis[0] = '└'
	axis[min(width-1, width/2)] = '┴'
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
	return []string{paint(prefix+string(axis), output.ColorBorder, false), paint(prefix+string(labelLine), output.ColorMuted, false)}
}

type historyMetric struct {
	name, icon, capacityLabel string
	series                    func(*UsageHistory) []client.Series
	failed                    func(UsageMetricFailures) bool
	format                    func(float64) string
	capacity                  func(client.App) *float64
}

var historyMetrics = []historyMetric{
	{"CPU Usage", "", "request", func(h *UsageHistory) []client.Series { return h.CPU }, func(f UsageMetricFailures) bool { return f.CPU }, formatCores, func(a client.App) *float64 { return usageCapacity(a, "cpu") }},
	{"CPU Throttle", "", "", func(h *UsageHistory) []client.Series { return h.CPUThrottle }, func(f UsageMetricFailures) bool { return f.CPUThrottle }, func(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }, func(client.App) *float64 { return nil }},
	{"RAM Usage", "", "limit", func(h *UsageHistory) []client.Series { return h.RAM }, func(f UsageMetricFailures) bool { return f.RAM }, formatBytes, func(a client.App) *float64 { return usageCapacity(a, "ram") }},
	{"Disk Usage", "", "capacity", func(h *UsageHistory) []client.Series { return h.Disk }, func(f UsageMetricFailures) bool { return f.Disk }, formatBytes, func(a client.App) *float64 { return usageCapacity(a, "disk") }},
	{"Network Receive", "↓", "", func(h *UsageHistory) []client.Series { return h.NetworkReceive }, func(f UsageMetricFailures) bool { return f.NetworkReceive }, func(v float64) string { return formatBytes(v) + "/s" }, func(client.App) *float64 { return nil }},
	{"Network Send", "↑", "", func(h *UsageHistory) []client.Series { return h.NetworkSend }, func(f UsageMetricFailures) bool { return f.NetworkSend }, func(v float64) string { return formatBytes(v) + "/s" }, func(client.App) *float64 { return nil }},
}

func historyPodColor(pod string) color.Color {
	colors := []color.Color{
		color.RGBA{R: 214, G: 51, B: 108, A: 255}, color.RGBA{R: 34, G: 139, B: 230, A: 255},
		color.RGBA{R: 64, G: 192, B: 87, A: 255}, color.RGBA{R: 240, G: 140, B: 0, A: 255},
		color.RGBA{R: 132, G: 94, B: 247, A: 255}, color.RGBA{R: 21, G: 170, B: 191, A: 255},
		color.RGBA{R: 224, G: 49, B: 49, A: 255}, color.RGBA{R: 240, G: 101, B: 149, A: 255},
		color.RGBA{R: 130, G: 201, B: 30, A: 255}, color.RGBA{R: 250, G: 176, B: 5, A: 255},
	}
	var hash uint32 = 2166136261
	for _, r := range pod {
		hash = (hash ^ uint32(r)) * 16777619
	}
	return colors[int(hash%uint32(len(colors)))]
}

func usageCapacity(app client.App, resource string) *float64 {
	var value float64
	switch resource {
	case "cpu":
		value, _ = parseUsageQuantity(app.CPURequest, true)
		if value <= 0 && app.Plan != nil {
			value = float64(app.Plan.Detail.CPURequest) / 1000
		}
	case "ram":
		value, _ = parseUsageQuantity(app.RAMLimit, false)
		if value <= 0 && app.Plan != nil {
			value = float64(app.Plan.Detail.RAMLimit) * 1e6
		}
	case "disk":
		if app.Disk != nil {
			value = float64(app.Disk.SizeInGi) * (1 << 30)
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

func historyPlot(series []client.Series, start, end time.Time, step time.Duration, width int, reference *float64) ([]string, float64, bool) {
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
	series = append([]client.Series(nil), series...)
	sort.Slice(series, func(i, j int) bool { return series[i].Pod < series[j].Pod })
	groups := min(count, max(1, width/len(series)))
	values := make([][]float64, len(series))
	present := make([][]bool, len(series))
	peak := 0.0
	found := false
	for podIndex, pod := range series {
		values[podIndex] = make([]float64, groups)
		present[podIndex] = make([]bool, groups)
		for _, sample := range pod.Values {
			if sample.Time.Before(start) || sample.Time.After(end) || math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) || sample.Value < 0 {
				continue
			}
			index := int(sample.Time.Sub(start) / step)
			group := min(groups-1, index*groups/count)
			values[podIndex][group] = math.Max(values[podIndex][group], sample.Value)
			present[podIndex][group] = true
			peak = math.Max(peak, sample.Value)
			found = true
		}
	}
	if !found {
		return nil, 0, false
	}
	scale := peak
	if reference != nil && *reference > scale {
		scale = *reference
	}
	const height = historyPlotHeight
	rows := make([][]rune, height)
	colors := make([][]color.Color, height)
	for row := range rows {
		rows[row] = make([]rune, width)
		colors[row] = make([]color.Color, width)
		for col := range rows[row] {
			rows[row][col] = ' '
		}
	}
	for podIndex := range series {
		previous := -1
		for group := range groups {
			if !present[podIndex][group] {
				continue
			}
			if previous >= 0 && group-previous > 1 {
				for gap := previous + 1; gap < group; gap++ {
					fraction := float64(gap-previous) / float64(group-previous)
					values[podIndex][gap] = values[podIndex][previous] + (values[podIndex][group]-values[podIndex][previous])*fraction
					present[podIndex][gap] = true
				}
			}
			previous = group
		}
	}
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
	if reference != nil && *reference > 0 && scale > 0 {
		line := height - 1 - int(math.Round(*reference/scale*float64(height-1)))
		for col := range rows[line] {
			if rows[line][col] == '█' {
				rows[line][col] = '┼'
			} else {
				rows[line][col] = '─'
			}
			colors[line][col] = output.ColorMatchBG
		}
	}
	out := make([]string, height)
	for row := range rows {
		var b strings.Builder
		for col, cell := range rows[row] {
			if cell == ' ' {
				b.WriteRune(cell)
				continue
			}
			if colors[row][col] == nil {
				b.WriteString(paint(string(cell), output.ColorMatchBG, false))
			} else {
				b.WriteString(paint(string(cell), colors[row][col], false))
			}
		}
		out[row] = b.String()
	}
	return out, peak, true
}

// historyChart sums values reported at the same timestamp and plots gaps as
// spaces. The chart uses a zero baseline and the peak in this query window.

func historyChart(series []client.Series, start, end time.Time, step time.Duration, width int) (string, float64, bool) {
	if step <= 0 || !end.After(start) || width < 1 {
		return "", 0, false
	}
	count := int(end.Sub(start)/step) + 1
	if count < 1 || count > 10000 {
		return "", 0, false
	}
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
		if present[i] {
			found = true
			peak = math.Max(peak, value)
		}
	}
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
