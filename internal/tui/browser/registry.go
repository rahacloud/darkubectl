package browser

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/client"
)

type registryTreeRow struct {
	registry int
	repo     int
	digest   int
	level    int
	label    string
}

type registrySnapshot struct {
	registries []client.Registry
	repos      map[string][]client.Repository
	digests    map[string][]client.Digest
	err        error
	meta       requestMeta
}

type registryDigestsResult struct {
	meta    requestMeta
	key     string
	digests []client.Digest
	err     error
}

func registryKey(id client.FlexID, image string) string { return string(id) + "\x00" + image }
func registryRegistryKey(id client.FlexID) string       { return "registry\x00" + string(id) }

func (m *browser) loadRegistry(force bool) tea.Cmd {
	if m.page != pageRegistry || (!force && (m.registryState.active() || m.registryState.status == requestReady || m.registryState.status == requestEmpty)) {
		return nil
	}
	meta, ctx := m.startRequest(requestRegistry, m.tenant, "", m.tenantGeneration)
	m.registryState.begin(meta, m.requestTime())
	if m.listRegistries == nil || m.listRegistryRepositories == nil || m.listRegistryDigests == nil {
		m.finishRequest(requestRegistry, meta)
		m.registryState.unavailable(fmt.Errorf("registry is unavailable in this mode"))
		return nil
	}
	return func() tea.Msg {
		regs, err := m.listRegistries(ctx, meta.tenant)
		snap := registrySnapshot{registries: regs, repos: map[string][]client.Repository{}, digests: map[string][]client.Digest{}, meta: meta, err: err}
		if err != nil {
			return snap
		}
		for _, reg := range regs {
			repos, readErr := m.listRegistryRepositories(ctx, meta.tenant, reg.ID)
			if readErr != nil {
				snap.err = readErr
				return snap
			}
			snap.repos[string(reg.ID)] = repos
		}
		return snap
	}
}

func (m *browser) handleRegistrySnapshot(msg registrySnapshot) (tea.Model, tea.Cmd) {
	m.finishRequest(requestRegistry, msg.meta)
	if !m.registryState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.registryState.fail(msg.err)
		return m, nil
	}
	m.registries, m.registryRepos, m.registryDigests = msg.registries, msg.repos, msg.digests
	m.registryDigestErrors = map[string]error{}
	m.registryState.succeed(len(msg.registries) == 0, m.requestTime())
	m.rebuildRegistryRows()
	return m, nil
}

func (m *browser) loadRegistryDigests(row registryTreeRow, force bool) tea.Cmd {
	if row.repo < 0 || row.registry < 0 || row.registry >= len(m.registries) {
		return nil
	}
	reg := m.registries[row.registry]
	repo := m.registryRepos[string(reg.ID)][row.repo]
	key := registryKey(reg.ID, repo.Name)
	if _, ok := m.registryDigests[key]; ok && !force {
		return nil
	}
	if _, loading := m.inFlight[requestKey{kind: requestRegistry, appID: key}]; loading && !force {
		return nil
	}
	meta, ctx := m.startRequest(requestRegistry, m.tenant, key, m.tenantGeneration)
	return func() tea.Msg {
		var digests []client.Digest
		var err error
		for attempt := 0; ; attempt++ {
			digests, err = m.listRegistryDigests(ctx, meta.tenant, reg.ID, repo.Name)
			if err == nil || !client.IsTransient(err) || attempt >= 1 {
				break
			}
			timer := time.NewTimer(350 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			case <-timer.C:
			}
			if ctx.Err() != nil {
				break
			}
		}
		return registryDigestsResult{meta: meta, key: key, digests: digests, err: err}
	}
}

