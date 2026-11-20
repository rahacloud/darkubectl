package browser

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	applyPortEditValue2          = 2
	beginPortEditValue4          = 4
	handlePortsKeyValue20        = 20
	portContentWidthValue2       = 2
	portTableWidthsValue4        = 4
	portTableWidthsValue8        = 8
	portsPaneLinesValue32        = 32
	portsPaneLinesValue58        = 58
	portsPaneLinesValue9         = 9
	portEditorFixedWidth         = 13
	portTableNameMaxWidth        = 24
	portTableNameMinWidth        = 24
	applyPortEditAdditionalField = 3
	portTableCompactFixedWidth   = 49
)

// PortConfig is the editable service projection for an app.
type PortConfig struct {
	Type            string
	InternalAddress string
	ExternalAddress string
	ExternalIP      string
	Ports           map[string]client.Port
}

type portsResult struct {
	meta   requestMeta
	config PortConfig
	err    error
}

type portsWriteResult struct {
	appID  string
	config PortConfig
	err    error
}

var portColumns = []string{"NAME", "PROTO", "CONTAINER", "SERVICE", "PUBLIC"}

const portDraftPrefix = "__new_port_"

func clonePortConfig(c PortConfig) PortConfig {
	ports := make(map[string]client.Port, len(c.Ports))
	maps.Copy(ports, c.Ports)
	c.Ports = ports
	return c
}

func (m *browser) loadPorts() tea.Cmd {
	id := m.selectedDetailTarget()
	if id == "" || m.ports == nil {
		m.portState.unavailable(errServicePortsUnavailable)
		return nil
	}
	meta, ctx := m.startRequest(requestPorts, m.tenant, id, m.selectionGeneration)
	m.portState.begin(meta, m.requestTime())
	return func() tea.Msg {
		config, err := m.ports(ctx, meta.tenant, id)
		return portsResult{meta: meta, config: config, err: err}
	}
}

func (m *browser) handlePortsResult(msg portsResult) tea.Model {
	m.finishRequest(requestPorts, msg.meta)
	if !m.portState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration ||
		msg.meta.appID != m.selectedDetailTarget() {
		return m
	}
	if msg.err != nil {
		m.portState.fail(errCouldNotLoadServicePorts)
		return m
	}
	m.portConfig = clonePortConfig(msg.config)
	m.portOriginal = clonePortConfig(msg.config)
	m.portCursorRow = min(m.portCursorRow, len(msg.config.Ports)+1)
	m.portState.succeed(len(msg.config.Ports) == 0, m.requestTime())
	return m
}

