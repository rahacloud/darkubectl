package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	buildsLimit      = 30
	buildOutputLimit = 2 << 20
)

func (m *browser) loadSelectedBuilds() tea.Cmd {
	if !m.selectedAppActive() {
		return nil
	}
	app := m.selectedApp()
	if app.BuildMethod != client.BuildMethodWebhook {
		return nil
	}
	if m.listBuilds == nil {
		m.buildsState.unavailable(fmt.Errorf("build history is not available in this mode"))
		return nil
	}
	meta, ctx := m.startRequest(requestBuilds, m.tenant, app.ID, m.selectionGeneration)
	m.buildsState.begin(meta, m.requestTime())
	return func() tea.Msg {
		rows, err := m.listBuilds(ctx, meta.tenant, meta.appID, buildsLimit)
		return buildsResult{meta: meta, builds: rows, err: err}
	}
}

func (m *browser) openSelectedBuild() tea.Cmd {
	if m.buildListCursor < 0 || m.buildListCursor >= len(m.builds) {
		return nil
	}
	b := m.builds[m.buildListCursor]
	if m.buildDetail != nil && m.buildDetail.ID != b.ID {
		m.buildDetail = nil
		m.buildDetailState.reset()
		m.buildSearch = ""
		m.summaryScroll = 0
	}
	if m.buildDetail == nil {
		m.summaryScroll = 0
	}
	m.buildSelectedID = b.ID
	if m.getBuild == nil {
		m.buildDetailState.unavailable(fmt.Errorf("build output is not available in this mode"))
		return nil
	}
	meta, ctx := m.startRequest(requestBuild, m.tenant, m.selectedDetailTarget(), m.selectionGeneration)
	m.buildDetailState.begin(meta, m.requestTime())
	return func() tea.Msg {
		detail, err := m.getBuild(ctx, meta.tenant, b.ID)
		return buildResult{meta: meta, id: b.ID, build: detail, err: err}
	}
}

