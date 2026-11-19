package browser

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
	"sort"
	"strings"
	"time"
)

func (m *browser) sortApps() {
	selectedID := ""
	if m.selected >= 0 && m.selected < len(m.apps) {
		selectedID = m.apps[m.selected].ID
	}
	sort.SliceStable(m.apps, func(i, j int) bool {
		a, b := m.apps[i], m.apps[j]
		var less bool
		switch m.sortBy {
		case sortNamespace:
			less = strings.ToLower(a.Namespace.Name) < strings.ToLower(b.Namespace.Name)
		case sortState:
			less = strings.ToLower(appState(a)) < strings.ToLower(appState(b))
		case sortUpdated:
			ta, ea := time.Parse(time.RFC3339, a.UpdatedAt)
			tb, eb := time.Parse(time.RFC3339, b.UpdatedAt)
			if ea != nil || eb != nil {
				if ea != eb {
					return ea == nil
				}
				less = false
			} else {
				less = ta.Before(tb)
			}
		default:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if m.sortDescending {
			less = !less
		}
		if a.ID == b.ID {
			return false
		}
		if valueEqualForSort(a, b, m.sortBy) {
			return strings.ToLower(a.ID) < strings.ToLower(b.ID)
		}
		return less
	})
	m.selected = min(m.selected, max(0, len(m.apps)-1))
	for i := range m.apps {
		if selectedID != "" && m.apps[i].ID == selectedID {
			m.selected = i
			break
		}
	}
	m.ensureFilterSelection()
}

func valueEqualForSort(a, b client.App, by appSort) bool {
	switch by {
	case sortNamespace:
		return strings.EqualFold(a.Namespace.Name, b.Namespace.Name)
	case sortState:
		return strings.EqualFold(appState(a), appState(b))
	case sortUpdated:
		return a.UpdatedAt == b.UpdatedAt
	default:
		return strings.EqualFold(a.Name, b.Name)
	}
}

func (m *browser) ensureFilterSelection() {
	visible := m.visibleAppIndices()
	if len(visible) == 0 {
		m.namespaceSelected = false
		return
	}
	for _, index := range visible {
		if index == m.selected {
			return
		}
	}
	m.selected = visible[0]
	m.namespaceSelected = false
}

func (m *browser) visibleAppIndices() []int {
	query := strings.TrimSpace(m.filterQuery)
	namespaceQuery, appQuery, scoped := strings.Cut(query, "/")
	namespaceQuery = strings.TrimSpace(namespaceQuery)
	appQuery = strings.TrimSpace(appQuery)
	indices := make([]int, 0, len(m.apps))
	for i, app := range m.apps {
		if m.namespaceScoped && (app.Namespace.ID != m.namespaceScope.id || app.Namespace.Name != m.namespaceScope.name) {
			continue
		}
		if scoped {
			if fuzzyMatch(app.Namespace.Name, namespaceQuery) && fuzzyMatch(app.Name, appQuery) {
				indices = append(indices, i)
			}
		} else if fuzzyMatch(app.Name, query) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *browser) selectedAppVisible() bool {
	if m.selected < 0 || m.selected >= len(m.apps) {
		return false
	}
	for _, index := range m.visibleAppIndices() {
		if index == m.selected {
			return true
		}
	}
	return false
}

func (m *browser) selectTree(index int) tea.Cmd {
	previousTarget := m.selectedDetailTarget()
	rows := m.treeNodes()
	if len(rows) == 0 {
		return nil
	}
	node := rows[max(0, min(index, len(rows)-1))]
	m.namespaceSelected = node.app < 0
	if node.app >= 0 {
		return m.selectAppIndex(node.app, false, previousTarget)
	}
	for _, i := range m.visibleAppIndices() {
		app := m.apps[i]
		if (namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name}) == node.key {
			m.selected = i
			m.detailApp = &m.apps[m.selected]
			return m.syncDetailTarget(previousTarget)
		}
	}
	return nil
}

func (m *browser) selectAppIndex(index int, loadIfUnchanged bool, previousTarget string) tea.Cmd {
	if index < 0 || index >= len(m.apps) {
		return nil
	}
	m.selected = index
	m.namespaceSelected = false
	selected := m.apps[index]
	m.detailApp = &selected
	if previousTarget != m.selectedDetailTarget() {
		return m.syncDetailTarget(previousTarget)
	}
	if loadIfUnchanged && m.detail {
		return m.loadSelectedSummary()
	}
	return nil
}

func (m *browser) moveTree(delta int) tea.Cmd {
	rows := m.treeNodes()
	if len(rows) > 0 {
		return m.selectTree(m.treeSelection(rows) + delta)
	}
	return nil
}

func (m *browser) setCollapsed(collapsed bool) {
	a := m.apps[m.selected]
	key := namespaceKey{id: a.Namespace.ID, name: a.Namespace.Name}
	if m.collapsed == nil {
		m.collapsed = make(map[namespaceKey]bool)
	}
	m.collapsed[key] = collapsed
	if collapsed {
		m.namespaceSelected = true
	} else {
		m.namespaceSelected = false
	}
}

func (m *browser) toggleNamespace() {
	a := m.apps[m.selected]
	key := namespaceKey{id: a.Namespace.ID, name: a.Namespace.Name}
	m.setCollapsed(!m.collapsed[key])
}

// appTree keeps namespace headings visible even when a group spans the viewport.
