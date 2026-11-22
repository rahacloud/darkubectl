package browser

import (
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	applyCICDEditValue1024    = 1024
	applyCICDEditValue128     = 128
	applyCICDEditValue2       = 2
	applyCICDEditValue3       = 3
	applyCICDEditValue4       = 4
	applyCICDEditValue5       = 5
	applyCICDEditValue6       = 6
	beginCICDEditValue7       = 7
	cicdNewArgumentFieldIndex = -2
)

const cicdSettingsFields = 8

func autoDeployLabel(value *bool) string {
	if value == nil {
		return "—"
	}
	if *value {
		return "On push"
	}
	return "Off"
}

func settingsFromApp(app client.App) client.GitBuildSettings {
	var autodeploy *bool
	if app.GitAutodeploy != nil {
		value := *app.GitAutodeploy
		autodeploy = &value
	}
	parsedArgs := buildArgs(app.GitBuildArgs)
	args := make([]client.GitBuildArg, 0, len(parsedArgs))
	for _, arg := range parsedArgs {
		args = append(args, client.GitBuildArg{Key: arg.name, Value: arg.value})
	}
	return client.GitBuildSettings{BuildMethod: app.BuildMethod, RepoURL: app.GitRepoURL, Branch: app.GitBranch, Provider: app.GitProvider,
		Dockerfile: app.GitDockerfile, Context: app.GitContext, Workdir: app.GitWorkdir,
		Builder: app.Builder, Autodeploy: autodeploy, Args: args}
}

func (m *browser) ensureCICDSettings() {
	if m.cicdSettingsLoaded || !m.selectedAppActive() || (m.selectedApp().BuildMethod != client.BuildMethodWebhook &&
		m.selectedApp().BuildMethod != client.BuildMethodGitlabCI) {
		return
	}
	s := settingsFromApp(m.selectedApp())
	m.cicdSettingsOriginal, m.cicdSettingsDraft = s, cloneGitSettings(s)
	m.cicdSettingsLoaded = true
}

func cloneGitSettings(s client.GitBuildSettings) client.GitBuildSettings {
	s.Args = append([]client.GitBuildArg(nil), s.Args...)
	return s
}

func (m *browser) cicdSettingsChanged() bool {
	return !m.cicdSettingsOriginal.Equal(m.cicdSettingsDraft)
}

func (m *browser) cicdRemovedArgs() []client.GitBuildArg {
	var removed []client.GitBuildArg
	for _, original := range m.cicdSettingsOriginal.Args {
		found := false
		for _, arg := range m.cicdSettingsDraft.Args {
			if arg.Key == original.Key {
				found = true
				break
			}
		}
		if !found {
			removed = append(removed, original)
		}
	}
	return removed
}

func (m *browser) cicdSelectedArgument() (client.GitBuildArg, bool) {
	i := m.cicdSettingsCursor - cicdSettingsFields
	if i >= 0 && i < len(m.cicdSettingsDraft.Args) {
		return m.cicdSettingsDraft.Args[i], true
	}
	i -= len(m.cicdSettingsDraft.Args) + 1
	removed := m.cicdRemovedArgs()
	if m.allowWrites && i >= 0 && i < len(removed) {
		return removed[i], true
	}
	return client.GitBuildArg{}, false
}

func (m *browser) cicdSettingsRows() int {
	rows := cicdSettingsFields + len(m.cicdSettingsDraft.Args)
	if m.allowWrites {
		rows += 1 + len(m.cicdRemovedArgs())
	} else if len(m.cicdSettingsDraft.Args) == 0 {
		rows++
	}
	return rows
}

func (m *browser) moveCICDSettings(delta int) {
	rows := []int{0, 1, 4, 5, 6, 7}
	if len(m.cicdSettingsDraft.Args) == 0 && !m.allowWrites {
		rows = append(rows, cicdSettingsFields)
	} else {
		for i := range len(m.cicdSettingsDraft.Args) {
			rows = append(rows, cicdSettingsFields+i)
		}
	}
	if m.allowWrites {
		rows = append(rows, cicdSettingsFields+len(m.cicdSettingsDraft.Args))
		for i := range m.cicdRemovedArgs() {
			rows = append(rows, cicdSettingsFields+len(m.cicdSettingsDraft.Args)+1+i)
		}
	}
	index := 0
	for i, row := range rows {
		if row == m.cicdSettingsCursor {
			index = i
			break
		}
	}
	index = max(0, min(len(rows)-1, index+delta))
	m.cicdSettingsCursor = rows[index]
}

