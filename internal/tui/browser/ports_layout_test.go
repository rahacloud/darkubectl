package browser

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

func TestPortTableFitsPaneAndShowsEveryColumn(t *testing.T) {
	values := []string{"main", "TCP", "27017", "27017", "30161"}
	for _, paneWidth := range []int{32, 40, 57, 58, 90} {
		m := &browser{width: paneWidth + 2, portMaximized: true}
		compact := paneWidth < 58
		for selected := range values {
			lines := portTableRow("› ", values, true, selected, false, nil, m.portTableWidths(compact), compact, false)
			joined := ansi.Strip(strings.Join(lines, " "))
			for _, value := range values {
				if !strings.Contains(joined, value) {
					t.Errorf("width %d, selected %d: missing %q in %q", paneWidth, selected, value, joined)
				}
			}
			for _, line := range lines {
				if width := ansi.StringWidth(line); width > paneWidth {
					t.Errorf("width %d, selected %d: row uses %d columns", paneWidth, selected, width)
				}
			}
		}
	}
}

func TestPortTableHeadersAlignWithValues(t *testing.T) {
	values := []string{"main", "TCP", "8080", "9091", "30421"}
	for _, width := range []int{40, 90} {
		m := &browser{width: width + 2, portMaximized: true}
		compact := width < 58
		widths := m.portTableWidths(compact)
		header := portTableRow("  ", portColumns, false, -1, false, nil, widths, compact, true)
		for _, selected := range []bool{false, true} {
			marker := cursorGlyph(selected) + " "
			row := portTableRow(marker, values, selected, 3, false, nil, widths, compact, false)
			for col, value := range values {
				line := 0
				if compact && col >= 3 {
					line = 1
				}
				head, body := ansi.Strip(header[line]), ansi.Strip(row[line])
				headerIndex, valueIndex := strings.Index(head, portColumns[col]), strings.Index(body, value)
				if headerIndex < 0 || valueIndex < 0 || ansi.StringWidth(head[:headerIndex]) != ansi.StringWidth(body[:valueIndex]) {
					t.Errorf("width %d selected %v column %s: header %q row %q", width, selected, portColumns[col], head, body)
				}
			}
		}
	}
}

func TestPortEditingStartsWithE(t *testing.T) {
	m := &browser{allowWrites: true, portCursorRow: 1, portConfig: PortConfig{Type: "ClusterIP", Ports: map[string]client.Port{"main": {Protocol: "TCP", ContainerPort: 8080, ServicePort: 80}}}}
	m.handlePortsKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.portEditing {
		t.Fatal("Enter unexpectedly started editing")
	}
	m.handlePortsKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if !m.portEditing || m.portEditValue != "main" {
		t.Fatal("e should edit the selected cell")
	}
}

func TestPortProtocolTogglesWithE(t *testing.T) {
	m := &browser{allowWrites: true, portCursorRow: 1, portCursorCol: 1, portConfig: PortConfig{Type: "ClusterIP", Ports: map[string]client.Port{"main": {Protocol: "TCP", ContainerPort: 8080, ServicePort: 80}}}}
	key := tea.KeyPressMsg{Code: 'e', Text: "e"}
	m.handlePortsKey(key)
	if m.portEditing || m.portConfig.Ports["main"].Protocol != "UDP" {
		t.Fatal("e should toggle TCP to UDP without opening an editor")
	}
	m.handlePortsKey(key)
	if m.portConfig.Ports["main"].Protocol != "TCP" {
		t.Fatal("e should toggle UDP to TCP")
	}
}

func TestPortCellKeepsEditingCaretVisible(t *testing.T) {
	cell := ansi.Strip(portCell("a-very-long-port-name", true, true, false, 8))
	if !strings.Contains(cell, "█") || ansi.StringWidth(cell) > 10 {
		t.Fatalf("editing cell = %q", cell)
	}
}
