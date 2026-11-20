package browser

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	appDomainsValue3     = 3
	formatCoresValue1000 = 1000
)

func (m *browser) summaryPaneLines() []string {
	if lines := m.appViewLines(); lines != nil {
		return lines
	}
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{summaryField("App", "not selected")}
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	stateGlyph, stateColor := stateStyle(appState(a))
	lines := []string{sectionHeading("Identity"), summaryField("Name", displayValue(a.Name))}
	if m.detailState.err != nil {
		lines = append(lines, summaryField("Refresh", paint("failed · "+safeText(m.detailState.err.Error()), browserNumber, false)))
		if m.detailState.status == requestStale {
			lines = append(lines, staleRequestTime("Summary", m.detailState))
		}
	} else if m.detailState.status == requestUnavailable {
		lines = append(lines, summaryField("Detail", "list snapshot"))
	}
	replicas := "-"
	if a.ReplicasPresent {
		replicas = strconv.Itoa(a.Replicas)
	}
	lines = append(lines,
		summaryField("Tenant", displayValue(m.tenant)),
		summaryField("Namespace", displayValue(a.Namespace.Name)),
		summaryField("Cluster", displayValue(a.Namespace.Cluster.Name)),
		summaryField("ID", displayValue(a.ID)),
		"", sectionHeading("Status"),
		summaryField("State", paint(stateGlyph+" "+safeText(appState(a)), stateColor, true)),
		summaryField("Description", displayValue(a.State.Description)),
		summaryField("Replicas", replicas),
		"", sectionHeading("Resources"),
		resourceField("", "CPU", resourceLimit(a, "cpu")),
		resourceField("", "RAM", resourceLimit(a, "ram")),
		resourceField("", "Disk", resourceLimit(a, "disk")),
		"", sectionHeading("Networking"))
	serviceType, servicePort, internalAddress, externalIP := serviceSummary(a)
	lines = append(lines,
		summaryField("Service", serviceType),
		summaryField("Port Service", servicePort),
		summaryField("Internal Address", internalAddress),
		summaryField("External IP", externalIP))
	lines = append(lines, summaryField("Outbound IP", m.egressIPForApp(a)))
	domains := appDomains(a)
	if len(domains) == 0 {
		lines = append(lines, summaryField("Domains", "-"))
	} else {
		for i, host := range domains {
			label := ""
			if i == 0 {
				label = labelDomains
			}
			lines = append(lines, summaryLabel(label)+domainLink(host))
		}
	}
	lines = append(lines,
		"", sectionHeading("Runtime"),
		summaryField("Readiness Probe", displayValue(readinessProbeDisplay(a))),
		summaryField("Command", displayValue(appCommandDisplay(a))),
		"", sectionHeading("Deployment"),
		summaryField("Image", displayValue(a.Image())),
		summaryField("Deployment type", deploymentTypeValue(a)),
		summaryField("Created via", creationMethodLabel(a.CreationMethod)),
		summaryField("Updated", formatLocalTime(a.UpdatedAt)))
	lines = append(lines, "", sectionHeading("Pods"))
	lines = m.appendPodsStatus(lines)
	return lines
}

func serviceSummary(app client.App) (string, string, string, string) {
	serviceType, servicePort, internalAddress, externalIP := "-", "-", "-", "-"
	if app.Service == nil {
		return serviceType, servicePort, internalAddress, externalIP
	}
	serviceType = displayValue(app.Service.Type)
	internalAddress = serviceInternalAddress(app.Service)
	externalIP = displayValue(app.Service.ExternalIP)
	servicePort = servicePortDisplay(app.Service)
	return serviceType, servicePort, internalAddress, externalIP
}

func serviceInternalAddress(service *client.AppService) string {
	if strings.TrimSpace(service.InternalAddress) == "" {
		return "-"
	}
	return internalAddressDisplay(service.InternalAddress, service.Ports)
}

func servicePortDisplay(service *client.AppService) string {
	port, ok := primaryServicePort(service.Ports)
	if !ok {
		return "-"
	}
	portNumber := port.ServicePort
	if portNumber == 0 {
		portNumber = port.ContainerPort
	}
	if portNumber == 0 {
		return "-"
	}
	return strconv.Itoa(portNumber)
}

func (m *browser) appViewLines() []string {
	switch m.activeAppView {
	case appViewUsage:
		return m.resourcesPaneLines()
	case appViewScaling:
		return m.scalingPaneLines()
	case appViewLogs:
		return m.logsPaneLines()
	case appViewBuilds:
		return m.buildsPaneLines()
	case appViewEnv:
		return m.envPaneLines()
	case appViewDomains:
		return m.domainsPaneLines()
	case appViewPorts:
		return m.portsPaneLines()
	default:
		return nil
	}
}

func deploymentTypeValue(app client.App) string {
	switch app.BuildMethod {
	case client.BuildMethodWebhook:
		return paint("↻", browserBranch, true) + " Darkube"
	case client.BuildMethodGitlabCI:
		return paint("⚙", browserAccent, true) + " Custom CI/CD"
	}
	return paint("?", browserMuted, true) + " Unknown"
}