func (m *browser) handleCICDSettingsKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.cicdEditing {
		return m.handleCICDEditingKey(msg)
	}
	if m.cicdReview {
		return m.handleCICDReviewKey(msg.String())
	}
	if m.cicdSaving {
		return nil, true
	}
	if m.cicdSettingsDraft.BuildMethod == client.BuildMethodGitlabCI {
		return m.handleGitlabSettingsKey(msg.String())
	}
	if m.cicdHistoryFocus {
		return m.handleCICDHistoryKey(msg.String())
	}
	return m.handleCICDNavigationKey(msg.String())
}

func (m *browser) handleCICDEditingKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case browserKeyEscape:
		m.cicdEditField, m.cicdEditValue, m.cicdEditKey = -1, "", ""
		m.cicdEditArg, m.cicdEditAdding, m.cicdEditing = false, false, false
	case keyCtrlU:
		m.cicdEditValue = ""
	case keyBackspace, keyCtrlH:
		if len(m.cicdEditValue) > 0 {
			r := []rune(m.cicdEditValue)
			m.cicdEditValue = string(r[:len(r)-1])
		}
	case keyEnter, inputKeyTab:
		if m.cicdEditAdding && m.cicdEditKey == "" {
			if strings.TrimSpace(m.cicdEditValue) == "" {
				m.cicdSettingsNotice = "Argument key cannot be empty"
				return nil, true
			}
			m.cicdEditKey, m.cicdEditValue = strings.TrimSpace(m.cicdEditValue), ""
			return nil, true
		}
		if msg.String() == inputKeyTab && !m.cicdEditArg {
			return nil, true
		}
		m.applyCICDEdit()
	default:
		if msg.Text != "" {
			m.cicdEditValue += safeText(msg.Text)
		}
	}
	return nil, true
}

func (m *browser) handleCICDReviewKey(key string) (tea.Cmd, bool) {
	switch key {
	case browserKeyEscape, "n":
		m.cicdReview = false
	case "y", keyEnter:
		m.cicdReview = false
		return m.saveCICDSettings(), true
	}
	return nil, true
}

func (m *browser) handleGitlabSettingsKey(key string) (tea.Cmd, bool) {
	switch key {
	case "t":
		if m.allowWrites {
			m.cicdSettingsDraft.BuildMethod = client.BuildMethodWebhook
			m.cicdSettingsNotice = "Deployment type staged; review and save to apply"
		}
		return nil, true
	case "s":
		if m.allowWrites && m.cicdSettingsChanged() {
			m.cicdReview = true
		} else {
			m.cicdSettingsNotice = messageNoStagedChanges
		}
		return nil, true
	case "x":
		if m.allowWrites {
			m.cicdSettingsDraft = cloneGitSettings(m.cicdSettingsOriginal)
			m.cicdSettingsNotice = messageChangesDiscarded
		}
		return nil, true
	case "v", "c", "y":
		return nil, false
	case "[", "]":
		return nil, false
	default:
		return nil, true
	}
}

func (m *browser) handleCICDHistoryKey(key string) (tea.Cmd, bool) {
	switch key {
	case "up", "k":
		if m.buildListCursor <= 0 {
			m.cicdHistoryFocus = false
			m.cicdSettingsCursor = m.cicdSettingsRows() - 1
			return nil, true
		}
		m.moveBuildSelection(-1)
		return nil, true
	case keyDown, "j":
		m.moveBuildSelection(1)
		return nil, true
	case inputKeyTab, inputKeyHome:
		m.cicdHistoryFocus = false
		return nil, true
	case keyEnter:
		return m.openSelectedBuild(), true
	}
	return nil, false
}

func (m *browser) handleCICDNavigationKey(key string) (tea.Cmd, bool) {
	if handled := m.handleCICDWriteKey(key); handled {
		return nil, true
	}
	switch key {
	case "up", "k":
		m.moveCICDSettings(-1)
		return nil, true
	case keyDown, "j":
		if m.cicdSettingsCursor == m.cicdSettingsRows()-1 && m.selectedApp().BuildMethod == client.BuildMethodWebhook {
			m.cicdHistoryFocus = true
		} else {
			m.moveCICDSettings(1)
		}
		return nil, true
	case inputKeyTab:
		if m.selectedApp().BuildMethod == client.BuildMethodWebhook {
			m.cicdHistoryFocus = true
		}
		return nil, true
	case "r":
		if m.cicdSettingsChanged() {
			m.cicdSettingsNotice = messageSaveOrDiscardBeforeRefresh
			return nil, true
		}
		m.cicdSettingsLoaded = false
		m.cicdSettingsNotice = ""
		m.cicdSettingsCursor = 0
		return m.loadSelectedSummary(), true
	case keyEnter:
		switch {
		case m.allowWrites && m.cicdSettingsCursor == cicdSettingsFields+len(m.cicdSettingsDraft.Args):
			m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args)
			m.cicdEditAdding, m.cicdEditArg, m.cicdEditing = true, true, true
			m.cicdEditKey, m.cicdEditValue, m.cicdEditField = "", "", cicdNewArgumentFieldIndex
		case m.allowWrites && m.cicdSettingsCursor > cicdSettingsFields+len(m.cicdSettingsDraft.Args):
			m.restoreCICDArgument()
		default:
			m.beginCICDEdit()
		}
		return nil, true
	}
	return nil, false
}

