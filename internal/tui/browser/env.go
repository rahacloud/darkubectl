package browser

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	handleEnvKeyValue8  = 8
	envDiffHeadingCount = 1
	envDiffSummaryLines = 2
)

const (
	envEditNone uint8 = iota
	envEditPlain
	envEditSecret
	envAddPlain
	envAddSecret
)

type envResult struct {
	meta    requestMeta
	plain   []client.EnvVar
	secrets []string
	err     error
}
type envWriteResult struct {
	appID string
	err   error
}
type envCopyResult struct{ err error }
type envRevealResult struct {
	appID, name, value string
	err                error
}

func (m *browser) loadEnv() tea.Cmd {
	id := m.selectedDetailTarget()
	if id == "" || m.environment == nil {
		m.envState.unavailable(errEnvironmentVariablesAreUnavailable)
		return nil
	}
	meta, ctx := m.startRequest(requestEnv, m.tenant, id, m.selectionGeneration)
	m.envState.begin(meta, m.requestTime())
	return func() tea.Msg {
		plain, secrets, err := m.environment(ctx, meta.tenant, id)
		return envResult{meta: meta, plain: plain, secrets: secrets, err: err}
	}
}

func (m *browser) handleEnvResult(msg envResult) tea.Model {
	m.finishRequest(requestEnv, msg.meta)
	if !m.envState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		msg.meta.appID != m.selectedDetailTarget() {
		return m
	}
	if msg.err != nil {
		m.envState.fail(errEnvironmentRequestFailed)
		return m
	}
	m.envPlain, m.envSecretNames = append([]client.EnvVar(nil), msg.plain...), append([]string(nil), msg.secrets...)
	sort.Slice(m.envPlain, func(i, j int) bool { return m.envPlain[i].Name < m.envPlain[j].Name })
	sort.Strings(m.envSecretNames)
	m.envOriginalPlain = append([]client.EnvVar(nil), m.envPlain...)
	m.envOriginalSecretNames = append([]string(nil), m.envSecretNames...)
	m.envSecretValues = make(map[string]string)
	m.envDeletedPlain = make(map[string]bool)
	m.envDeletedSecrets = make(map[string]bool)
	m.envMessage = ""
	m.envCursor = min(m.envCursor, max(0, len(m.envPlain)+len(m.envSecretNames)-1))
	m.envState.succeed(len(m.envPlain)+len(m.envSecretNames) == 0, m.requestTime())
	return m
}

func (m *browser) envPaneLines() []string {
	if !m.selectedAppActive() {
		return []string{summaryField("App", "not selected")}
	}
	m.envCursorLine = 0
	if m.envConfirm {
		diff := m.envDiffLines()
		lines := make([]string, 0, envDiffHeadingCount+len(diff)+envDiffSummaryLines)
		lines = append(lines, sectionHeading("Review changes"))
		lines = append(lines, diff...)
		lines = append(lines, "", summaryField("Apply", "save changes and restart app pods?"))
		return lines
	}
	lines := []string{sectionHeading("Plain")}
	if m.envState.active() {
		lines = append(lines, summaryField("Environment", "loading "+m.spinnerGlyph()))
	}
	if m.envState.err != nil {
		lines = append(lines, summaryField("Environment", paint("unavailable", browserNumber, false)))
	}
	if len(m.envPlain) == 0 {
		lines = append(lines, summaryField("Variables", "none"))
	}
	for i, e := range m.envPlain {
		selected := m.envCursor == i
		if selected {
			m.envCursorLine = len(lines)
		}
		value := safeText(e.Value)
		if m.envEditMode == envEditPlain && selected {
			value = safeText(m.envEditValue) + "█"
		}
		row := fmt.Sprintf("%s %s %s", cursorGlyph(selected), envChangeMarker(m, e.Name, false), summaryField(safeText(e.Name), value))
		lines = append(lines, envRow(row, selected))
	}
	lines = append(lines, "", sectionHeading("Secret"))
	if len(m.envSecretNames) == 0 {
		lines = append(lines, summaryField("Variables", "none"))
	}
	for i, name := range m.envSecretNames {
		selected := m.envCursor == len(m.envPlain)+i
		if selected {
			m.envCursorLine = len(lines)
		}
		value := "••••••••"
		if name == m.envRevealName {
			value = safeText(m.envRevealValue)
		}
		if m.envEditMode == envEditSecret && selected {
			value = maskedInput(m.envEditValue) + "█"
		}
		lines = append(lines, envRow(fmt.Sprintf("%s %s %s", cursorGlyph(selected), envChangeMarker(m, name, true),
			summaryField(safeText(name), value)), selected))
	}
	if m.envHasChanges() {
		lines = append(lines, "", paint(fmt.Sprintf("%d staged change(s)", m.envChangeCount()), browserAccent, false))
	}
	if m.envMessage != "" && m.envMessage != secretAccessDisabled {
		lines = append(lines, "", paint(safeText(m.envMessage), browserMuted, false))
	}
	return m.appendEnvEditPrompt(lines)
}