func (m *browser) portNames() []string {
	names := make([]string, 0, len(m.portConfig.Ports))
	for name := range m.portConfig.Ports {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *browser) portsChanged() bool {
	return !portConfigEqual(m.portOriginal, m.portConfig)
}

func portConfigEqual(a, b PortConfig) bool {
	if a.Type != b.Type || len(a.Ports) != len(b.Ports) {
		return false
	}
	for name, p := range a.Ports {
		if b.Ports[name] != p {
			return false
		}
	}
	return true
}

func portCell(value string, selected, editing, changed bool, width int) string {
	value = safeText(value)
	if editing {
		// Keep the caret visible when the edited value is wider than the cell.
		room := max(0, width-1)
		if room == 0 {
			value = ""
		} else if ansi.StringWidth(value) > room {
			value = "…" + ansi.Cut(value, max(0, ansi.StringWidth(value)-max(0, room-1)), ansi.StringWidth(value))
		}
		value += "█"
	} else {
		value = ansi.Truncate(value, width, "…")
	}
	pad := max(0, width-ansi.StringWidth(ansi.Strip(value)))
	if selected {
		return selectedRowStyle().Render(" " + value + strings.Repeat(" ", pad) + " ")
	}
	if changed {
		value = paint(value, browserAccent, true)
	} else {
		value = paint(value, browserValue, false)
	}
	return " " + value + strings.Repeat(" ", pad) + " "
}

func (m *browser) portsPaneLines() []string {
	if !m.selectedAppActive() {
		return []string{summaryField("App", "not selected")}
	}
	if m.portConfirm {
		return m.portConfirmationLines()
	}
	lines := []string{sectionHeading("Service access"), ""}
	if m.portState.active() {
		lines = append(lines, summaryField("Ports", "loading "+m.spinnerGlyph()))
	}
	if m.portState.err != nil {
		lines = append(lines, summaryField("Ports", paint("unavailable", browserNumber, false)))
	}
	lines = append(lines, paint("Internal address", browserMuted, true), "  "+paint(displayValue(m.portConfig.InternalAddress),
		browserValue, false))
	lines = append(lines, "")
	externalRow := len(lines)
	lines = append(lines, m.portExternalAccessLines()...)
	lines = append(lines, "", domainSectionHeading("Ports", len(m.portConfig.Ports), "port"), "")
	stacked := m.portContentWidth() < portsPaneLinesValue32
	compact := m.portContentWidth() < portsPaneLinesValue58
	widths := m.portTableWidths(compact)
	if !stacked {
		lines = append(lines, portTableRow("  ", portColumns, false, -1, false, nil, widths, compact, true)...)
	}
	names := m.portNames()
	if len(names) == 0 {
		lines = append(lines, summaryField("Ports", "none"))
	}
	lines = m.appendPortRows(lines, names, stacked, compact, widths)
	if m.portCursorRow == 0 {
		m.portCursorLine = externalRow
	}
	if m.portsChanged() {
		lines = append(lines, "", paint("● staged changes", browserAccent, true))
	}
	if m.portMessage != "" {
		lines = append(lines, "", paint(safeText(m.portMessage), browserMuted, false))
	}
	return lines
}

func (m *browser) portConfirmationLines() []string {
	lines := []string{sectionHeading("Review changes"), ""}
	lines = append(lines, m.portDiffLines()...)
	if (m.portOriginal.Type != serviceTypeLoadBalancer && m.portOriginal.Type != serviceTypeNodePort) &&
		(m.portConfig.Type == serviceTypeLoadBalancer || m.portConfig.Type == serviceTypeNodePort) {
		lines = append(lines, "", paint("External access makes declared ports reachable from the internet.", browserNumber, true))
	}
	return lines
}

func (m *browser) portExternalAccessLines() []string {
	enabled := m.portConfig.Type == serviceTypeLoadBalancer || m.portConfig.Type == serviceTypeNodePort
	access := "○ disabled"
	if enabled {
		access = paint("● enabled", browserSuccess, true)
	}
	lines := []string{portInteractiveRow(m.portCursorRow == 0, m.allowWrites, "External access", access)}
	if enabled && m.portConfig.ExternalAddress != "" {
		lines = append(lines, "  "+paint(displayValue(m.portConfig.ExternalAddress), browserValue, false))
	}
	if enabled && m.portConfig.ExternalIP != "" {
		lines = append(lines, "  "+paint(displayValue(m.portConfig.ExternalIP), browserMuted, false))
	}
	return lines
}

func (m *browser) appendPortRows(lines, names []string, stacked, compact bool, widths []int) []string {
	for i, name := range names {
		lines = append(lines, m.portRowLines(name, i+1, len(lines), stacked, compact, widths)...)
		if compact && i < len(names)-1 {
			lines = append(lines, "")
		}
	}
	return lines
}

func (m *browser) portRowLines(name string, row, line int, stacked, compact bool, widths []int) []string {
	p := m.portConfig.Ports[name]
	draft := strings.HasPrefix(name, portDraftPrefix)
	original, existed := m.portOriginal.Ports[name]
	displayName := name
	if draft {
		displayName = ""
	}
	values := []string{displayName, p.Protocol, portNumber(p.ContainerPort), portNumber(p.ServicePort), portNumber(p.NodePort)}
	changed := []bool{draft || !existed || original.ContainerPort != p.ContainerPort || original.ServicePort != p.ServicePort ||
		original.Protocol != p.Protocol, draft || !existed || original.Protocol != p.Protocol, draft || !existed ||
		original.ContainerPort != p.ContainerPort, draft || !existed || original.ServicePort != p.ServicePort, false}
	selected := m.portCursorRow == row
	selectedColumn := -1
	if selected {
		selectedColumn = m.portCursorCol
		m.portCursorLine = line
		if stacked {
			m.portCursorLine += m.portCursorCol
		} else if compact && m.portCursorCol >= 3 {
			m.portCursorLine++
		}
	}
	if m.portEditing && selected {
		values[m.portCursorCol] = m.portEditValue
	}
	if !stacked {
		return portTableRow(domainMarker(cursorGlyph(selected)+" ", selected), values, selected, selectedColumn, m.portEditing, changed,
			widths, compact, false)
	}
	return m.stackedPortRowLines(values, changed, row)
}

func (m *browser) stackedPortRowLines(values []string, changed []bool, row int) []string {
	lines := make([]string, 0, len(values))
	for col, value := range values {
		selected := m.portCursorRow == row && m.portCursorCol == col
		marker := "  "
		if selected {
			marker = domainMarker("› ", true)
		}
		label := domainCell(paint(portColumns[col], browserMuted, true), portsPaneLinesValue9)
		lines = append(lines, marker+label+portCell(value, selected, selected && m.portEditing, changed[col], max(1,
			m.portContentWidth()-portEditorFixedWidth)))
	}
	return lines
}

func (m *browser) portContentWidth() int {
	if m.portMaximized {
		return max(1, m.width-portContentWidthValue2)
	}
	_, width, _ := m.paneWidths()
	return width
}

func (m *browser) portTableWidths(compact bool) []int {
	if compact {
		// Two lines per port keep every cell accessible in the narrow app pane.
		return []int{max(portTableWidthsValue4, m.portContentWidth()-portTableNameMaxWidth), 5, 9, 9, 9}
	}
	return []int{max(portTableWidthsValue8, min(portTableNameMinWidth, m.portContentWidth()-portTableCompactFixedWidth)), 5, 9, 7, 6}
}

func portTableRow(marker string, values []string, selected bool, selectedColumn int, editing bool, changed []bool, widths []int,
	compact, header bool) []string {
	parts := make([]string, len(values))
	for i, value := range values {
		colChanged := changed != nil && i < len(changed) && changed[i]
		colEditing := editing && selectedColumn == i
		width := 12
		if i < len(widths) && widths[i] > 0 {
			width = widths[i]
		}
		if header {
			parts[i] = " " + domainCell(paint(value, browserMuted, true), width) + " "
		} else {
			parts[i] = portCell(value, selected && selectedColumn == i, colEditing, colChanged, width)
		}
	}
	if compact {
		return []string{marker + strings.Join(parts[:3], " "), "  " + strings.Join(parts[3:], " ")}
	}
	return []string{marker + strings.Join(parts, " ")}
}

func portInteractiveRow(selected, writable bool, label, value string) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	if selected && writable {
		label = paint(label, browserAccent, true)
	} else {
		label = paint(label, browserMuted, true)
	}
	return domainMarker(marker, selected) + label + "  " + value
}