func (m *browser) handleCICDWriteKey(key string) bool {
	switch key {
	case "e":
		if m.allowWrites {
			m.beginCICDEdit()
		}
	case "t":
		if m.allowWrites {
			m.cicdSettingsDraft.BuildMethod = client.BuildMethodGitlabCI
			m.cicdSettingsNotice = "Deployment type staged; review and save to apply"
		}
	case "+":
		if m.allowWrites {
			m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args)
			m.cicdEditAdding, m.cicdEditArg = true, true
			m.cicdEditKey, m.cicdEditValue, m.cicdEditField = "", "", cicdNewArgumentFieldIndex
			m.cicdEditing = true
		}
	case "d":
		if m.allowWrites {
			if m.cicdSettingsCursor > cicdSettingsFields+len(m.cicdSettingsDraft.Args) {
				m.restoreCICDArgument()
			} else {
				m.deleteCICDArgument()
			}
		}
	case "s":
		if m.allowWrites {
			if m.cicdSettingsChanged() {
				m.cicdReview = true
			} else {
				m.cicdSettingsNotice = "No staged changes"
			}
		}
	case "x":
		if m.allowWrites {
			m.cicdSettingsDraft = cloneGitSettings(m.cicdSettingsOriginal)
			m.cicdSettingsNotice = "Staged changes discarded"
		}
	default:
		return false
	}
	return true
}

func (m *browser) restoreCICDArgument() {
	arg, ok := m.cicdSelectedArgument()
	if !ok {
		return
	}
	position := len(m.cicdSettingsDraft.Args)
	for i, original := range m.cicdSettingsOriginal.Args {
		if original.Key != arg.Key {
			continue
		}
		for _, next := range m.cicdSettingsOriginal.Args[i+1:] {
			for j, draft := range m.cicdSettingsDraft.Args {
				if draft.Key == next.Key && j < position {
					position = j
				}
			}
		}
		break
	}
	m.cicdSettingsDraft.Args = append(m.cicdSettingsDraft.Args, client.GitBuildArg{})
	copy(m.cicdSettingsDraft.Args[position+1:], m.cicdSettingsDraft.Args[position:])
	m.cicdSettingsDraft.Args[position] = arg
	m.cicdSettingsCursor = cicdSettingsFields + position
}

func (m *browser) beginCICDEdit() {
	if !m.allowWrites || !m.selectedAppActive() || (m.selectedApp().BuildMethod != client.BuildMethodWebhook &&
		m.selectedApp().BuildMethod != client.BuildMethodGitlabCI) {
		return
	}
	row := m.cicdSettingsCursor
	if row == beginCICDEditValue7 {
		m.toggleCICDAutodeploy()
		return
	}
	if row >= cicdSettingsFields {
		arg := row - cicdSettingsFields
		if arg >= len(m.cicdSettingsDraft.Args) {
			return
		}
		m.cicdEditArg, m.cicdEditAdding = true, false
		m.cicdEditKey, m.cicdEditValue = m.cicdSettingsDraft.Args[arg].Key, m.cicdSettingsDraft.Args[arg].Value
		m.cicdEditField = row
		m.cicdEditing = true
		return
	}
	fieldValues := map[int]string{
		0: m.cicdSettingsDraft.RepoURL,
		1: m.cicdSettingsDraft.Branch,
		2: m.cicdSettingsDraft.Provider,
		3: m.cicdSettingsDraft.Builder,
		4: m.cicdSettingsDraft.Dockerfile,
		5: m.cicdSettingsDraft.Context,
		6: m.cicdSettingsDraft.Workdir,
	}
	value, ok := fieldValues[row]
	if !ok {
		return
	}
	m.cicdEditValue, m.cicdEditField = value, row
	m.cicdEditing = true
}

