package browser

import (
	"fmt"
	"image/color"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

type databasesResult struct {
	meta      requestMeta
	databases []client.Database
	err       error
}

func (m *browser) loadDatabases(force bool) tea.Cmd {
	if m.page != pageDB || (!force && (m.databaseState.active() || m.databaseState.status == requestReady || m.databaseState.status == requestEmpty || m.databaseState.status == requestUnavailable)) {
		return nil
	}
	meta, ctx := m.startRequest(requestDatabases, m.tenant, "", m.tenantGeneration)
	m.databaseState.begin(meta, m.requestTime())
	if m.listDatabases == nil {
		m.finishRequest(requestDatabases, meta)
		m.databaseState.unavailable(fmt.Errorf("database listing is unavailable"))
		return nil
	}
	return func() tea.Msg {
		databases, err := m.listDatabases(ctx, meta.tenant)
		return databasesResult{meta: meta, databases: databases, err: err}
	}
}

func (m *browser) handleDatabasesResult(msg databasesResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestDatabases, msg.meta)
	if !m.databaseState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.databaseState.fail(msg.err)
		return m, nil
	}
	selected := m.databaseSelected
	if selected == "" && len(m.visibleDatabases()) > 0 {
		previous := m.visibleDatabases()
		selected = m.databaseIdentity(previous[min(m.databaseCursor, len(previous)-1)])
	}
	m.databases = append([]client.Database(nil), msg.databases...)
	m.databaseState.succeed(len(msg.databases) == 0, m.requestTime())
	m.sortDatabases()
	m.databaseCursor = 0
	visible := m.visibleDatabases()
	for i := range visible {
		if selected != "" && m.databaseIdentity(visible[i]) == selected {
			m.databaseCursor = i
			break
		}
	}
	return m, m.updateDatabaseSelection()
}

func (m *browser) loadSelectedDatabaseDetail(force bool) tea.Cmd {
	db := m.selectedDatabase()
	if m.page != pageDB || db == nil || db.ID == "" {
		return nil
	}
	if m.databaseDetailTarget == db.ID {
		if m.databaseDetailState.active() && !force {
			return nil
		}
		if !force && (m.databaseDetailState.status == requestReady || m.databaseDetailState.status == requestUnavailable) {
			return nil
		}
	}
	if m.databaseDetailTarget != db.ID {
		m.resetDatabaseDetail()
	}
	if m.getDatabase == nil {
		m.databaseDetailTarget = db.ID
		m.databaseDetailState.unavailable(fmt.Errorf("database connection details are unavailable"))
		return nil
	}
	meta, ctx := m.startRequest(requestDatabaseDetail, m.tenant, db.ID, m.tenantGeneration)
	m.databaseDetailTarget = db.ID
	m.databaseDetailState.begin(meta, m.requestTime())
	m.databaseNotice = ""
	return func() tea.Msg {
		detail, err := m.getDatabase(ctx, meta.tenant, meta.appID)
		return databaseDetailResult{meta: meta, detail: detail, err: err}
	}
}

func (m *browser) handleDatabaseDetailResult(msg databaseDetailResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestDatabaseDetail, msg.meta)
	db := m.selectedDatabase()
	if !m.databaseDetailState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration || db == nil || db.ID != msg.meta.appID {
		return m, nil
	}
	if msg.err != nil || msg.detail == nil {
		if msg.err == nil {
			msg.err = fmt.Errorf("empty database detail response")
		}
		m.databaseDetailState.fail(msg.err)
		m.databaseDetail = nil
		return m, nil
	}
	m.databaseDetail = msg.detail
	m.databaseDetailState.succeed(false, m.requestTime())
	return m, nil
}

func (m *browser) cancelDatabaseDetailRequests() {
	for key, active := range m.inFlight {
		if key.kind == requestDatabaseDetail {
			active.cancel()
			delete(m.inFlight, key)
		}
	}
}

func (m *browser) resetDatabaseDetail() {
	m.cancelDatabaseDetailRequests()
	m.databaseDetail = nil
	m.databaseDetailState.reset()
	m.databaseDetailTarget = ""
	m.databaseConnectionRole = dbConnectionMaster
	m.databaseNotice = ""
}

