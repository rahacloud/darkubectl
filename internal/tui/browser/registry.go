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

const (
	registryErrorWidthRightInset    = 22
	registryConfirmationOuterMargin = 4
	registryTagOverlayOuterMargin   = 4
	handleRegistryKeyValue12        = 12
	handleRegistryMouseValue2       = 2
	handleRegistryMouseValue3       = 3
	handleRegistryMouseValue4       = 4
	loadRegistryDigestsValue350     = 350
	rebuildRegistryRowsValue2       = 2
	registryConfirmationValue6      = 6
	registryConfirmationValue72     = 72
	registryDetailLinesValue18      = 18
	registryPaneLinesValue2         = 2
	registryTagOverlayValue6        = 6
	registryTagOverlayValue64       = 64
	shortRegistryDigestValue20      = 20
	wrapRegistryTextValue12         = 12
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
	if m.page != pageRegistry || (!force && (m.registryState.active() || m.registryState.status == requestReady ||
		m.registryState.status == requestEmpty)) {
		return nil
	}
	meta, ctx := m.startRequest(requestRegistry, m.tenant, "", m.tenantGeneration)
	m.registryState.begin(meta, m.requestTime())
	if m.listRegistries == nil || m.listRegistryRepositories == nil || m.listRegistryDigests == nil {
		m.finishRequest(requestRegistry, meta)
		m.registryState.unavailable(errRegistryIsUnavailableInThisMode)
		return nil
	}
	return func() tea.Msg {
		regs, err := m.listRegistries(ctx, meta.tenant)
		snap := registrySnapshot{registries: regs, repos: map[string][]client.Repository{}, digests: map[string][]client.Digest{},
			meta: meta, err: err}
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

func (m *browser) handleRegistrySnapshot(msg registrySnapshot) tea.Model {
	m.finishRequest(requestRegistry, msg.meta)
	if !m.registryState.matches(msg.meta) || msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m
	}
	if msg.err != nil {
		m.registryState.fail(msg.err)
		return m
	}
	m.registries, m.registryRepos, m.registryDigests = msg.registries, msg.repos, msg.digests
	m.registryDigestErrors = map[string]error{}
	m.registryState.succeed(len(msg.registries) == 0, m.requestTime())
	m.rebuildRegistryRows()
	return m
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
			timer := time.NewTimer(loadRegistryDigestsValue350 * time.Millisecond)
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
	width = max(wrapRegistryTextValue12, width)
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

func (m *browser) handleRegistryDigests(msg registryDigestsResult) tea.Model {
	m.finishRequest(requestRegistry, msg.meta)
	if msg.meta.tenant != m.tenant || msg.meta.scopeGeneration != m.tenantGeneration {
		return m
	}
	if msg.err != nil {
		m.registryDigestErrors[msg.key] = msg.err
		return m
	}
	delete(m.registryDigestErrors, msg.key)
	m.registryDigests[msg.key] = mergeRegistryDigestAliases(msg.digests)
	m.rebuildRegistryRows()
	return m
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
	if len(digest) > shortRegistryDigestValue20 {
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
	return formatLocalWallTime(t, "Jan 02, 15:04")
}

func (m *browser) rebuildRegistryRows() {
	anchor := m.registrySelectionAnchor()
	rows := m.buildRegistryRows()
	m.registryRows = rows
	m.registryCursor = min(anchor.cursor, max(0, len(rows)-1))
	m.restoreRegistrySelection(anchor, rows)
}

type registrySelection struct {
	cursor                  int
	registry, image, digest string
}

func (m *browser) registrySelectionAnchor() registrySelection {
	anchor := registrySelection{cursor: m.registryCursor}
	if anchor.cursor < 0 || anchor.cursor >= len(m.registryRows) {
		return anchor
	}
	row := m.registryRows[anchor.cursor]
	if row.registry < 0 || row.registry >= len(m.registries) {
		return anchor
	}
	reg := m.registries[row.registry]
	anchor.registry = string(reg.ID)
	if row.repo < 0 || row.repo >= len(m.registryRepos[string(reg.ID)]) {
		return anchor
	}
	anchor.image = m.registryRepos[string(reg.ID)][row.repo].Name
	if row.digest >= 0 {
		digests := m.registryDigests[registryKey(reg.ID, anchor.image)]
		if row.digest < len(digests) {
			anchor.digest = digests[row.digest].Digest
		}
	}
	return anchor
}

func (m *browser) buildRegistryRows() []registryTreeRow {
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
						rows = append(rows, registryTreeRow{registry: ri, repo: pi, digest: di, level: rebuildRegistryRowsValue2, label: label})
					}
				}
			}
		}
	}
	return rows
}