func (m *browser) appendEnvEditPrompt(lines []string) []string {
	if m.envEditMode != envAddPlain && m.envEditMode != envAddSecret {
		return lines
	}
	if m.envEditName == "" {
		return append(lines, "", "Variable name  "+safeText(m.envEditValue)+"█")
	}
	value := safeText(m.envEditValue)
	if m.envEditMode == envEditSecret || m.envEditMode == envAddSecret {
		value = maskedInput(m.envEditValue)
	}
	return append(lines, "", value+"█")
}

func maskedInput(value string) string { return strings.Repeat("•", utf8.RuneCountInString(value)) }

func cursorGlyph(selected bool) string {
	if selected {
		return "›"
	}
	return " "
}
func envRow(s string, selected bool) string {
	if selected {
		return selectedRowStyle().Render(s)
	}
	return s
}

func envChangeMarker(m *browser, name string, secret bool) string {
	if secret {
		if m.envDeletedSecrets[name] {
			return "−"
		}
		if !containsEnvName(m.envOriginalSecretNames, name) {
			return "+"
		}
		if _, changed := m.envSecretValues[name]; changed {
			return "~"
		}
		return " "
	}
	if m.envDeletedPlain[name] {
		return "−"
	}
	for _, original := range m.envOriginalPlain {
		if original.Name == name {
			for _, current := range m.envPlain {
				if current.Name == name && current.Value != original.Value {
					return "~"
				}
			}
			return " "
		}
	}
	return "+"
}

func containsEnvName(names []string, name string) bool {
	return slices.Contains(names, name)
}

func (m *browser) envDesiredPlain() []client.EnvVar {
	plain := make([]client.EnvVar, 0, len(m.envPlain))
	for _, env := range m.envPlain {
		if !m.envDeletedPlain[env.Name] {
			plain = append(plain, env)
		}
	}
	return plain
}

func (m *browser) envDesiredSecrets() []string {
	names := make([]string, 0, len(m.envSecretNames))
	for _, name := range m.envSecretNames {
		if !m.envDeletedSecrets[name] {
			names = append(names, name)
		}
	}
	return names
}

func (m *browser) envHasChanges() bool { return m.envChangeCount() > 0 }

func (m *browser) envChangeCount() int {
	changes := 0
	currentPlain := m.envDesiredPlain()
	for _, original := range m.envOriginalPlain {
		found := false
		for _, current := range currentPlain {
			if current.Name == original.Name {
				found = true
				if current.Value != original.Value {
					changes++
				}
				break
			}
		}
		if !found {
			changes++
		}
	}
	for _, current := range currentPlain {
		if !containsEnvName(envNames(m.envOriginalPlain), current.Name) {
			changes++
		}
	}
	currentSecrets := m.envDesiredSecrets()
	for _, name := range m.envOriginalSecretNames {
		if !containsEnvName(currentSecrets, name) {
			changes++
		}
	}
	for _, name := range currentSecrets {
		if !containsEnvName(m.envOriginalSecretNames, name) {
			changes++
		} else if _, changed := m.envSecretValues[name]; changed {
			changes++
		}
	}
	return changes
}