func (m *browser) buildsPaneLines() []string {
	if m.namespaceSelected || !m.selectedAppActive() {
		return []string{"Select an app to view CI/CD."}
	}
	app := m.selectedApp()
	notices := []string{}
	if m.buildActionNotice != "" {
		notices = append(notices, paint(safeText(m.buildActionNotice), browserNumber, false))
	}
	if m.buildNotice != "" {
		notices = append(notices, paint(safeText(m.buildNotice), browserMuted, false))
	}
	if app.BuildMethod == "" {
		if m.detailState.active() {
			return []string{m.loadingMessage("Checking app build support…")}
		}
		return append(notices, sectionHeading("? Deployment type unknown"), "The deployment method could not be determined.")
	}
	if app.BuildMethod != client.BuildMethodWebhook && app.BuildMethod != client.BuildMethodGitlabCI {
		return append(notices, sectionHeading("? Deployment type unknown"), summaryField("Method", safeText(app.BuildMethod)))
	}
	m.ensureCICDSettings()
	if m.cicdReview {
		return m.cicdSettingsReviewLines()
	}
	if m.cicdSettingsDraft.BuildMethod == client.BuildMethodGitlabCI {
		lines := append(notices, sectionHeading("⚙ Custom CI/CD"), summaryField("Deployment type", "Custom CI/CD"), summaryField("App ID", app.ID), m.deployTokenLine())
		if m.cicdLoading {
			lines = append(lines, m.loadingMessage("Loading deploy token…"))
		}
		if m.cicdNotice != "" {
			lines = append(lines, paint(safeText(m.cicdNotice), browserMuted, false))
		}
		if m.cicdSettingsChanged() {
			lines = append(lines, paint("Deployment type change staged · press s to review", browserAccent, false))
		}
		if m.cicdSettingsNotice != "" {
			lines = append(lines, paint(safeText(m.cicdSettingsNotice), browserMuted, false))
		}
		return lines
	}
	if m.buildDetail != nil {
		return m.buildDetailLines()
	}
	lines := append([]string(nil), notices...)
	info := m.cicdDarkubeInfo(app)
	settingsRowLine := 0
	for i, line := range info {
		if strings.Contains(ansi.Strip(line), "› ") {
			settingsRowLine = i
			break
		}
	}
	m.cicdCursorLine = len(lines) + settingsRowLine
	lines = append(lines, info...)
	lines = append(lines, "", sectionHeading("History"))
	if m.buildsState.status == requestLoading {
		lines = append(lines, m.loadingMessage("Loading builds…"))
	}
	if m.buildsState.status == requestRefreshing {
		lines = append(lines, m.loadingMessage("Refreshing builds…"))
	}
	if m.buildsState.err != nil {
		state := "Build list unavailable"
		if permissionError(m.buildsState.err) {
			state = "Permission denied while reading builds"
		}
		if m.buildsState.status == requestStale {
			state = "Stale build list · refresh failed"
		}
		lines = append(lines, paint(state+": "+boundedErrorText(m.buildsState.err), browserNumber, false))
		if m.buildsState.status == requestStale {
			lines = append(lines, staleRequestTime("Builds", m.buildsState))
		}
	}
	if m.buildDetailState.err != nil && m.buildDetail == nil {
		lines = append(lines, paint("Could not load selected build: "+boundedErrorText(m.buildDetailState.err), browserNumber, false))
	}
	if m.buildsState.status == requestEmpty || (m.buildsState.status == requestReady && len(m.builds) == 0) {
		return append(lines, "No builds have been recorded for this app.")
	}
	if len(m.builds) == 0 {
		return append(lines, "No build data loaded. Press r to refresh.")
	}
	for i, b := range m.builds {
		glyph, tint := buildStatusStyle(b.Status)
		_, width, _ := m.paneWidths()
		if m.appViewMaximized() {
			width = m.width - 2
		}
		message := ansi.Truncate(strings.ReplaceAll(buildText(b.CommitMessage, "(no commit message)"), "\n", " "), max(1, width-5), "…")
		first := glyph + "  " + message
		if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
			first = glyph + " [" + buildText(b.Status, "unknown") + "]  " + message
		}
		branch := buildText(b.GitBranch, "—")
		hash := buildText(b.ShortCommitHash, "—")
		meta := fmt.Sprintf("#%s · %s · %s", buildText(string(b.ID), "—"), branch, hash)
		if started := buildTime(b.CreationTime); started != "" {
			meta += " · " + started
		}
		if i == m.buildListCursor && m.cicdHistoryFocus {
			first = selectedRowStyle().Render(first)
			meta = selectedRowStyle().Render(meta)
		} else {
			first = paint(first, tint, true)
			meta = paint(meta, browserMuted, false)
		}
		lines = append(lines, first, "   "+meta)
	}
	return lines
}

func (m *browser) buildListPrefixLines() int {
	n := len(m.cicdDarkubeInfo(m.selectedApp())) + 2
	if m.buildActionNotice != "" {
		n++
	}
	if m.buildNotice != "" {
		n++
	}
	if m.buildsState.status == requestLoading || m.buildsState.status == requestRefreshing {
		n++
	}
	if m.buildsState.err != nil {
		n++
		if m.buildsState.status == requestStale {
			n++
		}
	}
	if m.buildDetailState.err != nil && m.buildDetail == nil {
		n++
	}
	return n
}