func (m *browser) restoreRegistrySelection(anchor registrySelection, rows []registryTreeRow) {
	for i, row := range rows {
		reg := m.registries[row.registry]
		if string(reg.ID) != anchor.registry {
			continue
		}
		if anchor.image == "" && row.repo < 0 {
			m.registryCursor = i
			break
		}
		if row.repo < 0 || m.registryRepos[string(reg.ID)][row.repo].Name != anchor.image {
			continue
		}
		if anchor.digest == "" && row.digest < 0 {
			m.registryCursor = i
			break
		}
		if row.digest >= 0 {
			digests := m.registryDigests[registryKey(reg.ID, anchor.image)]
			if row.digest < len(digests) && digests[row.digest].Digest == anchor.digest {
				m.registryCursor = i
				break
			}
		}
	}
}

func (m *browser) registryPaneLines(height int) []string {
	lines := m.registryStateLines()
	if len(lines) == 0 {
		lines = make([]string, 0, len(m.registryRows))
		for i, row := range m.registryRows {
			lines = append(lines, m.registryRowLine(i, row))
		}
	}
	if len(lines) > height {
		start := registryViewportStart(len(lines), height, m.registryCursor)
		lines = lines[start:min(len(lines), start+height)]
	}
	return fitPaneLines(lines, height)
}

func (m *browser) registryStateLines() []string {
	switch {
	case m.registryState.active():
		return []string{m.loadingMessage("Registry and images")}
	case m.registryState.err != nil && len(m.registryRows) == 0:
		return []string{paint("Could not load registry: "+safeText(m.registryState.err.Error()), browserDanger, false)}
	case m.registryState.status == requestUnavailable:
		return []string{paint("Registry unavailable in mock mode.", browserMuted, false)}
	case len(m.registryRows) == 0:
		return []string{paint("No registries or images.", browserMuted, false)}
	}
	return nil
}

func (m *browser) registryRowLine(index int, row registryTreeRow) string {
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
		prefix = m.registryImagePrefix(reg, row)
	case registryPaneLinesValue2:
		prefix, label = m.registryDigestPrefixAndLabel(reg, row, label)
	}
	line := prefix + ansi.Truncate(label, max(1, m.paneWidthsInt()-ansi.StringWidth(prefix)), "…")
	if index == m.registryCursor {
		line = selectedRowStyle().Width(max(1, m.paneWidthsInt())).Render(line)
	}
	return line
}

func (m *browser) registryImagePrefix(reg client.Registry, row registryTreeRow) string {
	repos := m.registryRepos[string(reg.ID)]
	branch := "├─"
	if row.repo == len(repos)-1 {
		branch = "└─"
	}
	key := registryKey(reg.ID, repos[row.repo].Name)
	marker := "▸"
	if _, loading := m.inFlight[requestKey{kind: requestRegistry, appID: key}]; loading {
		marker = m.spinnerGlyph()
	} else if _, loaded := m.registryDigests[key]; loaded && !m.registryCollapsed[key] {
		marker = "▾"
	}
	return "  " + branch + " " + marker + " ▣ "
}

func (m *browser) registryDigestPrefixAndLabel(reg client.Registry, row registryTreeRow, label string) (string, string) {
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
	prefix := ancestor + branch + " ◆ "
	if row.digest >= 0 && row.digest < len(digests) {
		digest := digests[row.digest]
		label = registryDigestTreeLabel(digest.Tags, digest.Digest, max(1, m.paneWidthsInt()-ansi.StringWidth(prefix)))
	}
	return prefix, label
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
		m.appendRegistryImageDetailLines(&lines, reg, row, add, addSpinner)
	}
	if m.registryNotice != "" {
		lines = append(lines, "", paint(safeText(m.registryNotice), browserNumber, false))
	}
	return fitPaneLines(lines, height)
}