func envNames(envs []client.EnvVar) []string {
	names := make([]string, 0, len(envs))
	for _, env := range envs {
		names = append(names, env.Name)
	}
	return names
}

func (m *browser) envDiffLines() []string {
	var lines []string
	originalPlain := make(map[string]string, len(m.envOriginalPlain))
	currentPlain := make(map[string]string, len(m.envPlain))
	plainNames := make(map[string]struct{}, len(m.envOriginalPlain)+len(m.envPlain))
	for _, env := range m.envOriginalPlain {
		originalPlain[env.Name] = env.Value
		plainNames[env.Name] = struct{}{}
	}
	for _, env := range m.envDesiredPlain() {
		currentPlain[env.Name] = env.Value
		plainNames[env.Name] = struct{}{}
	}
	orderedPlain := make([]string, 0, len(plainNames))
	for name := range plainNames {
		orderedPlain = append(orderedPlain, name)
	}
	sort.Strings(orderedPlain)
	lines = append(lines, sectionHeading("Plain"))
	for _, name := range orderedPlain {
		oldValue, hadOld := originalPlain[name]
		newValue, hasNew := currentPlain[name]
		if hadOld && hasNew && oldValue == newValue {
			continue
		}
		if hadOld {
			lines = append(lines, paint("− "+safeText(name)+" = "+safeText(oldValue), browserNumber, false))
		}
		if hasNew {
			lines = append(lines, paint("+ "+safeText(name)+" = "+safeText(newValue), browserAccent, false))
		}
	}
	lines = append(lines, "", sectionHeading("Secret"))
	currentSecrets := m.envDesiredSecrets()
	secretNames := append([]string(nil), m.envOriginalSecretNames...)
	for _, name := range currentSecrets {
		if !containsEnvName(secretNames, name) {
			secretNames = append(secretNames, name)
		}
	}
	sort.Strings(secretNames)
	for _, name := range secretNames {
		hadOld := containsEnvName(m.envOriginalSecretNames, name)
		hasNew := containsEnvName(currentSecrets, name)
		_, valueChanged := m.envSecretValues[name]
		switch {
		case hadOld && !hasNew:
			lines = append(lines, paint("− "+safeText(name), browserNumber, false))
		case !hadOld && hasNew:
			lines = append(lines, paint("+ "+safeText(name)+" = ••••••••", browserAccent, false))
		case valueChanged && hasNew:
			lines = append(lines, paint("~ "+safeText(name)+" = ••••••••", browserAccent, false))
		}
	}
	return lines
}

type envSelection struct {
	secret bool
	index  int
	name   string
	value  string
	ok     bool
}

func (m *browser) selectedEnv() envSelection {
	if m.envCursor < 0 || m.envCursor >= len(m.envPlain)+len(m.envSecretNames) {
		return envSelection{}
	}
	if m.envCursor < len(m.envPlain) {
		e := m.envPlain[m.envCursor]
		return envSelection{index: m.envCursor, name: e.Name, value: e.Value, ok: true}
	}
	i := m.envCursor - len(m.envPlain)
	return envSelection{secret: true, index: i, name: m.envSecretNames[i], ok: true}
}

func (m *browser) selectedEnvDeleted() bool {
	selection := m.selectedEnv()
	if !selection.ok {
		return false
	}
	if selection.secret {
		return m.envDeletedSecrets[selection.name]
	}
	return m.envDeletedPlain[selection.name]
}

func (m *browser) discardEnvChanges() {
	m.envPlain = append([]client.EnvVar(nil), m.envOriginalPlain...)
	m.envSecretNames = append([]string(nil), m.envOriginalSecretNames...)
	m.envSecretValues = make(map[string]string)
	m.envDeletedPlain = make(map[string]bool)
	m.envDeletedSecrets = make(map[string]bool)
	m.envEditMode = envEditNone
	m.envEditName, m.envEditValue = "", ""
	m.envConfirm = false
	m.envRevealName, m.envRevealValue = "", ""
	m.envMessage = messageChangesDiscarded
	m.envCursor = min(m.envCursor, max(0, len(m.envPlain)+len(m.envSecretNames)-1))
}