func (m *browser) visibleDatabases() []client.Database {
	rows := make([]client.Database, 0, len(m.databases))
	for _, db := range m.databases {
		if fuzzyMatch(strings.Join([]string{db.Name, db.Engine, db.Version, db.Status.Status}, " "), m.databaseFilter) {
			rows = append(rows, db)
		}
	}
	return rows
}

func (m *browser) sortDatabases() {
	lessValue := func(a, b client.Database) int {
		var left, right string
		switch m.databaseSortBy {
		case dbSortEngine:
			left, right = strings.TrimSpace(a.Engine+" "+a.Version), strings.TrimSpace(b.Engine+" "+b.Version)
		case dbSortStatus:
			left, right = a.Status.Status, b.Status.Status
		default:
			left, right = a.Name, b.Name
		}
		left, right = strings.ToLower(left), strings.ToLower(right)
		if left == "" && right != "" {
			return 1
		}
		if right == "" && left != "" {
			return -1
		}
		if left < right {
			return -1
		}
		if left > right {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	}
	sort.SliceStable(m.databases, func(i, j int) bool {
		cmp := lessValue(m.databases[i], m.databases[j])
		if m.databaseSortDescending && cmp != 0 && strings.TrimSpace(databaseSortValue(m.databases[i], m.databaseSortBy)) != "" && strings.TrimSpace(databaseSortValue(m.databases[j], m.databaseSortBy)) != "" {
			return cmp > 0
		}
		return cmp < 0
	})
}

func databaseSortValue(db client.Database, by dbSort) string {
	switch by {
	case dbSortEngine:
		return strings.TrimSpace(db.Engine + " " + db.Version)
	case dbSortStatus:
		return db.Status.Status
	default:
		return db.Name
	}
}

func (m *browser) moveDatabaseSelection(delta int) tea.Cmd {
	rows := m.visibleDatabases()
	if len(rows) == 0 {
		m.databaseCursor = 0
		return m.updateDatabaseSelection()
	}
	m.databaseCursor = min(max(0, m.databaseCursor+delta), len(rows)-1)
	return m.updateDatabaseSelection()
}

func (m *browser) updateDatabaseSelection() tea.Cmd {
	previous := m.databaseSelected
	rows := m.visibleDatabases()
	if len(rows) == 0 {
		m.databaseCursor = 0
		m.databaseSelected = ""
		if previous != "" {
			m.resetDatabaseDetail()
		}
		return nil
	}
	m.databaseCursor = min(max(0, m.databaseCursor), len(rows)-1)
	m.databaseSelected = m.databaseIdentity(rows[m.databaseCursor])
	if previous != m.databaseSelected {
		m.resetDatabaseDetail()
		return m.loadSelectedDatabaseDetail(false)
	}
	return nil
}

func (m *browser) databaseIdentity(db client.Database) string {
	if db.ID != "" {
		count := 0
		for _, row := range m.databases {
			if row.ID == db.ID {
				count++
			}
		}
		if count == 1 {
			return "id:" + db.ID
		}
	}
	return fmt.Sprintf("fields:%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%s", db.Name, db.Engine, db.Version, db.Plan, db.Status.Role, db.NamespaceID, db.ClusterID, db.Status.Status, db.Status.Detail)
}

func (m *browser) selectedDatabase() *client.Database {
	for i := range m.databases {
		if m.databaseSelected != "" && m.databaseIdentity(m.databases[i]) == m.databaseSelected {
			return &m.databases[i]
		}
	}
	rows := m.visibleDatabases()
	if len(rows) == 0 {
		return nil
	}
	return &rows[min(m.databaseCursor, len(rows)-1)]
}

func (m *browser) databasePaneLines(height int) []string {
	rows := m.visibleDatabases()
	lines := []string{}
	if m.databaseState.active() && len(m.databases) == 0 {
		lines = append(lines, m.loadingMessage("Managed databases"))
	} else if m.databaseState.err != nil && len(m.databases) == 0 {
		lines = append(lines, paint("Could not load databases: "+safeText(m.databaseState.err.Error()), browserDanger, false), paint("Press r to retry", browserMuted, false))
	} else if m.databaseState.status == requestUnavailable {
		lines = append(lines, paint("Database listing is unavailable in this mode.", browserMuted, false))
	} else if len(m.databases) == 0 {
		label := "No managed databases in this tenant."
		if strings.HasPrefix(m.mode, "MOCK") {
			label = "No managed databases in this mock profile."
		}
		lines = append(lines, paint(label, browserMuted, false))
	} else if len(rows) == 0 {
		if m.databaseState.err != nil {
			lines = append(lines, paint("Stale · refresh failed: "+safeText(m.databaseState.err.Error()), browserNumber, false))
		}
		lines = append(lines, paint("No databases match this filter.", browserMuted, false))
	} else {
		if m.databaseState.active() {
			lines = append(lines, paint("Refreshing databases "+m.spinnerGlyph(), browserBranch, true))
		}
		if m.databaseState.err != nil {
			lines = append(lines, paint("Stale · refresh failed: "+safeText(m.databaseState.err.Error()), browserNumber, false))
		}
		for i, db := range rows {
			statusGlyph, statusColor := databaseStatusGlyph(db.Status.Status)
			name := "▤ " + safeText(db.Name)
			line := name + strings.Repeat(" ", max(1, m.paneWidthsInt()-ansi.StringWidth(name)-2)) + paint(statusGlyph, statusColor, true)
			meta := safeText(strings.TrimSpace(db.Engine+" "+db.Version)) + fmt.Sprintf(" · %d nodes", db.NumNodes)
			if i == m.databaseCursor {
				line = selectedRowStyle().Width(m.paneWidthsInt()).Render(line)
			}
			lines = append(lines, line, paint("   "+ansi.Truncate(meta, max(1, m.paneWidthsInt()-3), "…"), browserMuted, false))
		}
	}
	if len(lines) > height {
		start := max(0, min(m.databaseCursor*2, len(lines)-height))
		lines = lines[start:min(len(lines), start+height)]
	}
	return fitPaneLines(lines, height)
}

func databaseStatusGlyph(value string) (string, color.Color) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "running", "healthy", "ready", "active", "success", "succeeded":
		return "●", browserSuccess
	case "failed", "error", "unhealthy", "crashed":
		return "✖", browserDanger
	case "pending", "creating", "updating", "restarting", "warning", "degraded":
		return "▲", browserNumber
	default:
		return "?", browserMuted
	}
}

