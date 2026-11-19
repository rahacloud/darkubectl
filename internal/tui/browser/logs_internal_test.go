package browser

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/appstate"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestLogTargetSelectionUsesExplicitPodAndContainer(t *testing.T) {
	t.Parallel()
	calls := 0
	m := &browser{
		tenant: "tenant", detail: true, activeAppView: appViewLogs,
		apps: []client.App{{ID: "app"}},
		podList: []appstate.Pod{
			{Name: "pod-a", Containers: []appstate.Container{{Name: "main"}, {Name: "sidecar"}}},
			{Name: "pod-b", Containers: []appstate.Container{{Name: "worker"}}},
		},
		appLogs: func(_ context.Context, _, id string, opts client.LogOptions) ([]client.LogEntry, error) {
			calls++
			if id != "app" || opts.PodName != "pod-a" || opts.Container != "sidecar" || opts.Tail != 100 || opts.Previous {
				t.Errorf("unexpected request target/options: %s %+v", id, opts)
			}
			return []client.LogEntry{{Timestamp: "2026-11-19T10:00:00Z", Text: "line"}}, nil
		},
	}
	if cmd := m.prepareLogTarget(); cmd != nil || !m.logTargetPicker {
		t.Fatal("multiple targets should require a picker")
	}
	if !m.logFollowing {
		t.Fatal("current logs should follow by default while the target picker is open")
	}
	m.logPickerColumn = 1
	m.logContainerCursor = 1
	cmd := m.chooseLogTarget()
	if cmd == nil || m.logTargetPicker {
		t.Fatal("target selection should start a read")
	}
	msg := cmd()
	result, ok := msg.(logsResult)
	if !ok {
		t.Fatalf("command result = %T", msg)
	}
	m.handleLogsResult(result)
	if calls != 1 || m.logPod != "pod-a" || m.logContainer != "sidecar" || len(m.logEntries) != 1 {
		t.Fatalf("selection/read state: calls=%d pod=%q container=%q entries=%v", calls, m.logPod, m.logContainer, m.logEntries)
	}
}

func TestLogCurrentPreviousToggleAndEmptyPreviousMessage(t *testing.T) {
	t.Parallel()
	modes := []bool{}
	m := &browser{
		tenant: "tenant", detail: true, activeAppView: appViewLogs,
		apps: []client.App{{ID: "app"}}, logPod: "pod", logContainer: "main",
		logState: requestState{status: requestEmpty},
		appLogs: func(_ context.Context, _, _ string, opts client.LogOptions) ([]client.LogEntry, error) {
			modes = append(modes, opts.Previous)
			return nil, nil
		},
	}
	cmd := m.toggleLogPrevious()
	if cmd == nil {
		t.Fatal("previous toggle did not load logs")
	}
	result, ok := cmd().(logsResult)
	if !ok {
		t.Fatal("log command returned an unexpected message")
	}
	m.handleLogsResult(result)
	if !m.logPrevious || len(modes) != 1 || !modes[0] {
		t.Fatalf("previous mode = %t; modes=%v", m.logPrevious, modes)
	}
	got := ansi.Strip(strings.Join(m.logsPaneLines(), "\n"))
	if !strings.Contains(got, "No previous logs returned") || !strings.Contains(got, "MODE Previous (p)") {
		t.Fatalf("empty previous view = %s", got)
	}
	cmd = m.toggleLogPrevious()
	if cmd == nil {
		t.Fatal("current toggle did not load logs")
	}
	result, ok = cmd().(logsResult)
	if !ok {
		t.Fatal("log command returned an unexpected message")
	}
	m.handleLogsResult(result)
	if m.logPrevious || len(modes) != 2 || modes[1] {
		t.Fatalf("current mode = %t; modes=%v", m.logPrevious, modes)
	}
	if !m.logFollowing {
		t.Fatal("switching back to current logs should resume default following")
	}
}

func TestLogStatusDoesNotReplaceFollowStateWithRequestStatus(t *testing.T) {
	t.Parallel()
	m := &browser{
		detail: true, activeAppView: appViewLogs, apps: []client.App{{ID: "app"}},
		logPod: "pod", logContainer: "main", logFollowing: true,
		logState: requestState{status: requestLoading},
	}
	lines := m.logsPaneLines()
	followLine := ""
	for _, line := range lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "FOLLOW") {
			followLine = plain
			break
		}
	}
	if !strings.Contains(followLine, "Following") || strings.Contains(followLine, "Loading") || strings.Contains(followLine, "Refreshing") {
		t.Fatalf("follow status should describe follow mode only: %s", followLine)
	}
}

