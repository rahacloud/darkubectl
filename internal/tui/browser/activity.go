package browser

import (
	"errors"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	alertColumnWidthsValue10 = 10
	alertColumnWidthsValue12 = 12
	alertColumnWidthsValue2  = 2
)

const (
	activityFilterLineValue2        = 2
	alertColumnWidthsValue11        = 11
	alertColumnWidthsValue3         = 3
	alertColumnWidthsValue42        = 42
	alertColumnWidthsValue54        = 54
	activityAlertCompactWidth       = 5
	activityAlertFallbackWidth      = 8
	alertPaneLinesValue2            = 2
	alertSeverityRankValue2         = 2
	boundedActivityTextValue4096    = 4096
	cycleActivityStatusFilterValue2 = 2
	cycleActivityStatusFilterValue3 = 3
	handleActivityKeyValue2         = 2
	handleActivityKeyValue3         = 3
	handleActivityKeyValue8         = 8
	notificationPaneLinesValue2     = 2
)

var activityHTMLTag = regexp.MustCompile(`<[^>]*>`)
var activityUnsafeHTML = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)

var activitySortNames = [...]string{"Started", "Severity", "Status", "Name"}
var notificationSortNames = [...]string{"Time", "Read status", "Title"}

func (m *browser) loadActivityFeed(force bool) tea.Cmd {
	if m.page != pageActivity {
		return nil
	}
	if m.activityTab == activityAlerts {
		if !force && (m.activityAlertsState.active() || m.activityAlertsState.status == requestReady ||
			m.activityAlertsState.status == requestEmpty || m.activityAlertsState.status == requestUnavailable) {
			return nil
		}
		meta, ctx := m.startRequest(requestAlerts, m.tenant, "", m.tenantGeneration)
		m.activityAlertsState.begin(meta, m.requestTime())
		if m.alerts == nil {
			m.finishRequest(requestAlerts, meta)
			m.activityAlertsState.unavailable(errAlertsAreUnavailable)
			return nil
		}
		return func() tea.Msg {
			alerts, err := m.alerts(ctx, meta.tenant)
			return alertsResult{meta: meta, alerts: alerts, err: err}
		}
	}
	if !force && (m.activityNotificationsState.active() || m.activityNotificationsState.status == requestReady ||
		m.activityNotificationsState.status == requestEmpty || m.activityNotificationsState.status == requestUnavailable) {
		return nil
	}
	meta, ctx := m.startRequest(requestNotifications, "", "", 0)
	m.activityNotificationsState.begin(meta, m.requestTime())
	if m.notifications == nil {
		m.finishRequest(requestNotifications, meta)
		m.activityNotificationsState.unavailable(errNotificationsAreUnavailable)
		return nil
	}
	return func() tea.Msg {
		items, err := m.notifications(ctx)
		return notificationsResult{meta: meta, notifications: items, err: err}
	}
}

func (m *browser) handleAlertsResult(msg alertsResult) tea.Model {
	m.finishRequest(requestAlerts, msg.meta)
	if !m.activityAlertsState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m
	}
	if msg.err != nil {
		m.activityAlertsState.fail(msg.err)
		return m
	}
	selected := m.activityAlertSelected
	m.activityAlertsData = append([]client.Alert(nil), msg.alerts...)
	m.sortActivityAlerts()
	m.activityAlertsState.succeed(len(msg.alerts) == 0, m.requestTime())
	match, matches := 0, 0
	for i := range m.activityAlertsData {
		if selected != "" && m.activityAlertsData[i].ID == selected {
			match, matches = i, matches+1
		}
	}
	if matches == 1 {
		m.activityAlertCursor = match
	} else {
		m.activityAlertCursor = min(m.activityAlertCursor, max(0, len(m.activityAlertsData)-1))
	}
	m.updateActivityAlertSelection()
	return m
}