func (m *browser) databaseDetailLines(height int) []string {
	db := m.selectedDatabase()
	if db == nil {
		if m.databaseState.active() {
			return fitPaneLines([]string{paint("Loading "+m.spinnerGlyph(), browserBranch, true)}, height)
		}
		return fitPaneLines([]string{"Select a database to inspect its nodes."}, height)
	}
	lines := []string{}
	if m.databaseState.active() {
		lines = append(lines, paint("Refreshing snapshot "+m.spinnerGlyph(), browserBranch, true), "")
	}
	if m.databaseState.err != nil {
		lines = append(lines, paint("Stale · refresh failed: "+safeText(m.databaseState.err.Error()), browserNumber, false), staleRequestTime("Databases", m.databaseState), "")
	}
	lines = append(lines, paint("▤ "+safeText(db.Name), browserAccent, true), "")
	add := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			value = "—"
		}
		lines = append(lines, paint(fmt.Sprintf("%-13s", key), browserMuted, false)+safeText(value))
	}
	add("Engine", strings.TrimSpace(db.Engine+" "+db.Version))
	glyph, color := databaseStatusGlyph(db.Status.Status)
	lines = append(lines, paint(fmt.Sprintf("%-13s", "Status"), browserMuted, false)+paint(glyph+" "+emptyValue(db.Status.Status), color, true))
	add("Detail", db.Status.Detail)
	add("Role", db.Status.Role)
	add("Plan", db.Plan)
	add("Managed", map[bool]string{true: "Yes", false: "No"}[db.IsManaged])
	lines = append(lines, "", paint(fmt.Sprintf("NODES · %d", db.NumNodes), browserAccent, true))
	if len(db.Nodes) == 0 {
		lines = append(lines, paint("No node details returned.", browserMuted, false))
	}
	for _, node := range db.Nodes {
		ng, nc := databaseStatusGlyph(node.Status)
		lines = append(lines, paint(ng+" "+safeText(emptyValue(node.Name)), nc, true))
		lines = append(lines, paint(fmt.Sprintf("  CPU %-8s RAM %-8s Disk %-8s", databaseCPU(node.CPU), databaseRAM(node.RAM), databaseDisk(node.Disk)), browserMuted, false))
	}
	lines = append(lines, "")
	add("Database ID", db.ID)
	add("Namespace ID", fmt.Sprint(db.NamespaceID))
	add("Cluster ID", fmt.Sprint(db.ClusterID))
	lines = append(lines, "")
	lines = append(lines, m.databaseConnectionLines()...)
	if len(lines) > height {
		start := max(0, min(m.databaseScroll, len(lines)-height))
		lines = lines[start:min(len(lines), start+height)]
	}
	return fitPaneLines(lines, height)
}

