package browser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	logDefaultTailSize = 100
	logMinTailSize     = 50
	logMaxTailSize     = 1000
	logTailStep        = 50
	logMaxEntries      = 2000
	logMaxBytes        = 2 << 20
	logPollSeconds     = 3
	logMaxRetrySeconds = 30
	logBytesPerRune    = 4
	logPanePadding     = 2
	logPickerMaxWidth  = 72
	logModalMargin     = 4
	logCenterDivisor   = 2
	logTimestampWidth  = 30
)

var (
	errLogsUnavailable = errors.New("log reading is unavailable in this browser mode")
	errLogsAuth        = errors.New("console rejected the session; relaunch darkubectl after running darkubectl login")
	errLogsPermission  = errors.New("your account does not have permission to read logs for this app")
	errLogsConsoleJWT  = errors.New("logs require a Console JWT; run darkubectl login")
	errLogsReadFailed  = errors.New("log request failed")
)

func (m *browser) prepareLogTarget() tea.Cmd {
	if !m.logPrevious {
		m.logFollowing = !m.logFollowPausedByUser
	}
	if len(m.podList) == 0 {
		m.logNotice = "No pods are currently reported for this app."
		return nil
	}
	if m.logPod != "" {
		for _, pod := range m.podList {
			if pod.Name == m.logPod {
				if m.logContainer == "" || !podHasContainer(pod, m.logContainer) {
					m.logContainer = preferredLogContainer(pod)
				}
				return m.setLogTarget(pod.Name, m.logContainer)
			}
		}
	}
	index := preferredLogPodIndex(m.podList)
	pod := m.podList[index]
	m.logPodCursor, m.logContainerCursor = index, 0
	return m.setLogTarget(pod.Name, preferredLogContainer(pod))
}

// prefer a ready, non-terminating replica and a ready container so opening
// Logs starts with useful output even when the app has several pods.
func preferredLogPodIndex(pods []appstate.Pod) int {
	for i, pod := range pods {
		if pod.Ready && !pod.Terminating {
			return i
		}
	}
	for i, pod := range pods {
		if !pod.Terminating {
			return i
		}
	}
	return 0
}

func preferredLogContainer(pod appstate.Pod) string {
	for _, container := range pod.Containers {
		if container.Ready {
			return container.Name
		}
	}
	for _, container := range pod.Containers {
		if container.Name == "main" {
			return container.Name
		}
	}
	if len(pod.Containers) > 0 {
		return pod.Containers[0].Name
	}
	return "main"
}

func podHasContainer(pod appstate.Pod, name string) bool {
	for _, container := range pod.Containers {
		if container.Name == name {
			return true
		}
	}
	return name == "main" && len(pod.Containers) == 0
}

func (m *browser) selectedLogPod() (string, bool) {
	if m.logPodCursor < 0 || m.logPodCursor >= len(m.podList) {
		return "", false
	}
	return m.podList[m.logPodCursor].Name, true
}

func (m *browser) selectedLogContainer() string {
	if m.logPodCursor < 0 || m.logPodCursor >= len(m.podList) {
		return ""
	}
	containers := m.podList[m.logPodCursor].Containers
	if len(containers) == 0 {
		return "main"
	}
	return containers[max(0, min(m.logContainerCursor, len(containers)-1))].Name
}

func (m *browser) chooseLogTarget() tea.Cmd {
	pod, ok := m.selectedLogPod()
	if !ok {
		return nil
	}
	container := m.selectedLogContainer()
	m.logTargetPicker = false
	return m.setLogTarget(pod, container)
}

func (m *browser) cycleLogPod(delta int) tea.Cmd {
	if len(m.podList) < 2 {
		return nil
	}
	index := 0
	for i, pod := range m.podList {
		if pod.Name == m.logPod {
			index = i
			break
		}
	}
	index = (index + delta + len(m.podList)) % len(m.podList)
	m.logPodCursor = index
	pod := m.podList[index]
	container := "main"
	if len(pod.Containers) > 0 {
		container = pod.Containers[0].Name
		for _, candidate := range pod.Containers {
			if candidate.Name == m.logContainer {
				container = candidate.Name
				break
			}
		}
	}
	return m.setLogTarget(pod.Name, container)
}