func (m *browser) handleNotificationsResult(msg notificationsResult) tea.Model {
	m.finishRequest(requestNotifications, msg.meta)
	if !m.activityNotificationsState.matches(msg.meta) || msg.meta.tenant != "" || msg.meta.scopeGeneration != 0 {
		return m
	}
	if msg.err != nil {
		m.activityNotificationsState.fail(msg.err)
		return m
	}
	selected := m.activityNotificationSelected
	m.activityNotificationsData = append([]client.Notification(nil), msg.notifications...)
	m.sortActivityNotifications()
	m.activityNotificationsState.succeed(len(msg.notifications) == 0, m.requestTime())
	match, matches := 0, 0
	for i := range m.activityNotificationsData {
		if m.activityNotificationsData[i].Slug == selected && selected != 0 {
			match, matches = i, matches+1
		}
	}
	if matches == 1 {
		m.activityNotificationCursor = match
	} else {
		m.activityNotificationCursor = min(m.activityNotificationCursor, max(0, len(m.activityNotificationsData)-1))
	}
	m.updateActivityNotificationSelection()
	return m
}

func (m *browser) switchActivityTab(delta int) tea.Cmd {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		m.activityTab = activityNotifications
	} else {
		m.activityTab = activityAlerts
	}
	m.detail = false
	m.activityScroll = 0
	return m.loadActivityFeed(false)
}

func (m *browser) activityFilter() string {
	if m.activityTab == activityAlerts {
		return m.activityAlertFilter
	}
	return m.activityNotificationFilter
}

func (m *browser) setActivityFilter(value string) {
	if m.activityTab == activityAlerts {
		m.activityAlertFilter = value
		m.activityAlertCursor = 0
		m.updateActivityAlertSelection()
	} else {
		m.activityNotificationFilter = value
		m.activityNotificationCursor = 0
		m.updateActivityNotificationSelection()
	}
}

func (m *browser) handleActivityFilterKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case keyEnter:
		m.activityFilterEditing = false
	case browserKeyEscape:
		m.activityFilterEditing = false
		m.setActivityFilter(m.activityFilterBefore)
	case keyCtrlU:
		m.setActivityFilter("")
	case keyBackspace, keyCtrlH:
		value := []rune(m.activityFilter())
		if len(value) > 0 {
			m.setActivityFilter(string(value[:len(value)-1]))
		}
	default:
		if msg.Text != "" {
			m.setActivityFilter(m.activityFilter() + safeText(msg.Text))
		}
	}
}

func (m *browser) handleActivityKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if model, cmd, handled := m.handleActivityCommandKey(msg.String()); handled {
		return model, cmd
	}
	if handled := m.handleActivityNavigationKey(msg.String()); handled {
		return m, nil
	}
	switch msg.String() {
	case browserKeyEscape:
		if m.detail {
			m.detail = false
			m.activityDetailScroll = 0
		} else {
			m.page = pageApps
			m.detail = false
		}
		return m, nil
	case keyEnter:
		m.detail = !m.detail
		m.activityDetailScroll = 0
		return m, nil
	}
	return m, nil
}

func (m *browser) handleActivityNavigationKey(key string) bool {
	switch key {
	case "up", "k":
		if m.detail {
			m.activityDetailScroll = max(0, m.activityDetailScroll-1)
		} else {
			m.moveActivitySelection(-1)
		}
	case keyDown, "j":
		if m.detail {
			m.activityDetailScroll++
		} else {
			m.moveActivitySelection(1)
		}
	case inputKeyHome, "g":
		m.activityMoveToStart()
	case inputKeyEnd, "G":
		m.activityMoveToEnd()
	case inputKeyPgUp:
		m.activityMovePage(-1)
	case inputKeyPgDown:
		m.activityMovePage(1)
	default:
		return false
	}
	return true
}

func (m *browser) activityMoveToStart() {
	switch {
	case m.detail:
		m.activityDetailScroll = 0
	case m.activityTab == activityAlerts:
		m.activityAlertCursor = 0
		m.updateActivityAlertSelection()
	default:
		m.activityNotificationCursor = 0
		m.updateActivityNotificationSelection()
	}
}

func (m *browser) activityMoveToEnd() {
	switch {
	case m.detail:
		m.activityDetailScroll = len(m.activityDetails())
	case m.activityTab == activityAlerts:
		m.activityAlertCursor = max(0, len(m.activityAlertsVisible())-1)
		m.updateActivityAlertSelection()
	default:
		m.activityNotificationCursor = max(0, len(m.activityNotificationsVisible())-1)
		m.updateActivityNotificationSelection()
	}
}

func (m *browser) activityMovePage(direction int) {
	pageSize := max(1, m.height-handleActivityKeyValue8)
	if m.detail {
		m.activityDetailScroll = max(0, m.activityDetailScroll+direction*pageSize)
		return
	}
	m.moveActivitySelection(direction * pageSize)
}