func TestLogViewKeepsMouseEventsForAppSelection(t *testing.T) {
	t.Parallel()
	m := &browser{
		width: 120, height: 30, detail: true, activeAppView: appViewLogs,
		apps: []client.App{{ID: "app"}}, logPod: "pod", logContainer: "main",
	}
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("Logs mouse mode = %v, want cell motion for app selection", got)
	}
}

func TestMergeLogsSanitizesMergesAndBoundsBuffer(t *testing.T) {
	t.Parallel()
	m := &browser{logEntries: []client.LogEntry{{Timestamp: "2026-01-01T00:00:00Z", Text: "old"}}}
	m.mergeLogEntries([]client.LogEntry{
		{Timestamp: "2026-01-01T00:00:00Z", Text: "\x1b[31mupdated\x1b[0m\x07"},
		{Timestamp: "2026-01-01T00:00:01Z", Text: "next\nline"},
	})
	if len(m.logEntries) != 2 || m.logEntries[0].Text != "updated" || m.logEntries[1].Text != "next\nline" {
		t.Fatalf("merged entries = %+v", m.logEntries)
	}
	m.mergeLogEntries([]client.LogEntry{{Timestamp: "2026-01-02T00:00:00Z", Text: strings.Repeat("x", logMaxBytes+16)}})
	bytes := 0
	for _, entry := range m.logEntries {
		bytes += len(entry.Timestamp) + len(entry.Text)
	}
	if bytes > logMaxBytes || !strings.Contains(m.logNotice, "truncated") {
		t.Fatalf("oversized entry handling: %d bytes, %q", bytes, m.logNotice)
	}
	entries := make([]client.LogEntry, logMaxEntries+10)
	for i := range entries {
		entries[i] = client.LogEntry{Timestamp: fmt.Sprintf("%04d", i), Text: "x"}
	}
	m.mergeLogEntries(entries)
	if len(m.logEntries) > logMaxEntries {
		t.Fatalf("entry limit: len=%d notice=%q", len(m.logEntries), m.logNotice)
	}
}

func TestLogSearchAndWrappingControls(t *testing.T) {
	t.Parallel()
	m := &browser{
		detail: true, activeAppView: appViewLogs, apps: []client.App{{ID: "app"}},
		logEntries: []client.LogEntry{{Timestamp: "1", Text: "first error"}, {Timestamp: "2", Text: "second"}},
		logWrap:    true,
	}
	m.logSearch = "error"
	if got := m.logMatches(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("matches=%v", got)
	}
	m.moveLogMatch(0)
	if m.logScroll != 1 {
		t.Fatalf("match did not move viewport: scroll=%d", m.logScroll)
	}
	model, _ := m.handleKeyPress(tea.KeyPressMsg{Code: 'w', Text: "w"})
	updated, ok := model.(*browser)
	if !ok {
		t.Fatalf("model = %T, want *browser", model)
	}
	if updated.logWrap {
		t.Fatal("w should turn wrapping off when it starts enabled")
	}
	wrapped := wrapLogLine("abcdefgh", 4, true)
	if len(wrapped) != 2 || wrapped[0] != "abcd" || wrapped[1] != "efgh" {
		t.Fatalf("wrapped=%v", wrapped)
	}
}

func TestFormatLogPayloadKeepsPlainTextAndFormatsStructuredJSON(t *testing.T) {
	plain := "worker finished without a JSON envelope"
	level, lines := formatLogPayload(plain)
	if level != "" || len(lines) != 1 || lines[0] != plain {
		t.Fatalf("plain payload = level %q, lines %q", level, lines)
	}
	if got, _ := formatLogPayload("level=warn retrying request"); got != "WARN" {
		t.Fatalf("plain level detection = %q", got)
	}

	level, lines = formatLogPayload(`level=error ts=2026-11-19T05:37:18.424833Z caller=compactor.go:156 msg="compaction failed" tenant=single-tenant err="failed to read blocks\nerror fetching object from s3 backend: internal error"`)
	joined := strings.Join(lines, "\n")
	if level != "ERROR" || !strings.Contains(joined, "ERROR  compactor.go:156") ||
		!strings.Contains(joined, "compaction failed") || !strings.Contains(joined, "tenant: single-tenant") ||
		!strings.Contains(joined, "failed to read blocks\nerror fetching object from s3 backend: internal error") ||
		strings.Contains(joined, "ts:") || strings.Contains(joined, "msg:") {
		t.Fatalf("logfmt payload = level %q, lines %q", level, lines)
	}

	level, lines = formatLogPayload(`{"@timestamp":"2026-11-19T04:40:59Z","log":{"level":"ERROR","logger":"service.worker","message":"request failed","attempt":2,"process":{"thread":{"name":"http-1"}}}}`)
	joined = strings.Join(lines, "\n")
	if level != "ERROR" || !strings.Contains(joined, "ERROR  service.worker") ||
		!strings.Contains(joined, "request failed") || !strings.Contains(joined, "attempt: 2") ||
		!strings.Contains(joined, "process.thread.name: http-1") ||
		strings.Contains(joined, "@timestamp") {
		t.Fatalf("structured payload = level %q, lines %q", level, lines)
	}
}

