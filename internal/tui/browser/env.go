package browser

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/rahacloud/darkubectl/internal/client"
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
		m.envState.unavailable(fmt.Errorf("environment variables are unavailable"))
		return nil
	}
	meta, ctx := m.startRequest(requestEnv, m.tenant, id, m.selectionGeneration)
	m.envState.begin(meta, m.requestTime())
	return func() tea.Msg {
		plain, secrets, err := m.environment(ctx, meta.tenant, id)
		return envResult{meta: meta, plain: plain, secrets: secrets, err: err}
	}
}

func (m *browser) handleEnvResult(msg envResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestEnv, msg.meta)
	if !m.envState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration || msg.meta.appID != m.selectedDetailTarget() {
		return m, nil
	}
	if msg.err != nil {
		m.envState.fail(fmt.Errorf("environment request failed"))
		return m, nil
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
	return m, nil
}

func (m *browser) envPaneLines() []string {
	if !m.selectedAppActive() {
		return []string{summaryField("App", "not selected")}
	}
	m.envCursorLine = 0
	if m.envConfirm {
		lines := []string{sectionHeading("Review changes")}
		lines = append(lines, m.envDiffLines()...)
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
		lines = append(lines, envRow(fmt.Sprintf("%s %s %s", cursorGlyph(selected), envChangeMarker(m, name, true), summaryField(safeText(name), value)), selected))
	}
	if m.envHasChanges() {
		lines = append(lines, "", paint(fmt.Sprintf("%d staged change(s)", m.envChangeCount()), browserAccent, false))
	}
	if m.envMessage != "" {
		lines = append(lines, "", paint(safeText(m.envMessage), browserMuted, false))
	}
	if m.envEditMode == envEditPlain || m.envEditMode == envEditSecret || m.envEditMode == envAddPlain || m.envEditMode == envAddSecret {
		if m.envEditMode == envAddPlain || m.envEditMode == envAddSecret {
			if m.envEditName == "" {
				lines = append(lines, "", "Variable name: "+safeText(m.envEditValue)+"█")
			} else {
				value := safeText(m.envEditValue)
				if m.envEditMode == envEditSecret || m.envEditMode == envAddSecret {
					value = maskedInput(m.envEditValue)
				}
				lines = append(lines, "", value+"█")
			}
		}
	}
	return lines
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
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
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
		if hadOld && !hasNew {
			lines = append(lines, paint("− "+safeText(name), browserNumber, false))
		} else if !hadOld && hasNew {
			lines = append(lines, paint("+ "+safeText(name)+" = ••••••••", browserAccent, false))
		} else if valueChanged && hasNew {
			lines = append(lines, paint("~ "+safeText(name)+" = ••••••••", browserAccent, false))
		}
	}
	return lines
}

func (m *browser) selectedEnv() (secret bool, index int, name, value string, ok bool) {
	if m.envCursor < 0 || m.envCursor >= len(m.envPlain)+len(m.envSecretNames) {
		return
	}
	if m.envCursor < len(m.envPlain) {
		e := m.envPlain[m.envCursor]
		return false, m.envCursor, e.Name, e.Value, true
	}
	i := m.envCursor - len(m.envPlain)
	return true, i, m.envSecretNames[i], "", true
}

func (m *browser) selectedEnvDeleted() bool {
	secret, _, name, _, ok := m.selectedEnv()
	if !ok {
		return false
	}
	if secret {
		return m.envDeletedSecrets[name]
	}
	return m.envDeletedPlain[name]
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
	m.envMessage = "Staged changes discarded"
	m.envCursor = min(m.envCursor, max(0, len(m.envPlain)+len(m.envSecretNames)-1))
}