func portNumber(port int) string {
	if port == 0 {
		return "—"
	}
	return strconv.Itoa(port)
}

func portEditNumber(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}

func (m *browser) portDiffLines() []string {
	var lines []string
	if m.portOriginal.Type != m.portConfig.Type {
		lines = append(lines, fmt.Sprintf("~ External access: %s → %s", portExposureLabel(m.portOriginal.Type),
			portExposureLabel(m.portConfig.Type)))
	}
	oldNames := make(map[string]bool, len(m.portOriginal.Ports))
	originalNames := make([]string, 0, len(m.portOriginal.Ports))
	for name := range m.portOriginal.Ports {
		originalNames = append(originalNames, name)
	}
	sort.Strings(originalNames)
	for _, name := range originalNames {
		old := m.portOriginal.Ports[name]
		oldNames[name] = true
		current, ok := m.portConfig.Ports[name]
		if !ok {
			lines = append(lines, paint("− "+name, browserNumber, false))
			continue
		}
		if old != current {
			lines = append(lines, fmt.Sprintf("~ %s: %s/%d → %s/%d · %s", safeText(name), old.Protocol, old.ContainerPort, current.Protocol,
				current.ContainerPort, portNumber(current.ServicePort)))
		}
	}
	currentNames := m.portNames()
	for _, name := range currentNames {
		p := m.portConfig.Ports[name]
		if !oldNames[name] {
			servicePort := p.ServicePort
			if servicePort == 0 {
				servicePort = p.ContainerPort
			}
			line := "+ " + safeText(name) + ": " + p.Protocol + "/" + strconv.Itoa(p.ContainerPort) + " → " + strconv.Itoa(servicePort)
			if m.portConfig.Type == serviceTypeLoadBalancer || m.portConfig.Type == serviceTypeNodePort {
				line += " · public port assigned after save"
			}
			lines = append(lines, paint(line, browserAccent, true))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "No service changes")
	}
	return lines
}

