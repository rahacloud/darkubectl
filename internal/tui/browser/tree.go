package browser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	namespaceBreadcrumbValue2 = 2
	namespaceTopLineValue28   = 28
)

func (m *browser) sortLine() string {
	if m.page == pageActivity {
		return m.activitySortLine()
	}
	if m.page == pageDB {
		return m.databaseSortLine()
	}
	choice := int(m.sortBy)
	if m.sorting && m.sortChoice >= 0 && m.sortChoice < len(appSortNames) {
		choice = m.sortChoice
	}
	direction := "↑"
	if m.sortDescending {
		direction = "↓"
	}
	return paint("⇅", browserMuted, false) + " " + paint(appSortNames[choice], browserBranch, true) + " " + paint(direction,
		browserNumber, true)
}

func (m *browser) databaseSortLine() string {
	choice := int(m.databaseSortBy)
	if m.sorting && m.sortChoice >= 0 && m.sortChoice < len(dbSortNames) {
		choice = m.sortChoice
	}
	direction := "↑"
	if m.sorting {
		if m.sortDescending {
			direction = "↓"
		}
	} else if m.databaseSortDescending {
		direction = "↓"
	}
	return paint("⇅", browserMuted, false) + " " + paint(dbSortNames[choice], browserBranch, true) + " " + paint(direction,
		browserNumber, true)
}

func (m *browser) namespaceTopLine() string {
	if m.selectingNamespace && strings.TrimSpace(m.namespaceQuery) != "" {
		return paint("⎈ NS", browserMuted, true) + " " + paint("/ "+safeText(m.namespaceQuery), browserBranch, true)
	}
	if m.namespaceScoped {
		name := ansi.Truncate(safeText(m.namespaceBreadcrumb(m.namespaceScope)), namespaceTopLineValue28, "…")
		return paint("⎈ NS", browserMuted, true) + " " + paint(name, browserBranch, true)
	}
	return paint("⎈ NS", browserMuted, true) + " " + paint("All", browserBranch, true)
}

func fuzzyMatch(value, query string) bool {
	queryRunes := []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(queryRunes) == 0 {
		return true
	}
	queryIndex := 0
	for _, valueRune := range strings.ToLower(value) {
		if valueRune == queryRunes[queryIndex] {
			queryIndex++
			if queryIndex == len(queryRunes) {
				return true
			}
		}
	}
	return false
}

func (m *browser) breadcrumb() string {
	version := m.version
	if version == "" {
		version = "dev"
	}
	parts := []string{"Darkubectl " + safeText(version)}
	if m.tenant != "" {
		tenant := safeText(m.tenant)
		if m.tenantStatus != "" {
			tenant += " (" + m.tenantStatus + ")"
		}
		parts = append(parts, tenant)
	}
	if m.page == pageActivity {
		parts = append(parts, "Activity")
		return m.contextBreadcrumb(parts)
	}
	if m.page == pageTickets {
		parts = append(parts, "Tickets")
		return m.contextBreadcrumb(parts)
	}
	if m.page == pageRegistry {
		parts = append(parts, "Registry")
		return m.contextBreadcrumb(parts)
	}
	if m.page == pageDB {
		parts = append(parts, "DB")
		return m.contextBreadcrumb(parts)
	}
	parts = append(parts, "Apps")
	if m.namespaceScoped {
		parts = append(parts, safeText(m.namespaceBreadcrumb(m.namespaceScope)))
	}
	if !m.namespaceSelected && m.selectedAppVisible() {
		app := m.apps[m.selected]
		if !m.namespaceScoped {
			parts = append(parts, safeText(m.namespaceBreadcrumb(namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name})))
		}
		parts = append(parts, safeText(app.Name))
	}
	return m.contextBreadcrumb(parts)
}

func (m *browser) contextBreadcrumb(parts []string) string {
	separator := paint(" / ", browserMuted, false)
	crumbs := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == 0 {
			crumbs = append(crumbs, paint(part, browserAccent, true))
		} else {
			crumbs = append(crumbs, paint(part, browserBranch, i == len(parts)-1))
		}
	}
	path := strings.Join(crumbs, separator)
	if m.mode == connectionLabelLive {
		return m.liveGlyph() + " " + path
	}
	return paint("◈", browserNumber, true) + " " + paint("MOCK PROFILE · OFFLINE", browserMuted, false) + "  " + path
}

func (m *browser) namespaceBreadcrumb(key namespaceKey) string {
	name := key.name
	duplicates := make([]client.Namespace, 0, namespaceBreadcrumbValue2)
	for _, namespace := range m.namespaces {
		if namespace.Name == key.name {
			duplicates = append(duplicates, namespace)
		}
	}
	if len(duplicates) < namespaceBreadcrumbValue2 {
		return name
	}
	for _, namespace := range duplicates {
		if namespace.ID == key.id {
			if namespace.Cluster.Name != "" {
				return fmt.Sprintf("%s · %s · ID %d", name, namespace.Cluster.Name, namespace.ID)
			}
			return fmt.Sprintf("%s · ID %d", name, namespace.ID)
		}
	}
	return name
}