func (m *browser) databaseConnectionLines() []string {
	_, detailWidth, _ := m.paneWidths()
	width := max(18, detailWidth-4)
	lines := []string{paint("CONNECTION", browserAccent, true)}
	if databaseHasStandby(m.databaseDetail) {
		role := "Master"
		if m.databaseConnectionRole == dbConnectionStandby {
			role = "Standby"
		}
		lines = append(lines, paint("Node · "+role, browserBranch, true))
	}
	if m.databaseDetailState.active() {
		lines = append(lines, paint("Loading connection details "+m.spinnerGlyph(), browserMuted, false))
		return lines
	}
	if m.databaseDetailState.status == requestFailed || m.databaseDetailState.status == requestStale {
		lines = append(lines, paint("Could not load connection details.", browserDanger, false), paint("Press r to retry.", browserMuted, false))
		return lines
	}
	external, internal := m.databaseConnectionsForRole()
	if external == nil && internal == nil {
		lines = append(lines, paint("Connection details unavailable for this address.", browserMuted, false))
		return lines
	}
	lines = append(lines, paint("EXTERNAL", browserBranch, true))
	lines = appendDatabaseConnectionFields(lines, external)
	lines = append(lines, paint("INTERNAL", browserBranch, true))
	lines = appendDatabaseConnectionFields(lines, internal)
	if strings.HasPrefix(m.mode, "MOCK") {
		lines = append(lines, paint("Password:", browserMuted, false)+"synthetic mock credential")
	} else {
		consoleURL := databaseConsoleURL(m.tenant, m.selectedDatabase())
		lines = append(lines, paint("Password:", browserMuted, false)+"Retrieve it in Console (TOTP required).")
		lines = append(lines, paint("Please go to:", browserMuted, false))
		link := lipgloss.NewStyle().Underline(true).Hyperlink(consoleURL).Render(consoleURL)
		for _, part := range wrapRegistryText(link, width) {
			lines = append(lines, part)
		}
	}
	if external != nil {
		lines = append(lines, paint("External connection string:", browserMuted, false))
		for _, part := range wrapRegistryText(databaseConnectionURI(m.selectedDatabase(), external, false), width) {
			lines = append(lines, safeText(part))
		}
	}
	if m.databaseNotice != "" {
		lines = append(lines, paint(m.databaseNotice, browserNumber, false))
	}
	return lines
}

func (m *browser) databaseConnectionsForRole() (*client.DatabaseConnection, *client.DatabaseConnection) {
	if m.databaseDetail == nil {
		return nil, nil
	}
	set := m.databaseDetail.Connections.Master
	if m.databaseConnectionRole == dbConnectionStandby {
		set = m.databaseDetail.Connections.Standby
	}
	external, internal := set.External, set.Private
	if !databaseConnectionPresent(external) {
		external = nil
	}
	if !databaseConnectionPresent(internal) {
		internal = nil
	}
	if m.databaseConnectionRole == dbConnectionMaster {
		if external == nil && databaseConnectionPresent(m.databaseDetail.Connection) {
			external = m.databaseDetail.Connection
		}
		if internal == nil && databaseConnectionPresent(m.databaseDetail.PrivateConnection) {
			internal = m.databaseDetail.PrivateConnection
		}
	} else {
		if external == nil && databaseConnectionPresent(m.databaseDetail.StandbyConnection) {
			external = m.databaseDetail.StandbyConnection
		}
		if internal == nil && databaseConnectionPresent(m.databaseDetail.PrivateStandbyConnection) {
			internal = m.databaseDetail.PrivateStandbyConnection
		}
	}
	return external, internal
}