func (m *browser) handleEnvKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.envBusy {
		return nil
	}
	if m.envConfirm {
		return m.handleEnvConfirmKey(msg.String())
	}
	if m.envEditMode != envEditNone {
		return m.handleEnvEditingKey(msg)
	}
	return m.handleEnvBrowseKey(msg)
}

func (m *browser) handleEnvConfirmKey(key string) tea.Cmd {
	switch key {
	case "y", keyEnter:
		m.envConfirm = false
		return m.commitEnvChanges()
	case "n", browserKeyEscape:
		m.envConfirm = false
		m.envMessage = messageSaveCancelled
	case "up", "k":
		m.summaryScroll = max(0, m.summaryScroll-1)
	case keyDown, "j":
		m.summaryScroll++
	case inputKeyPgUp:
		m.summaryScroll = max(0, m.summaryScroll-max(1, m.height-handleEnvKeyValue8))
	case inputKeyPgDown:
		m.summaryScroll += max(1, m.height-handleEnvKeyValue8)
	}
	return nil
}

func (m *browser) handleEnvEditingKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case browserKeyEscape:
		m.envEditMode = envEditNone
		m.envEditValue = ""
		m.envEditName = ""
	case keyCtrlU:
		m.envEditValue = ""
	case keyBackspace, keyCtrlH:
		if len(m.envEditValue) > 0 {
			r := []rune(m.envEditValue)
			m.envEditValue = string(r[:len(r)-1])
		}
	case keyEnter:
		return m.submitEnvEdit()
	default:
		if msg.Text != "" {
			m.envEditValue += msg.Text
		}
	}
	return nil
}

func (m *browser) handleEnvBrowseKey(msg tea.KeyPressMsg) tea.Cmd {
	selection := m.selectedEnv()
	secret, ok := selection.secret, selection.ok
	previous := m.envCursor
	switch msg.String() {
	case "up":
		m.envCursor = max(0, m.envCursor-1)
	case keyDown, "j":
		m.envCursor = min(max(0, len(m.envPlain)+len(m.envSecretNames)-1), m.envCursor+1)
	case keyLeft, "right", inputKeyTab:
		if msg.String() == keyLeft {
			m.envCursor = 0
		} else if len(m.envPlain) > 0 {
			m.envCursor = len(m.envPlain)
		}
	default:
		if cmd := m.handleEnvActionKey(msg.String(), secret, ok); cmd != nil {
			return cmd
		}
	}
	if previous != m.envCursor && m.envRevealName != "" {
		m.envRevealName = ""
		m.envRevealValue = ""
	}
	return nil
}

func (m *browser) handleEnvActionKey(key string, secret, selected bool) tea.Cmd {
	switch key {
	case "e", "+", "A", "d", "s", "x":
		return m.handleEnvWriteAction(key, secret, selected)
	case "y", "v":
		return m.handleEnvClipboardAction(key, secret, selected)
	case "r":
		if m.envHasChanges() {
			m.envMessage = messageSaveOrDiscardBeforeRefresh
			return nil
		}
		m.envMessage = ""
		return m.loadEnv()
	case "m":
		m.envMaximized = !m.envMaximized
	case inputKeyPgUp:
		m.summaryScroll = max(0, m.summaryScroll-max(1, m.height-handleEnvKeyValue8))
	case inputKeyPgDown:
		m.summaryScroll += max(1, m.height-handleEnvKeyValue8)
	}
	return nil
}

func (m *browser) handleEnvWriteAction(key string, secret, selected bool) tea.Cmd {
	switch key {
	case "e":
		if selected && m.allowWrites && !m.selectedEnvDeleted() {
			selection := m.selectedEnv()
			name, value := selection.name, selection.value
			m.envEditName, m.envEditValue = name, value
			if secret {
				m.envEditMode, m.envEditValue = envEditSecret, ""
				m.envRevealName, m.envRevealValue = "", ""
			} else {
				m.envEditMode = envEditPlain
			}
		}
	case "+", "A":
		if m.allowWrites {
			if key == "+" {
				m.envEditMode = envAddPlain
			} else {
				m.envEditMode = envAddSecret
			}
			m.envEditName, m.envEditValue = "", ""
		}
	case "d":
		if selected && m.allowWrites {
			return m.deleteSelectedEnv()
		}
	case "s":
		if m.allowWrites {
			if m.envHasChanges() {
				m.envConfirm = true
				m.envMessage = ""
			} else {
				m.envMessage = messageNoStagedChanges
			}
		}
	case "x":
		if m.allowWrites && m.envHasChanges() {
			m.discardEnvChanges()
		}
	}
	return nil
}

