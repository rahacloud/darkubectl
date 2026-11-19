package browser

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
)

// DomainConfig is the verified app ingress configuration editable in the TUI.
type DomainConfig struct {
	Hosts                  []string
	Subdomain              string
	BaseDomain             string
	CNAME                  string
	Challenge              string
	SSL                    bool
	RedirectHTTPS          bool
	HTTP2                  bool
	DNS                    map[string]bool
	Certificates           map[string]string
	ChallengeRecords       map[string]string
	ChallengeRecordsFailed bool
}

type domainsResult struct {
	meta   requestMeta
	config DomainConfig
	err    error
}
type domainsWriteResult struct {
	appID  string
	config DomainConfig
	err    error
}
type domainCopyResult struct {
	tenant, appID, field string
	err                  error
}

func (m *browser) loadDomains() tea.Cmd {
	id := m.selectedDetailTarget()
	if id == "" || m.domains == nil {
		m.domainState.unavailable(fmt.Errorf("domain view unavailable"))
		return nil
	}
	meta, ctx := m.startRequest(requestDomains, m.tenant, id, m.selectionGeneration)
	m.domainState.begin(meta, m.requestTime())
	return func() tea.Msg {
		c, err := m.domains(ctx, meta.tenant, id)
		return domainsResult{meta: meta, config: c, err: err}
	}
}

func (m *browser) handleDomainsResult(msg domainsResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestDomains, msg.meta)
	if !m.domainState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.selectionGeneration || msg.meta.appID != m.selectedDetailTarget() {
		return m, nil
	}
	if msg.err != nil {
		m.domainState.fail(fmt.Errorf("could not load domain status"))
		return m, nil
	}
	m.domainConfig = cloneDomainConfig(msg.config)
	m.domainOriginal = cloneDomainConfig(msg.config)
	m.domainCursor = min(m.domainCursor, m.domainSettingsStart()+2)
	m.domainState.succeed(len(msg.config.Hosts) == 0 && msg.config.Subdomain == "", m.requestTime())
	return m, nil
}

func cloneDomainConfig(c DomainConfig) DomainConfig {
	c.Hosts = append([]string(nil), c.Hosts...)
	if c.DNS != nil {
		d := map[string]bool{}
		for k, v := range c.DNS {
			d[k] = v
		}
		c.DNS = d
	}
	if c.Certificates != nil {
		d := map[string]string{}
		for k, v := range c.Certificates {
			d[k] = v
		}
		c.Certificates = d
	}
	if c.ChallengeRecords != nil {
		d := map[string]string{}
		for k, v := range c.ChallengeRecords {
			d[k] = v
		}
		c.ChallengeRecords = d
	}
	return c
}