func (m *browser) handleEnvKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.envBusy {
		return nil
	}
	if m.envConfirm {
		switch msg.String() {
		case "y", "enter":
			m.envConfirm = false
			return m.commitEnvChanges()
		case "n", "esc":
			m.envConfirm = false
			m.envMessage = "Save cancelled · staged changes remain"
		case "up", "k":
			m.summaryScroll = max(0, m.summaryScroll-1)
		case "down", "j":
			m.summaryScroll++
		case "pgup":
			m.summaryScroll = max(0, m.summaryScroll-max(1, m.height-8))
		case "pgdown":
			m.summaryScroll += max(1, m.height-8)
		}
		return nil
	}
	if m.envEditMode != envEditNone {
		switch msg.String() {
		case "esc":
			m.envEditMode = envEditNone
			m.envEditValue = ""
			m.envEditName = ""
		case "ctrl+u":
			m.envEditValue = ""
		case "backspace", "ctrl+h":
			if len(m.envEditValue) > 0 {
				r := []rune(m.envEditValue)
				m.envEditValue = string(r[:len(r)-1])
			}
		case "enter":
			return m.submitEnvEdit()
		default:
			if msg.Text != "" {
				m.envEditValue += msg.Text
			}
		}
		return nil
	}
	secret, _, _, _, ok := m.selectedEnv()
	previous := m.envCursor
	switch msg.String() {
	case "up":
		m.envCursor = max(0, m.envCursor-1)
	case "down", "j":
		m.envCursor = min(max(0, len(m.envPlain)+len(m.envSecretNames)-1), m.envCursor+1)
	case "left", "right", "tab":
		if msg.String() == "left" {
			m.envCursor = 0
		} else if len(m.envPlain) > 0 {
			m.envCursor = len(m.envPlain)
		}
	case "e":
		if ok && m.allowWrites && !m.selectedEnvDeleted() {
			m.envEditName, m.envEditValue = func() (string, string) { _, _, n, v, _ := m.selectedEnv(); return n, v }()
			if secret {
				m.envEditMode = envEditSecret
				m.envEditValue = ""
				m.envRevealName, m.envRevealValue = "", ""
			} else {
				m.envEditMode = envEditPlain
			}
		}
	case "+":
		if m.allowWrites {
			m.envEditMode = envAddPlain
			m.envEditName = ""
			m.envEditValue = ""
		}
	case "A":
		if m.allowWrites {
			m.envEditMode = envAddSecret
			m.envEditName = ""
			m.envEditValue = ""
		}
	case "d":
		if ok && m.allowWrites {
			return m.deleteSelectedEnv()
		}
	case "s":
		if m.allowWrites {
			if m.envHasChanges() {
				m.envConfirm = true
				m.envMessage = ""
			} else {
				m.envMessage = "No staged changes"
			}
		}
	case "x":
		if m.allowWrites && m.envHasChanges() {
			m.discardEnvChanges()
		}
	case "y":
		if ok {
			if secret {
				return m.copySelectedSecret()
			}
			return m.copySelectedPlain()
		}
	case "v":
		if ok && secret {
			if m.envRevealName == func() string { _, _, n, _, _ := m.selectedEnv(); return n }() {
				m.envRevealName = ""
				m.envRevealValue = ""
			} else {
				return m.revealSelectedSecret()
			}
		}
	case "r":
		if m.envHasChanges() {
			m.envMessage = "Save or discard staged changes before refreshing"
			return nil
		}
		m.envMessage = ""
		return m.loadEnv()
	case "m":
		m.envMaximized = !m.envMaximized
	case "pgup":
		m.summaryScroll = max(0, m.summaryScroll-max(1, m.height-8))
	case "pgdown":
		m.summaryScroll += max(1, m.height-8)
	}
	if previous != m.envCursor && m.envRevealName != "" {
		m.envRevealName = ""
		m.envRevealValue = ""
	}
	return nil
}