func (m *browser) cicdDarkubeInfo(app client.App) []string {
	m.ensureCICDSettings()
	settings := m.cicdSettingsDraft
	value := func(s string) string { return buildText(s, "—") }
	fields := []struct{ name, value string }{{"Repository", redactRepoURL(settings.RepoURL)}, {"Branch", settings.Branch}, {"Dockerfile path", settings.Dockerfile}, {"Build context", settings.Context}, {"Workdir", settings.Workdir}}
	lines := []string{sectionHeading("↻ Darkube · Settings"), summaryField("Deployment type", "Darkube"), summaryField("App ID", app.ID), m.deployTokenLine()}
	if m.cicdLoading {
		lines = append(lines, m.loadingMessage("Loading deploy token…"))
	}
	if m.cicdNotice != "" {
		lines = append(lines, paint(safeText(m.cicdNotice), browserMuted, false))
	}
	for i, field := range fields {
		v := value(field.value)
		editIndex := []int{0, 1, 4, 5, 6}[i]
		if m.cicdEditing && m.cicdEditField == editIndex {
			v = m.cicdEditValue + "▏"
			if i == 0 {
				v = redactRepoURL(v)
			}
		}
		lines = append(lines, m.cicdSettingLine(editIndex, field.name, v))
	}
	auto := "—"
	if settings.Autodeploy != nil {
		if *settings.Autodeploy {
			auto = "On push"
		} else {
			auto = "Off"
		}
	}
	if m.cicdEditing && m.cicdEditField == 7 {
		auto = m.cicdEditValue + "▏"
	}
	lines = append(lines, m.cicdSettingLine(7, "Auto deploy", auto), "", sectionHeading("Build arguments"))
	args := settings.Args
	if len(args) == 0 {
		lines = append(lines, m.cicdSettingLine(cicdSettingsFields, "No build arguments", ""))
	}
	for i, arg := range args {
		value := "••••••••"
		if m.cicdReveal {
			value = safeText(arg.Value)
		}
		if m.cicdEditing && m.cicdEditArg && m.cicdEditField == cicdSettingsFields+i {
			value = "••••••••▏"
		}
		lines = append(lines, m.cicdSettingLine(cicdSettingsFields+i, arg.Key, value))
	}
	addValue := ""
	if m.cicdEditing && m.cicdEditAdding {
		if m.cicdEditKey == "" {
			addValue = m.cicdEditValue + "▏"
		} else {
			addValue = "••••••••▏"
		}
	}
	if m.allowWrites {
		lines = append(lines, m.cicdSettingLine(cicdSettingsFields+len(args), "+ Add build argument", addValue))
	}
	if m.cicdSettingsChanged() {
		lines = append(lines, "", paint(fmt.Sprintf("%d staged setting change(s)", m.cicdSettingsChangeCount()), browserAccent, false))
	}
	if m.cicdSettingsNotice != "" {
		lines = append(lines, paint(safeText(m.cicdSettingsNotice), browserMuted, false))
	}
	return lines
}

func (m *browser) deployTokenLine() string {
	value := "••••••••••••"
	if m.cicdToken == "" {
		value = "Loading…"
		if !m.cicdLoading {
			value = "Unavailable · press v to retry"
		}
	}
	if m.cicdTokenReveal && m.cicdToken != "" {
		value = safeText(m.cicdToken)
	}
	return summaryField("Deploy token", value)
}

func (m *browser) cicdSettingLine(row int, label, value string) string {
	selected := m.allowWrites && row == m.cicdSettingsCursor && !m.cicdHistoryFocus
	marker := " "
	if selected {
		marker = "›"
	}
	line := fmt.Sprintf("%s %s", marker, summaryField(safeText(label), safeText(value)))
	if selected {
		return selectedRowStyle().Render(line)
	}
	return line
}

func (m *browser) cicdSettingsChangeCount() int {
	a, b := m.cicdSettingsOriginal, m.cicdSettingsDraft
	n := 0
	if a.BuildMethod != b.BuildMethod {
		n++
	}
	if a.RepoURL != b.RepoURL {
		n++
	}
	if a.Branch != b.Branch {
		n++
	}
	if a.Provider != b.Provider {
		n++
	}
	if a.Builder != b.Builder {
		n++
	}
	if a.Dockerfile != b.Dockerfile {
		n++
	}
	if a.Context != b.Context {
		n++
	}
	if a.Workdir != b.Workdir {
		n++
	}
	if autoDeployLabel(a.Autodeploy) != autoDeployLabel(b.Autodeploy) {
		n++
	}
	if len(a.Args) != len(b.Args) || fmt.Sprint(a.Args) != fmt.Sprint(b.Args) {
		n++
	}
	return n
}

func cicdMethodLabel(method string) string {
	if method == client.BuildMethodWebhook {
		return "Darkube"
	}
	if method == client.BuildMethodGitlabCI {
		return "Custom CI/CD"
	}
	return buildText(method, "Unknown")
}

type buildArg struct{ name, value string }

func redactRepoURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "—"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