func (m *browser) setLogTarget(pod, container string) tea.Cmd {
	m.logTargetPicker = false
	m.cancelRequest(requestKey{kind: requestLogs})
	m.logPollInFlight = false
	m.logEntries = nil
	m.logScroll = 0
	m.logNewEntries = 0
	m.logPrevious = false
	m.logFollowing = !m.logFollowPausedByUser
	m.logRetryCount, m.logRetryDelay = 0, 0
	m.logNotice = ""
	m.logCopyStatus = ""
	m.logState.reset()
	m.logPod, m.logContainer = pod, container
	return m.loadSelectedLogs()
}

func (m *browser) loadSelectedLogs() tea.Cmd {
	appID := m.selectedDetailTarget()
	if appID == "" || m.logPod == "" || m.logContainer == "" || m.logPollInFlight {
		return nil
	}
	if m.appLogs == nil {
		m.logState.unavailable(errLogsUnavailable)
		return nil
	}
	meta, requestCtx := m.startRequest(requestLogs, m.tenant, appID, m.selectionGeneration)
	m.logState.begin(meta, m.requestTime())
	m.logPollInFlight = true
	tenant, pod, container, previous, read, tail := m.tenant, m.logPod, m.logContainer, m.logPrevious, m.appLogs, m.currentLogTail()
	return func() tea.Msg {
		entries, err := read(requestCtx, tenant, appID, client.LogOptions{
			PodName: pod, Container: container, Tail: tail, Previous: previous,
		})
		return logsResult{meta: meta, entries: entries, err: err, previous: previous, pod: pod, container: container}
	}
}

func (m *browser) currentLogTail() int {
	if m.logTail < logMinTailSize || m.logTail > logMaxTailSize {
		m.logTail = logDefaultTailSize
	}
	return m.logTail
}

func (m *browser) adjustLogTail(delta int) tea.Cmd {
	current := m.currentLogTail()
	next := min(logMaxTailSize, max(logMinTailSize, current+delta))
	if next == current {
		return nil
	}
	m.logTail = next
	m.cancelRequest(requestKey{kind: requestLogs})
	m.logPollInFlight = false
	m.logEntries = nil
	m.logScroll = 0
	m.logNewEntries = 0
	m.logRetryCount, m.logRetryDelay = 0, 0
	m.logNotice = ""
	m.logCopyStatus = ""
	m.logState.reset()
	return m.loadSelectedLogs()
}

func (m *browser) copyLogs() tea.Cmd {
	entries := append([]client.LogEntry(nil), m.logEntries...)
	utc, count := m.logUTC, len(entries)
	content := logClipboardText(entries, utc)
	return func() tea.Msg {
		return logCopyResult{count: count, err: clipboard.WriteAll(content)}
	}
}

func (m *browser) handleLogsCopied(msg logCopyResult) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.logCopyStatus = "Clipboard unavailable"
		return m, nil
	}
	m.logCopyStatus = fmt.Sprintf("Copied %d entries", msg.count)
	return m, nil
}

func logClipboardText(entries []client.LogEntry, utc bool) string {
	var result strings.Builder
	for _, entry := range entries {
		_, parts := formatLogPayload(entry.Text)
		prefix := formatLogTimestamp(entry.Timestamp, utc) + "  "
		for _, part := range parts {
			for i, line := range strings.Split(part, "\n") {
				if result.Len() > 0 {
					result.WriteByte('\n')
				}
				if i == 0 {
					result.WriteString(prefix)
				} else {
					result.WriteString(strings.Repeat(" ", len(prefix)))
				}
				result.WriteString(expandLogTabs(line))
			}
			prefix = strings.Repeat(" ", len(prefix))
		}
	}
	return result.String()
}

