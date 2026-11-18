package browser

import (
	"charm.land/lipgloss/v2"
	"encoding/json"
	"fmt"
	"github.com/rahacloud/darkubectl/internal/client"
	"github.com/rahacloud/darkubectl/internal/output"
	"math"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
)

func (m *browser) summaryPaneLines() []string {
	if m.activeAppView == appViewUsage {
		return m.usagePaneLines()
	}
	if m.activeAppView == appViewPods {
		return m.podsPaneLines()
	}
	heading := m.appViewTabs()
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{heading, paint("No app selected. Move to an app to preview its summary.", output.ColorMuted, false)}
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	stateGlyph, stateColor := stateStyle(appState(a))
	lines := []string{heading, paint("· "+safeText(a.Name), output.ColorKey, true)}
	if m.detailState.err != nil {
		lines = append(lines, paint("Detail refresh failed: "+safeText(m.detailState.err.Error()), output.ColorNumber, false))
		if m.detailState.status == requestStale {
			lines = append(lines, staleRequestTime("Summary", m.detailState))
		}
	} else if m.detailState.status == requestUnavailable {
		lines = append(lines, paint("App detail is unavailable; showing the list snapshot.", output.ColorMuted, false))
	}
	if m.usageState.err != nil {
		lines = append(lines, paint("Resource usage unavailable: "+safeText(m.usageState.err.Error()), output.ColorMuted, false))
		if m.usageState.status == requestStale {
			lines = append(lines, staleRequestTime("Usage", m.usageState))
		}
	} else if m.usageState.status == requestUnavailable {
		lines = append(lines, paint("Usage unavailable · showing configured totals", output.ColorMuted, false))
	}
	replicas := "-"
	if a.ReplicasPresent {
		replicas = fmt.Sprintf("%d", a.Replicas)
	}
	lines = append(lines, "",
		summaryField("Tenant", displayValue(m.tenant)),
		summaryField("Namespace", displayValue(a.Namespace.Name)),
		summaryField("Cluster", displayValue(a.Namespace.Cluster.Name)),
		summaryField("State", paint(stateGlyph+" "+safeText(appState(a)), stateColor, true)),
		summaryField("Description", displayValue(a.State.Description)),
		summaryField("Replicas", replicas))
	plan := planName(a.Plan)
	if plan == "-" {
		lines = append(lines, summaryLabel("Plan"))
	} else {
		lines = append(lines, summaryField("Plan", plan))
	}
	usage := m.detailUsage
	if m.usageAppID != a.ID {
		usage = nil
	}
	cpuTotal := "-"
	ramTotal := "-"
	if a.Plan != nil {
		if a.Plan.Detail.CPURequest > 0 {
			cpuTotal = fmt.Sprintf("%dm", a.Plan.Detail.CPURequest)
		}
		if a.Plan.Detail.RAMLimit > 0 {
			ramTotal = fmt.Sprintf("%dM", a.Plan.Detail.RAMLimit)
		}
	}
	if cpuTotal == "-" {
		cpuTotal = displayValue(a.CPURequest)
	}
	if ramTotal == "-" {
		ramTotal = displayValue(a.RAMLimit)
	}
	lines = append(lines,
		planResourceField("", "CPU", resourceUsageText(usageValue(usage, func(u *AppUsage) *float64 { return u.CPUCores }), cpuTotal, formatCores, usage != nil && usage.Failed.CPU)),
		planResourceField("", "RAM", resourceUsageText(usageValue(usage, func(u *AppUsage) *float64 { return u.RAMBytes }), ramTotal, formatBytes, usage != nil && usage.Failed.RAM)))
	if (a.Disk != nil && a.Disk.SizeInGi > 0) || (usage != nil && (usage.DiskBytes != nil || usage.Failed.Disk)) {
		diskTotal := "-"
		if a.Disk != nil && a.Disk.SizeInGi > 0 {
			diskTotal = fmt.Sprintf("%d GiB", a.Disk.SizeInGi)
		}
		lines = append(lines, planResourceField("", "Disk", resourceUsageText(usageValue(usage, func(u *AppUsage) *float64 { return u.DiskBytes }), diskTotal, formatBytes, usage != nil && usage.Failed.Disk)))
	}
	if a.Service != nil {
		if value := displayValue(a.Service.Type); value != "-" {
			lines = append(lines, summaryField("Service", value))
		}
		if port, ok := primaryServicePort(a.Service.Ports); ok {
			servicePort := port.ServicePort
			if servicePort == 0 {
				servicePort = port.ContainerPort
			}
			if servicePort > 0 {
				lines = append(lines, summaryField("Port Service", fmt.Sprintf("%d", servicePort)))
			}
		}
		if value := strings.TrimSpace(a.Service.InternalAddress); value != "" {
			lines = append(lines, summaryField("Internal Address", internalAddressDisplay(value, a.Service.Ports)))
		}
		lines = append(lines, summaryField("External IP", displayValue(a.Service.ExternalIP)))
	} else {
		lines = append(lines, summaryField("External IP", "-"))
	}
	lines = append(lines, summaryField("Outbound IP", m.egressIPForApp(a)))
	if probe := readinessProbeDisplay(a); probe != "" {
		lines = append(lines, summaryField("Readiness Probe", probe))
	}
	if command := appCommandDisplay(a); command != "" {
		lines = append(lines, summaryField("Command", command))
	}
	if len(a.Inputs) > 0 {
		lines = append(lines, summaryField("Inputs", strings.Join(a.Inputs, ", ")))
	}
	lines = append(lines, summaryField("Created via", creationMethodLabel(a.CreationMethod)), summaryField("Image", displayValue(a.Image())))
	domains := appDomains(a)
	if len(domains) == 0 {
		lines = append(lines, summaryField("Domains", "-"))
	} else {
		for i, host := range domains {
			label := ""
			if i == 0 {
				label = "Domains"
			}
			lines = append(lines, summaryLabel(label)+domainLink(host))
		}
	}
	lines = append(lines, summaryField("Updated", formatLocalTime(a.UpdatedAt)), summaryField("ID", displayValue(a.ID)))
	return lines
}