func (m *browser) submitEnvEdit() tea.Cmd {
	mode, name, value := m.envEditMode, strings.TrimSpace(m.envEditName), m.envEditValue
	if mode == envAddPlain || mode == envAddSecret {
		if name == "" {
			m.envEditName = value
			m.envEditValue = ""
			return nil
		}
		value = m.envEditValue
	}
	if name == "" {
		m.envMessage = "Variable name cannot be empty"
		return nil
	}
	if (mode == envAddPlain || mode == envAddSecret) && m.envNameExists(name) {
		m.envMessage = "Variable name already exists"
		return nil
	}
	if mode == envEditPlain || mode == envAddPlain {
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
	} else if mode == envEditSecret || mode == envAddSecret {
		found := containsEnvName(m.envSecretNames, name)
		if !found {
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
	m.envEditMode = envEditNone
	m.envEditName, m.envEditValue = "", ""
	m.envMessage = ""
	return nil
}

func (m *browser) deleteSelectedEnv() tea.Cmd {
	secret, _, name, _, ok := m.selectedEnv()
	if !ok {
		return nil
	}
	if !secret {
		if !containsEnvName(envNames(m.envOriginalPlain), name) {
			m.envPlain = removePlainEnv(m.envPlain, name)
		} else if m.envDeletedPlain[name] {
			delete(m.envDeletedPlain, name)
		} else {
			if m.envDeletedPlain == nil {
				m.envDeletedPlain = make(map[string]bool)
			}
			m.envDeletedPlain[name] = true
		}
	} else {
		if m.envRevealName == name {
			m.envRevealName, m.envRevealValue = "", ""
		}
		if !containsEnvName(m.envOriginalSecretNames, name) {
			m.envSecretNames = removeSecretEnv(m.envSecretNames, name)
			delete(m.envSecretValues, name)
		} else if m.envDeletedSecrets[name] {
			delete(m.envDeletedSecrets, name)
		} else {
			if m.envDeletedSecrets == nil {
				m.envDeletedSecrets = make(map[string]bool)
			}
			m.envDeletedSecrets[name] = true
		}
	}
	m.envMessage = ""
	m.envCursor = min(m.envCursor, max(0, len(m.envPlain)+len(m.envSecretNames)-1))
	return nil
}

func (m *browser) envNameExists(name string) bool {
	for _, env := range m.envPlain {
		if env.Name == name {
			return true
		}
	}
	for _, envName := range m.envSecretNames {
		if envName == name {
			return true
		}
	}
	return false
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
	for name, value := range m.envSecretValues {
		secretValues[name] = value
	}
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
	_, _, name, _, _ := m.selectedEnv()
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
					err = clipboard.WriteAll(e.Value)
					found = true
					break
				}
			}
			if !found {
				err = fmt.Errorf("secret not found")
			}
		}
		return envCopyResult{err: err}
	}
}

func (m *browser) copySelectedPlain() tea.Cmd {
	_, _, _, value, _ := m.selectedEnv()
	return func() tea.Msg { return envCopyResult{err: clipboard.WriteAll(value)} }
}

func (m *browser) revealSelectedSecret() tea.Cmd {
	_, _, name, _, _ := m.selectedEnv()
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
			err = fmt.Errorf("secret not found")
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
func (m *browser) handleEnvCopy(msg envCopyResult) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.envMessage = "Copy failed · check clipboard access"
	} else {
		m.envMessage = "Value copied to clipboard"
	}
	return m, nil
}

func (m *browser) handleEnvReveal(msg envRevealResult) (tea.Model, tea.Cmd) {
	if msg.appID != m.selectedDetailTarget() {
		return m, nil
	}
	if msg.err != nil {
		m.envMessage = "Could not read selected secret"
		return m, nil
	}
	_, _, name, _, ok := m.selectedEnv()
	if !ok || name != msg.name {
		return m, nil
	}
	if value, staged := m.envSecretValues[msg.name]; staged {
		msg.value = value
	}
	m.envRevealName, m.envRevealValue = msg.name, msg.value
	m.envMessage = "Secret visible"
	return m, nil
}