func (m *browser) handleLogsResult(msg logsResult) (tea.Model, tea.Cmd) {
	currentRequest := m.logState.matches(msg.meta)
	m.finishRequest(requestLogs, msg.meta)
	if currentRequest {
		m.logPollInFlight = false
	}
	if !currentRequest || msg.meta.tenant != m.tenant ||
		msg.meta.scopeGeneration != m.selectionGeneration || m.selectedDetailTarget() != msg.meta.appID ||
		msg.pod != m.logPod || msg.container != m.logContainer || msg.previous != m.logPrevious {
		return m, nil
	}
	if msg.err != nil {
		var apiErr *client.APIError
		_ = errors.As(msg.err, &apiErr)
		switch {
		case apiErr != nil && apiErr.StatusCode == http.StatusUnauthorized:
			m.logFollowing = false
			m.logState.fail(errLogsAuth)
		case apiErr != nil && apiErr.StatusCode == http.StatusForbidden:
			m.logFollowing = false
			m.logState.fail(errLogsPermission)
		case strings.Contains(msg.err.Error(), "requires a Console JWT"):
			m.logFollowing = false
			m.logState.unavailable(errLogsConsoleJWT)
		default:
			m.logState.fail(errLogsReadFailed)
		}
		if m.logFollowing {
			m.logRetryCount++
			seconds := min(logMaxRetrySeconds, logPollSeconds<<min(m.logRetryCount-1, logRetryExponentCap))
			m.logRetryDelay = seconds
			return m, m.scheduleLogPoll(time.Duration(seconds) * time.Second)
		}
		return m, nil
	}
	m.mergeLogEntries(msg.entries)
	m.logRetryCount = 0
	m.logRetryDelay = 0
	m.logState.succeed(len(m.logEntries) == 0, m.requestTime())
	if m.logFollowing && !m.logPrevious {
		return m, m.scheduleLogPoll(logPollSeconds * time.Second)
	}
	return m, nil
}

func (m *browser) mergeLogEntries(incoming []client.LogEntry) {
	atBottom := m.logScroll == 0
	byTime := make(map[string]string, len(m.logEntries)+len(incoming))
	for _, entry := range m.logEntries {
		byTime[entry.Timestamp] = entry.Text
	}
	newEntries := 0
	newRows := 0
	previousTimestamps := make(map[string]struct{}, len(m.logEntries))
	for _, entry := range m.logEntries {
		previousTimestamps[entry.Timestamp] = struct{}{}
	}
	sharedTimestamp := false
	notice := ""
	for _, entry := range incoming {
		if _, exists := previousTimestamps[entry.Timestamp]; exists {
			sharedTimestamp = true
		} else if len(m.logEntries) > 0 {
			newEntries++
		}
		text := sanitizeLog(entry.Text)
		if _, exists := byTime[entry.Timestamp]; !exists {
			newRows += logEntryLineCount(text)
		}
		runes := []rune(text)
		if len(text) > logMaxBytes {
			text = string(runes[:min(len(runes), logMaxBytes/logBytesPerRune)]) + "\n[entry truncated]"
			notice = "An oversized log entry was truncated."
		}
		byTime[entry.Timestamp] = text
	}
	if len(m.logEntries) > 0 && len(incoming) >= m.currentLogTail() && !sharedTimestamp {
		notice = "Some log entries may be missing between refreshes."
	}
	timestamps := make([]string, 0, len(byTime))
	for stamp := range byTime {
		timestamps = append(timestamps, stamp)
	}
	sort.Strings(timestamps)
	for len(timestamps) > logMaxEntries || logEntriesBytes(timestamps, byTime) > logMaxBytes {
		if len(timestamps) == 0 {
			break
		}
		delete(byTime, timestamps[0])
		timestamps = timestamps[1:]
		if notice == "" {
			notice = "Older log entries were removed to keep the buffer within its limit."
		}
	}
	m.logNotice = notice
	m.logEntries = make([]client.LogEntry, 0, len(timestamps))
	for _, stamp := range timestamps {
		m.logEntries = append(m.logEntries, client.LogEntry{Timestamp: stamp, Text: byTime[stamp]})
	}
	if atBottom {
		m.logScroll = 0
		m.logNewEntries = 0
	} else {
		m.logNewEntries += newEntries
		m.logScroll += newRows
	}
}

func logEntriesBytes(stamps []string, values map[string]string) int {
	total := 0
	for _, stamp := range stamps {
		total += len(stamp) + len(values[stamp])
	}
	return total
}

func sanitizeLog(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
}

func (m *browser) scheduleLogPoll(delay time.Duration) tea.Cmd {
	appID, pod, container, previous := m.selectedDetailTarget(), m.logPod, m.logContainer, m.logPrevious
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return logsPollMsg{appID: appID, pod: pod, container: container, previous: previous}
	})
}