func appDomains(app client.App) []string {
	domains := make([]string, 0, len(app.ExternalHosts)+3)
	seen := make(map[string]struct{}, len(app.ExternalHosts)+3)
	hosts := append([]string{app.CustomDomainAddress}, app.ExternalHosts...)
	if app.Service != nil {
		hosts = append(hosts, app.Service.ExternalAddress)
	}
	for _, domain := range hosts {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
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
			return safeText(net.JoinHostPort(address, fmt.Sprint(servicePort)))
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
	if app.CustomConfig != nil && app.CustomConfig.Container != nil && app.CustomConfig.Container.ReadinessProbe != nil {
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
	}
	if path := strings.TrimSpace(app.ReadinessProbePath); path != "" {
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
	return paint(fmt.Sprintf("%-16s ", label), output.ColorMuted, true)
}

func staleRequestTime(label string, state requestState) string {
	if state.lastSuccessAt.IsZero() {
		return paint(label+" has no successful snapshot yet.", output.ColorMuted, false)
	}
	when := state.lastSuccessAt.Local().Format("Jan 2, 3:04 PM MST")
	return paint(label+" last succeeded "+when+" · press r to retry", output.ColorMuted, false)
}

func summaryField(label, value string) string {
	return summaryLabel(label) + paint(value, output.ColorBranch, false)
}

func planResourceField(icon, label, value string) string {
	return summaryLabel("") + paint(icon+" "+label+" ", output.ColorMuted, true) + paint(value, output.ColorBranch, false)
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return safeText(value)
}

func planName(plan *client.Plan) string {
	if plan == nil {
		return "-"
	}
	if plan.Name != "" {
		return safeText(plan.Name)
	}
	if plan.CodeName != "" {
		return safeText(plan.CodeName)
	}
	return "-"
}

func formatCores(cores float64) string {
	return fmt.Sprintf("%dm", int(math.Round(cores*1000)))
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
		return paint(host, output.ColorKey, false)
	}
	style := lipgloss.NewStyle().Underline(true).Hyperlink(target)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.Foreground(output.ColorBranch)
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
		return "◇ Unknown"
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
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return fallbackText(value)
	}
	return parsed.Local().Format("Jan 2, 2006 · 3:04 PM MST")
}

func appState(a client.App) string {
	if a.State.Text != "" {
		return a.State.Text
	}
	if a.State.StateType != "" {
		return a.State.StateType
	}
	return "Unknown"
}