func TestLogPayloadPreservesNewlinesAndExpandsTabs(t *testing.T) {
	_, parts := formatLogPayload(`{"message":"first line\nsecond\tcolumn"}`)
	if len(parts) != 1 || !strings.Contains(parts[0], "first line\nsecond\tcolumn") {
		t.Fatalf("JSON message separators were not decoded: %q", parts)
	}
	if got := logEntryLineCount(`{"message":"first line\nsecond\tcolumn"}`); got != 2 {
		t.Fatalf("JSON message line count = %d, want 2", got)
	}
	if got := expandLogTabs("first\tcolumn"); got != "first    column" {
		t.Fatalf("tab expansion = %q", got)
	}
	if got := logEntryLineCount("first line\nsecond line"); got != 2 {
		t.Fatalf("plain message line count = %d, want 2", got)
	}
}

func TestLogTimestampUsesReadableLocalAndUTCModes(t *testing.T) {
	stamp := "2026-11-19T04:40:59.824173Z"
	local, utc := formatLogTimestamp(stamp, false), formatLogTimestamp(stamp, true)
	if !strings.HasPrefix(local, "2026-11-19 ") || !strings.HasPrefix(utc, "2026-11-19 ") || !strings.HasSuffix(utc, "Z") {
		t.Fatalf("local timestamp %q, UTC timestamp %q", local, utc)
	}
	if got := formatLogTimestamp("not-a-time", false); got != "not-a-time" {
		t.Fatalf("unparseable timestamp = %q", got)
	}
}

func TestCycleLogPodKeepsMatchingContainerWhenAvailable(t *testing.T) {
	t.Parallel()
	m := &browser{
		tenant: "tenant", detail: true, apps: []client.App{{ID: "app"}},
		logPod: "pod-a", logContainer: "sidecar",
		podList: []appstate.Pod{
			{Name: "pod-a", Containers: []appstate.Container{{Name: "sidecar"}}},
			{Name: "pod-b", Containers: []appstate.Container{{Name: "main"}, {Name: "sidecar"}}},
		},
		appLogs: func(_ context.Context, _, _ string, opts client.LogOptions) ([]client.LogEntry, error) {
			if opts.PodName != "pod-b" || opts.Container != "sidecar" {
				t.Errorf("cycled target = %s/%s", opts.PodName, opts.Container)
			}
			return nil, nil
		},
	}
	cmd := m.cycleLogPod(1)
	if cmd == nil || m.logPod != "pod-b" || m.logContainer != "sidecar" {
		t.Fatalf("cycled target = %s/%s; cmd is nil: %t", m.logPod, m.logContainer, cmd == nil)
	}
	if _, ok := cmd().(logsResult); !ok {
		t.Fatal("pod cycle returned an unexpected message")
	}
}

func TestAdjustLogTailRequestsUpdatedEntryCount(t *testing.T) {
	t.Parallel()
	var requested int
	m := &browser{
		tenant: "tenant", detail: true, apps: []client.App{{ID: "app"}},
		logPod: "pod", logContainer: "main",
		appLogs: func(_ context.Context, _, _ string, opts client.LogOptions) ([]client.LogEntry, error) {
			requested = opts.Tail
			return nil, nil
		},
	}
	cmd := m.adjustLogTail(logTailStep)
	if cmd == nil || m.logTail != logDefaultTailSize+logTailStep {
		t.Fatalf("tail size = %d; cmd is nil: %t", m.logTail, cmd == nil)
	}
	if _, ok := cmd().(logsResult); !ok || requested != logDefaultTailSize+logTailStep {
		t.Fatalf("request tail = %d, want %d", requested, logDefaultTailSize+logTailStep)
	}
}

func TestLogClipboardTextFormatsEntriesAndSeparators(t *testing.T) {
	entries := []client.LogEntry{{
		Timestamp: "2026-11-19T04:40:59Z",
		Text:      `{"log":{"level":"INFO","message":"first\nsecond\tcolumn"}}`,
	}}
	got := logClipboardText(entries, true)
	if !strings.Contains(got, "INFO") || !strings.Contains(got, "first\n") || !strings.Contains(got, "second    column") {
		t.Fatalf("clipboard text = %q", got)
	}
}
