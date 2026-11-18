package browser

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/rahacloud/darkubectl/internal/client"
	"sort"
	"strings"
)

func (m *browser) sortNamespaces() {
	sort.Slice(m.namespaces, func(i, j int) bool {
		if !strings.EqualFold(m.namespaces[i].Name, m.namespaces[j].Name) {
			return strings.ToLower(m.namespaces[i].Name) < strings.ToLower(m.namespaces[j].Name)
		}
		return m.namespaces[i].ID < m.namespaces[j].ID
	})
}

func (m *browser) openNamespacePicker() tea.Cmd {
	m.selectingNamespace = true
	m.namespaceQuery = ""
	m.namespaceCursor = 0
	choices := m.namespaceChoices()
	for i, choice := range choices {
		if choice.scoped == m.namespaceScoped && (!choice.scoped || choice.key == m.namespaceScope) {
			m.namespaceCursor = i
			break
		}
	}
	return m.loadNamespaces()
}

func (m *browser) handleNamespaceKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	choices := m.namespaceChoices()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingNamespace = false
		m.namespaceQuery = ""
	case "enter":
		if len(choices) > 0 {
			m.chooseNamespace(choices[m.namespaceCursor])
		}
	case "up":
		m.namespaceCursor = max(0, m.namespaceCursor-1)
	case "down":
		m.namespaceCursor = min(max(0, len(choices)-1), m.namespaceCursor+1)
	case "ctrl+u":
		m.namespaceQuery = ""
		m.namespaceCursor = 0
	case "backspace", "ctrl+h":
		query := []rune(m.namespaceQuery)
		if len(query) > 0 {
			m.namespaceQuery = string(query[:len(query)-1])
			m.namespaceCursor = 0
		}
	default:
		if msg.Text != "" {
			m.namespaceQuery += safeText(msg.Text)
			m.namespaceCursor = 0
		}
	}
	return m, nil
}

func (m *browser) namespaceChoices() []namespaceChoice {
	choices := []namespaceChoice{{}}
	for _, namespace := range m.namespaces {
		key := namespaceKey{id: namespace.ID, name: namespace.Name}
		query := strings.TrimSpace(m.namespaceQuery)
		if query != "" && !fuzzyMatch(namespace.Name, query) && !fuzzyMatch(namespace.Cluster.Name, query) && !fuzzyMatch(fmt.Sprint(namespace.ID), query) {
			continue
		}
		choices = append(choices, namespaceChoice{key: key, scoped: true})
	}
	if strings.TrimSpace(m.namespaceQuery) != "" && !fuzzyMatch("All namespaces", m.namespaceQuery) {
		choices = choices[1:]
	}
	return choices
}

func (m *browser) namespaceAppCount(key namespaceKey) int {
	count := 0
	for _, app := range m.apps {
		if app.Namespace.ID == key.id && app.Namespace.Name == key.name {
			count++
		}
	}
	return count
}

func (m *browser) chooseNamespace(choice namespaceChoice) {
	m.namespaceScoped = choice.scoped
	m.namespaceScope = choice.key
	m.selectingNamespace = false
	m.namespaceQuery = ""
	m.filterQuery = ""
	m.filterEditing = false
	m.namespaceSelected = false
	m.detail = false
	m.ensureFilterSelection()
}

func (m *browser) syncNamespaceCursor() {
	choices := m.namespaceChoices()
	for i, choice := range choices {
		if choice.scoped == m.namespaceScoped && (!choice.scoped || choice.key == m.namespaceScope) {
			m.namespaceCursor = i
			return
		}
	}
	m.namespaceCursor = 0
}

func uniqueNamespaces(input []client.Namespace) []client.Namespace {
	out := make([]client.Namespace, 0, len(input))
	seen := make(map[namespaceKey]struct{}, len(input))
	for _, namespace := range input {
		if strings.TrimSpace(namespace.Name) == "" {
			continue
		}
		key := namespaceKey{id: namespace.ID, name: namespace.Name}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, namespace)
	}
	return out
}

func mergeNamespaces(inventory, appNamespaces []client.Namespace) []client.Namespace {
	merged := append([]client.Namespace(nil), inventory...)
	seen := make(map[namespaceKey]struct{}, len(inventory)+len(appNamespaces))
	for _, namespace := range inventory {
		seen[namespaceKey{id: namespace.ID, name: namespace.Name}] = struct{}{}
	}
	for _, namespace := range appNamespaces {
		key := namespaceKey{id: namespace.ID, name: namespace.Name}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, namespace)
	}
	return uniqueNamespaces(merged)
}