func (m *browser) toggleCICDAutodeploy() {
	if m.cicdSettingsDraft.Autodeploy == nil {
		value := true
		m.cicdSettingsDraft.Autodeploy = &value
		return
	}
	value := !*m.cicdSettingsDraft.Autodeploy
	m.cicdSettingsDraft.Autodeploy = &value
}

func (m *browser) applyCICDEdit() {
	value := m.cicdEditValue
	if m.cicdEditArg {
		if !m.applyCICDArgumentEdit(value) {
			return
		}
	} else if !m.applyCICDFieldEdit(value) {
		return
	}
	m.cicdEditField, m.cicdEditValue, m.cicdEditKey = -1, "", ""
	m.cicdEditArg, m.cicdEditAdding, m.cicdEditing = false, false, false
	m.cicdSettingsNotice = ""
}

func (m *browser) applyCICDArgumentEdit(value string) bool {
	key := strings.TrimSpace(m.cicdEditKey)
	if !m.cicdEditAdding {
		index := m.cicdEditField - cicdSettingsFields
		if index >= 0 && index < len(m.cicdSettingsDraft.Args) {
			m.cicdSettingsDraft.Args[index].Value = value
		}
		return true
	}
	if key == "" {
		m.cicdSettingsNotice = "Argument key cannot be empty"
		return false
	}
	for _, arg := range m.cicdSettingsDraft.Args {
		if arg.Key == key {
			m.cicdSettingsNotice = "Argument key already exists"
			return false
		}
	}
	m.cicdSettingsDraft.Args = append(m.cicdSettingsDraft.Args, client.GitBuildArg{Key: key, Value: value})
	m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args) - 1
	return true
}

func (m *browser) applyCICDFieldEdit(value string) bool {
	switch m.cicdEditField {
	case 0:
		if utf8.RuneCountInString(value) > applyCICDEditValue1024 {
			m.cicdSettingsNotice = "Repository URL is longer than 1024 characters"
			return false
		}
		m.cicdSettingsDraft.RepoURL = value
	case 1:
		if utf8.RuneCountInString(value) > applyCICDEditValue128 {
			m.cicdSettingsNotice = "Branch is longer than 128 characters"
			return false
		}
		m.cicdSettingsDraft.Branch = value
	case applyCICDEditValue2:
		if value != "Github" && value != "Gitlab" {
			m.cicdSettingsNotice = "Provider must be Github or Gitlab"
			return false
		}
		m.cicdSettingsDraft.Provider = value
	case applyCICDEditValue3:
		if value != "dockerfile" && value != "heroku_buildpacks" {
			m.cicdSettingsNotice = "Builder must be dockerfile or heroku_buildpacks"
			return false
		}
		m.cicdSettingsDraft.Builder = value
	case applyCICDEditValue4:
		if utf8.RuneCountInString(value) > applyCICDEditValue128 {
			m.cicdSettingsNotice = "Dockerfile path is longer than 128 characters"
			return false
		}
		m.cicdSettingsDraft.Dockerfile = value
	case applyCICDEditValue5:
		if utf8.RuneCountInString(value) > applyCICDEditValue128 {
			m.cicdSettingsNotice = "Build context is longer than 128 characters"
			return false
		}
		m.cicdSettingsDraft.Context = value
	case applyCICDEditValue6:
		if utf8.RuneCountInString(value) > applyCICDEditValue128 {
			m.cicdSettingsNotice = "Workdir is longer than 128 characters"
			return false
		}
		m.cicdSettingsDraft.Workdir = value
	}
	return true
}

func (m *browser) deleteCICDArgument() {
	i := m.cicdSettingsCursor - cicdSettingsFields
	if i < 0 || i >= len(m.cicdSettingsDraft.Args) {
		return
	}
	m.cicdSettingsDraft.Args = append(m.cicdSettingsDraft.Args[:i], m.cicdSettingsDraft.Args[i+1:]...)
	m.cicdSettingsCursor = min(m.cicdSettingsCursor, m.cicdSettingsRows()-1)
}

func (m *browser) saveCICDSettings() tea.Cmd {
	if !m.allowWrites {
		m.cicdSettingsNotice = "Writes are disabled · relaunch with --allow-writes"
		return nil
	}
	if m.updateGitBuildSettings == nil && !m.actionIsMock() {
		m.cicdSettingsNotice = "CI/CD settings updates are unavailable"
		return nil
	}
	tenant, id := m.tenant, m.selectedDetailTarget()
	expected, desired := cloneGitSettings(m.cicdSettingsOriginal), cloneGitSettings(m.cicdSettingsDraft)
	m.cicdSaving, m.cicdSettingsNotice = true, "Saving and verifying settings"
	return func() tea.Msg {
		if m.actionIsMock() {
			return cicdSettingsWriteResult{tenant: tenant, appID: id, mock: true}
		}
		return cicdSettingsWriteResult{tenant: tenant, appID: id, err: m.updateGitBuildSettings(m.ctx, tenant, id, expected, desired)}
	}
}