func portExposureLabel(svcType string) string {
	if svcType == serviceTypeLoadBalancer || svcType == serviceTypeNodePort {
		return "enabled (" + svcType + ")"
	}
	return "disabled (ClusterIP)"
}

func (m *browser) handlePortsKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.portBusy {
		return nil
	}
	if m.portConfirm {
		return m.handlePortConfirmKey(msg.String())
	}
	if m.portEditing {
		m.handlePortEditKey(msg)
		return nil
	}
	return m.handlePortBrowseKey(msg.String())
}

func (m *browser) handlePortConfirmKey(key string) tea.Cmd {
	switch key {
	case keyEnter, "y":
		m.portConfirm = false
		return m.savePorts()
	case browserKeyEscape, "n":
		m.portConfirm = false
		m.portMessage = messageSaveCancelled
	case "up", "k":
		m.summaryScroll = max(0, m.summaryScroll-1)
	case keyDown, "j":
		m.summaryScroll++
	}
	return nil
}

func (m *browser) handlePortEditKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case browserKeyEscape:
		m.portEditing = false
		m.portEditValue = ""
	case keyEnter:
		m.applyPortEdit()
	case keyCtrlU:
		m.portEditValue = ""
	case keyBackspace, keyCtrlH:
		r := []rune(m.portEditValue)
		if len(r) > 0 {
			m.portEditValue = string(r[:len(r)-1])
		}
	default:
		if msg.Text != "" {
			m.portEditValue += msg.Text
		}
	}
}

func (m *browser) handlePortBrowseKey(key string) tea.Cmd {
	names := m.portNames()
	maxRow := len(names)
	switch key {
	case "up", "k":
		m.portCursorRow = max(0, m.portCursorRow-1)
	case keyDown, "j":
		m.portCursorRow = min(maxRow, m.portCursorRow+1)
	case keyLeft, "h":
		if m.portCursorRow > 0 {
			m.portCursorCol = max(0, m.portCursorCol-1)
		}
	case keyRight, "l", inputKeyTab:
		if m.portCursorRow > 0 {
			m.portCursorCol = min(len(portColumns)-1, m.portCursorCol+1)
		}
	case inputKeyShiftTab:
		if m.portCursorRow > 0 {
			m.portCursorCol = max(0, m.portCursorCol-1)
		}
	case "e":
		m.togglePortExposure(names)
	case "+", "d", "s", "x":
		return m.handlePortWriteKey(key, names)
	case "r":
		return m.refreshPortBrowse()
	case "m":
		m.portMaximized = !m.portMaximized
	case "y":
		return m.copyPortBrowseCell()
	case "c":
		return m.copyPortBrowseAddress()
	}
	return nil
}

func (m *browser) refreshPortBrowse() tea.Cmd {
	if m.portsChanged() {
		m.portMessage = messageSaveOrDiscardBeforeRefresh
		return nil
	}
	m.portState.reset()
	return m.loadPorts()
}

func (m *browser) copyPortBrowseCell() tea.Cmd {
	if m.portCursorRow == 0 {
		return nil
	}
	return m.copyPortCell()
}

func (m *browser) copyPortBrowseAddress() tea.Cmd {
	value := m.portConfig.InternalAddress
	if (m.portConfig.Type == serviceTypeLoadBalancer || m.portConfig.Type == serviceTypeNodePort) && m.portConfig.ExternalAddress != "" {
		value = m.portConfig.ExternalAddress
	}
	if value == "" {
		return nil
	}
	return func() tea.Msg { return clipboardResult{err: clipboard.WriteAll(value)} }
}

