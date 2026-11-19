package browser

import tea "charm.land/bubbletea/v2"

func (m *browser) handleBrowserMouse(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.help || m.selectingTenant || m.selectingNamespace || m.filterEditing {
		return nil
	}
	if m.detail && m.activeAppView == appViewLogs && m.logMaximized {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	appsLeft := contextWidth + 3
	appsRight := appsLeft + appsWidth + 2
	if msg.X >= appsLeft && msg.X < appsRight {
		nodes := m.treeNodes()
		nodeTop := len(m.appPaneLines(0))
		row := msg.Y - bodyY - 1 - nodeTop
		if row < 0 || row >= len(nodes) {
			m.detail = false
			m.contextFocused = false
			return nil
		}
		node := nodes[row]
		m.contextFocused = false
		if node.app < 0 {
			if m.collapsed == nil {
				m.collapsed = make(map[namespaceKey]bool)
			}
			m.collapsed[node.key] = !m.collapsed[node.key]
			return nil
		}
		previousTarget := m.selectedDetailTarget()
		m.detail = true
		return m.selectAppIndex(node.app, true, previousTarget)
	}
	viewLeft := appsRight + 1
	if msg.X >= viewLeft && msg.X < viewLeft+summaryWidth+2 && msg.Y == bodyY {
		m.detail = true
		return m.selectClickedAppView(msg.X-(viewLeft+1), summaryWidth)
	}
	if msg.X < contextWidth+2 {
		m.contextFocused = true
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(4, (bodyHeight-1)/2)
		if msg.Y >= bodyY+tenantHeight+1 {
			m.contextRow = 1
			return nil
		}
		m.contextRow = 0
		capacity := max(1, tenantHeight-2)
		first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
		index := first + msg.Y - bodyY - 1
		if index >= 0 && index < len(m.tenants) && m.tenants[index].name != m.tenant {
			m.tenantCursor = index
			return m.chooseTenant(m.tenants[index].name)
		}
	}
	return nil
}

func (m *browser) selectClickedAppView(x, width int) tea.Cmd {
	if !m.selectedAppActive() {
		return nil
	}
	view, ok := appViewTabAt(x, width, m.activeAppView)
	if !ok || view == m.activeAppView {
		return nil
	}
	return m.switchAppView(int(view) - int(m.activeAppView))
}

func appViewTabAt(x, width int, active appView) (appView, bool) {
	labels := appViewTabLabels(width)
	position := 2 + len("[4] ") // border prefix and pane number
	for index, label := range labels {
		start := position
		span := len(label)
		if appView(index) == active {
			span += 2 // selected tab includes its brackets
		}
		if x >= start && x < start+span {
			return appView(index), true
		}
		position += span + 1 // one separating space
	}
	return 0, false
}

func (m *browser) browserBodyY() int {
	y := 2 // breadcrumb and spacer
	if m.actionNotice != "" {
		y++
	}
	if m.profileState.status == requestFailed || m.profileState.status == requestStale {
		y++
		if m.profileState.status == requestStale {
			y++
		}
	}
	return y
}

func (m *browser) handleBrowserWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.help || m.selectingTenant || m.selectingNamespace || m.filterEditing {
		return nil
	}
	delta := 1
	if msg.Button == tea.MouseWheelUp {
		delta = -1
	} else if msg.Button != tea.MouseWheelDown {
		return nil
	}
	bodyY := m.browserBodyY()
	if msg.Y < bodyY || msg.Y >= m.height-browserFooterHeight {
		return nil
	}
	appsWidth, summaryWidth, contextWidth := m.paneWidths()
	appsLeft := contextWidth + 3
	appsRight := appsLeft + appsWidth + 2
	if msg.X >= appsLeft && msg.X < appsRight {
		visible := m.visibleAppIndices()
		if len(visible) == 0 {
			return nil
		}
		position := 0
		for i, index := range visible {
			if index == m.selected {
				position = i
				break
			}
		}
		position = min(len(visible)-1, max(0, position+delta))
		return m.selectAppIndex(visible[position], false, m.selectedDetailTarget())
	}
	if msg.X >= appsRight+1 && msg.X < appsRight+1+summaryWidth+2 {
		if m.activeAppView == appViewLogs {
			m.logScroll = max(0, m.logScroll-delta*3)
		} else if m.activeAppView == appViewUsage {
			m.usageScroll = max(0, m.usageScroll-delta*3)
		} else {
			m.summaryScroll = max(0, m.summaryScroll-delta*3)
		}
	}
	if msg.X < contextWidth+2 {
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(4, (bodyHeight-1)/2)
		m.contextFocused = true
		m.contextRow = 0
		if msg.Y >= bodyY+tenantHeight+1 {
			m.contextRow = 1
		}
	}
	return nil
}