func wrapRegistryText(value string, width int) []string {
	runes := []rune(safeText(value))
	width = max(12, width)
	var lines []string
	for len(runes) > 0 {
		n := min(width, len(runes))
		if n < len(runes) {
			if cut := strings.LastIndex(string(runes[:n]), " "); cut > 0 {
				n = len([]rune(string(runes[:n])[:cut]))
			}
		}
		lines = append(lines, string(runes[:n]))
		runes = runes[n:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	return lines
}

func (m *browser) handleRegistryDigests(msg registryDigestsResult) (tea.Model, tea.Cmd) {
	m.finishRequest(requestRegistry, msg.meta)
	if msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.registryDigestErrors[msg.key] = msg.err
		return m, nil
	}
	delete(m.registryDigestErrors, msg.key)
	m.registryDigests[msg.key] = mergeRegistryDigestAliases(msg.digests)
	m.rebuildRegistryRows()
	return m, nil
}

func mergeRegistryDigestAliases(input []client.Digest) []client.Digest {
	out := make([]client.Digest, 0, len(input))
	indexes := make(map[string]int, len(input))
	for _, item := range input {
		if index, ok := indexes[item.Digest]; ok {
			seen := make(map[string]bool, len(out[index].Tags))
			for _, tag := range out[index].Tags {
				seen[tag] = true
			}
			for _, tag := range item.Tags {
				if !seen[tag] {
					out[index].Tags = append(out[index].Tags, tag)
					seen[tag] = true
				}
			}
			if item.Size > out[index].Size {
				out[index].Size = item.Size
			}
			if item.LastPushed > out[index].LastPushed {
				out[index].LastPushed = item.LastPushed
			}
			continue
		}
		indexes[item.Digest] = len(out)
		item.Tags = append([]string(nil), item.Tags...)
		out = append(out, item)
	}
	return out
}

func shortRegistryDigest(digest string) string {
	if strings.HasPrefix(digest, "sha256:") && len(digest) > 19 {
		return digest[:19] + "…"
	}
	if len(digest) > 20 {
		return digest[:20] + "…"
	}
	return digest
}

func registryBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	value := float64(n)
	i := 0
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func registryAge(value string) string {
	if value == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return safeText(value)
	}
	return t.Local().Format("Jan 02, 15:04")
}

func (m *browser) rebuildRegistryRows() {
	selected := m.registryCursor
	selectedRegistry, selectedImage, selectedDigest := "", "", ""
	if selected >= 0 && selected < len(m.registryRows) {
		old := m.registryRows[selected]
		if old.registry >= 0 && old.registry < len(m.registries) {
			reg := m.registries[old.registry]
			selectedRegistry = string(reg.ID)
			if old.repo >= 0 && old.repo < len(m.registryRepos[string(reg.ID)]) {
				selectedImage = m.registryRepos[string(reg.ID)][old.repo].Name
				if old.digest >= 0 {
					digests := m.registryDigests[registryKey(reg.ID, selectedImage)]
					if old.digest < len(digests) {
						selectedDigest = digests[old.digest].Digest
					}
				}
			}
		}
	}
	rows := make([]registryTreeRow, 0)
	for ri, reg := range m.registries {
		rows = append(rows, registryTreeRow{registry: ri, repo: -1, digest: -1, level: 0, label: reg.Name})
		if m.registryCollapsed[registryRegistryKey(reg.ID)] {
			continue
		}
		for pi, repo := range m.registryRepos[string(reg.ID)] {
			rows = append(rows, registryTreeRow{registry: ri, repo: pi, digest: -1, level: 1, label: repo.Name})
			key := registryKey(reg.ID, repo.Name)
			if !m.registryCollapsed[key] {
				if digests, loaded := m.registryDigests[key]; loaded {
					for di, digest := range digests {
						label := strings.Join(digest.Tags, ", ")
						if label == "" {
							label = shortRegistryDigest(digest.Digest) + " · untagged"
						}
						rows = append(rows, registryTreeRow{registry: ri, repo: pi, digest: di, level: 2, label: label})
					}
				}
			}
		}
	}
	m.registryRows = rows
	m.registryCursor = min(selected, max(0, len(rows)-1))
	for i, row := range rows {
		reg := m.registries[row.registry]
		if string(reg.ID) != selectedRegistry {
			continue
		}
		if selectedImage == "" && row.repo < 0 {
			m.registryCursor = i
			break
		}
		if row.repo < 0 || m.registryRepos[string(reg.ID)][row.repo].Name != selectedImage {
			continue
		}
		if selectedDigest == "" && row.digest < 0 {
			m.registryCursor = i
			break
		}
		if row.digest >= 0 {
			digests := m.registryDigests[registryKey(reg.ID, selectedImage)]
			if row.digest < len(digests) && digests[row.digest].Digest == selectedDigest {
				m.registryCursor = i
				break
			}
		}
	}
}