func buildArgs(raw json.RawMessage) []buildArg {
	var data any
	if json.Unmarshal(raw, &data) != nil {
		return nil
	}
	var args []buildArg
	switch v := data.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, buildArg{safeText(k), fmt.Sprint(v[k])})
		}
	case []any:
		for _, item := range v {
			switch x := item.(type) {
			case string:
				parts := strings.SplitN(x, "=", 2)
				value := ""
				if len(parts) == 2 {
					value = parts[1]
				}
				args = append(args, buildArg{safeText(parts[0]), value})
			case map[string]any:
				for _, key := range []string{"name", "key"} {
					if s, ok := x[key].(string); ok && s != "" {
						value, _ := x["value"].(string)
						args = append(args, buildArg{safeText(s), value})
						break
					}
				}
			}
		}
	}
	return args
}

func (m *browser) requestCICDToken(copyToken bool) tea.Cmd {
	if m.cicdLoading {
		return nil
	}
	if m.cicdToken != "" {
		if copyToken {
			return m.copyCICDValue(m.cicdToken)
		}
		m.cicdTokenReveal = !m.cicdTokenReveal
		return nil
	}
	if m.deployToken == nil {
		m.cicdNotice = "Deploy token is unavailable in this mode."
		return nil
	}
	tenant, appID, generation := m.tenant, m.selectedDetailTarget(), m.cicdGeneration
	fetch, ctx := m.deployToken, m.ctx
	reveal := m.cicdTokenReveal
	if ctx == nil {
		ctx = context.Background()
	}
	m.cicdLoading = true
	return func() tea.Msg {
		token, err := fetch(ctx, tenant, appID)
		return cicdTokenResult{tenant: tenant, appID: appID, generation: generation, token: token, copy: copyToken, reveal: reveal, err: err}
	}
}

func (m *browser) copyCICDValue(value string) tea.Cmd {
	tenant, appID := m.tenant, m.selectedDetailTarget()
	return func() tea.Msg { return cicdCopyResult{tenant: tenant, appID: appID, err: clipboard.WriteAll(value)} }
}

func (m *browser) buildDetailLines() []string {
	b := *m.buildDetail
	glyph, tint := buildStatusStyle(b.Status)
	lines := []string{paint(glyph+" "+buildText(b.CommitMessage, "(no commit message)"), tint, true),
		summaryField("Build", buildText(string(b.ID), "—")), summaryField("Status", buildText(b.Status, "unknown")),
		summaryField("Branch", buildText(b.GitBranch, "—")), summaryField("Commit", buildText(b.ShortCommitHash, "—")),
		summaryField("Started", buildTime(b.CreationTime)), summaryField("Duration", buildDuration(b.CreationTime, b.EndTime)),
		summaryField("App ID", m.selectedApp().ID), m.deployTokenLine()}
	if m.buildDetailState.status == requestStale && m.buildDetailState.err != nil {
		lines = append(lines, paint("Stale output · refresh failed: "+boundedErrorText(m.buildDetailState.err), browserNumber, false))
	}
	if m.buildActionNotice != "" {
		lines = append(lines, paint(safeText(m.buildActionNotice), browserNumber, false))
	}
	if errText := boundedBuildError(b.Errors); errText != "" {
		lines = append(lines, summaryField("Details", errText))
	}
	lines = append(lines, "", sectionHeading("Build output"))
	if m.buildSearchEditing {
		lines = append(lines, paint("Search: "+safeText(m.buildSearch)+"▌", browserAccent, true))
	} else if m.buildSearch != "" {
		lines = append(lines, paint("Search: "+safeText(m.buildSearch), browserMuted, false))
	}
	if m.buildCopyStatus != "" {
		lines = append(lines, paint(safeText(m.buildCopyStatus), browserMuted, false))
	}
	output := sanitizeBuildOutput(b.Log)
	if output == "" {
		if m.buildDetailState.err != nil {
			lines = append(lines, "Could not load build output: "+boundedErrorText(m.buildDetailState.err))
		} else {
			lines = append(lines, "No output was recorded for this build.")
		}
	} else {
		lines = append(lines, strings.Split(output, "\n")...)
	}
	return lines
}