func (m *browser) toggleLogPrevious() tea.Cmd {
	m.logPrevious = !m.logPrevious
	m.logFollowing = !m.logPrevious && !m.logFollowPausedByUser
	m.cancelRequest(requestKey{kind: requestLogs})
	m.logPollInFlight = false
	m.logEntries = nil
	m.logScroll, m.logNewEntries = 0, 0
	m.logCopyStatus = ""
	m.logState.reset()
	return m.loadSelectedLogs()
}

func (m *browser) clearLogs() {
	m.cancelRequest(requestKey{kind: requestLogs})
	m.logEntries = nil
	m.logState.reset()
	m.logRetryCount, m.logRetryDelay = 0, 0
	m.logPod, m.logContainer = "", ""
	m.logPrevious, m.logFollowing, m.logPollInFlight = false, false, false
	m.logFollowPausedByUser = false
	m.logTargetPicker, m.logSearchEditing = false, false
	m.logSearch, m.logNotice = "", ""
	m.logScroll, m.logNewEntries = 0, 0
	m.logMaximized = false
	m.logCopyStatus = ""
}

func (m *browser) logMatches() []int {
	query := strings.ToLower(m.logSearch)
	if query == "" {
		return nil
	}
	matches := make([]int, 0)
	for i, entry := range m.logEntries {
		if strings.Contains(strings.ToLower(entry.Timestamp+" "+entry.Text), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

func (m *browser) logsPaneLines() []string {
	lines := []string{}
	if m.logPod == "" {
		lines = append(lines, summaryField("Target", "waiting for pod snapshot"))
		switch m.podsState.status {
		case requestLoading, requestRefreshing:
			lines = append(lines, summaryField("Pods", m.loadingMessage("loading…")))
		case requestFailed, requestUnavailable:
			message := "pod snapshot unavailable"
			if m.podsState.err != nil {
				message += " · " + safeText(m.podsState.err.Error())
			}
			lines = append(lines, paint(message+" · press r to retry", browserNumber, false))
		case requestEmpty:
			lines = append(lines, summaryField("Pods", "none reported for this app"))
		default:
			if m.logNotice != "" {
				lines = append(lines, paint(safeText(m.logNotice), browserMuted, false))
			}
		}
		return lines
	}
	lines = append(lines, m.logMetadataLines()...)
	if m.logCopyStatus != "" {
		lines[len(lines)-1] += "  ·  " + paint(safeText(m.logCopyStatus), browserMuted, false)
	}
	if m.logSearchEditing {
		lines = append(lines, "Search: "+safeText(m.logSearch)+"█")
	} else if m.logSearch != "" {
		matches := m.logMatches()
		matchNumber := min(m.logMatch+1, max(1, len(matches)))
		lines = append(lines, fmt.Sprintf("Search: %s   Match %d/%d", safeText(m.logSearch), matchNumber, len(matches)))
	}
	if m.logNotice != "" {
		lines = append(lines, paint(safeText(m.logNotice), browserNumber, false))
	}
	if m.logState.status == requestUnavailable || m.logState.status == requestFailed || m.logState.status == requestStale {
		if m.logState.err != nil {
			lines = append(lines, paint(safeText(m.logState.err.Error()), browserNumber, false))
		}
	}
	lines = m.appendLogEmptyState(lines)
	for _, entry := range m.logEntries {
		level, parts := formatLogPayload(strings.ReplaceAll(entry.Text, "\r", ""))
		stamp := ansi.Truncate(formatLogTimestamp(entry.Timestamp, m.logUTC), logTimestampWidth, "…")
		prefix := stamp + "  "
		for n, part := range parts {
			for physicalLine, segment := range strings.Split(part, "\n") {
				linePrefix := strings.Repeat(" ", ansi.StringWidth(prefix))
				if n == 0 && physicalLine == 0 {
					linePrefix = prefix
				}
				wrapped := wrapLogLine(linePrefix+safeText(expandLogTabs(segment)), m.logPaneWidth(), m.logWrap)
				for i := range wrapped {
					if i == 0 && n == 0 && physicalLine == 0 && level != "" {
						wrapped[i] = colorLogLevelLine(wrapped[i], level)
					}
					lines = append(lines, wrapped[i])
				}
			}
		}
	}
	if m.logNewEntries > 0 {
		lines = append(lines, paint(fmt.Sprintf("%d new entries", m.logNewEntries), browserBranch, true))
	}
	return lines
}

func (m *browser) logMetadataLines() []string {
	if m.logPod == "" {
		return nil
	}
	podLabel := m.logPod
	for i, pod := range m.podList {
		if pod.Name == m.logPod {
			podLabel = fmt.Sprintf("%s (%d/%d)", m.logPod, i+1, len(m.podList))
			break
		}
	}
	follow := "Paused"
	followColor := browserMuted
	if m.logFollowing {
		follow = fmt.Sprintf("Following · %ds", logPollSeconds)
		followColor = browserSuccess
		if m.logRetryDelay > 0 {
			follow = fmt.Sprintf("Retrying · %ds", m.logRetryDelay)
		}
	}
	mode, modeColor := "Current", browserSuccess
	if m.logPrevious {
		mode, modeColor = "Previous", browserNumber
	}
	wrap, wrapColor := "OFF", browserMuted
	if m.logWrap {
		wrap, wrapColor = "ON", browserSuccess
	}
	return append(packLogFields(m.logPaneWidth(),
		logField("POD", safeText(podLabel), browserValue),
		logField("CONTAINER", safeText(m.logContainer), browserValue)),
		packLogFields(m.logPaneWidth(),
			logField("MODE", mode, modeColor), logField("FOLLOW", follow, followColor),
			logField("TAIL", fmt.Sprintf("%d", m.currentLogTail()), browserValue),
			logField("TIME", logClockLabel(m.logUTC), browserAccent),
			logField("WRAP", wrap, wrapColor))...)
}

func (m *browser) appendLogEmptyState(lines []string) []string {
	if len(m.logEntries) != 0 {
		return lines
	}
	switch {
	case m.logPrevious && !m.logState.active() && m.logState.err == nil:
		return append(lines, summaryField("Logs", "no previous entries"))
	case m.logState.status == requestLoading:
		return append(lines, summaryField("Logs", m.loadingMessage("loading…")))
	default:
		return append(lines, summaryField("Logs", "no entries"))
	}
}

func (m *browser) logPaneWidth() int {
	if m.logMaximized {
		return max(1, m.width-2)
	}
	_, width, _ := m.paneWidths()
	return max(1, width-logPanePadding)
}

func formatLogTimestamp(value string, utc bool) string {
	stamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	if utc {
		return stamp.UTC().Format("2006-01-02 15:04:05.000Z")
	}
	return stamp.Local().Format("2006-01-02 15:04:05.000 MST")
}

func logClockLabel(utc bool) string {
	if utc {
		return "UTC"
	}
	return "Local " + time.Now().Format("-0700")
}

func formatLogPayload(value string) (string, []string) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &root); err != nil || len(root) == 0 {
		if level, lines, ok := formatLogfmtPayload(value); ok {
			return level, lines
		}
		return plainLogLevel(value), strings.Split(value, "\n")
	}
	fields := make(map[string]json.RawMessage, len(root))
	var nested map[string]json.RawMessage
	if raw, ok := root["log"]; ok {
		_ = json.Unmarshal(raw, &nested)
	}
	for key, raw := range root {
		if key != "@timestamp" && key != "timestamp" && key != "log" {
			fields[key] = raw
		}
	}
	for key, raw := range nested {
		if key != "@timestamp" && key != "timestamp" {
			fields[key] = raw
		}
	}
	level := takeLogField(fields, "level", "severity", "log.level")
	logger := takeLogField(fields, "logger", "logger_name", "name")
	message := takeLogField(fields, "message", "msg", "event")
	lines := make([]string, 0, len(fields)+2)
	if level != "" || logger != "" {
		lines = append(lines, strings.TrimSpace(strings.ToUpper(level)+"  "+logger))
	}
	if message != "" {
		lines = append(lines, message)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = appendJSONLogField(lines, key, fields[key])
	}
	if len(lines) == 0 {
		return "", []string{value}
	}
	return strings.ToUpper(level), lines
}

func formatLogfmtPayload(value string) (string, []string, bool) {
	fields, ok := parseLogfmtFields(value)
	if !ok || len(fields) < 2 {
		return "", nil, false
	}
	level := firstLogfmtField(fields, "level", "severity")
	message := firstLogfmtField(fields, "msg", "message", "event")
	caller := firstLogfmtField(fields, "caller", "logger", "logger_name")
	if level == "" && message == "" && caller == "" {
		return "", nil, false
	}

	lines := make([]string, 0, len(fields)+2)
	if level != "" || caller != "" {
		lines = append(lines, strings.TrimSpace(strings.ToUpper(level)+"  "+caller))
	}
	if message != "" {
		lines = append(lines, message)
	}
	for _, key := range []string{"level", "severity", "msg", "message", "event", "caller", "logger", "logger_name", "ts", "timestamp", "time"} {
		delete(fields, key)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+": "+fields[key])
	}
	return strings.ToUpper(level), lines, true
}

