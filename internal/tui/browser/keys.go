package browser

import tea "charm.land/bubbletea/v2"

func (m *browser) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.actionBusy {
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.actionMenu {
		return m, m.handleActionMenuKey(msg)
	}
	if m.confirmAction != appActionNone {
		return m, m.handleActionConfirmationKey(msg)
	}
	if m.selectingNamespace {
		return m.handleNamespaceKey(msg)
	}
	if m.filterEditing {
		return m.handleFilterKey(msg)
	}
	if m.sorting {
		return m.handleSortKey(msg)
	}
	if m.help {
		return m.handleHelpKey(msg)
	}

	switch msg.String() {
	case "1":
		if !m.help && !m.sorting {
			return m, m.focusPane(0)
		}
	case "2":
		if !m.help && !m.sorting {
			return m, m.focusPane(1)
		}
	case "3":
		if !m.help && !m.sorting {
			return m, m.focusPane(2)
		}
	case "4":
		if !m.help && !m.sorting {
			return m, m.focusPane(3)
		}
	case "tab":
		return m, m.cycleFocus(false)
	case "shift+tab", "backtab":
		return m, m.cycleFocus(true)
	case "ctrl+c", "q":
		return m, tea.Quit
	case "a":
		m.openActionMenu()
	case "[":
		return m, m.switchAppView(-1)
	case "]":
		return m, m.switchAppView(1)
	case "w":
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			m.historyRange = (m.historyRange + 1) % len(usageRanges)
			m.cancelRequest(requestKey{kind: requestUsageHistory})
			m.historyState.reset()
			m.history = nil
			return m, m.loadUsageHistory()
		}
	case "p":
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			m.nextHistoryPod()
		}
	case "m":
		if m.detail && m.activeAppView == appViewUsage && m.selectedAppActive() {
			app := m.apps[m.selected]
			if m.detailApp != nil && m.detailApp.ID == app.ID {
				app = *m.detailApp
			}
			metrics := availableHistoryMetrics(app)
			if len(metrics) > 0 {
				m.historyMetric = (m.historyMetric + 1) % len(metrics)
			}
		}
	case "n", ":":
		if !m.help && !m.selectingTenant {
			return m, m.openNamespacePicker()
		}
	case "?":
		m.help = !m.help
	case "/":
		if !m.help && !m.selectingTenant {
			previousTarget := m.selectedDetailTarget()
			m.filterEditing = true
			m.contextFocused = false
			m.detail = false
			m.filterBeforeEdit = m.filterQuery
			m.namespaceSelected = false
			m.filterSelection = ""
			if m.selected >= 0 && m.selected < len(m.apps) {
				m.filterSelection = m.apps[m.selected].ID
			}
			m.syncDetailTarget(previousTarget)
		}
	case "s":
		if !m.help && !m.selectingTenant {
			m.sorting = true
			m.sortChoice = int(m.sortBy)
		}
	case "left", "h":
		if m.sorting {
			m.sortDescending = false
		} else if !m.help && m.contextFocused {
			m.selectingTenant = false
		} else if !m.help && !m.selectingTenant && !m.detail && m.selectedAppVisible() {
			m.setCollapsed(true)
		} else if !m.help && !m.selectingTenant && m.detail {
			m.detail = false
		}
	case "right", "l":
		if m.sorting {
			m.sortDescending = true
		} else if !m.help && m.contextFocused {
			m.selectingTenant = false
			m.contextFocused = false
			m.detail = false
		} else if !m.help && !m.selectingTenant && !m.detail && m.namespaceSelected && m.selectedAppVisible() {
			m.setCollapsed(false)
		} else if !m.help && !m.selectingTenant && !m.detail && !m.namespaceSelected && len(m.apps) > 0 {
			m.detail = true
			return m, m.loadSelectedSummary()
		}
	case browserKeyEscape:
		if m.sorting {
			m.sorting = false
		} else if m.selectingTenant {
			m.selectingTenant = false
		} else if m.help {
			m.help = false
		} else {
			if m.detail {
				m.detail = false
			} else if m.filterQuery != "" {
				m.filterQuery = ""
				m.ensureFilterSelection()
			}
		}
	case "up", "k":
		if m.sorting {
			m.sortChoice = (m.sortChoice + len(appSortNames) - 1) % len(appSortNames)
		} else if m.selectingTenant {
			return m, m.moveTenantSelection(-1)
		} else if m.contextFocused && m.contextRow == 0 {
			return m, m.moveTenantSelection(-1)
		} else if m.contextFocused {
			m.contextRow = max(0, m.contextRow-1)
		} else if !m.help && !m.detail {
			return m, m.moveTree(-1)
		} else if !m.help && m.detail {
			return m, m.moveTree(-1)
		}
	case "down", "j":
		if m.sorting {
			m.sortChoice = (m.sortChoice + 1) % len(appSortNames)
		} else if m.selectingTenant {
			return m, m.moveTenantSelection(1)
		} else if m.contextFocused && m.contextRow == 0 {
			return m, m.moveTenantSelection(1)
		} else if m.contextFocused {
			m.contextRow = min(1, m.contextRow+1)
		} else if !m.help && !m.detail {
			return m, m.moveTree(1)
		} else if !m.help && m.detail {
			return m, m.moveTree(1)
		}
	case "home", "g":
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.selectTree(0)
		}
	case "end", "G":
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.selectTree(len(m.treeNodes()) - 1)
		}
	case "pgup":
		if m.detail && (m.activeAppView == appViewUsage || m.activeAppView == appViewPods) && m.selectedAppActive() {
			m.usageScroll = max(0, m.usageScroll-max(1, m.height-8))
			return m, nil
		}
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.moveTree(-max(1, m.height-8))
		}
	case "pgdown":
		if m.detail && (m.activeAppView == appViewUsage || m.activeAppView == appViewPods) && m.selectedAppActive() {
			m.usageScroll += max(1, m.height-8)
			return m, nil
		}
		if !m.help && !m.contextFocused && !m.selectingTenant {
			return m, m.moveTree(max(1, m.height-8))
		}
	case "enter":
		if m.sorting {
			m.sortBy = appSort(m.sortChoice)
			m.sorting = false
			m.sortApps()
		} else if m.selectingTenant || m.contextFocused && m.contextRow == 0 {
			// Tenant changes are applied as soon as the highlighted row moves.
		} else if m.contextFocused {
			// Apps is the only active global page; the remaining entries are disabled.
		} else if !m.help && len(m.visibleAppIndices()) > 0 {
			if m.namespaceSelected {
				if m.filterQuery == "" {
					m.toggleNamespace()
				} else {
					m.namespaceSelected = false
					m.detail = true
					return m, m.loadSelectedSummary()
				}
			} else {
				m.detail = true
				return m, m.loadSelectedSummary()
			}
		}
	case "r":
		if m.selectingTenant {
			return m, m.loadProfile()
		}
		if m.detail {
			if m.activeAppView == appViewUsage {
				return m, tea.Batch(m.loadSelectedUsage(), m.loadUsageHistory())
			}
			if m.activeAppView == appViewPods {
				return m, m.loadSelectedPods()
			}
			return m, tea.Batch(m.loadSelectedSummary(), m.loadEgressRoutes(true))
		}
		if !m.help && !m.appsState.active() {
			return m, tea.Batch(m.load(), m.loadEgressRoutes(true))
		}
	}
	return m, nil
}