func buildDetailStickyLines(lines []string) int {
	for i, line := range lines {
		if ansi.Strip(line) == "Build output" {
			sticky := i + 1
			for sticky < len(lines) && (strings.HasPrefix(ansi.Strip(lines[sticky]), "Search:") || strings.Contains(ansi.Strip(lines[sticky]), "Copied ") || ansi.Strip(lines[sticky]) == "Copying build output…") {
				sticky++
			}
			return sticky
		}
	}
	return 7
}

func sanitizeBuildOutput(value string) string {
	truncated := len(value) > buildOutputLimit
	if truncated {
		value = value[:buildOutputLimit]
	}
	value = strings.ToValidUTF8(value, "�")
	value = safeText(ansi.Strip(value))
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	if truncated {
		value += "\n\n[Output truncated at 2 MiB]"
	}
	return value
}

func boundedBuildError(value any) string {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []string:
		text = strings.Join(v, "; ")
	case nil:
		return ""
	default:
		return ""
	}
	text = safeText(ansi.Strip(text))
	if len(text) > 512 {
		text = text[:512] + "…"
	}
	return text
}

func boundedErrorText(err error) string {
	if err == nil {
		return "unknown error"
	}
	text := safeText(ansi.Strip(err.Error()))
	if len(text) > 512 {
		text = text[:512] + "…"
	}
	return text
}

func buildText(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return safeText(v)
}
func buildTime(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	if t, e := time.Parse(time.RFC3339Nano, v); e == nil {
		return t.Local().Format("Jan 02 15:04")
	}
	return safeText(v)
}
func buildDuration(start, end string) string {
	a, e := time.Parse(time.RFC3339Nano, start)
	if e != nil {
		return "—"
	}
	if end == "" {
		return "in progress"
	}
	b, e := time.Parse(time.RFC3339Nano, end)
	if e != nil {
		return "—"
	}
	return b.Sub(a).Round(time.Second).String()
}
func buildStatusStyle(status string) (string, color.Color) {
	switch status {
	case client.BuildSucceeded:
		return "✓", browserSuccess
	case client.BuildFailed:
		return "✕", browserDanger
	case client.BuildInitiated, client.BuildRunning:
		return "◌", browserAccent
	case client.BuildCanceled:
		return "−", browserMuted
	default:
		return "?", browserNumber
	}
}

func permissionError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "403") || strings.Contains(s, "permission") || strings.Contains(s, "forbidden")
}

func (m *browser) moveBuildSelection(delta int) {
	if len(m.builds) == 0 {
		return
	}
	m.buildListCursor = max(0, min(len(m.builds)-1, m.buildListCursor+delta))
	m.buildSelectedID = m.builds[m.buildListCursor].ID
	visible := max(1, m.height/2-6)
	line := m.buildListPrefixLines() + m.buildListCursor*2
	if line < m.summaryScroll {
		m.summaryScroll = line
	}
	if line >= m.summaryScroll+visible {
		m.summaryScroll = line - visible + 1
	}
}

func (m *browser) handleBuildSearchKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case browserKeyEscape:
		m.buildSearch = m.buildSearchBefore
		m.buildSearchEditing = false
	case keyEnter:
		m.buildSearchEditing = false
		m.buildMatch = 0
		m.moveBuildMatch(0)
	case keyBackspace, keyCtrlH:
		r := []rune(m.buildSearch)
		if len(r) > 0 {
			m.buildSearch = string(r[:len(r)-1])
		}
	case keyCtrlU:
		m.buildSearch = ""
	default:
		if msg.Text != "" {
			m.buildSearch += safeText(msg.Text)
		}
	}
}

func (m *browser) moveBuildMatch(delta int) {
	if m.buildDetail == nil || m.buildSearch == "" {
		return
	}
	lines := strings.Split(sanitizeBuildOutput(m.buildDetail.Log), "\n")
	var matches []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), strings.ToLower(m.buildSearch)) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		m.buildCopyStatus = "No matches"
		return
	}
	m.buildMatch = (m.buildMatch + delta + len(matches)) % len(matches)
	allLines := m.buildDetailLines()
	outputStart := 0
	for i, line := range allLines {
		if ansi.Strip(line) == "Build output" {
			outputStart = i + 1
			break
		}
	}
	if m.buildSearch != "" {
		if outputStart < len(allLines) && strings.HasPrefix(ansi.Strip(allLines[outputStart]), "Search:") {
			outputStart++
		}
	}
	m.summaryScroll = max(0, outputStart+matches[m.buildMatch]-buildDetailStickyLines(allLines))
	m.buildCopyStatus = fmt.Sprintf("Match %d of %d", m.buildMatch+1, len(matches))
}