func parseLogfmtFields(value string) (map[string]string, bool) {
	fields := make(map[string]string)
	for i := 0; i < len(value); {
		for i < len(value) && (value[i] == ' ' || value[i] == '\t' || value[i] == '\r' || value[i] == '\n') {
			i++
		}
		if i == len(value) {
			break
		}
		keyStart := i
		for i < len(value) && value[i] != '=' && value[i] != ' ' && value[i] != '\t' && value[i] != '\r' && value[i] != '\n' {
			i++
		}
		if i == keyStart || i == len(value) || value[i] != '=' {
			return nil, false
		}
		key := value[keyStart:i]
		i++
		if i < len(value) && value[i] == '"' {
			start := i
			i++
			escaped := false
			for i < len(value) {
				if escaped {
					escaped = false
					i++
					continue
				}
				if value[i] == '\\' {
					escaped = true
					i++
					continue
				}
				if value[i] == '"' {
					i++
					break
				}
				i++
			}
			quoted := value[start:i]
			decoded, err := strconv.Unquote(quoted)
			if err != nil {
				return nil, false
			}
			fields[key] = decoded
			if i < len(value) && value[i] != ' ' && value[i] != '\t' && value[i] != '\r' && value[i] != '\n' {
				return nil, false
			}
			continue
		}
		start := i
		for i < len(value) && value[i] != ' ' && value[i] != '\t' && value[i] != '\r' && value[i] != '\n' {
			i++
		}
		fields[key] = value[start:i]
	}
	return fields, len(fields) > 0
}