type cicdSettingsWriteResult struct {
	tenant, appID string
	mock          bool
	err           error
}

func (m *browser) handleCICDSettingsWrite(msg cicdSettingsWriteResult) tea.Cmd {
	if msg.tenant != m.tenant || msg.appID != m.selectedDetailTarget() {
		m.cicdSaving = false
		return nil
	}
	m.cicdSaving = false
	if msg.err != nil {
		m.cicdSettingsNotice = "Save failed · staged changes kept"
		if strings.Contains(strings.ToLower(msg.err.Error()), "changed since") {
			m.cicdSettingsNotice = "Settings changed elsewhere · refresh before saving again"
		}
		return nil
	}
	m.cicdSettingsOriginal = cloneGitSettings(m.cicdSettingsDraft)
	m.cicdSettingsNotice = "Saved and verified"
	if msg.mock {
		m.cicdSettingsNotice = "MOCK · simulated save"
		return nil
	}
	m.detailState.reset()
	return m.loadSelectedSummary()
}

func (m *browser) cicdSettingsReviewLines() []string {
	lines := []string{sectionHeading("Review CI/CD changes"), summaryField("Deployment type",
		cicdMethodLabel(m.cicdSettingsDraft.BuildMethod))}
	add := func(name, old, next string) {
		if old != next {
			lines = append(lines, paint("− "+safeText(name)+" = "+old, browserNumber, false), paint("+ "+safeText(name)+" = "+next,
				browserAccent, false))
		}
	}
	a, b := m.cicdSettingsOriginal, m.cicdSettingsDraft
	if a.BuildMethod != b.BuildMethod {
		lines = append(lines, paint("− Deployment type = "+cicdMethodLabel(a.BuildMethod), browserNumber, false),
			paint("+ Deployment type = "+cicdMethodLabel(b.BuildMethod), browserAccent, false))
	}
	add("Repository", redactRepoURL(a.RepoURL), redactRepoURL(b.RepoURL))
	add("Branch", a.Branch, b.Branch)
	add("Provider", a.Provider, b.Provider)
	add("Builder", a.Builder, b.Builder)
	add("Dockerfile", a.Dockerfile, b.Dockerfile)
	add("Context", a.Context, b.Context)
	add("Workdir", a.Workdir, b.Workdir)
	add("Auto deploy", autoDeployLabel(a.Autodeploy), autoDeployLabel(b.Autodeploy))
	if cicdBuildArgsChanged(a.Args, b.Args) {
		lines = append(lines, sectionHeading("Build arguments"))
		lines = append(lines, cicdSettingsReviewArgumentLines(a.Args, b.Args, m.allowSecrets)...)
	}
	lines = append(lines, "", summaryField("Save", "write settings and verify read-back"))
	return lines
}

func cicdBuildArgsChanged(old, next []client.GitBuildArg) bool {
	if len(old) != len(next) {
		return true
	}
	values := buildArgumentValues(old)
	for _, arg := range next {
		if value, ok := values[arg.Key]; !ok || value != arg.Value {
			return true
		}
	}
	return false
}

func cicdSettingsReviewArgumentLines(old, next []client.GitBuildArg, reveal bool) []string {
	oldValues, nextValues := buildArgumentValues(old), buildArgumentValues(next)
	keys := make(map[string]bool, len(oldValues)+len(nextValues))
	for key := range oldValues {
		keys[key] = true
	}
	for key := range nextValues {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	var lines []string
	for _, key := range ordered {
		oldValue, hadOld := oldValues[key]
		nextValue, hasNext := nextValues[key]
		if hadOld && hasNext && oldValue == nextValue {
			continue
		}
		if hadOld {
			lines = append(lines, "− "+safeText(key)+" = "+cicdReviewArgumentValue(oldValue, reveal))
		}
		if hasNext {
			lines = append(lines, "+ "+safeText(key)+" = "+cicdReviewArgumentValue(nextValue, reveal))
		}
	}
	return lines
}

func buildArgumentValues(args []client.GitBuildArg) map[string]string {
	values := make(map[string]string, len(args))
	for _, arg := range args {
		values[arg.Key] = arg.Value
	}
	return values
}

func cicdReviewArgumentValue(value string, reveal bool) string {
	if !reveal {
		return maskedValueGlyphs
	}
	return safeText(value)
}
