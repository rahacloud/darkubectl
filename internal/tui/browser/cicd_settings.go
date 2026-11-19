package browser

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
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
	if m.cicdSettingsLoaded || !m.selectedAppActive() || (m.selectedApp().BuildMethod != client.BuildMethodWebhook && m.selectedApp().BuildMethod != client.BuildMethodGitlabCI) {
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

func (m *browser) cicdSettingsRows() int {
	rows := cicdSettingsFields + max(1, len(m.cicdSettingsDraft.Args))
	if m.allowWrites {
		rows++
	}
	return rows
}

func (m *browser) moveCICDSettings(delta int) {
	rows := []int{0, 1, 4, 5, 6, 7}
	if len(m.cicdSettingsDraft.Args) == 0 {
		rows = append(rows, cicdSettingsFields)
	} else {
		for i := 0; i < len(m.cicdSettingsDraft.Args); i++ {
			rows = append(rows, cicdSettingsFields+i)
		}
	}
	if m.allowWrites {
		rows = append(rows, cicdSettingsFields+len(m.cicdSettingsDraft.Args))
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
		switch msg.String() {
		case "esc":
			m.cicdEditField, m.cicdEditValue, m.cicdEditKey = -1, "", ""
			m.cicdEditArg, m.cicdEditAdding, m.cicdEditing = false, false, false
		case "ctrl+u":
			m.cicdEditValue = ""
		case "backspace", "ctrl+h":
			if len(m.cicdEditValue) > 0 {
				r := []rune(m.cicdEditValue)
				m.cicdEditValue = string(r[:len(r)-1])
			}
		case "enter":
			if m.cicdEditAdding && m.cicdEditKey == "" {
				if strings.TrimSpace(m.cicdEditValue) == "" {
					m.cicdSettingsNotice = "Argument key cannot be empty"
					return nil, true
				}
				m.cicdEditKey, m.cicdEditValue = strings.TrimSpace(m.cicdEditValue), ""
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
	if m.cicdReview {
		switch msg.String() {
		case "esc", "n":
			m.cicdReview = false
		case "y", "enter":
			m.cicdReview = false
			return m.saveCICDSettings(), true
		}
		return nil, true
	}
	if m.cicdSaving {
		return nil, true
	}
	if m.cicdSettingsDraft.BuildMethod == client.BuildMethodGitlabCI {
		switch msg.String() {
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
				m.cicdSettingsNotice = "No staged changes"
			}
			return nil, true
		case "x":
			if m.allowWrites {
				m.cicdSettingsDraft = cloneGitSettings(m.cicdSettingsOriginal)
				m.cicdSettingsNotice = "Staged changes discarded"
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
	if m.cicdHistoryFocus {
		switch msg.String() {
		case "up", "k":
			if m.buildListCursor <= 0 {
				m.cicdHistoryFocus = false
				m.cicdSettingsCursor = m.cicdSettingsRows() - 1
				return nil, true
			}
			m.moveBuildSelection(-1)
			return nil, true
		case "down", "j":
			m.moveBuildSelection(1)
			return nil, true
		case "tab", "home":
			m.cicdHistoryFocus = false
			return nil, true
		case "enter":
			return m.openSelectedBuild(), true
		}
		return nil, false
	}
	switch msg.String() {
	case "up", "k":
		m.moveCICDSettings(-1)
		return nil, true
	case "down", "j":
		if m.cicdSettingsCursor == m.cicdSettingsRows()-1 && m.selectedApp().BuildMethod == client.BuildMethodWebhook {
			m.cicdHistoryFocus = true
		} else {
			m.moveCICDSettings(1)
		}
		return nil, true
	case "tab":
		if m.selectedApp().BuildMethod == client.BuildMethodWebhook {
			m.cicdHistoryFocus = true
		}
		return nil, true
	case "e":
		if m.allowWrites {
			m.beginCICDEdit()
		}
		return nil, true
	case "t":
		if m.allowWrites {
			m.cicdSettingsDraft.BuildMethod = client.BuildMethodGitlabCI
			m.cicdSettingsNotice = "Deployment type staged; review and save to apply"
		}
		return nil, true
	case "+":
		if m.allowWrites {
			m.cicdSettingsCursor = m.cicdSettingsRows() - 1
			m.cicdEditAdding = true
			m.cicdEditArg = true
			m.cicdEditKey = ""
			m.cicdEditValue = ""
			m.cicdEditField = -2
			m.cicdEditing = true
		}
		return nil, true
	case "d":
		if m.allowWrites {
			m.deleteCICDArgument()
		}
		return nil, true
	case "s":
		if m.allowWrites {
			if m.cicdSettingsChanged() {
				m.cicdReview = true
			} else {
				m.cicdSettingsNotice = "No staged changes"
			}
		}
		return nil, true
	case "x":
		if m.allowWrites {
			m.cicdSettingsDraft = cloneGitSettings(m.cicdSettingsOriginal)
			m.cicdSettingsNotice = "Staged changes discarded"
		}
		return nil, true
	case "r":
		if m.cicdSettingsChanged() {
			m.cicdSettingsNotice = "Save or discard staged changes before refreshing"
			return nil, true
		}
		m.cicdSettingsLoaded = false
		m.cicdSettingsNotice = ""
		m.cicdSettingsCursor = 0
		return m.loadSelectedSummary(), true
	case "enter":
		if m.allowWrites && m.cicdSettingsCursor == cicdSettingsFields+len(m.cicdSettingsDraft.Args) {
			m.cicdSettingsCursor = m.cicdSettingsRows() - 1
			m.cicdEditAdding, m.cicdEditArg, m.cicdEditing = true, true, true
			m.cicdEditKey, m.cicdEditValue, m.cicdEditField = "", "", -2
		} else {
			m.beginCICDEdit()
		}
		return nil, true
	}
	return nil, false
}

func (m *browser) beginCICDEdit() {
	if !m.allowWrites || !m.selectedAppActive() || (m.selectedApp().BuildMethod != client.BuildMethodWebhook && m.selectedApp().BuildMethod != client.BuildMethodGitlabCI) {
		return
	}
	row := m.cicdSettingsCursor
	if row < cicdSettingsFields {
		if row == 7 {
			if m.cicdSettingsDraft.Autodeploy == nil {
				value := true
				m.cicdSettingsDraft.Autodeploy = &value
			} else {
				value := !*m.cicdSettingsDraft.Autodeploy
				m.cicdSettingsDraft.Autodeploy = &value
			}
			return
		}
		fields := []string{m.cicdSettingsDraft.RepoURL, m.cicdSettingsDraft.Branch, m.cicdSettingsDraft.Provider, m.cicdSettingsDraft.Builder, m.cicdSettingsDraft.Dockerfile, m.cicdSettingsDraft.Context, m.cicdSettingsDraft.Workdir}
		m.cicdEditValue, m.cicdEditField = fields[row], row
		m.cicdEditing = true
		return
	}
	arg := row - cicdSettingsFields
	if arg >= len(m.cicdSettingsDraft.Args) {
		return
	}
	m.cicdEditArg, m.cicdEditAdding = true, false
	m.cicdEditKey, m.cicdEditValue = m.cicdSettingsDraft.Args[arg].Key, m.cicdSettingsDraft.Args[arg].Value
	m.cicdEditField = row
	m.cicdEditing = true
}

func (m *browser) applyCICDEdit() {
	value := m.cicdEditValue
	if m.cicdEditArg {
		key := strings.TrimSpace(m.cicdEditKey)
		if m.cicdEditAdding {
			if key == "" {
				m.cicdSettingsNotice = "Argument key cannot be empty"
				return
			}
			for _, a := range m.cicdSettingsDraft.Args {
				if a.Key == key {
					m.cicdSettingsNotice = "Argument key already exists"
					return
				}
			}
			m.cicdSettingsDraft.Args = append(m.cicdSettingsDraft.Args, client.GitBuildArg{Key: key, Value: value})
			m.cicdSettingsCursor = cicdSettingsFields + len(m.cicdSettingsDraft.Args) - 1
		} else {
			i := m.cicdEditField - cicdSettingsFields
			if i >= 0 && i < len(m.cicdSettingsDraft.Args) {
				m.cicdSettingsDraft.Args[i].Value = value
			}
		}
	} else {
		switch m.cicdEditField {
		case 0:
			if utf8.RuneCountInString(value) > 1024 {
				m.cicdSettingsNotice = "Repository URL is longer than 1024 characters"
				return
			}
			m.cicdSettingsDraft.RepoURL = value
		case 1:
			if utf8.RuneCountInString(value) > 128 {
				m.cicdSettingsNotice = "Branch is longer than 128 characters"
				return
			}
			m.cicdSettingsDraft.Branch = value
		case 2:
			if value != "Github" && value != "Gitlab" {
				m.cicdSettingsNotice = "Provider must be Github or Gitlab"
				return
			}
			m.cicdSettingsDraft.Provider = value
		case 3:
			if value != "dockerfile" && value != "heroku_buildpacks" {
				m.cicdSettingsNotice = "Builder must be dockerfile or heroku_buildpacks"
				return
			}
			m.cicdSettingsDraft.Builder = value
		case 4:
			if utf8.RuneCountInString(value) > 128 {
				m.cicdSettingsNotice = "Dockerfile path is longer than 128 characters"
				return
			}
			m.cicdSettingsDraft.Dockerfile = value
		case 5:
			if utf8.RuneCountInString(value) > 128 {
				m.cicdSettingsNotice = "Build context is longer than 128 characters"
				return
			}
			m.cicdSettingsDraft.Context = value
		case 6:
			if utf8.RuneCountInString(value) > 128 {
				m.cicdSettingsNotice = "Workdir is longer than 128 characters"
				return
			}
			m.cicdSettingsDraft.Workdir = value
		}
	}
	m.cicdEditField, m.cicdEditValue, m.cicdEditKey = -1, "", ""
	m.cicdEditArg, m.cicdEditAdding, m.cicdEditing = false, false, false
	m.cicdSettingsNotice = ""
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
	m.cicdSaving, m.cicdSettingsNotice = true, "Saving and verifying settings…"
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
	lines := []string{sectionHeading("Review CI/CD changes"), summaryField("Deployment type", cicdMethodLabel(m.cicdSettingsDraft.BuildMethod))}
	add := func(name, old, next string) {
		if old != next {
			lines = append(lines, paint("− "+safeText(name)+" = "+old, browserNumber, false), paint("+ "+safeText(name)+" = "+next, browserAccent, false))
		}
	}
	a, b := m.cicdSettingsOriginal, m.cicdSettingsDraft
	if a.BuildMethod != b.BuildMethod {
		lines = append(lines, paint("− Deployment type = "+cicdMethodLabel(a.BuildMethod), browserNumber, false), paint("+ Deployment type = "+cicdMethodLabel(b.BuildMethod), browserAccent, false))
	}
	add("Repository", redactRepoURL(a.RepoURL), redactRepoURL(b.RepoURL))
	add("Branch", a.Branch, b.Branch)
	add("Provider", a.Provider, b.Provider)
	add("Builder", a.Builder, b.Builder)
	add("Dockerfile", a.Dockerfile, b.Dockerfile)
	add("Context", a.Context, b.Context)
	add("Workdir", a.Workdir, b.Workdir)
	add("Auto deploy", autoDeployLabel(a.Autodeploy), autoDeployLabel(b.Autodeploy))
	if len(a.Args) != len(b.Args) || fmt.Sprint(a.Args) != fmt.Sprint(b.Args) {
		lines = append(lines, sectionHeading("Build arguments"))
		oldArgs := map[string]string{}
		newArgs := map[string]string{}
		for _, arg := range a.Args {
			oldArgs[arg.Key] = arg.Value
		}
		for _, arg := range b.Args {
			newArgs[arg.Key] = arg.Value
		}
		keys := map[string]bool{}
		for k := range oldArgs {
			keys[k] = true
		}
		for k := range newArgs {
			keys[k] = true
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			old, hadOld := oldArgs[key]
			next, hasNext := newArgs[key]
			if hadOld && hasNext && old == next {
				continue
			}
			if hadOld {
				lines = append(lines, "− "+safeText(key)+" = ••••••••")
			}
			if hasNext {
				lines = append(lines, "+ "+safeText(key)+" = ••••••••")
			}
		}
	}
	lines = append(lines, "", summaryField("Save", "write settings and verify read-back"))
	return lines
}