func firstLogfmtField(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			return value
		}
	}
	return ""
}

func logEntryLineCount(value string) int {
	_, parts := formatLogPayload(value)
	count := 0
	for _, part := range parts {
		count += strings.Count(part, "\n") + 1
	}
	return max(1, count)
}

func expandLogTabs(value string) string {
	return strings.ReplaceAll(value, "\t", "    ")
}

func appendJSONLogField(lines []string, path string, raw json.RawMessage) []string {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil && object != nil {
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = appendJSONLogField(lines, path+"."+key, object[key])
		}
		return lines
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) == nil && values != nil {
		for i, value := range values {
			lines = appendJSONLogField(lines, fmt.Sprintf("%s[%d]", path, i), value)
		}
		return lines
	}
	var stringValue string
	if json.Unmarshal(raw, &stringValue) == nil {
		return append(lines, path+": "+stringValue)
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) == nil {
		lines = append(lines, path+": "+compact.String())
	}
	return lines
}

func plainLogLevel(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	first := strings.Trim(fields[0], "[](){}<>:;,\"'")
	if isKnownLogLevel(first) {
		return strings.ToUpper(first)
	}
	for _, field := range fields {
		field = strings.Trim(field, "[](){}<>,;\"'")
		for _, separator := range []string{"=", ":"} {
			key, value, found := strings.Cut(field, separator)
			if found && strings.EqualFold(key, "level") && isKnownLogLevel(value) {
				return strings.ToUpper(strings.Trim(value, ":"))
			}
		}
	}
	return ""
}

func isKnownLogLevel(value string) bool {
	switch strings.ToUpper(strings.Trim(value, ":")) {
	case "TRACE", "DEBUG", "INFO", "WARN", "WARNING", "ERROR", "FATAL", "CRITICAL", "PANIC":
		return true
	default:
		return false
	}
}