func (m *browser) handleEnvClipboardAction(key string, secret, selected bool) tea.Cmd {
	if !selected {
		return nil
	}
	if key == "y" {
		if secret {
			return m.copySelectedSecret()
		}
		return m.copySelectedPlain()
	}
	if !secret {
		return nil
	}
	name := m.selectedEnv().name
	if m.envRevealName == name {
		m.envRevealName, m.envRevealValue = "", ""
		return nil
	}
	return m.revealSelectedSecret()
}

func (m *browser) submitEnvEdit() tea.Cmd {
	mode, name, value := m.envEditMode, strings.TrimSpace(m.envEditName), m.envEditValue
	name, value, ready := m.prepareEnvEdit(mode, name, value)
	if !ready {
		return nil
	}
	switch mode {
	case envEditPlain, envAddPlain:
		m.applyPlainEnvEdit(name, value)
	case envEditSecret, envAddSecret:
		m.applySecretEnvEdit(name, value)
	}
	m.envEditMode = envEditNone
	m.envEditName, m.envEditValue = "", ""
	m.envMessage = ""
	return nil
}

func (m *browser) prepareEnvEdit(mode uint8, name, value string) (string, string, bool) {
	if mode == envAddPlain || mode == envAddSecret {
		if name == "" {
			m.envEditName = value
			m.envEditValue = ""
			return name, value, false
		}
		value = m.envEditValue
	}
	if name == "" {
		m.envMessage = "Variable name cannot be empty"
		return name, value, false
	}
	if (mode == envAddPlain || mode == envAddSecret) && m.envNameExists(name) {
		m.envMessage = "Variable name already exists"
		return name, value, false
	}
	return name, value, true
}

func (m *browser) applyPlainEnvEdit(name, value string) {
	found := false
	for i := range m.envPlain {
		if m.envPlain[i].Name == name {
			m.envPlain[i].Value = value
			found = true
			break
		}
	}
	if !found {
		m.envPlain = append(m.envPlain, client.EnvVar{Name: name, Value: value})
	}
	sort.Slice(m.envPlain, func(i, j int) bool { return m.envPlain[i].Name < m.envPlain[j].Name })
	m.envCursor = 0
	for i := range m.envPlain {
		if m.envPlain[i].Name == name {
			m.envCursor = i
			break
		}
	}
}

func (m *browser) applySecretEnvEdit(name, value string) {
	if !containsEnvName(m.envSecretNames, name) {
		m.envSecretNames = append(m.envSecretNames, name)
		sort.Strings(m.envSecretNames)
	}
	if m.envSecretValues == nil {
		m.envSecretValues = make(map[string]string)
	}
	m.envSecretValues[name] = value
	if m.envRevealName == name {
		m.envRevealName, m.envRevealValue = "", ""
	}
	m.envCursor = len(m.envPlain)
	for i, secretName := range m.envSecretNames {
		if secretName == name {
			m.envCursor += i
			break
		}
	}
}

func (m *browser) deleteSelectedEnv() tea.Cmd {
	selection := m.selectedEnv()
	secret, name, ok := selection.secret, selection.name, selection.ok
	if !ok {
		return nil
	}
	if secret {
		m.deleteSelectedSecretEnv(name)
	} else {
		m.deleteSelectedPlainEnv(name)
	}
	m.envMessage = ""
	m.envCursor = min(m.envCursor, max(0, len(m.envPlain)+len(m.envSecretNames)-1))
	return nil
}