func (m *browser) togglePortExposure(names []string) {
	if m.portCursorRow == 0 {
		if m.allowWrites {
			if m.portConfig.Type == serviceTypeLoadBalancer || m.portConfig.Type == "NodePort" {
				m.portConfig.Type = "ClusterIP"
			} else {
				m.portConfig.Type = "LoadBalancer"
			}
		}
		return
	}
	if !m.allowWrites || m.portCursorCol >= 4 || m.portCursorRow > len(names) {
		return
	}
	if m.portCursorCol == 1 {
		name := names[m.portCursorRow-1]
		port := m.portConfig.Ports[name]
		if strings.EqualFold(port.Protocol, "TCP") || port.Protocol == "" {
			port.Protocol = portProtocolUDP
		} else {
			port.Protocol = portProtocolTCP
		}
		m.portConfig.Ports[name] = port
		m.portMessage = ""
		return
	}
	m.beginPortEdit()
}

func (m *browser) handlePortWriteKey(key string, names []string) tea.Cmd {
	switch key {
	case "+":
		if m.allowWrites {
			draft := fmt.Sprintf("%s%d", portDraftPrefix, len(m.portConfig.Ports)+1)
			m.portConfig.Ports[draft] = client.Port{Protocol: portProtocolTCP}
			m.portCursorRow, m.portCursorCol = sort.SearchStrings(m.portNames(), draft)+1, 0
			m.portEditing, m.portEditValue = true, ""
		}
	case "d":
		if m.allowWrites && m.portCursorRow > 0 && m.portCursorRow <= len(names) {
			delete(m.portConfig.Ports, names[m.portCursorRow-1])
			m.portCursorRow = min(m.portCursorRow, len(m.portConfig.Ports))
		}
	case "s":
		if m.allowWrites && m.portsChanged() {
			if err := normalizeAndValidatePorts(&m.portConfig); err != nil {
				m.portMessage = err.Error()
				return nil
			}
			m.portConfirm = true
			m.summaryScroll = 1 << handlePortsKeyValue20
		}
	case "x":
		if m.allowWrites && m.portsChanged() {
			m.portConfig = clonePortConfig(m.portOriginal)
			m.portMessage = messageChangesDiscarded
		}
	}
	return nil
}

func (m *browser) beginPortEdit() {
	names := m.portNames()
	if m.portCursorRow <= 0 {
		return
	}
	name := ""
	if m.portCursorRow <= len(names) {
		name = names[m.portCursorRow-1]
	}
	if name == "" {
		m.portCursorCol = 0
		m.portEditing = true
		m.portEditValue = ""
		return
	}
	p := m.portConfig.Ports[name]
	values := []string{name, p.Protocol, portEditNumber(p.ContainerPort), portEditNumber(p.ServicePort), portNumber(p.NodePort)}
	if m.portCursorCol >= beginPortEditValue4 {
		return
	}
	m.portEditing = true
	m.portEditValue = values[m.portCursorCol]
}

func (m *browser) applyPortEdit() {
	value := strings.TrimSpace(m.portEditValue)
	names := m.portNames()
	isNew := m.portCursorRow > 0 && m.portCursorRow <= len(names) && strings.HasPrefix(names[m.portCursorRow-1], portDraftPrefix)
	oldName := ""
	if m.portCursorRow > 0 && m.portCursorRow <= len(names) {
		oldName = names[m.portCursorRow-1]
	}
	if m.portCursorCol == 0 {
		if !m.applyPortNameEdit(value, oldName) {
			return
		}
	} else if !m.applyPortFieldEdit(value, oldName, isNew) {
		return
	}
	m.portEditing = false
	m.portEditValue = ""
	m.portMessage = ""
	if m.portCursorCol == 0 {
		m.portCursorRow = sort.SearchStrings(m.portNames(), value) + 1
	}
}