func (m *browser) copyBuildOutput() tea.Cmd {
	if m.buildDetail == nil {
		return nil
	}
	content := sanitizeBuildOutput(m.buildDetail.Log)
	return func() tea.Msg { return buildCopyResult{bytes: len(content), err: clipboard.WriteAll(content)} }
}

func (m *browser) buildActionItems() []buildAction {
	if !m.actionIsMock() && (!m.allowWrites || m.buildLatest == nil || m.retryBuild == nil || m.stopBuild == nil) {
		return nil
	}
	items := []buildAction{buildActionLatest}
	if m.buildSelectedID != "" {
		items = append(items, buildActionRetry)
	}
	if m.selectedBuildInProgress() {
		items = append(items, buildActionStop)
	}
	return items
}

func (m *browser) selectedBuildInProgress() bool {
	if m.buildDetail != nil && m.buildDetail.ID == m.buildSelectedID {
		return m.buildDetail.InProgress()
	}
	return m.buildListCursor >= 0 && m.buildListCursor < len(m.builds) && m.builds[m.buildListCursor].ID == m.buildSelectedID && m.builds[m.buildListCursor].InProgress()
}

func (m *browser) openBuildActionMenu() {
	if m.cicdSettingsChanged() {
		m.buildNotice = "Save or discard staged CI/CD settings before starting a build action."
		return
	}
	if m.width < 60 || m.height < 20 {
		m.buildNotice = "Resize to at least 60 × 20 to use build actions."
		return
	}
	if !m.selectedAppActive() || m.selectedApp().BuildMethod != client.BuildMethodWebhook {
		m.buildNotice = "Build actions require Darkube deployment."
		return
	}
	if !m.actionIsMock() && !m.allowWrites {
		m.buildNotice = "Writes are disabled. Relaunch with --allow-writes."
		return
	}
	items := m.buildActionItems()
	if len(items) == 0 {
		m.buildNotice = "Build actions are unavailable until build data and write adapters are ready."
		return
	}
	app := m.selectedApp()
	m.buildConfirmTenant = m.tenant
	m.buildConfirmAppID = app.ID
	m.buildConfirmAppName = app.Name
	m.buildActionCursor = 0
	m.buildActionMenu = true
	m.buildActionNotice = ""
}

func (m *browser) handleBuildActionMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	items := m.buildActionItems()
	if len(items) == 0 {
		m.buildActionMenu = false
		return nil
	}
	switch msg.String() {
	case "esc", "q":
		m.buildActionMenu = false
	case "up", "k":
		m.buildActionCursor = (m.buildActionCursor + len(items) - 1) % len(items)
	case "down", "j":
		m.buildActionCursor = (m.buildActionCursor + 1) % len(items)
	case "enter":
		m.beginBuildActionConfirmation(items[m.buildActionCursor])
	}
	return nil
}

func (m *browser) beginBuildActionConfirmation(action buildAction) {
	if !m.selectedAppActive() || m.selectedDetailTarget() != m.buildConfirmAppID || m.tenant != m.buildConfirmTenant {
		m.buildActionMenu = false
		m.buildActionNotice = "Build action target changed; reopen the menu."
		return
	}
	if action == buildActionStop && !m.selectedBuildInProgress() {
		m.buildActionMenu = false
		m.buildActionNotice = "Stop is available only for a running build."
		return
	}
	m.buildConfirm = action
	m.buildConfirmButton = 0
	m.buildConfirmBuildID = m.buildSelectedID
	m.buildActionMenu = false
}

func (m *browser) handleBuildConfirmKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "n":
		m.buildConfirm = buildActionNone
		return nil
	case "left", "h", "shift+tab", "backtab":
		m.buildConfirmButton = 0
	case "right", "l", "tab":
		m.buildConfirmButton = 1
	case "y":
		m.buildConfirmButton = 1
	case "enter":
		if m.buildConfirmButton == 1 {
			return m.submitBuildAction()
		}
		m.buildConfirm = buildActionNone
	}
	return nil
}