func (m *browser) deleteSelectedPlainEnv(name string) {
	if !containsEnvName(envNames(m.envOriginalPlain), name) {
		m.envPlain = removePlainEnv(m.envPlain, name)
		return
	}
	if m.envDeletedPlain[name] {
		delete(m.envDeletedPlain, name)
		return
	}
	if m.envDeletedPlain == nil {
		m.envDeletedPlain = make(map[string]bool)
	}
	m.envDeletedPlain[name] = true
}

func (m *browser) deleteSelectedSecretEnv(name string) {
	if m.envRevealName == name {
		m.envRevealName, m.envRevealValue = "", ""
	}
	if !containsEnvName(m.envOriginalSecretNames, name) {
		m.envSecretNames = removeSecretEnv(m.envSecretNames, name)
		delete(m.envSecretValues, name)
		return
	}
	if m.envDeletedSecrets[name] {
		delete(m.envDeletedSecrets, name)
		return
	}
	if m.envDeletedSecrets == nil {
		m.envDeletedSecrets = make(map[string]bool)
	}
	m.envDeletedSecrets[name] = true
}

func (m *browser) envNameExists(name string) bool {
	for _, env := range m.envPlain {
		if env.Name == name {
			return true
		}
	}
	return slices.Contains(m.envSecretNames, name)
}

func removePlainEnv(envs []client.EnvVar, name string) []client.EnvVar {
	out := envs[:0]
	for _, env := range envs {
		if env.Name != name {
			out = append(out, env)
		}
	}
	return out
}

func removeSecretEnv(names []string, name string) []string {
	out := names[:0]
	for _, current := range names {
		if current != name {
			out = append(out, current)
		}
	}
	return out
}

func (m *browser) commitEnvChanges() tea.Cmd {
	if !m.allowWrites {
		m.envMessage = "Writes are disabled · relaunch with --allow-writes"
		return nil
	}
	plain := append([]client.EnvVar(nil), m.envDesiredPlain()...)
	secretNames := append([]string(nil), m.envDesiredSecrets()...)
	secretValues := make(map[string]string, len(m.envSecretValues))
	maps.Copy(secretValues, m.envSecretValues)
	plainChanged := !samePlainEnv(plain, m.envOriginalPlain)
	secretChanged := len(secretNames) != len(m.envOriginalSecretNames) || len(secretValues) > 0
	if !plainChanged && !secretChanged {
		m.envMessage = "No staged changes"
		return nil
	}
	if plainChanged && m.updatePlainEnv == nil {
		m.envMessage = "Plain environment updates are unavailable"
		return nil
	}
	if secretChanged && (m.secretEnvs == nil || m.updateSecretEnv == nil) {
		m.envMessage = "Secret environment updates are unavailable"
		return nil
	}
	tenant, id := m.tenant, m.selectedDetailTarget()
	m.envBusy = true
	m.envMessage = "Saving environment changes…"
	return func() tea.Msg {
		var err error
		if plainChanged {
			err = m.updatePlainEnv(m.ctx, tenant, id, plain)
		}
		if err == nil && secretChanged {
			var current []client.EnvVar
			current, err = m.secretEnvs(m.ctx, tenant, id)
			if err == nil {
				currentValues := make(map[string]string, len(current))
				for _, env := range current {
					currentValues[env.Name] = env.Value
				}
				secrets := make([]client.EnvVar, 0, len(secretNames))
				for _, name := range secretNames {
					value, exists := secretValues[name]
					if !exists {
						value = currentValues[name]
					}
					secrets = append(secrets, client.EnvVar{Name: name, Value: value})
				}
				err = m.updateSecretEnv(m.ctx, tenant, id, secrets)
			}
		}
		return envWriteResult{appID: id, err: err}
	}
}

func samePlainEnv(a, b []client.EnvVar) bool {
	if len(a) != len(b) {
		return false
	}
	values := make(map[string]string, len(a))
	for _, env := range a {
		values[env.Name] = env.Value
	}
	for _, env := range b {
		value, ok := values[env.Name]
		if !ok || value != env.Value {
			return false
		}
	}
	return true
}

func (m *browser) clearPendingEnv() {
	m.envOriginalPlain = nil
	m.envOriginalSecretNames = nil
	m.envSecretValues = nil
	m.envDeletedPlain = nil
	m.envDeletedSecrets = nil
}