func appendDatabaseConnectionFields(lines []string, connection *client.DatabaseConnection) []string {
	if !databaseConnectionPresent(connection) {
		return append(lines, paint("  Unavailable", browserMuted, false))
	}
	values := []struct{ name, value string }{
		{"  Username", connection.Username}, {"  Host", connection.Host},
		{"  Port", strconv.Itoa(connection.Port)}, {"  Database", connection.Database},
	}
	for _, field := range values {
		if field.name == "  Port" && connection.Port == 0 || field.value == "" {
			field.value = "—"
		}
		lines = append(lines, paint(fmt.Sprintf("%-13s", field.name+":"), browserMuted, false)+safeText(field.value))
	}
	return lines
}

func databaseConnectionPresent(connection *client.DatabaseConnection) bool {
	return connection != nil && (connection.Host != "" || connection.Username != "" || connection.Password != "" || connection.Database != "" || connection.Port != 0)
}

func databaseHasStandby(detail *client.DatabaseDetail) bool {
	if detail == nil {
		return false
	}
	return databaseConnectionPresent(detail.Connections.Standby.External) || databaseConnectionPresent(detail.Connections.Standby.Private) || databaseConnectionPresent(detail.StandbyConnection) || databaseConnectionPresent(detail.PrivateStandbyConnection)
}

func databaseConnectionURI(db *client.Database, connection *client.DatabaseConnection, replacePassword bool) string {
	if connection == nil {
		return ""
	}
	scheme := "database"
	if db != nil {
		switch strings.ToLower(db.Engine) {
		case "postgres", "postgresql":
			scheme = "postgres"
		case "mysql", "mariadb":
			scheme = "mysql"
		case "mongodb", "mongo":
			scheme = "mongodb"
		case "redis":
			scheme = "redis"
		}
	}
	hostport := connection.Host
	if connection.Port != 0 {
		hostport = net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))
	}
	if connection.Port == 0 && strings.Contains(hostport, ":") && !strings.HasPrefix(hostport, "[") {
		hostport = "[" + hostport + "]"
	}
	base := (&url.URL{Scheme: scheme, Host: hostport, Path: "/" + connection.Database}).String()
	authority := strings.TrimPrefix(base, scheme+"://")
	user := url.User(connection.Username).String()
	if replacePassword {
		user = url.UserPassword(connection.Username, "REPLACE_PASSWORD").String()
	}
	if user == "" {
		return scheme + "://" + authority
	}
	return scheme + "://" + user + "@" + authority
}

func databaseConsoleURL(tenant string, db *client.Database) string {
	if db == nil {
		return "https://console.hamravesh.com"
	}
	return "https://console.hamravesh.com/@" + url.PathEscape(tenant) + "/dbaas/app/" + url.PathEscape(db.ID)
}

func (m *browser) copyDatabaseConnection() tea.Cmd {
	db := m.selectedDatabase()
	external, _ := m.databaseConnectionsForRole()
	connection := external
	if db == nil || connection == nil {
		m.databaseNotice = "No connection string to copy"
		return nil
	}
	value := databaseConnectionURI(db, connection, true)
	tenant, id := m.tenant, db.ID
	m.databaseNotice = "Copying connection string…"
	return func() tea.Msg {
		return databaseCopyResult{tenant: tenant, databaseID: id, err: clipboard.WriteAll(value)}
	}
}

func emptyValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return value
}
func databaseCPU(value int) string {
	if value%1000 == 0 {
		return fmt.Sprintf("%d cores", value/1000)
	}
	return fmt.Sprintf("%dm", value)
}
func databaseRAM(value int) string  { return fmt.Sprintf("%d MB", value) }
func databaseDisk(value int) string { return fmt.Sprintf("%d GB", value) }