func (m *browser) handleActivityCommandKey(key string) (tea.Model, tea.Cmd, bool) {
	switch key {
	case "1", "2", "3", "4":
		return m, m.focusPane(int(key[0] - '1')), true
	case inputKeyTab:
		return m, m.cycleFocus(false), true
	case inputKeyShiftTab, inputKeyBacktab:
		return m, m.cycleFocus(true), true
	case inputKeyCtrlC, "q":
		return m, tea.Quit, true
	case "?":
		m.help = true
	case keyLeft, "h":
		return m, m.switchActivityTab(-1), true
	case keyRight, "l":
		return m, m.switchActivityTab(1), true
	case "f":
		m.cycleActivityStatusFilter()
	case "s":
		m.sorting = true
		_, sortBy, descending := m.activitySortOptions()
		m.sortChoice, m.sortDescending = int(sortBy), descending
	case "/":
		m.activityFilterBefore = m.activityFilter()
		m.activityFilterEditing = true
	case "r":
		return m, m.loadActivityFeed(true), true
	default:
		return nil, nil, false
	}
	return m, nil, true
}

func (m *browser) cycleActivityStatusFilter() {
	if m.activityTab == activityAlerts {
		m.activityAlertStatusFilter = (m.activityAlertStatusFilter + 1) % cycleActivityStatusFilterValue3
		m.activityAlertCursor = 0
		m.updateActivityAlertSelection()
		return
	}
	m.activityNotificationUnreadFilter = (m.activityNotificationUnreadFilter + 1) % cycleActivityStatusFilterValue2
	m.activityNotificationCursor = 0
	m.updateActivityNotificationSelection()
}