func takeLogField(fields map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		delete(fields, key)
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return value
		}
		return strings.Trim(string(raw), "\"")
	}
	return ""
}

func colorLogLevelLine(line, level string) string {
	color := browserMuted
	switch strings.ToUpper(level) {
	case "ERROR", "FATAL", "CRITICAL", "PANIC":
		color = browserDanger
	case "WARN", "WARNING":
		color = browserNumber
	case "INFO":
		color = browserSuccess
	case "DEBUG", "TRACE":
		color = browserMuted
	}
	return paint(line, color, true)
}

func wrapLogLine(line string, width int, enabled bool) []string {
	if !enabled || width < 2 || ansi.StringWidth(line) <= width {
		return []string{line}
	}
	runes := []rune(line)
	result := make([]string, 0, (len(runes)+width-1)/width)
	for len(runes) > 0 {
		count := min(width, len(runes))
		result = append(result, string(runes[:count]))
		runes = runes[count:]
	}
	return result
}

func (m *browser) logsViewport(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	sticky := 0
	if !m.logTargetPicker {
		sticky = min(len(m.logMetadataLines()), len(lines))
	}
	if m.logSearch != "" || m.logSearchEditing {
		sticky = min(sticky+1, len(lines))
	}
	body := lines[sticky:]
	visible := max(0, height-sticky)
	maxScroll := max(0, len(body)-visible)
	m.logScroll = min(m.logScroll, maxScroll)
	end := len(body) - m.logScroll
	start := max(0, end-visible)
	return append(append([]string(nil), lines[:sticky]...), body[start:end]...)
}

func (m *browser) moveLogMatch(delta int) {
	matches := m.logMatches()
	if len(matches) == 0 {
		return
	}
	m.logMatch = (m.logMatch + delta + len(matches)) % len(matches)
	position := matches[m.logMatch]
	m.logScroll = max(0, len(m.logEntries)-position-1)
}

func logField(label, value string, foreground color.Color) string {
	return paint(label, browserMuted, true) + " " + paint(value, foreground, true)
}

func packLogFields(width int, fields ...string) []string {
	width = max(1, width)
	lines := make([]string, 0, len(fields))
	line := ""
	for _, field := range fields {
		separator := "  ·  "
		candidate := field
		if line != "" {
			candidate = line + separator + field
		}
		if line != "" && ansi.StringWidth(candidate) > width {
			lines = append(lines, line)
			line = field
		} else {
			line = candidate
		}
		if ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width, "…")
			lines = append(lines, line)
			line = ""
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func (m *browser) logTargetModal(lines []string) []string {
	if !m.logTargetPicker || !m.terminalChoosing {
		return lines
	}
	panelWidth := min(logPickerMaxWidth, m.width-logModalMargin)
	innerWidth := max(2, panelWidth-6)
	gap := 3
	podWidth := max(1, (innerWidth-gap)/2)
	containerWidth := max(1, innerWidth-podWidth-gap)
	capacity := max(1, min(8, m.height-14))
	podStart := max(0, min(m.logPodCursor-capacity+1, len(m.podList)-capacity))
	podEnd := min(len(m.podList), podStart+capacity)
	containers := []string{"main · CLI default"}
	if m.logPodCursor >= 0 && m.logPodCursor < len(m.podList) && len(m.podList[m.logPodCursor].Containers) > 0 {
		containers = containers[:0]
		for _, container := range m.podList[m.logPodCursor].Containers {
			containers = append(containers, safeText(container.Name))
		}
	}
	containerStart := max(0, min(m.logContainerCursor-capacity+1, len(containers)-capacity))
	containerEnd := min(len(containers), containerStart+capacity)
	leftHeader, rightHeader := fmt.Sprintf("PODS · %d", len(m.podList)), fmt.Sprintf("CONTAINERS · %d", len(containers))
	if m.logPickerColumn == 0 {
		leftHeader = paint(leftHeader, browserAccent, true)
	} else {
		rightHeader = paint(rightHeader, browserAccent, true)
	}
	title := "CHOOSE LOG TARGET"
	if m.terminalChoosing {
		title = "CHOOSE TERMINAL TARGET"
	}
	content := []string{
		paint(title, browserAccent, true),
		paint(strings.Repeat("─", innerWidth), browserBorder, false),
		pickerColumns(leftHeader, rightHeader, podWidth, containerWidth, gap),
	}
	for row := 0; row < capacity; row++ {
		podIndex, containerIndex := podStart+row, containerStart+row
		podLine, containerLine := "", ""
		if podIndex < podEnd {
			pod := m.podList[podIndex]
			status := pod.State
			if status == "" {
				status = pod.Phase
			}
			if status == "" {
				status = logUnknownState
			}
			podLine = safeText(pod.Name) + " · " + safeText(status)
			podLine = pickerRow(podLine, podWidth, podIndex == m.logPodCursor && m.logPickerColumn == 0)
		}
		if containerIndex < containerEnd {
			containerLine = pickerRow(containers[containerIndex], containerWidth,
				containerIndex == m.logContainerCursor && m.logPickerColumn == 1)
		}
		content = append(content, pickerColumns(podLine, containerLine, podWidth, containerWidth, gap))
	}
	if podStart > 0 || podEnd < len(m.podList) {
		content = append(content, paint(fmt.Sprintf("Pods %d–%d of %d", podStart+1, podEnd, len(m.podList)), browserMuted, false))
	}
	if containerStart > 0 || containerEnd < len(containers) {
		content = append(content, paint(fmt.Sprintf("Containers %d–%d of %d", containerStart+1, containerEnd, len(containers)), browserMuted, false))
	}
	panel := tenantPanelStyle().Width(panelWidth).Render(strings.Join(content, "\n"))
	panelLines := strings.Split(panel, "\n")
	startY, startX := max(0, (m.height-len(panelLines))/logCenterDivisor), max(0, (m.width-panelWidth)/logCenterDivisor)
	for i, line := range panelLines {
		y := startY + i
		if y >= len(lines) {
			break
		}
		base := ansi.Truncate(lines[y], m.width, "")
		base += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(base)))
		lines[y] = ansi.Cut(base, 0, startX) + line + ansi.Cut(base, startX+panelWidth, m.width)
	}
	return lines
}