func (m *browser) handleDatabaseKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.databaseFilterEditing {
		switch key {
		case "enter":
			m.databaseFilterEditing = false
		case "esc":
			m.databaseFilter = m.databaseFilterBefore
			m.databaseFilterEditing = false
		case "backspace":
			r := []rune(m.databaseFilter)
			if len(r) > 0 {
				m.databaseFilter = string(r[:len(r)-1])
			}
		case "ctrl+u":
			m.databaseFilter = ""
		default:
			if msg.Text != "" {
				m.databaseFilter += safeText(msg.Text)
			}
		}
		m.databaseCursor = 0
		return m, m.updateDatabaseSelection()
	}
	var cmd tea.Cmd
	switch key {
	case "r":
		cmd = tea.Batch(m.loadDatabases(true), m.loadSelectedDatabaseDetail(true))
	case "/":
		m.databaseFilterBefore = m.databaseFilter
		m.databaseFilterEditing = true
		return m, nil
	case "up", "k":
		if m.detail {
			m.databaseScroll = max(0, m.databaseScroll-1)
		} else {
			cmd = m.moveDatabaseSelection(-1)
		}
	case "down", "j":
		if m.detail {
			m.databaseScroll++
		} else {
			cmd = m.moveDatabaseSelection(1)
		}
	case "home", "g":
		if m.detail {
			m.databaseScroll = 0
		} else {
			m.databaseCursor = 0
			cmd = m.updateDatabaseSelection()
		}
	case "end", "G":
		if m.detail {
			m.databaseScroll = len(m.databaseDetailLines(m.height))
		} else {
			m.databaseCursor = max(0, len(m.visibleDatabases())-1)
			cmd = m.updateDatabaseSelection()
		}
	case "pgup":
		if m.detail {
			m.databaseScroll = max(0, m.databaseScroll-max(1, m.height-8))
		} else {
			cmd = m.moveDatabaseSelection(-max(1, m.height-8))
		}
	case "pgdown":
		if m.detail {
			m.databaseScroll += max(1, m.height-8)
		} else {
			cmd = m.moveDatabaseSelection(max(1, m.height-8))
		}
	case "s":
		m.sorting = true
		m.sortChoice = int(m.databaseSortBy)
		m.sortDescending = m.databaseSortDescending
	case "esc":
		if m.detail {
			m.detail = false
		} else if m.databaseFilter != "" {
			m.databaseFilter = ""
			m.databaseCursor = 0
			cmd = m.updateDatabaseSelection()
		} else {
			m.page = pageApps
			m.contextFocused = false
			m.resetDatabaseDetail()
		}
	case "enter":
		m.detail = !m.detail
		m.databaseScroll = 0
	case "left", "h":
		if m.detail {
			m.detail = false
		}
	case "right", "l":
		if !m.detail {
			m.detail = true
			m.databaseScroll = 0
		}
	case "m", "b":
		if key == "b" && !databaseHasStandby(m.databaseDetail) {
			m.databaseNotice = "No standby connection is available"
		} else {
			if key == "m" {
				m.databaseConnectionRole = dbConnectionMaster
			} else {
				m.databaseConnectionRole = dbConnectionStandby
			}
		}
	case "y":
		cmd = m.copyDatabaseConnection()
	}
	return m, cmd
}

func (m *browser) handleDatabaseMouse(msg tea.MouseClickMsg) tea.Cmd {
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	if msg.X < contextWidth+2 {
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(4, (bodyHeight-1)/2)
		if msg.Y >= bodyY+tenantHeight+1 {
			row := msg.Y - (bodyY + tenantHeight + 1) + 1
			if row >= 1 && row <= 6 {
				m.contextFocused = true
				m.contextRow = row
				m.detail = false
				return m.activatePageRow(row)
			}
		}
		return nil
	}
	if m.width < 100 && m.detail {
		return nil
	}
	listLeft := contextWidth + 3
	listRight := listLeft + appsWidth + 2
	if msg.X >= listLeft && msg.X < listRight {
		rows := m.visibleDatabases()
		row := (msg.Y - bodyY - 1) / 2
		if row >= 0 && row < len(rows) {
			m.databaseCursor = row
			m.contextFocused = false
			return m.updateDatabaseSelection()
		}
		return nil
	}
	if msg.X >= listRight+1 && msg.X < listRight+1+summaryWidth+2 {
		m.detail = true
	}
	return nil
}