func appDomains(app client.App) []string {
	domains := make([]string, 0, len(app.ExternalHosts)+appDomainsValue3)
	seen := make(map[string]struct{}, len(app.ExternalHosts)+appDomainsValue3)
	hosts := append([]string{app.CustomDomainAddress}, app.ExternalHosts...)
	if app.Service != nil {
		hosts = append(hosts, app.Service.ExternalAddress)
	}
	for _, domain := range hosts {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if app.Service != nil && strings.HasSuffix(strings.ToLower(domain), ".hsvc.ir") {
			if port, ok := primaryServicePort(app.Service.Ports); ok && port.ServicePort > 0 {
				domain = net.JoinHostPort(domain, strconv.Itoa(port.ServicePort))
			}
		}
		key := strings.ToLower(domain)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		domains = append(domains, domain)
	}
	return domains
}

func internalAddressDisplay(address string, ports map[string]client.Port) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return safeText(address)
	}
	port, ok := primaryServicePort(ports)
	if ok {
		servicePort := port.ServicePort
		if servicePort == 0 {
			servicePort = port.ContainerPort
		}
		if servicePort > 0 {
			return safeText(net.JoinHostPort(address, strconv.Itoa(servicePort)))
		}
	}
	return safeText(address)
}

func primaryServicePort(ports map[string]client.Port) (client.Port, bool) {
	if port, ok := ports["main"]; ok {
		return port, true
	}
	if len(ports) == 1 {
		for _, port := range ports {
			return port, true
		}
	}
	return client.Port{}, false
}

func appCommandDisplay(app client.App) string {
	command := strings.TrimSpace(app.Command)
	args := strings.TrimSpace(app.Args)
	switch {
	case command == "":
		return args
	case args == "":
		return command
	default:
		return command + " " + args
	}
}

func readinessProbeDisplay(app client.App) string {
	if app.CustomConfig == nil || app.CustomConfig.Container == nil || app.CustomConfig.Container.ReadinessProbe == nil {
		return readinessPathDisplay(app.ReadinessProbePath)
	}
	probe := app.CustomConfig.Container.ReadinessProbe
	if probe.Exec != nil && len(probe.Exec.Command) > 0 {
		return "exec: " + strings.Join(probe.Exec.Command, " ")
	}
	if probe.HTTPGet != nil {
		value := "HTTP GET " + displayValue(probe.HTTPGet.Path)
		if port := jsonScalar(probe.HTTPGet.Port); port != "" {
			value += ":" + port
		}
		return value
	}
	if probe.TCPSocket != nil {
		return "TCP " + jsonScalar(probe.TCPSocket.Port)
	}
	return readinessPathDisplay(app.ReadinessProbePath)
}

func readinessPathDisplay(path string) string {
	if path = strings.TrimSpace(path); path != "" {
		return "HTTP GET " + safeText(path)
	}
	return ""
}

func jsonScalar(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return safeText(value)
	}
	if string(raw) == "null" {
		return ""
	}
	return safeText(string(raw))
}

func summaryLabel(label string) string {
	return paint(fmt.Sprintf("%-16s ", label), browserMuted, true)
}

func sectionHeading(label string) string {
	return paint(label, browserAccent, true)
}

func staleRequestTime(label string, state requestState) string {
	if state.lastSuccessAt.IsZero() {
		return summaryField(label+" updated", "no successful snapshot")
	}
	when := formatLocalWallTime(state.lastSuccessAt, "Jan 2, 3:04 PM MST")
	return summaryField(label+" updated", when)
}

func summaryField(label, value string) string {
	return summaryLabel(label) + paint(value, browserValue, false)
}

func resourceField(icon, label, value string) string {
	return summaryLabel(icon+" "+label) + paint(value, browserValue, false)
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return safeText(value)
}

func formatCores(cores float64) string {
	return fmt.Sprintf("%dm", int(math.Round(cores*formatCoresValue1000)))
}

func formatBytes(bytes float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := bytes
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func domainLink(host string) string {
	host = strings.TrimSpace(safeText(host))
	if host == "" {
		return "-"
	}
	target := host
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return paint(host, browserKey, false)
	}
	style := lipgloss.NewStyle().Underline(true).Hyperlink(target)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.Foreground(browserBranch)
	}
	return style.Render(host)
}

func creationMethodLabel(method string) string {
	method = strings.TrimSpace(method)
	switch strings.ToLower(method) {
	case "git_repo_url":
		return " Git"
	case "docker_image":
		return " Docker"
	case "":
		return "-"
	}
	short := strings.ReplaceAll(method, "_", " ")
	short = strings.ReplaceAll(short, "-", " ")
	short = strings.TrimSuffix(short, "new")
	short = strings.TrimSpace(short)
	if short == "" {
		short = method
	}
	words := strings.Fields(short)
	for i, word := range words {
		if strings.EqualFold(word, "postgresql") {
			words[i] = "PostgreSQL"
		} else {
			runes := []rune(strings.ToLower(word))
			if len(runes) > 0 {
				runes[0] = unicode.ToUpper(runes[0])
			}
			words[i] = string(runes)
		}
	}
	return " " + safeText(strings.Join(words, " "))
}

func formatLocalTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fallbackText(value)
	}
	return formatLocalWallTime(parsed, "Jan 2, 2006 · 3:04 PM MST")
}

func appState(a client.App) string {
	if a.State.Text != "" {
		return a.State.Text
	}
	if a.State.StateType != "" {
		return a.State.StateType
	}
	return "-"
}