func (m *browser) appendRegistryImageDetailLines(lines *[]string, reg client.Registry, row registryTreeRow, add func(string,
	string), addSpinner func(string)) {
	repo := m.registryRepos[string(reg.ID)][row.repo]
	*lines = append(*lines, paint(map[bool]string{true: "IMAGE", false: "MANIFEST"}[row.digest < 0], browserAccent, true))
	add("Registry", reg.Name)
	add("Image", repo.Name)
	add("Last pushed", registryAge(repo.LastPushed))
	if row.digest >= 0 {
		m.appendRegistryDigestDetail(reg, repo, row, add)
		return
	}
	m.appendRegistryManifestDetail(reg, repo, add, addSpinner)
}

func (m *browser) appendRegistryDigestDetail(reg client.Registry, repo client.Repository, row registryTreeRow, add func(string, string)) {
	digest := m.registryDigests[registryKey(reg.ID, repo.Name)][row.digest]
	add("Digest", digest.Digest)
	add("Size", registryBytes(digest.Size))
	add("Pushed", registryAge(digest.LastPushed))
	if len(digest.Tags) == 0 {
		add("Tags", "(untagged)")
	} else {
		add("Tags", strings.Join(digest.Tags, ", "))
	}
	add("Reference", reg.PushPrefix()+"/"+repo.Name+"@"+digest.Digest)
}

func (m *browser) appendRegistryManifestDetail(reg client.Registry, repo client.Repository, add func(string, string),
	addSpinner func(string)) {
	add("Recent tag", repo.RecentTag)
	add("Reference", reg.PushPrefix()+"/"+repo.Name)
	key := registryKey(reg.ID, repo.Name)
	err := m.registryDigestErrors[key]
	if err != nil {
		add("Manifests", "load failed")
		_, _, contextWidth := m.paneWidths()
		errorWidth := max(registryDetailLinesValue18, m.paneWidthsInt()+contextWidth/2-registryErrorWidthRightInset)
		for _, part := range wrapRegistryText(err.Error(), errorWidth) {
			add("", part)
		}
		add("Action", "press Enter to retry")
		return
	}
	if digests, loaded := m.registryDigests[key]; loaded {
		add("Manifests", strconv.Itoa(len(digests)))
		return
	}
	if _, loading := m.inFlight[requestKey{kind: requestRegistry, appID: key}]; loading {
		addSpinner("Manifests")
		return
	}
	add("Manifests", "press Enter to load")
}

func (m *browser) moveRegistrySelection(delta int) {
	if len(m.registryRows) > 0 {
		m.registryCursor = min(max(0, m.registryCursor+delta), len(m.registryRows)-1)
	}
}

func (m *browser) handleRegistryKey(key string) tea.Cmd {
	if m.registryConfirm {
		return m.handleRegistryConfirmKey(key)
	}
	if m.registryTagMode {
		return m.handleRegistryTagKey(key)
	}
	return m.handleRegistryBrowseKey(key)
}