func (m *browser) activityAlertsVisible() []client.Alert {
	out := make([]client.Alert, 0, len(m.activityAlertsData))
	query := strings.TrimSpace(m.activityAlertFilter)
	for _, a := range m.activityAlertsData {
		if m.activityAlertStatusFilter == 1 && !alertIsKnownFiring(a.Status) || m.activityAlertStatusFilter == 2 &&
			!alertIsKnownResolved(a.Status) {
			continue
		}
		if query != "" && !fuzzyMatch(strings.Join([]string{a.AlertName, a.Status, a.Severity, a.Instance, a.Condition}, " "), query) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (m *browser) activityNotificationsVisible() []client.Notification {
	out := make([]client.Notification, 0, len(m.activityNotificationsData))
	query := strings.TrimSpace(m.activityNotificationFilter)
	for _, n := range m.activityNotificationsData {
		if m.activityNotificationUnreadFilter == 1 && !n.Unread {
			continue
		}
		if query != "" && !fuzzyMatch(strings.Join([]string{n.Title, n.TargetType, n.Description}, " "), query) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (m *browser) sortActivityAlerts() {
	sort.SliceStable(m.activityAlertsData, func(i, j int) bool {
		a, b := m.activityAlertsData[i], m.activityAlertsData[j]
		var cmp int
		knownA, knownB := true, true
		sortBy, desc := m.activitySortAlerts, m.activitySortAlertsDescending
		switch sortBy {
		case activitySortSeverity:
			ra, rb := alertSeverityRank(a.Severity), alertSeverityRank(b.Severity)
			knownA, knownB = ra >= 0, rb >= 0
			cmp = compareKnown(ra, rb)
		case activitySortStatus:
			ra, rb := alertStatusRank(a.Status), alertStatusRank(b.Status)
			knownA, knownB = ra >= 0, rb >= 0
		case activitySortName:
			cmp = strings.Compare(strings.ToLower(a.AlertName), strings.ToLower(b.AlertName))
		default:
			cmp = compareTimes(a.StartsAt, b.StartsAt)
			knownA, knownB = activityTimeKnown(a.StartsAt), activityTimeKnown(b.StartsAt)
		}
		if knownA != knownB {
			return knownA
		}
		if desc && knownA {
			cmp = -cmp
		}
		if cmp == 0 {
			cmp = strings.Compare(a.ID, b.ID)
		}
		return cmp < 0
	})
}

func (m *browser) sortActivityNotifications() {
	sort.SliceStable(m.activityNotificationsData, func(i, j int) bool {
		a, b := m.activityNotificationsData[i], m.activityNotificationsData[j]
		var cmp int
		knownA, knownB := true, true
		switch m.activitySortNotifications {
		case activitySortSeverity:
			cmp = boolCompare(a.Unread, b.Unread)
		case activitySortStatus:
			cmp = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		default:
			cmp = compareTimes(a.Timestamp, b.Timestamp)
			knownA, knownB = activityTimeKnown(a.Timestamp), activityTimeKnown(b.Timestamp)
		}
		if knownA != knownB {
			return knownA
		}
		if m.activitySortNotificationsDescending && knownA {
			cmp = -cmp
		}
		if cmp == 0 {
			cmp = a.Slug - b.Slug
		}
		return cmp < 0
	})
}

func compareTimes(a, b string) int {
	ta, ea := time.Parse(time.RFC3339Nano, a)
	tb, eb := time.Parse(time.RFC3339Nano, b)
	if ea != nil || eb != nil {
		if !errors.Is(ea, eb) {
			if ea == nil {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	}
	if ta.Before(tb) {
		return -1
	}
	if ta.After(tb) {
		return 1
	}
	return 0
}

func compareKnown(a, b int) int {
	if a < 0 || b < 0 {
		if a < 0 && b >= 0 {
			return 1
		}
		if b < 0 && a >= 0 {
			return -1
		}
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func boolCompare(a, b bool) int {
	if a == b {
		return 0
	}
	if a {
		return 1
	}
	return -1
}

func alertSeverityRank(value string) int {
	switch strings.ToLower(value) {
	case statusCritical:
		return 0
	case statusWarning:
		return 1
	case statusInfo:
		return alertSeverityRankValue2
	default:
		return -1
	}
}

func alertStatusRank(value string) int {
	switch strings.ToLower(value) {
	case statusFiring:
		return 0
	case statusResolved:
		return 1
	default:
		return -1
	}
}

func alertIsKnownFiring(value string) bool   { return alertStatusRank(value) == 0 }
func alertIsKnownResolved(value string) bool { return alertStatusRank(value) == 1 }

func alertStatusCell(value string) string {
	switch strings.ToLower(value) {
	case statusFiring:
		return paint("● Firing", browserDanger, true)
	case statusResolved:
		return paint("✓ Resolved", browserSuccess, true)
	case "":
		return paint("? Unknown", browserMuted, false)
	default:
		return paint("? "+safeText(value), browserMuted, false)
	}
}

func alertSeverityCell(value string) string {
	switch strings.ToLower(value) {
	case statusCritical:
		return paint("✖ Critical", browserDanger, true)
	case "warning":
		return paint("▲ Warning", browserNumber, true)
	case "info":
		return paint("◆ Info", browserAccent, true)
	case "":
		return paint("? Unknown", browserMuted, false)
	default:
		return paint("? "+safeText(value), browserMuted, false)
	}
}

func (m *browser) updateActivityAlertSelection() {
	rows := m.activityAlertsVisible()
	if len(rows) == 0 {
		m.activityAlertSelected = ""
		m.activityAlertCursor = 0
		return
	}
	m.activityAlertCursor = min(max(0, m.activityAlertCursor), len(rows)-1)
	m.activityAlertSelected = rows[m.activityAlertCursor].ID
}

func (m *browser) updateActivityNotificationSelection() {
	rows := m.activityNotificationsVisible()
	if len(rows) == 0 {
		m.activityNotificationSelected = 0
		m.activityNotificationCursor = 0
		return
	}
	m.activityNotificationCursor = min(max(0, m.activityNotificationCursor), len(rows)-1)
	m.activityNotificationSelected = rows[m.activityNotificationCursor].Slug
}

func (m *browser) moveActivitySelection(delta int) {
	if m.activityTab == activityAlerts {
		rows := m.activityAlertsVisible()
		if len(rows) == 0 {
			return
		}
		m.activityAlertCursor = min(max(0, m.activityAlertCursor+delta), len(rows)-1)
		m.activityAlertSelected = rows[m.activityAlertCursor].ID
	} else {
		rows := m.activityNotificationsVisible()
		if len(rows) == 0 {
			return
		}
		m.activityNotificationCursor = min(max(0, m.activityNotificationCursor+delta), len(rows)-1)
		m.activityNotificationSelected = rows[m.activityNotificationCursor].Slug
	}
}

func (m *browser) activitySortOptions() ([]string, activitySort, bool) {
	if m.activityTab == activityAlerts {
		return activitySortNames[:], m.activitySortAlerts, m.activitySortAlertsDescending
	}
	return notificationSortNames[:], m.activitySortNotifications, m.activitySortNotificationsDescending
}

func (m *browser) applyActivitySort(choice int, descending bool) {
	if m.activityTab == activityAlerts {
		selected := m.activityAlertSelected
		m.activitySortAlerts = activitySort(choice)
		m.activitySortAlertsDescending = descending
		m.sortActivityAlerts()
		for i, a := range m.activityAlertsVisible() {
			if selected != "" && a.ID == selected {
				m.activityAlertCursor = i
				break
			}
		}
		m.updateActivityAlertSelection()
	} else {
		selected := m.activityNotificationSelected
		m.activitySortNotifications = activitySort(choice)
		m.activitySortNotificationsDescending = descending
		m.sortActivityNotifications()
		for i, n := range m.activityNotificationsVisible() {
			if selected != 0 && n.Slug == selected {
				m.activityNotificationCursor = i
				break
			}
		}
		m.updateActivityNotificationSelection()
	}
}

func (m *browser) setActivitySortDirection(descending bool) {
	if m.activityTab == activityAlerts {
		m.activitySortAlertsDescending = descending
	} else {
		m.activitySortNotificationsDescending = descending
	}
}

func (m *browser) activityPaneLines(height int) []string {
	if m.activityTab == activityAlerts {
		return m.alertPaneLines(height)
	}
	return m.notificationPaneLines(height)
}

func (m *browser) alertPaneLines(height int) []string {
	width, _, _ := m.paneWidths()
	statusWidth, severityWidth, startedWidth, compact := alertColumnWidths(width)
	statusHeading, severityHeading, startedHeading := "STATUS", "SEVERITY", "STARTED"
	alertHeading := "ALERT"
	if compact {
		statusHeading, severityHeading, startedHeading = "ST", "S", "TIME"
		alertHeading = "NAME"
	}
	heading := padActivityCell(statusHeading, statusWidth) + " " + padActivityCell(severityHeading,
		severityWidth) + " " + padActivityCell(startedHeading, startedWidth) + " " + alertHeading
	lines := []string{paint(activityWidth(heading, width), browserMuted, true)}
	stateLines := m.activityStateLines(m.activityAlertsState, "Alerts")
	lines = append(lines, stateLines...)
	rows := m.activityAlertsVisible()
	if len(rows) == 0 && !m.activityAlertsState.active() && m.activityAlertsState.err == nil {
		msg := "No alerts"
		switch m.activityAlertStatusFilter {
		case 1:
			msg = "No firing alerts"
		case alertPaneLinesValue2:
			msg = "No resolved alerts"
		}
		lines = append(lines, paint(msg, browserMuted, false))
	}
	for i, a := range rows {
		status, severity := alertStatusCell(a.Status), alertSeverityCell(a.Severity)
		if compact {
			status = alertStatusGlyph(a.Status) + " " + strings.ToUpper(string([]rune(alertStatusShort(a.Status))[0]))
			severity = alertSeverityGlyph(a.Severity)
		}
		started := shortActivityTime(a.StartsAt)
		if compact {
			started = shortActivityClock(a.StartsAt)
		}
		label := padActivityCell(status, statusWidth) + " " + padActivityCell(severity, severityWidth) + " " + padActivityCell(started,
			startedWidth) + " " + safeText(a.AlertName)
		label = activityWidth(label, width)
		if i == m.activityAlertCursor {
			label = selectedRowStyle().Render(label)
		}
		lines = append(lines, label)
	}
	selectedRow := -1
	if len(rows) > 0 {
		selectedRow = 1 + len(stateLines) + m.activityAlertCursor
	}
	return activityListViewport(lines, height, &m.activityScroll, selectedRow)
}

func alertColumnWidths(width int) (int, int, int, bool) {
	switch {
	case width >= alertColumnWidthsValue54:
		return alertColumnWidthsValue11, alertColumnWidthsValue12, alertColumnWidthsValue12, false
	case width >= alertColumnWidthsValue42:
		return alertColumnWidthsValue10, alertColumnWidthsValue10, activityAlertFallbackWidth, false
	default:
		return alertColumnWidthsValue3, alertColumnWidthsValue2, activityAlertCompactWidth, true
	}
}

func padActivityCell(value string, width int) string {
	value = ansi.Truncate(value, max(0, width), "…")
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}

func alertStatusGlyph(value string) string {
	switch strings.ToLower(value) {
	case "firing":
		return paint("●", browserDanger, true)
	case "resolved":
		return paint("✓", browserSuccess, true)
	default:
		return paint("?", browserMuted, false)
	}
}

func alertStatusShort(value string) string {
	switch strings.ToLower(value) {
	case "firing":
		return "Firing"
	case "resolved":
		return "Resolved"
	default:
		return logUnknownState
	}
}

func shortActivityClock(raw string) string {
	if raw == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return safeText(raw)
	}
	return formatLocalWallTime(t, "15:04")
}

func alertSeverityGlyph(value string) string {
	switch strings.ToLower(value) {
	case statusCritical:
		return paint("✖", browserDanger, true)
	case "warning":
		return paint("▲", browserNumber, true)
	case "info":
		return paint("◆", browserAccent, true)
	default:
		return paint("?", browserMuted, false)
	}
}

func (m *browser) notificationPaneLines(height int) []string {
	lines := []string{paint("ACCOUNT-WIDE · LATEST 50", browserMuted, false), paint("STATUS       TIME          TITLE", browserMuted, true)}
	stateLines := m.activityStateLines(m.activityNotificationsState, "Notifications")
	lines = append(lines, stateLines...)
	rows := m.activityNotificationsVisible()
	if len(rows) == 0 && !m.activityNotificationsState.active() && m.activityNotificationsState.err == nil {
		msg := "No notifications"
		if m.activityNotificationUnreadFilter == 1 {
			msg = "No unread notifications"
		}
		lines = append(lines, paint(msg, browserMuted, false))
	}
	for i, n := range rows {
		status := paint("✓ Read", browserMuted, false)
		if n.Unread {
			status = paint("✉ Unread", browserAccent, true)
		}
		label := status + "  " + shortActivityTime(n.Timestamp) + "  " + safeText(n.Title)
		if i == m.activityNotificationCursor {
			width, _, _ := m.paneWidths()
			label = selectedRowStyle().Render(activityWidth(label, width))
		}
		lines = append(lines, label)
	}
	selectedRow := -1
	if len(rows) > 0 {
		selectedRow = notificationPaneLinesValue2 + len(stateLines) + m.activityNotificationCursor
	}
	return activityListViewport(lines, height, &m.activityScroll, selectedRow)
}

func (m *browser) activityStateLines(state requestState, name string) []string {
	var lines []string
	if state.active() {
		lines = append(lines, m.loadingMessage("Loading "+strings.ToLower(name)+"…"))
	}
	switch state.status {
	case requestStale:
		lines = append(lines, paint("Stale · refresh failed: "+safeText(state.err.Error()), browserNumber, false), staleRequestTime(name, state))
	case requestFailed:
		lines = append(lines, paint("Could not load "+strings.ToLower(name)+": "+safeText(state.err.Error()), browserDanger, false))
	case requestUnavailable:
		lines = append(lines, paint(name+" unavailable", browserMuted, false))
	default:
		return lines
	}
	return lines
}

func activityViewport(lines []string, height int, scroll *int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		*scroll = 0
		return lines
	}
	*scroll = min(max(0, *scroll), len(lines)-height)
	return lines[*scroll : *scroll+height]
}

func activityListViewport(lines []string, height int, scroll *int, selectedRow int) []string {
	if height <= 0 {
		return nil
	}
	if selectedRow >= 0 {
		if selectedRow < *scroll {
			*scroll = selectedRow
		} else if selectedRow >= *scroll+height {
			*scroll = selectedRow - height + 1
		}
	}
	return activityViewport(lines, height, scroll)
}

func shortActivityTime(raw string) string {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return safeText(raw)
	}
	return formatLocalWallTime(t, "Jan 02 15:04")
}

func stripActivityHTML(raw string) string {
	raw = activityUnsafeHTML.ReplaceAllString(raw, " ")
	return safeText(strings.TrimSpace(html.UnescapeString(activityHTMLTag.ReplaceAllString(raw, " "))))
}

func (m *browser) activityDetails() []string {
	if m.activityTab == activityAlerts {
		rows := m.activityAlertsVisible()
		if len(rows) == 0 || m.activityAlertCursor >= len(rows) {
			return []string{paint("Select an alert to inspect details.", browserMuted, false)}
		}
		a := rows[m.activityAlertCursor]
		lines := []string{paint("⚠ "+safeText(a.AlertName), browserAccent, true), summaryField("Status", alertStatusCell(a.Status)),
			summaryField("Severity", alertSeverityCell(a.Severity)), summaryField("Instance", dashActivity(a.Instance)),
			summaryField("Owner", dashActivity(a.ServiceOwner)), summaryField("Condition", dashActivity(a.Condition)),
			summaryField("Started", dashActivity(shortActivityTime(a.StartsAt))), summaryField("Ended",
				dashActivity(shortActivityTime(a.EndsAt))), summaryField("Window", dashActivity(a.TimeWindow))}
		description := a.AlertDescription
		if description == "" {
			description = a.DescriptionFA
		}
		if description != "" {
			lines = append(lines, "", sectionHeading("Description"), boundedActivityText(description))
		}
		return lines
	}
	rows := m.activityNotificationsVisible()
	if len(rows) == 0 || m.activityNotificationCursor >= len(rows) {
		return []string{paint("Select a notification to read its message.", browserMuted, false)}
	}
	n := rows[m.activityNotificationCursor]
	status := "Read"
	if n.Unread {
		status = "Unread"
	}
	lines := []string{summaryField("Status", status), summaryField("Time", shortActivityTime(n.Timestamp)), summaryField("Type",
		dashActivity(n.TargetType)), summaryField("Target", dashActivity(n.ActionLabel)), "", sectionHeading("Message"),
		safeText(n.Title), boundedActivityText(n.Description)}
	return lines
}

func dashActivity(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return safeText(value)
}

func (m *browser) activitySortLine() string {
	names, choice, desc := m.activitySortOptions()
	if m.sorting {
		choice = activitySort(m.sortChoice)
	}
	direction := "↑"
	if desc {
		direction = "↓"
	}
	return paint("⇅", browserMuted, false) + " " + paint(names[int(choice)], browserBranch, true) + " " + paint(direction, browserNumber, true)
}

func (m *browser) activityFilterLine() string {
	filter := m.currentActivityFilter()
	label := activityStatusFilterLabel(m.activityTab, filter)
	indicator, color := "◉", browserNumber
	if m.activityTab == activityAlerts {
		switch filter {
		case 1:
			indicator, color = "●", browserDanger
		case activityFilterLineValue2:
			indicator, color = "✓", browserSuccess
		}
	} else if filter == 1 {
		indicator, color = "✉", browserAccent
	}
	text := paint(indicator, color, true) + " " + paint(label, color, true)
	if query := m.activityFilter(); query != "" || m.activityFilterEditing {
		text += "  " + paint("⌕", browserMuted, false) + " " + paint(query, browserBranch, true)
		if m.activityFilterEditing {
			text += paint("▏", browserNumber, true)
		}
	}
	return text
}

func (m *browser) currentActivityFilter() int {
	if m.activityTab == activityAlerts {
		return m.activityAlertStatusFilter
	}
	return m.activityNotificationUnreadFilter
}

func (m *browser) activityDetailLines(height int) []string {
	lines := m.activityDetails()
	if len(lines) <= height {
		m.activityDetailScroll = 0
		return lines
	}
	sticky := min(1, len(lines))
	body := max(0, height-sticky)
	maxScroll := max(0, len(lines)-height)
	m.activityDetailScroll = min(max(0, m.activityDetailScroll), maxScroll)
	end := min(len(lines), sticky+m.activityDetailScroll+body)
	return append(append([]string(nil), lines[:sticky]...), lines[sticky+m.activityDetailScroll:end]...)
}

func activityStatusFilterLabel(tab activityTab, filter int) string {
	if tab == activityAlerts {
		labels := []string{labelAll, "Firing", "Resolved"}
		return labels[min(max(filter, 0), len(labels)-1)]
	}
	return []string{"All", "Unread"}[min(max(filter, 0), 1)]
}

func boundedActivityText(s string) string {
	runes := []rune(s)
	if len(runes) > boundedActivityTextValue4096 {
		s = string(runes[:4096]) + "…"
	}
	return stripActivityHTML(s)
}

func activityTimeKnown(s string) bool { _, err := time.Parse(time.RFC3339Nano, s); return err == nil }

func activityWidth(value string, width int) string { return ansi.Truncate(value, max(1, width), "…") }
