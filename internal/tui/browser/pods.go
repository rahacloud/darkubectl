package browser

import (
	"fmt"
	"time"
)

const (
	podAgeHoursPerDay = 24
	podAgeValue24     = 24
	podAgeValue60     = 60
)

func (m *browser) appendPodsStatus(lines []string) []string {
	var done bool
	lines, done = m.appendPodSnapshotState(lines)
	if done {
		return lines
	}
	for i, pod := range m.podList {
		ready, total := pod.ReadyCount()
		status := pod.State
		if status == "" {
			status = pod.Phase
		}
		if status == "" {
			status = logUnknownState
		}
		if pod.Terminating {
			status = "Terminating"
		}
		age := "-"
		if !pod.CreatedAt.IsZero() {
			age = podAge(time.Since(pod.CreatedAt))
		}
		readyText := "-"
		if total > 0 {
			readyText = fmt.Sprintf("%d/%d", ready, total)
		}
		glyph, tint := "●", browserSuccess
		if !pod.Ready {
			glyph, tint = "●", browserNumber
		}
		lines = append(lines,
			paint(fmt.Sprintf("%s %s", glyph, safeText(pod.Name)), tint, true),
			fmt.Sprintf("  Ready %s   Status %s   Restarts %d   Age %s", readyText, safeText(status), pod.Restarts(), age))
		for _, container := range pod.Containers {
			state := container.State
			if state == "" {
				state = "Unknown"
			}
			row := fmt.Sprintf("  └ %s · %s · %d restarts", safeText(container.Name), safeText(state), container.RestartCount)
			if container.LastState != "" {
				row += " · previous " + safeText(container.LastState)
			}
			lines = append(lines, paint(row, browserMuted, false))
		}
		if i < len(m.podList)-1 {
			lines = append(lines, "")
		}
	}
	if len(m.podList) > 0 {
		lines = append(lines, "", paint("Snapshot · "+formatLocalWallTime(m.podsState.lastSuccessAt, "Jan 2, 3:04 PM MST"), browserMuted, false))
	}
	return lines
}

func (m *browser) appendPodSnapshotState(lines []string) ([]string, bool) {
	switch m.podsState.status {
	case requestLoading:
		return append(lines, m.loadingMessage("Loading pod snapshot…")), true
	case requestRefreshing:
		lines = append(lines, m.loadingMessage("Refreshing pod snapshot…"))
	case requestUnavailable:
		message := "Pod data is unavailable in this browser mode."
		if m.podsState.err != nil {
			message = safeText(m.podsState.err.Error())
		}
		return append(lines, paint(message, browserMuted, false)), true
	case requestFailed:
		message := "Pod snapshot unavailable"
		if m.podsState.err != nil {
			message += " · " + safeText(m.podsState.err.Error())
		}
		return append(lines, paint(message+" · press r to retry", browserNumber, false)), true
	case requestStale:
		lines = append(lines, paint("Refresh failed · showing last successful snapshot", browserNumber, false))
	case requestEmpty:
		return append(lines, paint("No pods are currently reported for this app.", browserMuted, false)), true
	default:
		if m.podsState.lastSuccessAt.IsZero() {
			lines = append(lines, paint("Press r to load the current pod snapshot.", browserMuted, false), "")
		}
	}
	return lines, false
}

func podAge(age time.Duration) string {
	if age < 0 {
		age = 0
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(age.Hours()), int(age.Minutes())%podAgeValue60)
	}
	return fmt.Sprintf("%dd%dh", int(age.Hours()/podAgeHoursPerDay), int(age.Hours())%podAgeValue24)
}