func (m *browser) handleRegistryConfirmKey(key string) tea.Cmd {
	switch key {
	case browserKeyEscape, "n":
		m.registryConfirm, m.registryDeleteTag, m.registryTagCopy = false, false, false
		m.registryConfirmText = ""
	case keyEnter:
		return m.runRegistryDelete()
	case keyBackspace:
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

func (m *browser) handleRegistryTagKey(key string) tea.Cmd {
	row := m.registryRows[m.registryCursor]
	reg := m.registries[row.registry]
	image := m.registryRepos[string(reg.ID)][row.repo].Name
	digest := m.registryDigests[registryKey(reg.ID, image)][row.digest]
	switch key {
	case browserKeyEscape:
		m.registryTagMode, m.registryTagCopy = false, false
	case "up", "k":
		m.registryTagCursor = max(0, m.registryTagCursor-1)
	case keyDown, "j":
		m.registryTagCursor = min(len(digest.Tags)-1, m.registryTagCursor+1)
	case keyEnter, "d", "y":
		return m.selectRegistryTag(digest.Tags)
	}
	return nil
}

func (m *browser) selectRegistryTag(tags []string) tea.Cmd {
	if m.registryTagCopy {
		if m.registryTagCursor < len(tags) {
			m.registryTagMode, m.registryTagCopy = false, false
			return m.copyRegistryReference(tags[m.registryTagCursor])
		}
		return nil
	}
	if m.allowWrites && m.deleteRegistryTags != nil {
		m.registryTagMode, m.registryDeleteTag, m.registryConfirm = false, true, true
		m.registryTagCopy, m.registryConfirmText = false, ""
		m.registryNotice = "Confirm removal of the selected tag."
	}
	return nil
}

func (m *browser) handleRegistryBrowseKey(key string) tea.Cmd {
	switch key {
	case "up", "k":
		m.moveRegistrySelection(-1)
	case keyDown, "j":
		m.moveRegistrySelection(1)
	case inputKeyPgUp, inputKeyPageUp:
		m.moveRegistrySelection(-max(1, m.height-handleRegistryKeyValue12))
	case inputKeyPgDown, inputKeyPageDown:
		m.moveRegistrySelection(max(1, m.height-handleRegistryKeyValue12))
	case inputKeyHome, "g":
		m.registryCursor = 0
	case inputKeyEnd, "G":
		m.registryCursor = max(0, len(m.registryRows)-1)
	case "r":
		return m.loadRegistry(true)
	case "d":
		return m.beginRegistryDelete()
	case "t":
		m.beginRegistryTagRemoval()
	case "y":
		return m.copySelectedRegistryReference()
	case keyEnter, keyRight, "l":
		return m.expandRegistrySelection()
	case browserKeyEscape:
		m.page = pageApps
		m.contextRow = 1
		m.contextFocused = true
		m.detail = false
	case keyLeft, "h":
		return m.collapseRegistrySelection()
	}
	return nil
}

func (m *browser) beginRegistryDelete() tea.Cmd {
	if !m.allowWrites || len(m.registryRows) == 0 {
		return nil
	}
	if m.registryRows[m.registryCursor].repo < 0 {
		m.registryNotice = "Select an image or manifest to delete."
		return nil
	}
	m.registryConfirm, m.registryDeleteTag, m.registryConfirmText = true, false, ""
	m.registryNotice = "Type delete, then press Enter to confirm."
	return nil
}

func (m *browser) beginRegistryTagRemoval() {
	if !m.allowWrites || !m.validRegistrySelection() {
		return
	}
	row := m.registryRows[m.registryCursor]
	if row.digest < 0 {
		return
	}
	reg := m.registries[row.registry]
	image := m.registryRepos[string(reg.ID)][row.repo].Name
	if len(m.registryDigests[registryKey(reg.ID, image)][row.digest].Tags) == 0 {
		return
	}
	m.registryTagMode, m.registryTagCopy, m.registryTagCursor = true, false, 0
}

func (m *browser) validRegistrySelection() bool {
	return m.registryCursor >= 0 && m.registryCursor < len(m.registryRows)
}

func (m *browser) copySelectedRegistryReference() tea.Cmd {
	if !m.validRegistrySelection() {
		return nil
	}
	row := m.registryRows[m.registryCursor]
	if row.repo < 0 {
		m.registryNotice = "Select an image or manifest to copy."
		return nil
	}
	reg := m.registries[row.registry]
	image := m.registryRepos[string(reg.ID)][row.repo]
	if row.digest < 0 {
		if image.RecentTag == "" {
			m.registryNotice = "This image has no recent tag."
			return nil
		}
		return m.copyRegistryReference(image.RecentTag)
	}
	digest := m.registryDigests[registryKey(reg.ID, image.Name)][row.digest]
	switch len(digest.Tags) {
	case 0:
		return m.copyRegistryReference("")
	case 1:
		return m.copyRegistryReference(digest.Tags[0])
	default:
		m.registryTagMode, m.registryTagCopy, m.registryTagCursor = true, true, 0
		return nil
	}
}

func (m *browser) expandRegistrySelection() tea.Cmd {
	if !m.validRegistrySelection() {
		return nil
	}
	row := m.registryRows[m.registryCursor]
	reg := m.registries[row.registry]
	if m.registryCollapsed == nil {
		m.registryCollapsed = map[string]bool{}
	}
	if row.repo < 0 {
		m.registryCollapsed[registryRegistryKey(reg.ID)] = false
		m.rebuildRegistryRows()
		return nil
	}
	if row.digest >= 0 {
		return nil
	}
	image := m.registryRepos[string(reg.ID)][row.repo].Name
	key := registryKey(reg.ID, image)
	if _, loaded := m.registryDigests[key]; !loaded {
		return m.loadRegistryDigests(row, false)
	}
	m.registryCollapsed[key] = false
	m.rebuildRegistryRows()
	return nil
}

func (m *browser) collapseRegistrySelection() tea.Cmd {
	if m.validRegistrySelection() {
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
		image := m.registryRepos[string(reg.ID)][row.repo].Name
		m.registryCollapsed[registryKey(reg.ID, image)] = true
		m.rebuildRegistryRows()
		return nil
	}
	if m.detail {
		m.detail = false
		return nil
	}
	m.page, m.contextRow, m.contextFocused = pageApps, 1, true
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
		m.registryNotice = messageClipboardUnavailable
		return
	}
	m.registryNotice = "Copied image reference to clipboard."
}

func (m *browser) runRegistryDelete() tea.Cmd {
	if m.registryConfirmText != actionDelete || len(m.registryRows) == 0 || m.registryBusy {
		m.registryNotice = "Type delete exactly to confirm."
		return nil
	}
	row := m.registryRows[m.registryCursor]
	reg := m.registries[row.registry]
	if row.repo < 0 {
		m.registryNotice = "Select an image to delete."
		m.registryConfirm = false
		return nil
	}
	image := m.registryRepos[string(reg.ID)][row.repo].Name
	op, notice := m.registryDeleteOperation(reg, row, image)
	if notice != "" {
		m.registryNotice = notice
		return nil
	}
	m.registryBusy = true
	m.registryConfirm = false
	m.registryNotice = "Deleting…"
	return func() tea.Msg { err := op(m.ctx); return registryDeleteResult{tenant: m.tenant, row: row, err: err} }
}

func (m *browser) registryDeleteOperation(reg client.Registry, row registryTreeRow, image string) (func(context.Context) error, string) {
	if m.registryDeleteTag {
		d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
		if m.deleteRegistryTags == nil || m.registryTagCursor >= len(d.Tags) {
			return nil, "Tag deletion unavailable."
		}
		tag := d.Tags[m.registryTagCursor]
		return func(ctx context.Context) error {
			return m.deleteRegistryTags(ctx, m.tenant, reg.ID, image, d.Digest, []string{tag})
		}, ""
	}
	if row.digest < 0 {
		if m.deleteRegistryRepository == nil {
			return nil, "Image deletion unavailable."
		}
		return func(ctx context.Context) error { return m.deleteRegistryRepository(ctx, m.tenant, reg.ID, image) }, ""
	}
	d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
	if m.deleteRegistryDigests == nil {
		return nil, "Digest deletion unavailable."
	}
	return func(ctx context.Context) error {
		return m.deleteRegistryDigests(ctx, m.tenant, reg.ID, image, []string{d.Digest})
	}, ""
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
	width := min(registryConfirmationValue72, m.width-registryConfirmationOuterMargin)
	inner := width - registryConfirmationValue6
	if m.registryDeleteTag && row.digest >= 0 {
		image := m.registryRepos[string(reg.ID)][row.repo].Name
		d := m.registryDigests[registryKey(reg.ID, image)][row.digest]
		if m.registryTagCursor < len(d.Tags) {
			target += " tag " + d.Tags[m.registryTagCursor]
		}
	}
	content := []string{paint("✖ Delete registry content", browserDanger, true), paint(strings.Repeat("─", inner), browserBorder,
		false), "Target  " + safeText(target), "This action cannot be undone.",
		paint("Type delete  "+safeText(m.registryConfirmText)+"█", browserKey, true), paint("Esc cancels · Enter confirms", browserMuted,
			false)}
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
	width := min(registryTagOverlayValue64, m.width-registryTagOverlayOuterMargin)
	inner := width - registryTagOverlayValue6
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
		return m.handleContextPaneMouse(msg, bodyY, false)
	}
	appsWidth, _, _ := m.paneWidths()
	left := contextWidth + handleRegistryMouseValue3
	if msg.X >= left && msg.X < left+appsWidth+2 {
		paneHeight := max(1, m.height-bodyY-browserFooterHeight-handleRegistryMouseValue3)
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