func (m *browser) registryPaneLines(height int) []string {
	lines := []string{}
	switch {
	case m.registryState.active():
		lines = append(lines, m.loadingMessage("Registry and images"))
	case m.registryState.err != nil && len(m.registryRows) == 0:
		lines = append(lines, paint("Could not load registry: "+safeText(m.registryState.err.Error()), browserDanger, false))
	case m.registryState.status == requestUnavailable:
		lines = append(lines, paint("Registry unavailable in mock mode.", browserMuted, false))
	case len(m.registryRows) == 0:
		lines = append(lines, paint("No registries or images.", browserMuted, false))
	default:
		for i, row := range m.registryRows {
			prefix, label := "", safeText(row.label)
			reg := m.registries[row.registry]
			switch row.level {
			case 0:
				marker := "▾"
				if m.registryCollapsed[registryRegistryKey(reg.ID)] {
					marker = "▸"
				}
				prefix = marker + " ◇ "
			case 1:
				repos := m.registryRepos[string(reg.ID)]
				branch := "├─"
				if row.repo == len(repos)-1 {
					branch = "└─"
				}
				image := repos[row.repo].Name
				key := registryKey(reg.ID, image)
				marker := "▸"
				if _, loading := m.inFlight[requestKey{kind: requestRegistry, appID: key}]; loading {
					marker = m.spinnerGlyph()
				} else if _, loaded := m.registryDigests[key]; loaded && !m.registryCollapsed[key] {
					marker = "▾"
				}
				prefix = "  " + branch + " " + marker + " ▣ "
			case 2:
				repos := m.registryRepos[string(reg.ID)]
				image := repos[row.repo].Name
				digests := m.registryDigests[registryKey(reg.ID, image)]
				ancestor := "  │ "
				if row.repo == len(repos)-1 {
					ancestor = "    "
				}
				branch := "├─"
				if row.digest == len(digests)-1 {
					branch = "└─"
				}
				prefix = ancestor + branch + " ◆ "
				if row.digest >= 0 && row.digest < len(digests) {
					d := digests[row.digest]
					label = registryDigestTreeLabel(d.Tags, d.Digest, max(1, m.paneWidthsInt()-ansi.StringWidth(prefix)))
				}
			}
			line := prefix + ansi.Truncate(label, max(1, m.paneWidthsInt()-ansi.StringWidth(prefix)), "…")
			if i == m.registryCursor {
				line = selectedRowStyle().Width(max(1, m.paneWidthsInt())).Render(line)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) > height {
		start := registryViewportStart(len(lines), height, m.registryCursor)
		lines = lines[start:min(len(lines), start+height)]
	}
	return fitPaneLines(lines, height)
}

func registryDigestTreeLabel(tags []string, digest string, width int) string {
	if len(tags) == 0 {
		return ansi.Truncate(shortRegistryDigest(digest)+" · untagged", width, "…")
	}
	joined := strings.Join(tags, ", ")
	if ansi.StringWidth(joined) <= width {
		return joined
	}
	if len(tags) > 1 {
		suffix := fmt.Sprintf(" +%d tags", len(tags)-1)
		return ansi.Truncate(tags[0], max(1, width-ansi.StringWidth(suffix)), "…") + suffix
	}
	return ansi.Truncate(tags[0], width, "…")
}

func registryViewportStart(rowCount, height, cursor int) int {
	if height <= 0 || rowCount <= height {
		return 0
	}
	return max(0, min(cursor-height+1, rowCount-height))
}

func (m *browser) paneWidthsInt() int { apps, _, _ := m.paneWidths(); return apps }

func (m *browser) registryDetailLines(height int) []string {
	if m.registryState.err != nil && len(m.registryRows) > 0 {
		return fitPaneLines([]string{paint("Stale · "+safeText(m.registryState.err.Error()), browserNumber, false)}, height)
	}
	if len(m.registryRows) == 0 {
		return fitPaneLines([]string{"Select Registry to load its contents."}, height)
	}
	row := m.registryRows[m.registryCursor]
	reg := m.registries[row.registry]
	lines := []string{}
	add := func(k, v string) {
		lines = append(lines, paint(fmt.Sprintf("%-15s", k), browserMuted, false)+safeText(v))
	}
	addSpinner := func(k string) {
		lines = append(lines, paint(fmt.Sprintf("%-15s", k), browserMuted, false)+paint(m.spinnerGlyph(), browserBranch, true))
	}
	if row.repo < 0 {
		lines = append(lines, paint("REGISTRY", browserAccent, true))
		add("Name", reg.Name)
		add("Address", reg.Address)
		add("Username", reg.Username)
		add("Push prefix", reg.PushPrefix())
		if reg.StorageUsageBytes != nil {
			add("Storage", registryBytes(*reg.StorageUsageBytes))
		}
	} else {
		repo := m.registryRepos[string(reg.ID)][row.repo]
		lines = append(lines, paint(map[bool]string{true: "IMAGE", false: "MANIFEST"}[row.digest < 0], browserAccent, true))
		add("Registry", reg.Name)
		add("Image", repo.Name)
		add("Last pushed", registryAge(repo.LastPushed))
		if row.digest >= 0 {
			d := m.registryDigests[registryKey(reg.ID, repo.Name)][row.digest]
			add("Digest", d.Digest)
			add("Size", registryBytes(d.Size))
			add("Pushed", registryAge(d.LastPushed))
			if len(d.Tags) == 0 {
				add("Tags", "(untagged)")
			} else {
				add("Tags", strings.Join(d.Tags, ", "))
			}
			add("Reference", reg.PushPrefix()+"/"+repo.Name+"@"+d.Digest)
		} else {
			add("Recent tag", repo.RecentTag)
			add("Reference", reg.PushPrefix()+"/"+repo.Name)
			key := registryKey(reg.ID, repo.Name)
			if err := m.registryDigestErrors[key]; err != nil {
				add("Manifests", "load failed")
				_, _, contextWidth := m.paneWidths()
				errorWidth := max(18, m.paneWidthsInt()+contextWidth/2-22)
				for _, part := range wrapRegistryText(err.Error(), errorWidth) {
					add("", part)
				}
				add("Action", "press Enter to retry")
			} else if digests, loaded := m.registryDigests[key]; loaded {
				add("Manifests", fmt.Sprintf("%d", len(digests)))
			} else if _, loading := m.inFlight[requestKey{kind: requestRegistry, appID: key}]; loading {
				addSpinner("Manifests")
			} else {
				add("Manifests", "press Enter to load")
			}
		}
	}
	if m.registryNotice != "" {
		lines = append(lines, "", paint(safeText(m.registryNotice), browserNumber, false))
	}
	return fitPaneLines(lines, height)
}

func (m *browser) moveRegistrySelection(delta int) {
	if len(m.registryRows) > 0 {
		m.registryCursor = min(max(0, m.registryCursor+delta), len(m.registryRows)-1)
	}
}

func (m *browser) handleRegistryKey(key string) tea.Cmd {
	if m.registryConfirm {
		switch key {
		case "esc", "n":
			m.registryConfirm = false
			m.registryConfirmText = ""
			m.registryDeleteTag = false
			m.registryTagCopy = false
		case "enter":
			return m.runRegistryDelete()
		case "backspace":
			if len(m.registryConfirmText) > 0 {
				m.registryConfirmText = m.registryConfirmText[:len(m.registryConfirmText)-1]
			}
		default:
			if len(key) == 1 {
				m.registryConfirmText += key
			}
		}
		return nil
	}
	if m.registryTagMode {
		row := m.registryRows[m.registryCursor]
		reg := m.registries[row.registry]
		image := m.registryRepos[string(reg.ID)][row.repo].Name
		d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
		switch key {
		case "esc":
			m.registryTagMode = false
			m.registryTagCopy = false
		case "up", "k":
			m.registryTagCursor = max(0, m.registryTagCursor-1)
		case "down", "j":
			m.registryTagCursor = min(len(d.Tags)-1, m.registryTagCursor+1)
		case "enter", "d", "y":
			if m.registryTagCopy {
				if m.registryTagCursor < len(d.Tags) {
					m.registryTagMode = false
					m.registryTagCopy = false
					return m.copyRegistryReference(d.Tags[m.registryTagCursor])
				}
				return nil
			}
			if m.allowWrites && m.deleteRegistryTags != nil {
				m.registryTagMode = false
				m.registryDeleteTag = true
				m.registryTagCopy = false
				m.registryConfirm = true
				m.registryConfirmText = ""
				m.registryNotice = "Confirm removal of the selected tag."
			}
		}
		return nil
	}
	switch key {
	case "up", "k":
		m.moveRegistrySelection(-1)
	case "down", "j":
		m.moveRegistrySelection(1)
	case "pgup", "pageup":
		m.moveRegistrySelection(-max(1, m.height-12))
	case "pgdown", "pagedown":
		m.moveRegistrySelection(max(1, m.height-12))
	case "home", "g":
		m.registryCursor = 0
	case "end", "G":
		m.registryCursor = max(0, len(m.registryRows)-1)
	case "r":
		return m.loadRegistry(true)
	case "d":
		if m.allowWrites && len(m.registryRows) > 0 {
			row := m.registryRows[m.registryCursor]
			if row.repo < 0 {
				m.registryNotice = "Select an image or manifest to delete."
				return nil
			}
			m.registryConfirm = true
			m.registryDeleteTag = false
			m.registryConfirmText = ""
			m.registryNotice = "Type delete, then press Enter to confirm."
		}
	case "t":
		if m.allowWrites && len(m.registryRows) > 0 {
			row := m.registryRows[m.registryCursor]
			if row.digest >= 0 {
				reg := m.registries[row.registry]
				image := m.registryRepos[string(reg.ID)][row.repo].Name
				tags := m.registryDigests[registryKey(reg.ID, image)][row.digest].Tags
				if len(tags) > 0 {
					m.registryTagMode = true
					m.registryTagCopy = false
					m.registryTagCursor = 0
				}
			}
		}
	case "y":
		if m.registryCursor >= 0 && m.registryCursor < len(m.registryRows) {
			row := m.registryRows[m.registryCursor]
			if row.repo >= 0 {
				reg := m.registries[row.registry]
				image := m.registryRepos[string(reg.ID)][row.repo]
				if row.digest < 0 {
					if image.RecentTag == "" {
						m.registryNotice = "This image has no recent tag."
					} else {
						return m.copyRegistryReference(image.RecentTag)
					}
				} else {
					d := m.registryDigests[registryKey(reg.ID, image.Name)][row.digest]
					if len(d.Tags) > 1 {
						m.registryTagMode = true
						m.registryTagCopy = true
						m.registryTagCursor = 0
					} else if len(d.Tags) == 1 {
						return m.copyRegistryReference(d.Tags[0])
					} else {
						return m.copyRegistryReference("")
					}
				}
			}
		}
	case "enter", "right", "l":
		if m.registryCursor >= 0 && m.registryCursor < len(m.registryRows) {
			row := m.registryRows[m.registryCursor]
			reg := m.registries[row.registry]
			if m.registryCollapsed == nil {
				m.registryCollapsed = map[string]bool{}
			}
			if row.repo < 0 {
				m.registryCollapsed[registryRegistryKey(reg.ID)] = false
				m.rebuildRegistryRows()
			} else if row.digest < 0 {
				image := m.registryRepos[string(reg.ID)][row.repo].Name
				key := registryKey(reg.ID, image)
				if _, loaded := m.registryDigests[key]; loaded {
					m.registryCollapsed[key] = false
					m.rebuildRegistryRows()
				} else {
					return m.loadRegistryDigests(row, false)
				}
			}
		}
	case "esc":
		m.page = pageApps
		m.contextRow = 1
		m.contextFocused = true
		m.detail = false
	case "left", "h":
		if m.registryCursor >= 0 && m.registryCursor < len(m.registryRows) {
			row := m.registryRows[m.registryCursor]
			reg := m.registries[row.registry]
			if m.registryCollapsed == nil {
				m.registryCollapsed = map[string]bool{}
			}
			if row.repo < 0 {
				m.registryCollapsed[registryRegistryKey(reg.ID)] = true
				m.rebuildRegistryRows()
				return nil
			}
			if row.repo >= 0 && row.digest < 0 {
				image := m.registryRepos[string(reg.ID)][row.repo].Name
				m.registryCollapsed[registryKey(reg.ID, image)] = true
				m.rebuildRegistryRows()
				return nil
			}
			if row.digest >= 0 {
				image := m.registryRepos[string(reg.ID)][row.repo].Name
				m.registryCollapsed[registryKey(reg.ID, image)] = true
				m.rebuildRegistryRows()
				return nil
			}
		}
		if m.detail {
			m.detail = false
		} else {
			m.page = pageApps
			m.contextRow = 1
			m.contextFocused = true
		}
	}
	return nil
}

func registryImageReference(reg client.Registry, image, tag, digest string) string {
	base := reg.PushPrefix() + "/" + image
	if tag != "" {
		return base + ":" + tag
	}
	if digest != "" {
		return base + "@" + digest
	}
	return base
}

func (m *browser) copyRegistryReference(tag string) tea.Cmd {
	if m.registryCursor < 0 || m.registryCursor >= len(m.registryRows) {
		return nil
	}
	row := m.registryRows[m.registryCursor]
	if row.repo < 0 {
		m.registryNotice = "Select an image or manifest to copy."
		return nil
	}
	reg := m.registries[row.registry]
	image := m.registryRepos[string(reg.ID)][row.repo]
	digest := ""
	if row.digest >= 0 {
		digest = m.registryDigests[registryKey(reg.ID, image.Name)][row.digest].Digest
	}
	ref := registryImageReference(reg, image.Name, tag, digest)
	return func() tea.Msg {
		err := clipboard.WriteAll(ref)
		return registryCopyResult{tenant: m.tenant, reference: ref, err: err}
	}
}

type registryCopyResult struct {
	tenant, reference string
	err               error
}

func (m *browser) handleRegistryCopyResult(msg registryCopyResult) {
	if msg.tenant != m.tenant {
		return
	}
	if msg.err != nil {
		m.registryNotice = "Clipboard unavailable"
		return
	}
	m.registryNotice = "Copied image reference to clipboard."
}

func (m *browser) runRegistryDelete() tea.Cmd {
	if m.registryConfirmText != "delete" || len(m.registryRows) == 0 || m.registryBusy {
		m.registryNotice = "Type delete exactly to confirm."
		return nil
	}
	row := m.registryRows[m.registryCursor]
	reg := m.registries[row.registry]
	image := ""
	if row.repo >= 0 {
		image = m.registryRepos[string(reg.ID)][row.repo].Name
	}
	if row.repo < 0 {
		m.registryNotice = "Select an image to delete."
		m.registryConfirm = false
		return nil
	}
	var op func(context.Context) error
	if m.registryDeleteTag {
		image = m.registryRepos[string(reg.ID)][row.repo].Name
		d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
		if m.deleteRegistryTags == nil || m.registryTagCursor >= len(d.Tags) {
			m.registryNotice = "Tag deletion unavailable."
			return nil
		}
		tag := d.Tags[m.registryTagCursor]
		op = func(ctx context.Context) error {
			return m.deleteRegistryTags(ctx, m.tenant, reg.ID, image, d.Digest, []string{tag})
		}
	} else if row.digest < 0 {
		if m.deleteRegistryRepository == nil {
			m.registryNotice = "Image deletion unavailable."
			return nil
		}
		op = func(ctx context.Context) error { return m.deleteRegistryRepository(ctx, m.tenant, reg.ID, image) }
	} else {
		d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
		if m.deleteRegistryDigests == nil {
			m.registryNotice = "Digest deletion unavailable."
			return nil
		}
		op = func(ctx context.Context) error {
			return m.deleteRegistryDigests(ctx, m.tenant, reg.ID, image, []string{d.Digest})
		}
	}
	m.registryBusy = true
	m.registryConfirm = false
	m.registryNotice = "Deleting…"
	return func() tea.Msg { err := op(m.ctx); return registryDeleteResult{tenant: m.tenant, row: row, err: err} }
}

type registryDeleteResult struct {
	tenant string
	row    registryTreeRow
	err    error
}

func (m *browser) handleRegistryDeleteResult(msg registryDeleteResult) tea.Cmd {
	m.registryBusy = false
	if msg.tenant != m.tenant {
		return nil
	}
	if msg.err != nil {
		m.registryNotice = "Delete failed: " + safeText(msg.err.Error())
		return nil
	}
	m.registryNotice = "Deleted. Refreshing registry…"
	return m.loadRegistry(true)
}

func (m *browser) registryConfirmation(background []string) []string {
	if len(m.registryRows) == 0 {
		return background
	}
	row := m.registryRows[m.registryCursor]
	reg := m.registries[row.registry]
	target := reg.Name
	if row.repo >= 0 {
		target += "/" + m.registryRepos[string(reg.ID)][row.repo].Name
	}
	if row.digest >= 0 {
		d := m.registryDigests[registryKey(reg.ID, m.registryRepos[string(reg.ID)][row.repo].Name)][row.digest]
		target += "@" + shortRegistryDigest(d.Digest)
	}
	width := min(72, m.width-4)
	inner := width - 6
	if m.registryDeleteTag && row.digest >= 0 {
		image := m.registryRepos[string(reg.ID)][row.repo].Name
		d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
		if m.registryTagCursor < len(d.Tags) {
			target += " tag " + d.Tags[m.registryTagCursor]
		}
	}
	content := []string{paint("✖ Delete registry content", browserDanger, true), paint(strings.Repeat("─", inner), browserBorder, false), "Target: " + safeText(target), "This action cannot be undone.", paint("Type delete: "+safeText(m.registryConfirmText)+"█", browserKey, true), paint("Esc cancels · Enter confirms", browserMuted, false)}
	for i := range content {
		content[i] = ansi.Truncate(content[i], inner, "…")
	}
	panel := tenantPanelStyle().Width(width).Render(strings.Join(content, "\n"))
	return m.actionOverlay(background, panel, width)
}

func (m *browser) registryTagOverlay(background []string) []string {
	if len(m.registryRows) == 0 {
		return background
	}
	row := m.registryRows[m.registryCursor]
	if row.digest < 0 {
		return background
	}
	reg := m.registries[row.registry]
	image := m.registryRepos[string(reg.ID)][row.repo].Name
	d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
	width := min(64, m.width-4)
	inner := width - 6
	title, description := "TAG ACTION · "+safeText(image), "Choose one tag to remove; the manifest stays:"
	if m.registryTagCopy {
		title = "COPY IMAGE REFERENCE · " + safeText(image)
		description = "Choose the tag to copy with its registry address:"
	}
	content := []string{paint(title, browserAccent, true), paint(strings.Repeat("─", inner), browserBorder, false), description}
	for i, tag := range d.Tags {
		label := "  " + safeText(tag)
		if i == m.registryTagCursor {
			label = selectedRowStyle().Width(inner).Render(label)
		}
		content = append(content, label)
	}
	if m.registryTagCopy {
		content = append(content, "", paint("↑↓ choose · Enter copy reference · Esc cancel", browserMuted, false))
	} else {
		content = append(content, "", paint("↑↓ choose · Enter remove selected tag · Esc cancel", browserMuted, false))
	}
	for i := range content {
		content[i] = ansi.Truncate(content[i], inner, "…")
	}
	return m.actionOverlay(background, tenantPanelStyle().Width(width).Render(strings.Join(content, "\n")), width)
}

func (m *browser) handleRegistryMouse(msg tea.MouseClickMsg) tea.Cmd {
	bodyY := m.browserBodyY()
	if msg.Y < bodyY {
		return nil
	}
	_, _, contextWidth := m.paneWidths()
	if msg.X < contextWidth+2 {
		bodyHeight := max(1, m.height-bodyY-browserFooterHeight-1)
		tenantHeight := max(4, (bodyHeight-1)/2)
		if msg.Y >= bodyY+tenantHeight+1 {
			row := msg.Y - (bodyY + tenantHeight + 2) + 1
			if row >= 1 && row <= 6 {
				m.contextFocused = true
				m.contextRow = row
				m.detail = false
				return m.activatePageRow(row)
			}
			return nil
		}
		capacity := max(1, tenantHeight-2)
		first := max(0, min(m.tenantCursor-capacity+1, len(m.tenants)-capacity))
		index := first + msg.Y - bodyY - 1
		if index >= 0 && index < len(m.tenants) {
			m.contextFocused = true
			m.contextRow = 0
			m.tenantCursor = index
			if m.tenants[index].name != m.tenant {
				return m.chooseTenant(m.tenants[index].name)
			}
		}
		return nil
	}
	appsWidth, _, _ := m.paneWidths()
	left := contextWidth + 3
	if msg.X >= left && msg.X < left+appsWidth+2 {
		paneHeight := max(1, m.height-bodyY-browserFooterHeight-3)
		start := registryViewportStart(len(m.registryRows), paneHeight, m.registryCursor)
		index := start + msg.Y - bodyY - 1
		if index >= 0 && index < len(m.registryRows) {
			m.registryCursor = index
			m.contextFocused = false
			m.detail = false
		}
	}
	return nil
}

func registryTagSummary(tags []string) string {
	if len(tags) == 0 {
		return "untagged · "
	}
	return strings.Join(tags, ", ") + " · "
}