func (m *browser) submitBuildAction() tea.Cmd {
	act, tenant, appID, buildID := m.buildConfirm, m.buildConfirmTenant, m.buildConfirmAppID, m.buildConfirmBuildID
	if act == buildActionNone || tenant != m.tenant || appID != m.selectedDetailTarget() {
		m.buildConfirm = buildActionNone
		m.buildActionNotice = "Build action target changed; nothing was submitted."
		return nil
	}
	m.buildConfirm = buildActionNone
	m.buildActionBusy = true
	m.actionBusy = true
	getApp, listBuilds, latest, retry, stop := m.getApp, m.listBuilds, m.buildLatest, m.retryBuild, m.stopBuild
	mock := m.actionIsMock()
	return func() tea.Msg {
		ctx := m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		var err error
		var retryID client.BuildID
		if !mock {
			if getApp == nil || listBuilds == nil {
				return buildActionResult{tenant: tenant, appID: appID, buildID: buildID, action: act, err: fmt.Errorf("could not verify app and build target")}
			}
			app, e := getApp(ctx, tenant, appID)
			if e != nil {
				err = fmt.Errorf("target verification failed: %w", e)
			} else if app == nil || app.ID != appID || app.BuildMethod != client.BuildMethodWebhook {
				err = fmt.Errorf("selected app is no longer a verified Darkube build target")
			}
			if err == nil && act != buildActionLatest {
				rows, e := listBuilds(ctx, tenant, appID, buildsLimit)
				if e != nil {
					err = fmt.Errorf("build ownership verification failed: %w", e)
				} else {
					var verified *client.Build
					for i := range rows {
						if rows[i].ID == buildID {
							verified = &rows[i]
							break
						}
					}
					if verified == nil {
						err = fmt.Errorf("build is no longer in this app's recent build list")
					} else if act == buildActionStop && !verified.InProgress() {
						err = fmt.Errorf("build is no longer running; stop was not submitted")
					}
				}
			}
		}
		if err == nil {
			switch act {
			case buildActionLatest:
				err = latest(ctx, tenant, appID)
			case buildActionRetry:
				var b *client.Build
				b, err = retry(ctx, tenant, buildID)
				if b != nil {
					retryID = b.ID
				}
			case buildActionStop:
				err = stop(ctx, tenant, buildID)
			}
		}
		return buildActionResult{tenant: tenant, appID: appID, buildID: buildID, action: act, retryID: retryID, mock: mock, err: err}
	}
}

func (m *browser) handleBuildActionResult(msg buildActionResult) (tea.Model, tea.Cmd) {
	m.buildActionBusy = false
	m.actionBusy = false
	if msg.err != nil {
		m.buildActionNotice = "Rejected · " + boundedErrorText(msg.err)
	} else {
		m.buildActionNotice = buildActionAccepted(msg.action)
		if msg.mock {
			m.buildActionNotice = "MOCK · simulated · " + m.buildActionNotice
		}
	}
	if msg.tenant != m.tenant || msg.appID != m.selectedDetailTarget() {
		return m, nil
	}
	if msg.err == nil {
		m.buildActionAwaiting = msg.action
		m.buildActionAwaitingID = msg.buildID
		m.buildActionRetryID = msg.retryID
		if msg.retryID != "" {
			for i, b := range m.builds {
				if b.ID == msg.retryID {
					m.buildListCursor = i
					m.buildSelectedID = b.ID
					break
				}
			}
		}
		listCmd := m.loadSelectedBuilds()
		if msg.action == buildActionStop && m.buildDetail != nil {
			return m, tea.Batch(listCmd, m.openSelectedBuild())
		}
		return m, listCmd
	}
	return m, nil
}

func buildActionAccepted(action buildAction) string {
	switch action {
	case buildActionLatest:
		return "Accepted · build and deploy submitted; completion is not yet observed"
	case buildActionRetry:
		return "Accepted · retry submitted; completion is not yet observed"
	case buildActionStop:
		return "Accepted · stop submitted; final state is not yet observed"
	}
	return "Accepted"
}