func (m *browser) applyPortNameEdit(value, oldName string) bool {
	if !validPortName(value) {
		m.portMessage = "Use a lowercase port name with letters, numbers or hyphens"
		return false
	}
	if value == oldName {
		return true
	}
	if _, exists := m.portConfig.Ports[value]; exists {
		m.portMessage = "Port names must be unique"
		return false
	}
	p := m.portConfig.Ports[oldName]
	delete(m.portConfig.Ports, oldName)
	m.portConfig.Ports[value] = p
	return true
}

func (m *browser) applyPortFieldEdit(value, oldName string, isNew bool) bool {
	if isNew {
		m.portMessage = "Set the port name first"
		return false
	}
	p := m.portConfig.Ports[oldName]
	switch m.portCursorCol {
	case 1:
		protocol := strings.ToUpper(value)
		if protocol != "TCP" && protocol != "UDP" {
			m.portMessage = "Protocol must be TCP or UDP"
			return false
		}
		p.Protocol = protocol
	case applyPortEditValue2, applyPortEditAdditionalField:
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			m.portMessage = "Port must be between 1 and 65535"
			return false
		}
		if m.portCursorCol == applyPortEditValue2 {
			p.ContainerPort = n
			if p.ServicePort == 0 {
				p.ServicePort = n
			}
		} else {
			p.ServicePort = n
		}
	}
	m.portConfig.Ports[oldName] = p
	return true
}

func normalizeAndValidatePorts(config *PortConfig) error {
	external := config.Type == "LoadBalancer" || config.Type == "NodePort"
	if external && len(config.Ports) == 0 {
		return errAddAtLeastOnePortBeforeEnabling
	}
	for name, port := range config.Ports {
		if strings.HasPrefix(name, portDraftPrefix) || !validPortName(name) {
			return errFinishNamingEveryPortBeforeSaving
		}
		if port.Protocol != "TCP" && port.Protocol != "UDP" {
			return fmt.Errorf("%w: %s protocol must be TCP or UDP", errSProtocolMustBeTCPOrUDP, name)
		}
		if port.ContainerPort < 1 || port.ContainerPort > 65535 {
			return fmt.Errorf("%w: %s container port must be between 1 and 65535", errSContainerPortMustBeBetween1, name)
		}
		if port.ServicePort == 0 {
			port.ServicePort = port.ContainerPort
		}
		if port.ServicePort < 1 || port.ServicePort > 65535 {
			return fmt.Errorf("%w: %s service port must be between 1 and 65535", errSServicePortMustBeBetween1, name)
		}
		config.Ports[name] = port
	}
	return nil
}

func validPortName(name string) bool {
	if len(name) == 0 || len(name) > 15 || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func (m *browser) copyPortCell() tea.Cmd {
	names := m.portNames()
	if m.portCursorRow > len(names) {
		return nil
	}
	name := names[m.portCursorRow-1]
	p := m.portConfig.Ports[name]
	values := []string{name, p.Protocol, strconv.Itoa(p.ContainerPort), strconv.Itoa(p.ServicePort), portNumber(p.NodePort)}
	value := values[m.portCursorCol]
	return func() tea.Msg { return clipboardResult{err: clipboard.WriteAll(value)} }
}

func (m *browser) savePorts() tea.Cmd {
	if m.updatePorts == nil {
		m.portMessage = "Port writes unavailable"
		return nil
	}
	m.portBusy = true
	m.portMessage = "Saving service ports…"
	id, tenant, config := m.selectedDetailTarget(), m.tenant, clonePortConfig(m.portConfig)
	return func() tea.Msg {
		err := m.updatePorts(m.ctx, tenant, id, config)
		return portsWriteResult{appID: id, config: config, err: err}
	}
}

func (m *browser) handlePortsWrite(msg portsWriteResult) tea.Cmd {
	if msg.appID != m.selectedDetailTarget() {
		return nil
	}
	m.portBusy = false
	if msg.err != nil {
		m.portMessage = "Could not save service ports"
		return nil
	}
	m.portConfig = clonePortConfig(msg.config)
	m.portOriginal = clonePortConfig(msg.config)
	m.portMessage = "Service ports saved"
	return tea.Batch(m.loadPorts(), m.loadSelectedSummary())
}

type clipboardResult struct{ err error }