func (m *browser) copySelectedSecret() tea.Cmd {
	if !m.allowSecrets {
		m.envMessage = secretAccessDisabled
		return nil
	}
	name := m.selectedEnv().name
	if value, staged := m.envSecretValues[name]; staged {
		return func() tea.Msg { return envCopyResult{err: clipboard.WriteAll(value)} }
	}
	if m.secretEnvs == nil {
		m.envMessage = "Secret reading is unavailable"
		return nil
	}
	tenant, id := m.tenant, m.selectedDetailTarget()
	m.envMessage = "Reading secret for clipboard…"
	return func() tea.Msg {
		values, err := m.secretEnvs(m.ctx, tenant, id)
		if err == nil {
			found := false
			for _, e := range values {
				if e.Name == name {
					found = true
					err = clipboard.WriteAll(e.Value)
					break
				}
			}
			if !found {
				err = errSecretNotFound
			}
		}
		return envCopyResult{err: err}
	}
}

func (m *browser) copySelectedPlain() tea.Cmd {
	value := m.selectedEnv().value
	return func() tea.Msg { return envCopyResult{err: clipboard.WriteAll(value)} }
}

func (m *browser) revealSelectedSecret() tea.Cmd {
	if !m.allowSecrets {
		m.envMessage = secretAccessDisabled
		return nil
	}
	name := m.selectedEnv().name
	if value, staged := m.envSecretValues[name]; staged {
		m.envRevealName, m.envRevealValue = name, value
		m.envMessage = "Staged secret visible"
		return nil
	}
	if m.secretEnvs == nil {
		m.envMessage = "Secret reading is unavailable"
		return nil
	}
	tenant, id := m.tenant, m.selectedDetailTarget()
	m.envMessage = "Reading selected secret…"
	return func() tea.Msg {
		values, err := m.secretEnvs(m.ctx, tenant, id)
		if err == nil {
			for _, e := range values {
				if e.Name == name {
					return envRevealResult{appID: id, name: name, value: e.Value}
				}
			}
		}
		return envRevealResult{appID: id, name: name, err: err}
	}
}

func (m *browser) handleEnvWrite(msg envWriteResult) (tea.Model, tea.Cmd) {
	if msg.appID != m.selectedDetailTarget() {
		m.envBusy = false
		return m, nil
	}
	m.envBusy = false
	m.envEditMode = envEditNone
	m.envEditValue = ""
	m.envEditName = ""
	if msg.err != nil {
		m.envMessage = "Save failed; some changes may have applied · staged changes remain"
		return m, nil
	}
	m.envPlain = append([]client.EnvVar(nil), m.envDesiredPlain()...)
	m.envOriginalPlain = append([]client.EnvVar(nil), m.envPlain...)
	m.envSecretNames = append([]string(nil), m.envDesiredSecrets()...)
	m.envOriginalSecretNames = append([]string(nil), m.envSecretNames...)
	m.envSecretValues = make(map[string]string)
	m.envDeletedPlain = make(map[string]bool)
	m.envDeletedSecrets = make(map[string]bool)
	m.envMessage = "Saved · app rollout may take time"
	m.envState.reset()
	return m, m.loadEnv()
}
func (m *browser) handleEnvCopy(msg envCopyResult) tea.Model {
	if msg.err != nil {
		m.envMessage = "Copy failed · check clipboard access"
	} else {
		m.envMessage = "Value copied to clipboard"
	}
	return m
}

func (m *browser) handleEnvReveal(msg envRevealResult) tea.Model {
	if msg.appID != m.selectedDetailTarget() {
		return m
	}
	if msg.err != nil {
		m.envMessage = "Could not read selected secret"
		return m
	}
	selection := m.selectedEnv()
	if !selection.ok || selection.name != msg.name {
		return m
	}
	if value, staged := m.envSecretValues[msg.name]; staged {
		msg.value = value
	}
	m.envRevealName, m.envRevealValue = msg.name, msg.value
	m.envMessage = "Secret visible"
	return m
}