func (m *browser) treeNodes() []treeNode {
	visible := m.visibleAppIndices()
	namespaceIDs := make(map[string]map[int]bool)
	type namespaceGroup struct {
		key  namespaceKey
		apps []int
	}
	groupsByKey := make(map[namespaceKey]int)
	groups := make([]namespaceGroup, 0)
	for _, appIndex := range visible {
		app := m.apps[appIndex]
		key := namespaceKey{id: app.Namespace.ID, name: app.Namespace.Name}
		groupIndex, exists := groupsByKey[key]
		if !exists {
			groupIndex = len(groups)
			groupsByKey[key] = groupIndex
			groups = append(groups, namespaceGroup{key: key})
		}
		groups[groupIndex].apps = append(groups[groupIndex].apps, appIndex)
		if namespaceIDs[app.Namespace.Name] == nil {
			namespaceIDs[app.Namespace.Name] = make(map[int]bool)
		}
		namespaceIDs[app.Namespace.Name][app.Namespace.ID] = true
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].key.name != groups[j].key.name {
			return strings.ToLower(groups[i].key.name) < strings.ToLower(groups[j].key.name)
		}
		return groups[i].key.id < groups[j].key.id
	})
	var rows []treeNode
	for _, group := range groups {
		app := m.apps[group.apps[0]]
		heading := namespaceHeading(app, namespaceIDs, len(group.apps))
		rows = append(rows, treeNode{app: -1, key: group.key, heading: heading})
		if m.filterQuery == "" && m.collapsed[group.key] {
			continue
		}
		for i, appIndex := range group.apps {
			rows = append(rows, treeNode{app: appIndex, key: group.key, heading: heading, last: i == len(group.apps)-1})
		}
	}
	return rows
}

func namespaceHeading(app client.App, namespaceIDs map[string]map[int]bool, appCount int) string {
	name := app.Namespace.Name
	if name == "" {
		name = "Unknown namespace"
	}
	if len(namespaceIDs[app.Namespace.Name]) > 1 {
		name += fmt.Sprintf(" (ID %d)", app.Namespace.ID)
	}
	appLabel := labelApps
	if appCount == 1 {
		appLabel = labelApp
	}
	return fmt.Sprintf("%s  ·  %d %s", name, appCount, appLabel)
}

func (m *browser) treeSelection(rows []treeNode) int {
	if len(m.visibleAppIndices()) == 0 {
		return 0
	}
	a := m.apps[m.selected]
	key := namespaceKey{id: a.Namespace.ID, name: a.Namespace.Name}
	for i, row := range rows {
		if row.key == key && ((m.namespaceSelected && row.app < 0) || (!m.namespaceSelected && row.app == m.selected)) {
			return i
		}
	}
	return 0
}

func (m *browser) appTree(height int) []string {
	return m.appTreeForWidth(height, m.width)
}

func (m *browser) appTreeForWidth(height, width int) []string {
	rows := m.treeNodes()
	if len(rows) == 0 || height < 2 {
		return nil
	}
	selectedRow := m.treeSelection(rows)
	nameWidth := 0
	for _, appIndex := range m.visibleAppIndices() {
		app := m.apps[appIndex]
		nameWidth = max(nameWidth, ansi.StringWidth(safeText(app.Name)))
	}
	const minNameWidth = 8
	const reservedWidth = 30
	nameWidth = min(nameWidth, max(minNameWidth, width-reservedWidth))
	start := max(0, selectedRow-height+1)
	var result []string
	if rows[start].app >= 0 {
		result = append(result, "  "+paint("▾ "+safeText(rows[start].heading), browserBranch, true))
		if selectedRow-start >= height-1 {
			start++
		}
	}
	for i := start; i < len(rows) && len(result) < height; i++ {
		node := rows[i]
		var line string
		if node.app < 0 {
			indicator := "▾"
			if m.collapsed[node.key] {
				indicator = "▸"
			}
			line = "  " + paint(indicator+" "+safeText(node.heading), browserBranch, true)
		} else {
			branch := "├── "
			if node.last {
				branch = "└── "
			}
			app := m.apps[node.app]
			name := ansi.Truncate(safeText(app.Name), nameWidth, "…")
			padding := strings.Repeat(" ", max(0, nameWidth-ansi.StringWidth(name)))
			state := safeText(appState(app))
			glyph, stateColor := stateStyle(state)
			line = "  " + paint(branch, browserBorder, false) + name + padding + "  " + paint(glyph+" "+state, stateColor, false)
		}
		if i == selectedRow {
			plain := ansi.Truncate(ansi.Strip(line), width, "")
			plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
			line = selectedRowStyle().Width(width).Render(plain)
		}
		result = append(result, line)
	}
	return result
}