func (m *browser) domainRecordNames() []string {
	if m.domainConfig.Challenge != "dns01" {
		return nil
	}
	names := make([]string, 0, len(m.domainConfig.ChallengeRecords))
	for name := range m.domainConfig.ChallengeRecords {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *browser) domainsPaneLines() []string {
	if !m.selectedAppActive() {
		return []string{summaryField("App", "not selected")}
	}
	lines := []string{domainSectionHeading("Custom domains", len(m.domainConfig.Hosts), "host")}
	m.domainCursorLine = 0
	if m.domainState.active() && len(m.domainConfig.Hosts) == 0 {
		lines = append(lines, "Loading domain status…")
	}
	if m.domainState.err != nil {
		lines = append(lines, paint("Domain status unavailable · "+safeText(m.domainState.err.Error()), browserNumber, false))
	}
	addingExternal := m.domainEditing && !m.domainEditSubdomain
	if len(m.domainConfig.Hosts) == 0 && !addingExternal {
		lines = append(lines, summaryField("Domains", "none"))
	} else {
		hostWidth, dnsWidth, certWidth := m.domainColumnWidths()
		lines = append(lines, domainHostRow("  ", paint("DOMAIN", browserMuted, true), paint("DNS", browserMuted, true), paint("CERTIFICATE", browserMuted, true), hostWidth, dnsWidth, certWidth))
	}
	hostWidth, dnsWidth, certWidth := m.domainColumnWidths()
	for i, h := range m.domainConfig.Hosts {
		if m.domainCursor == i {
			m.domainCursorLine = len(lines)
		}
		selected := m.domainCursor == i
		mark := "  "
		if selected && m.allowWrites {
			mark = "› "
		}
		dns := paint("· n/a", browserMuted, false)
		if v, ok := m.domainConfig.DNS[h]; ok {
			if v {
				dns = paint("● ready", browserSuccess, true)
			} else {
				dns = paint("○ missing", browserNumber, false)
			}
		}
		cert := "unknown"
		if v, ok := m.domainConfig.Certificates[h]; ok {
			if v == "" {
				cert = "not issued"
			} else {
				cert = v
			}
		}
		lines = append(lines, domainHostRow(domainMarker(mark, selected && m.allowWrites), domainLink(h), dns, safeText(cert), hostWidth, dnsWidth, certWidth))
	}
	if addingExternal {
		m.domainCursorLine = len(lines)
		lines = append(lines, domainHostRow(domainMarker("› ", true), paint(safeText(m.domainInput)+"█", browserAccent, true), paint("new", browserMuted, false), paint("staged on Enter", browserMuted, false), hostWidth, dnsWidth, certWidth))
	}
	challengeSelected := m.domainCursor == len(m.domainConfig.Hosts) && !addingExternal
	if challengeSelected {
		m.domainCursorLine = len(lines)
		marker := "  "
		if m.allowWrites {
			marker = domainMarker("› ", true)
		}
		lines = append(lines, marker+summaryField("Certificate challenge", paint(challengeLabel(m.domainConfig.Challenge), browserAccent, true)))
	} else {
		lines = append(lines, summaryField("Certificate challenge", paint(challengeLabel(m.domainConfig.Challenge), browserAccent, true)))
	}
	if m.domainConfig.Challenge == "dns01" {
		lines = append(lines, "", domainSectionHeading("DNS challenge records", len(m.domainConfig.ChallengeRecords), "record"))
		if strings.Join(m.domainConfig.Hosts, "\x00") != strings.Join(m.domainOriginal.Hosts, "\x00") {
			lines = append(lines, paint("Records reflect saved domains; save to refresh.", browserMuted, false))
		}
		switch {
		case m.domainState.active():
			lines = append(lines, paint("Loading records…", browserMuted, false))
		case m.domainConfig.ChallengeRecordsFailed:
			lines = append(lines, paint("Could not load records. Press r to retry.", browserNumber, false))
		case len(m.domainConfig.ChallengeRecords) == 0:
			lines = append(lines, paint("No records returned.", browserMuted, false))
		}
		names := m.domainRecordNames()
		typeWidth, nameWidth, valueWidth := m.domainRecordColumnWidths(names)
		if len(names) > 0 {
			lines = append(lines, domainRecordRow("  ", paint("TYPE", browserMuted, true), paint("NAME", browserMuted, true), paint("VALUE", browserMuted, true), typeWidth, nameWidth, valueWidth))
		}
		for i, name := range names {
			selected := m.domainCursor == len(m.domainConfig.Hosts)+1+i
			marker := "  "
			if selected {
				marker = "› "
				m.domainCursorLine = len(lines) + 1
			}
			lines = append(lines, domainRecordRow(domainMarker(marker, selected), paint("CNAME", browserAccent, true), paint(safeText(name), browserValue, false), paint(safeText(m.domainConfig.ChallengeRecords[name]), browserValue, false), typeWidth, nameWidth, valueWidth))
		}
	}
	lines = append(lines, "", sectionHeading("Platform subdomain"))
	subdomainLine := len(lines) - 1
	if m.domainCursor == m.domainSubdomainCursor() {
		m.domainCursorLine = subdomainLine
	}
	lines = append(lines, "")
	lines[subdomainLine+1] = domainRow(m.domainConfig.Subdomain, m.domainConfig.BaseDomain, m.domainInput, m.domainEditing && m.domainEditSubdomain, m.allowWrites && m.domainCursor == m.domainSubdomainCursor())
	lines = append(lines, "", sectionHeading("Ingress"), summaryField("CNAME target", displayValue(m.domainConfig.CNAME)))
	if m.domainConfig.CNAME != "" {
		lines = append(lines, paint("Point each custom host's DNS CNAME at this target.", browserMuted, false))
	}
	settingsStart := m.domainSettingsStart()
	if m.domainCursor == settingsStart {
		m.domainCursorLine = len(lines)
	}
	lines = append(lines, domainSettingLine("SSL", m.domainConfig.SSL, m.allowWrites && m.domainCursor == settingsStart))
	if m.domainCursor == settingsStart+1 {
		m.domainCursorLine = len(lines)
	}
	lines = append(lines, domainSettingLine("HTTPS redirect", m.domainConfig.RedirectHTTPS, m.allowWrites && m.domainCursor == settingsStart+1))
	if m.domainCursor == settingsStart+2 {
		m.domainCursorLine = len(lines)
	}
	lines = append(lines, domainSettingLine("HTTP/2", m.domainConfig.HTTP2, m.allowWrites && m.domainCursor == settingsStart+2))
	if len(domainDiff(m.domainOriginal, m.domainConfig)) > 0 {
		lines = append(lines, "", paint("● staged changes", browserAccent, true))
	}
	if m.domainConfirm {
		lines = append(lines, "", sectionHeading("Review changes"))
		lines = append(lines, domainDiff(m.domainOriginal, m.domainConfig)...)
	}
	if m.domainMessage != "" {
		lines = append(lines, "", paint(safeText(m.domainMessage), browserMuted, false))
	}
	return lines
}

func (m *browser) domainContentWidth() int {
	if m.domainMaximized {
		return max(16, m.width-4)
	}
	_, right, _ := m.paneWidths()
	return max(16, right-2)
}
func domainSectionHeading(title string, count int, noun string) string {
	label := noun
	if count != 1 {
		label += "s"
	}
	return sectionHeading(title) + "  " + paint(fmt.Sprintf("%d %s", count, label), browserMuted, false)
}
func domainMarker(marker string, selected bool) string {
	if selected {
		return paint(marker, browserAccent, true)
	}
	return marker
}
func (m *browser) domainColumnWidths() (host, dns, cert int) {
	available := m.domainContentWidth() - 2
	longest := len("DOMAIN")
	for _, h := range m.domainConfig.Hosts {
		longest = max(longest, ansi.StringWidth(h))
	}
	host = min(max(8, longest), max(8, available-15))
	dns = 11
	cert = max(1, available-host-dns-2)
	return
}
func (m *browser) domainRecordColumnWidths(names []string) (typ, name, value int) {
	available := m.domainContentWidth() - 2
	longest := len("NAME")
	for _, n := range names {
		longest = max(longest, ansi.StringWidth(n))
	}
	typ = 7
	name = min(max(14, longest), max(14, available/3))
	value = max(1, available-typ-name-2)
	return
}
func domainCell(value string, width int) string {
	if width < 1 {
		return ""
	}
	out := ansi.Truncate(value, width, "…")
	return out + strings.Repeat(" ", max(0, width-ansi.StringWidth(out)))
}
func domainHostRow(marker, host, dns, cert string, hostWidth, dnsWidth, certWidth int) string {
	return marker + domainCell(host, hostWidth) + " " + domainCell(dns, dnsWidth) + " " + domainCell(cert, certWidth)
}
func domainRecordRow(marker, kind, name, value string, typeWidth, nameWidth, valueWidth int) string {
	return marker + domainCell(kind, typeWidth) + " " + domainCell(name, nameWidth) + " " + domainCell(value, valueWidth)
}

func (m *browser) domainSubdomainCursor() int {
	return len(m.domainConfig.Hosts) + 1 + len(m.domainRecordNames())
}
func (m *browser) domainSettingsStart() int { return m.domainSubdomainCursor() + 1 }
func domainSuffix(base string) string {
	if base == "" {
		return ""
	}
	return "." + base
}
func domainFullSubdomain(label, base string) string {
	if label == "" {
		return "-"
	}
	return label + domainSuffix(base)
}
func domainRow(label, base, input string, editing, selected bool) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	address := paint("not set", browserMuted, false)
	if editing {
		address = paint(safeText(input)+"█"+domainSuffix(base), browserAccent, true)
	} else if label != "" {
		address = domainLink(domainFullSubdomain(label, base))
	}
	return domainMarker(marker, selected) + address
}
func validPlatformLabel(label string) bool {
	if label == "" {
		return true
	}
	if len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
func isPlatformHost(host, base string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	base = strings.ToLower(strings.TrimSuffix(base, "."))
	if base == "" {
		base = "darkube.app"
	}
	return host == base || strings.HasSuffix(host, "."+base)
}

func challengeLabel(v string) string {
	if v == "" {
		return "http01"
	}
	return v
}
func domainSettingLine(name string, enabled bool, selected bool) string {
	glyph := paint("○", browserMuted, false)
	if enabled {
		glyph = paint("●", browserSuccess, true)
	}
	marker := "  "
	if selected {
		marker = "› "
	}
	return domainMarker(marker, selected) + summaryField(name, glyph)
}
func domainDiff(old, cur DomainConfig) []string {
	var out []string
	for _, host := range old.Hosts {
		if !containsString(cur.Hosts, host) {
			out = append(out, paint("− "+safeText(host), browserNumber, false))
		}
	}
	for _, host := range cur.Hosts {
		if !containsString(old.Hosts, host) {
			out = append(out, paint("+ "+safeText(host), browserSuccess, false))
		}
	}
	if old.Subdomain != cur.Subdomain {
		out = append(out, summaryField("Platform subdomain", domainFullSubdomain(old.Subdomain, old.BaseDomain)+" → "+domainFullSubdomain(cur.Subdomain, cur.BaseDomain)))
	}
	if old.Challenge != cur.Challenge {
		out = append(out, summaryField("Challenge", cur.Challenge))
	}
	if old.SSL != cur.SSL {
		out = append(out, summaryField("SSL", glyphBool(cur.SSL)))
	}
	if old.RedirectHTTPS != cur.RedirectHTTPS {
		out = append(out, summaryField("HTTPS redirect", glyphBool(cur.RedirectHTTPS)))
	}
	if old.HTTP2 != cur.HTTP2 {
		out = append(out, summaryField("HTTP/2", glyphBool(cur.HTTP2)))
	}
	return out
}
func glyphBool(v bool) string {
	if v {
		return paint("●", browserSuccess, true)
	}
	return paint("○", browserMuted, false)
}

func (m *browser) handleDomainsKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.domainBusy {
		return nil
	}
	if m.domainEditing {
		switch msg.String() {
		case "esc":
			m.domainEditing = false
			m.domainEditSubdomain = false
			m.domainInput = ""
		case "enter":
			h := strings.TrimSpace(m.domainInput)
			if m.domainEditSubdomain {
				if validPlatformLabel(h) {
					m.domainConfig.Subdomain = h
					if h != "" {
						m.domainConfig.SSL = true
					}
				} else {
					m.domainMessage = "Use a bare label without dots or spaces"
				}
				m.domainEditing = false
				m.domainEditSubdomain = false
				m.domainInput = ""
				return nil
			}
			if h != "" && !strings.ContainsAny(h, " \t\r\n") && !isPlatformHost(h, m.domainConfig.BaseDomain) && !containsString(m.domainConfig.Hosts, h) {
				m.domainConfig.Hosts = append(m.domainConfig.Hosts, h)
				sort.Strings(m.domainConfig.Hosts)
				m.domainCursor = 0
				for i, host := range m.domainConfig.Hosts {
					if host == h {
						m.domainCursor = i
						break
					}
				}
				if strings.HasPrefix(h, "*.") {
					m.domainConfig.Challenge = "dns01"
					m.domainConfig.SSL = true
				}
				m.domainEditing = false
				m.domainInput = ""
				return nil
			} else if h != "" {
				m.domainMessage = "Enter a unique hostname; platform subdomains use the platform subdomain setting"
				return nil
			}
			m.domainEditing = false
			m.domainInput = ""
		case "backspace":
			r := []rune(m.domainInput)
			if len(r) > 0 {
				m.domainInput = string(r[:len(r)-1])
			}
		case "ctrl+u":
			m.domainInput = ""
		default:
			if msg.Text != "" {
				m.domainInput += msg.Text
			}
		}
		return nil
	}
	if m.domainConfirm {
		switch msg.String() {
		case "esc":
			m.domainConfirm = false
		case "enter":
			return m.saveDomains()
		case "pgup", "pageup":
			m.summaryScroll = max(0, m.summaryScroll-8)
		case "pgdown", "pagedown":
			m.summaryScroll += 8
		}
		return nil
	}
	switch msg.String() {
	case "up":
		m.domainCursor = max(0, m.domainCursor-1)
	case "down":
		m.domainCursor = min(m.domainSettingsStart()+2, m.domainCursor+1)
	case "n", "y":
		names := m.domainRecordNames()
		i := m.domainCursor - len(m.domainConfig.Hosts) - 1
		if i >= 0 && i < len(names) {
			name := names[i]
			field := "name"
			value := name
			if msg.String() == "y" {
				field = "target"
				value = m.domainConfig.ChallengeRecords[name]
			}
			tenant, id := m.tenant, m.selectedDetailTarget()
			return func() tea.Msg {
				return domainCopyResult{tenant: tenant, appID: id, field: field, err: clipboard.WriteAll(value)}
			}
		}
	case "pgup", "pageup":
		m.summaryScroll = max(0, m.summaryScroll-8)
	case "pgdown", "pagedown":
		m.summaryScroll += 8
	case "+":
		if m.allowWrites {
			m.domainEditing = true
			m.domainEditSubdomain = false
			m.domainInput = ""
			m.domainCursor = len(m.domainConfig.Hosts)
		}
	case "d":
		if m.allowWrites && m.domainCursor == m.domainSubdomainCursor() {
			m.domainConfig.Subdomain = ""
		} else if m.allowWrites && m.domainCursor < len(m.domainConfig.Hosts) && len(m.domainConfig.Hosts) > 0 {
			m.domainConfig.Hosts = append(m.domainConfig.Hosts[:m.domainCursor], m.domainConfig.Hosts[m.domainCursor+1:]...)
			m.domainCursor = min(m.domainCursor, max(0, len(m.domainConfig.Hosts)-1))
		}
	case "e":
		if m.allowWrites {
			if m.domainCursor == m.domainSubdomainCursor() {
				m.domainEditing = true
				m.domainEditSubdomain = true
				m.domainInput = m.domainConfig.Subdomain
				return nil
			}
			index := m.domainCursor - len(m.domainConfig.Hosts)
			if index > 0 && index <= len(m.domainRecordNames()) {
				break
			}
			if index > 0 {
				index -= len(m.domainRecordNames())
			}
			if index > 1 {
				index--
			}
			switch index {
			case 0:
				if m.domainConfig.Challenge == "dns01" {
					if containsWildcard(m.domainConfig.Hosts) {
						m.domainMessage = "Wildcard domains require the DNS challenge"
						break
					}
					m.domainConfig.Challenge = "http01"
				} else {
					m.domainConfig.Challenge = "dns01"
				}
				m.domainConfig.SSL = true
			case 1:
				m.domainConfig.SSL = !m.domainConfig.SSL
			case 2:
				m.domainConfig.RedirectHTTPS = !m.domainConfig.RedirectHTTPS
			case 3:
				m.domainConfig.HTTP2 = !m.domainConfig.HTTP2
			}
		}
	case "s":
		if m.allowWrites && len(domainDiff(m.domainOriginal, m.domainConfig)) > 0 {
			m.domainConfirm = true
			m.summaryScroll = 1 << 20
		}
	case "x":
		m.domainConfig = cloneDomainConfig(m.domainOriginal)
		m.domainMessage = "Staged changes discarded"
	case "m":
		m.domainMaximized = !m.domainMaximized
	case "r":
		if !m.domainBusy {
			if len(domainDiff(m.domainOriginal, m.domainConfig)) > 0 {
				m.domainMessage = "Save or discard staged changes before refreshing"
				return nil
			}
			m.domainState.reset()
			return m.loadDomains()
		}
	}
	return nil
}