func pickerColumns(left, right string, leftWidth, rightWidth, gap int) string {
	left = ansi.Truncate(left, leftWidth, "…")
	right = ansi.Truncate(right, rightWidth, "…")
	left += strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(left)))
	return left + strings.Repeat(" ", gap) + right
}

func pickerRow(label string, width int, selected bool) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	line := ansi.Truncate(marker+safeText(label), width, "…")
	line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	if selected {
		return selectedRowStyle().Render(line)
	}
	return line
}

func (m *browser) handleLogTargetKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case browserKeyEscape, "t":
		m.logTargetPicker = false
	case keyLeft, "h":
		m.logPickerColumn = max(0, m.logPickerColumn-1)
	case keyRight, "l":
		m.logPickerColumn = min(1, m.logPickerColumn+1)
	case keyUp, "k":
		if m.logPickerColumn == 0 {
			m.logPodCursor = max(0, m.logPodCursor-1)
			m.logContainerCursor = 0
		} else if m.logPodCursor >= 0 && m.logPodCursor < len(m.podList) {
			m.logContainerCursor = max(0, m.logContainerCursor-1)
		}
	case keyDown, "j":
		if m.logPickerColumn == 0 {
			m.logPodCursor = min(len(m.podList)-1, m.logPodCursor+1)
			m.logContainerCursor = 0
		} else if m.logPodCursor >= 0 && m.logPodCursor < len(m.podList) {
			m.logContainerCursor = min(max(0, len(m.podList[m.logPodCursor].Containers)-1), m.logContainerCursor+1)
		}
	case keyEnter:
		return m.chooseLogTarget()
	}
	return nil
}

func (m *browser) handleLogSearchKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case browserKeyEscape:
		m.logSearch = m.logSearchBefore
		m.logSearchEditing = false
	case keyEnter:
		m.logSearchEditing = false
		m.logMatch = 0
		m.moveLogMatch(0)
	case keyBackspace, keyCtrlH:
		r := []rune(m.logSearch)
		if len(r) > 0 {
			m.logSearch = string(r[:len(r)-1])
		}
	case keyCtrlU:
		m.logSearch = ""
	default:
		if msg.Text != "" {
			m.logSearch += safeText(msg.Text)
		}
	}
}
