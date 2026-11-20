package browser

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type terminalFinishedMsg struct{ err error }

func (m *browser) beginTerminalPicker() tea.Cmd {
	if m.podsState.status == requestIdle || m.podsState.status == requestFailed || m.podsState.status == requestUnavailable {
		m.terminalRequestPending = true
		return m.loadSelectedPods()
	}
	if m.podsState.active() {
		m.terminalRequestPending = true
		return nil
	}
	if len(m.podList) == 0 {
		m.actionNotice = "No pods are available for terminal access."
		return nil
	}
	m.terminalChoosing, m.logTargetPicker = true, true
	m.logPickerColumn, m.logPodCursor, m.logContainerCursor = 0, 0, 0
	for i, pod := range m.podList {
		if pod.Name == m.terminalPod {
			m.logPodCursor = i
			break
		}
	}
	return nil
}

func (m *browser) handleTerminalPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case browserKeyEscape, "t":
		m.logTargetPicker = false
		m.terminalChoosing = false
	case keyLeft, "h":
		m.logPickerColumn = max(0, m.logPickerColumn-1)
	case keyRight, "l":
		m.logPickerColumn = min(1, m.logPickerColumn+1)
	case "up", "k":
		if m.logPickerColumn == 0 {
			m.logPodCursor = max(0, m.logPodCursor-1)
			m.logContainerCursor = 0
		} else if m.logPodCursor >= 0 && m.logPodCursor < len(m.podList) {
			m.logContainerCursor = max(0, m.logContainerCursor-1)
		}
	case keyDown, "j":
		if m.logPickerColumn == 0 {
			m.logPodCursor = min(len(m.podList)-1, m.logPodCursor+1)
			m.logContainerCursor = 0
		} else if m.logPodCursor >= 0 && m.logPodCursor < len(m.podList) {
			m.logContainerCursor = min(max(0, len(m.podList[m.logPodCursor].Containers)-1), m.logContainerCursor+1)
		}
	case keyEnter:
		if m.logPodCursor < 0 || m.logPodCursor >= len(m.podList) {
			return nil
		}
		pod := m.podList[m.logPodCursor]
		container := defaultNameMain
		if len(pod.Containers) > 0 {
			container = pod.Containers[min(m.logContainerCursor, len(pod.Containers)-1)].Name
		}
		m.terminalPod, m.terminalContainer = pod.Name, container
		m.logTargetPicker = false
		m.terminalChoosing = false
		return m.connectTerminal()
	}
	return nil
}

func (m *browser) connectTerminal() tea.Cmd {
	if m.terminal == nil {
		m.actionNotice = "Terminal is unavailable"
		return nil
	}
	if m.terminalPod == "" && len(m.podList) == 1 {
		m.terminalPod = m.podList[0].Name
	}
	if m.terminalPod == "" {
		m.actionNotice = "Choose a pod before opening the terminal"
		return nil
	}
	var appName string
	if m.selectedAppVisible() {
		appName = m.apps[m.selected].Name
	}
	found := false
	for _, pod := range m.podList {
		if pod.Name == m.terminalPod {
			found = true
			if m.terminalContainer == "" && len(pod.Containers) > 0 {
				m.terminalContainer = pod.Containers[0].Name
			}
			break
		}
	}
	if !found {
		m.terminalPod, m.terminalContainer = "", ""
		m.actionNotice = "Selected pod is no longer listed; reopen Terminal to refresh"
		return nil
	}
	if m.terminalContainer == "" {
		m.terminalContainer = defaultNameMain
	}
	command := m.terminal(m.ctx, m.tenant, m.selectedDetailTarget(), appName, m.terminalPod, m.terminalContainer)
	if command == nil {
		m.actionNotice = "Could not prepare terminal"
		return nil
	}
	m.actionNotice = fmt.Sprintf("Opening terminal for %s · pod %s · container %s", appName, m.terminalPod, m.terminalContainer)
	return tea.Exec(command, func(err error) tea.Msg { return terminalFinishedMsg{err: err} })
}

func (m *browser) handleTerminalFinished(msg terminalFinishedMsg) tea.Model {
	switch {
	case msg.err != nil && strings.Contains(msg.err.Error(), "Console login required"):
		m.actionNotice = "Console login required · run darkubectl login"
	case msg.err != nil:
		m.actionNotice = "Terminal session ended with an error"
	default:
		m.actionNotice = "Terminal session ended"
	}
	m.terminalPod, m.terminalContainer = "", ""
	return m
}

// The fixture runner prints the same target/controls header while making no
// network connection and executing no shell command.
type mockTerminalCommand struct {
	appName, pod, container string
	stdin                   io.Reader
	stdout, stderr          io.Writer
}

func (c *mockTerminalCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *mockTerminalCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *mockTerminalCommand) SetStderr(w io.Writer) { c.stderr = w }
func (c *mockTerminalCommand) Run() error {
	const preview = "\r\nMOCK TERMINAL PREVIEW · no remote connection made\r\n" +
		"App: %s · pod: %s · container: %s\r\n" +
		"Live controls: Ctrl+D exits · Ctrl+C interrupts the remote process · Ctrl+L clears the screen. " +
		"Use your terminal's usual copy and paste shortcuts.\r\n" +
		"Press Ctrl+D to return to the browser. Commands are not executed in mock mode.\r\nmock$ "
	if _, err := fmt.Fprintf(c.stdout, preview, c.appName, c.pod, c.container); err != nil {
		return err
	}
	reader := bufio.NewReader(c.stdin)
	for {
		b, err := reader.ReadByte()
		if err == io.EOF || b == 0x04 {
			return nil
		}
		if err != nil {
			return err
		}
		if b == '\r' || b == '\n' {
			if _, writeErr := fmt.Fprint(c.stdout, "\r\nMOCK · shell commands are disabled\r\nmock$ "); writeErr != nil {
				return writeErr
			}
		}
	}
}