func containsWildcard(hosts []string) bool {
	for _, host := range hosts {
		if strings.HasPrefix(host, "*.") {
			return true
		}
	}
	return false
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (m *browser) saveDomains() tea.Cmd {
	if m.updateDomains == nil {
		m.domainMessage = "Domain writes unavailable"
		return nil
	}
	m.domainBusy = true
	m.domainConfirm = false
	m.domainMessage = "Saving domain settings…"
	id, tenant, cfg := m.selectedDetailTarget(), m.tenant, cloneDomainConfig(m.domainConfig)
	return func() tea.Msg {
		err := m.updateDomains(m.ctx, tenant, id, cfg)
		return domainsWriteResult{appID: id, config: cfg, err: err}
	}
}

func (m *browser) handleDomainsWrite(msg domainsWriteResult) tea.Cmd {
	if msg.appID != m.selectedDetailTarget() {
		return nil
	}
	m.domainBusy = false
	if msg.err != nil {
		m.domainMessage = "Save failed · staged changes remain"
		return nil
	}
	m.domainConfig = cloneDomainConfig(msg.config)
	m.domainConfig.ChallengeRecords = nil
	m.domainConfig.ChallengeRecordsFailed = false
	m.domainOriginal = cloneDomainConfig(msg.config)
	m.domainMessage = "Saved · ingress reconciliation may take time"
	m.domainState.reset()
	return m.loadDomains()
}
