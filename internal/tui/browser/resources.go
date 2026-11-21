package browser

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

const (
	resourcePVCNameWidth     = 20
	resourcePVCMinNameWidth  = 12
	resourcePVCColumnCount   = 3
	resourcePVCWidthDivisor  = 2
	demoPlanCPURequest       = 500
	handleResourcesKeyValue5 = 5
	handleResourcesKeyValue8 = 8
	resourceEditUnitValue2   = 2
	resourceEditUnitValue3   = 3
	resourceEditUnitValue8   = 8
	resourceValueValue2      = 2
	resourceValueValue3      = 3
	resourceValueValue4      = 4
	resourceValueValue5      = 5
	resourceValueValue6      = 6
	resourceValueValue7      = 7
	resourceValueValue8      = 8
	stageResourceEditValue2  = 2
	stageResourceEditValue3  = 3
	stageResourceEditValue4  = 4
	stageResourceEditValue6  = 6
	stageResourceEditValue7  = 7
	stageResourceEditValue8  = 8
)

var resourceFields = []string{"Plan", "CPU request", "RAM limit", "Disk size (GiB)", "Replicas", "Autoscaling", "Min replicas",
	"Max replicas", "CPU target"}

type resourceWriteResult struct {
	tenant, appID string
	draft         ResourceConfig
	err           error
}
type resourcePlansResult struct {
	tenant string
	plans  []client.Plan
	err    error
}

func (m *browser) loadResourcePlans() tea.Cmd {
	if m.listResourcePlans == nil {
		return nil
	}
	tenant, fetch := m.tenant, m.listResourcePlans
	return func() tea.Msg {
		plans, err := fetch(m.ctx, tenant)
		return resourcePlansResult{tenant: tenant, plans: plans, err: err}
	}
}

func (m *browser) handleResourcePlans(msg resourcePlansResult) {
	if msg.tenant != m.tenant {
		return
	}
	if msg.err != nil {
		m.resourcePlanErr = "Could not load plans"
		return
	}
	m.resourcePlanErr = ""
	m.resourcePlans = make([]client.Plan, 0, len(msg.plans))
	for _, plan := range msg.plans {
		if plan.PlanType == labelApp {
			m.resourcePlans = append(m.resourcePlans, plan)
		}
	}
	if len(m.resourcePlans) == 0 {
		m.resourcePlanErr = "No app plans are available"
	}
}

func (m *browser) selectedResourceApp() client.App {
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	return a
}

func resourceConfigForApp(a client.App) ResourceConfig {
	d := ResourceConfig{CPU: a.CPURequest, RAM: a.RAMLimit, Replicas: a.Replicas, Autoscaling: a.IsHPAEnabled}
	if a.Disk != nil {
		d.DiskGi = a.Disk.SizeInGi
	}
	if a.Plan != nil {
		d.Plan = a.Plan.ID
	}
	if a.CustomConfig != nil && a.CustomConfig.HPA != nil {
		h := a.CustomConfig.HPA
		d.Autoscaling = h.Enabled || a.IsHPAEnabled
		d.MinReplicas, d.MaxReplicas, d.CPUTarget = h.MinReplicas, h.MaxReplicas, h.TargetCPUUtilizationPercentage
	}
	return d
}

func (m *browser) ensureResourceDraft() {
	if !m.selectedAppActive() {
		return
	}
	if m.resourceOriginal.Plan == "" && m.resourceOriginal.CPU == "" && m.resourceOriginal.RAM == "" {
		m.resourceOriginal = resourceConfigForApp(m.selectedResourceApp())
		m.resourceDraft = m.resourceOriginal
	}
	if strings.HasPrefix(m.mode, "MOCK") && len(m.resourcePlans) == 0 {
		a := m.selectedResourceApp()
		if a.Plan != nil {
			m.resourcePlans = append(m.resourcePlans, *a.Plan)
		}
		m.resourcePlans = append(m.resourcePlans, client.Plan{ID: "plan-standard-demo", Name: "Standard", CodeName: "standard",
			PlanType: "app", CostType: "fixed", Detail: client.PlanDetail{CPURequest: demoPlanCPURequest, RAMLimit: mockPlanRAMLimit}})
	}
}

func (m *browser) resourcesPaneLines() []string {
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{summaryField("App", "not selected")}
	}
	m.ensureResourceDraft()
	a := m.selectedResourceApp()
	lines := []string{sectionHeading("Resources"), ""}
	if m.detailState.err != nil {
		lines = append(lines, summaryField("Refresh", "failed · "+safeText(m.detailState.err.Error())))
	}
	changed := resourceConfigChanged(m.resourceOriginal, m.resourceDraft)
	lines = m.appendResourceSettings(lines, a, changed)
	if m.resourceReview {
		return m.resourceReviewLines(changed)
	}
	if m.resourcePlanPicker {
		return m.resourcePlanLines()
	}
	return m.resourceUsageLines(lines, a)
}

func (m *browser) appendResourceSettings(lines []string, a client.App, changed []bool) []string {
	d := m.resourceDraft
	if !m.allowWrites && m.resourceCursor == 0 {
		m.resourceCursor = 1
	}
	settings := []string{"", d.CPU, d.RAM, "not configured", strconv.Itoa(d.Replicas),
		map[bool]string{true: "● enabled",
			false: "○ disabled"}[d.Autoscaling], strconv.Itoa(d.MinReplicas), strconv.Itoa(d.MaxReplicas), fmt.Sprintf("%d%%", d.CPUTarget)}
	if a.Disk != nil {
		settings[3] = strconv.Itoa(d.DiskGi)
	}
	for i, label := range resourceFields {
		if i == 0 {
			if !m.allowWrites {
				continue
			}
			label = "Change plan"
		}
		value := settings[i]
		if i == 5 && d.Autoscaling {
			value = paint(value, browserSuccess, true)
		}
		if i == 4 && d.Autoscaling {
			value += paint(" · controlled by autoscaler", browserMuted, false)
		}
		marker := "  "
		label = fmt.Sprintf("%-16s", label)
		if m.resourceCursor == i {
			marker = domainMarker("› ", true)
			label = paint(label, browserAccent, true)
		} else {
			label = paint(label, browserMuted, true)
		}
		if m.resourceEditing && m.resourceCursor == i {
			value = m.resourceEditValue + "█" + resourceEditUnit(m.resourceCursor)
		}
		if changed[i] {
			value = paint(value, browserAccent, true)
		}
		lines = append(lines, marker+label+" "+value)
	}
	if m.resourceBusy {
		lines = append(lines, "", summaryField("Saving", "updating app resources"))
	}
	if m.resourceMessage != "" {
		lines = append(lines, "", paint(safeText(m.resourceMessage), browserMuted, false))
	}
	if changedAny(changed) {
		lines = append(lines, "", paint("● staged changes", browserAccent, true))
	}
	return lines
}

func (m *browser) resourcePlanName(id string, app client.App) string {
	if app.Plan != nil && app.Plan.ID == id && app.Plan.Name != "" {
		return app.Plan.Name
	}
	for _, plan := range m.resourcePlans {
		if plan.ID == id {
			if plan.Name != "" {
				return plan.Name
			}
			if plan.CodeName != "" {
				return plan.CodeName
			}
		}
	}
	return "Unknown plan"
}

func (m *browser) resourceReviewLines(changed []bool) []string {
	lines := []string{sectionHeading("Review resource changes"), ""}
	for i, label := range resourceFields {
		if changed[i] {
			oldValue, newValue := resourceValue(m.resourceOriginal, i), resourceValue(m.resourceDraft, i)
			if i == 0 {
				app := m.selectedResourceApp()
				oldValue, newValue = m.resourcePlanName(m.resourceOriginal.Plan, app), m.resourcePlanName(m.resourceDraft.Plan, app)
			}
			lines = append(lines, fmt.Sprintf("~ %s  %s → %s", label, oldValue, newValue))
		}
	}
	lines = append(lines, "", paint("Saving may restart the app or change its billed plan.", browserNumber, true))
	if changed[3] {
		lines = append(lines, paint("Disk growth is one-way; shrinking is not supported.", browserNumber, true))
	}
	return lines
}

func (m *browser) resourcePlanLines() []string {
	lines := []string{sectionHeading("Choose plan"), ""}
	if m.resourcePlanErr != "" {
		lines = append(lines, paint(m.resourcePlanErr, browserNumber, false))
	}
	if len(m.resourcePlans) == 0 {
		lines = append(lines, summaryField("Plans", "loading or unavailable"))
	}
	for i, plan := range m.resourcePlans {
		name := plan.Name
		if name == "" {
			name = plan.CodeName
		}
		if name == "" {
			name = plan.ID
		}
		description := plan.CostType
		if plan.Detail.CPURequest > 0 {
			description += fmt.Sprintf(" · %dm CPU", plan.Detail.CPURequest)
		}
		if plan.Detail.RAMLimit > 0 {
			description += fmt.Sprintf(" · %dM RAM", plan.Detail.RAMLimit)
		}
		line := "  " + name + "  " + paint(description, browserMuted, false)
		if i == m.resourcePlanCursor {
			line = selectedRowStyle().Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *browser) resourceUsageLines(lines []string, a client.App) []string {
	lines = append(lines, "", sectionHeading("Current usage"))
	usage := m.detailUsage
	if m.usageAppID != a.ID {
		usage = nil
	}
	lines = append(lines,
		usageField(" CPU", usageValue(usage, func(u *AppUsage) *float64 { return u.CPUCores }), resourceLimit(a, "cpu"), formatCores,
			usage != nil && usage.Failed.CPU),
		usageField(" RAM", usageValue(usage, func(u *AppUsage) *float64 { return u.RAMBytes }), resourceLimit(a, "ram"), formatBytes,
			usage != nil && usage.Failed.RAM))
	if a.Disk != nil && a.Disk.SizeInGi > 0 {
		lines = append(lines, usageField(" Disk", usageValue(usage, func(u *AppUsage) *float64 { return u.DiskBytes }), resourceLimit(a,
			"disk"), formatBytes, usage != nil && usage.Failed.Disk))
	}
	lines = append(lines, "")
	lines = append(lines, m.resourcePVCsLines(a)...)
	lines = append(lines, m.historyLines()...)
	if !m.usageState.lastSuccessAt.IsZero() {
		lines = append(lines, "", summaryField("Updated", formatLocalWallTime(m.usageState.lastSuccessAt, "Jan 2, 3:04 PM MST")))
	}
	lines = append(lines, summaryField("Usage window", "last 5 minutes"))
	return lines
}

func (m *browser) resourcePVCsLines(a client.App) []string {
	if m.pvcsState.status == requestEmpty || m.pvcsState.status == requestIdle || m.pvcsState.status == requestUnavailable {
		return nil
	}
	if m.pvcsState.status == requestLoading || m.pvcsState.status == requestRefreshing {
		return []string{paint("Loading persistent volumes…", browserMuted, false), ""}
	}
	if m.pvcsState.status == requestFailed || m.pvcsState.status == requestStale {
		return []string{paint("Persistent volumes unavailable", browserNumber, false), ""}
	}
	if len(m.appPVCs) == 0 {
		return nil
	}
	width := m.domainContentWidth()
	nameWidth := min(max(len("NAME"), resourcePVCNameWidth), max(resourcePVCMinNameWidth, width/resourcePVCWidthDivisor))
	sizeWidth, statusWidth := 8, 10
	classWidth := max(1, width-nameWidth-sizeWidth-statusWidth-resourcePVCColumnCount)
	lines := []string{"", sectionHeading(fmt.Sprintf("Persistent volumes · %d", len(m.appPVCs))),
		resourcePVCHeader(nameWidth, sizeWidth, statusWidth, classWidth)}
	for _, pvc := range m.appPVCs {
		glyph, statusColor := "●", browserSuccess
		switch strings.ToLower(pvc.Status) {
		case "pending", "terminating":
			glyph, statusColor = "◷", browserNumber
		case "lost", "failed", "error":
			glyph, statusColor = "●", browserDanger
		case "":
			glyph, statusColor = "○", browserMuted
		}
		status := paint(glyph+" "+displayValue(pvc.Status), statusColor, true)
		row := domainCell(paint(safeText(pvc.Name), browserAccent, true), nameWidth) + " " +
			domainCell(paint(safeText(pvc.Size), browserValue, true), sizeWidth) + " " +
			domainCell(status, statusWidth) + " " + domainCell(safeText(pvc.StorageClass), classWidth)
		lines = append(lines, row)
		for _, partition := range appDiskPartitions(a) {
			if partition != "" {
				lines = append(lines, paint("  Mount  "+safeText(partition), browserMuted, false))
			}
		}
	}
	return append(lines, "")
}

func resourcePVCHeader(nameWidth, sizeWidth, statusWidth, classWidth int) string {
	columns := []string{
		domainCell("NAME", nameWidth), domainCell("SIZE", sizeWidth),
		domainCell("STATUS", statusWidth), domainCell("STORAGE CLASS", classWidth),
	}
	return paint(strings.Join(columns, " "), browserMuted, true)
}

func appDiskPartitions(a client.App) []string {
	if a.Disk == nil {
		return nil
	}
	mounts := make([]string, 0, len(a.Disk.Partitions))
	for _, partition := range a.Disk.Partitions {
		if path := strings.TrimSpace(partition.MountPath); path != "" {
			mounts = append(mounts, path)
		}
	}
	return slices.Compact(mounts)
}

func resourceConfigChanged(a, b ResourceConfig) []bool {
	return []bool{a.Plan != b.Plan, a.CPU != b.CPU, a.RAM != b.RAM, a.DiskGi != b.DiskGi, a.Replicas != b.Replicas,
		a.Autoscaling != b.Autoscaling, a.MinReplicas != b.MinReplicas, a.MaxReplicas != b.MaxReplicas, a.CPUTarget != b.CPUTarget}
}
func changedAny(v []bool) bool {
	for _, x := range v {
		if x {
			return true
		}
	}
	return false
}
func resourceValue(d ResourceConfig, i int) string {
	switch i {
	case 0:
		return d.Plan
	case 1:
		return d.CPU
	case resourceValueValue2:
		return d.RAM
	case resourceValueValue3:
		return strconv.Itoa(d.DiskGi)
	case resourceValueValue4:
		return strconv.Itoa(d.Replicas)
	case resourceValueValue5:
		if d.Autoscaling {
			return "enabled"
		}
		return "disabled"
	case resourceValueValue6:
		return strconv.Itoa(d.MinReplicas)
	case resourceValueValue7:
		return strconv.Itoa(d.MaxReplicas)
	case resourceValueValue8:
		return fmt.Sprintf("%d%%", d.CPUTarget)
	}
	return ""
}

func (m *browser) handleResourcesKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.resourceBusy {
		return nil
	}
	if m.resourceReview {
		return m.handleResourceReviewKey(msg.String())
	}
	if m.resourcePlanPicker {
		m.handleResourcePlanPickerKey(msg.String())
		return nil
	}
	if m.resourceEditing {
		m.handleResourceEditingKey(msg)
		return nil
	}
	return m.handleResourceBrowseKey(msg.String())
}

func (m *browser) handleResourceReviewKey(key string) tea.Cmd {
	switch key {
	case keyEnter, "y":
		m.resourceReview = false
		return m.saveResources()
	case browserKeyEscape, "n":
		m.resourceReview = false
		m.resourceMessage = messageSaveCancelled
	}
	return nil
}

func (m *browser) handleResourcePlanPickerKey(key string) {
	switch key {
	case browserKeyEscape:
		m.resourcePlanPicker = false
	case "up", "k":
		m.resourcePlanCursor = max(0, m.resourcePlanCursor-1)
	case keyDown, "j":
		m.resourcePlanCursor = min(len(m.resourcePlans)-1, m.resourcePlanCursor+1)
	case keyEnter:
		if m.resourcePlanCursor >= 0 && m.resourcePlanCursor < len(m.resourcePlans) {
			p := m.resourcePlans[m.resourcePlanCursor]
			m.resourceDraft.Plan = p.ID
			if p.Detail.CPURequest > 0 {
				m.resourceDraft.CPU = fmt.Sprintf("%dm", p.Detail.CPURequest)
			}
			if p.Detail.RAMLimit > 0 {
				m.resourceDraft.RAM = fmt.Sprintf("%dM", p.Detail.RAMLimit)
			}
			m.resourcePlanPicker = false
			m.resourceMessage = ""
		}
	}
}

func (m *browser) handleResourceEditingKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case browserKeyEscape:
		m.resourceEditing = false
	case keyEnter:
		m.stageResourceEdit()
	case keyCtrlU:
		m.resourceEditValue = ""
	case keyBackspace, keyCtrlH:
		r := []rune(m.resourceEditValue)
		if len(r) > 0 {
			m.resourceEditValue = string(r[:len(r)-1])
		}
	default:
		if msg.Text != "" && resourceDigits(msg.Text) {
			m.resourceEditValue += msg.Text
		}
	}
}

func (m *browser) handleResourceBrowseKey(key string) tea.Cmd {
	switch key {
	case "up", "k":
		first := 0
		if !m.allowWrites {
			first = 1
		}
		m.resourceCursor = max(first, m.resourceCursor-1)
	case keyDown, "j":
		m.resourceCursor = min(len(resourceFields)-1, m.resourceCursor+1)
	case "e":
		return m.beginResourceEdit()
	case "s":
		return m.handleResourceBrowseAction(key)
	case "x", "r", "w", "m", "p":
		return m.handleResourceBrowseAction(key)
	}
	return nil
}

func (m *browser) beginResourceEdit() tea.Cmd {
	if !m.allowWrites {
		return nil
	}
	if m.resourceCursor == handleResourcesKeyValue5 {
		m.resourceDraft.Autoscaling = !m.resourceDraft.Autoscaling
		return nil
	}
	if m.resourceCursor == 0 {
		return m.openResourcePlanPicker()
	}
	if message := m.resourceEditBlockMessage(); message != "" {
		m.resourceMessage = message
		return nil
	}
	m.resourceEditing = true
	m.resourceEditValue = resourceValue(m.resourceDraft, m.resourceCursor)
	if m.resourceCursor == 1 || m.resourceCursor == 2 {
		m.resourceEditValue = strings.TrimSuffix(m.resourceEditValue, resourceEditUnit(m.resourceCursor))
	}
	if m.resourceCursor == handleResourcesKeyValue8 {
		m.resourceEditValue = strings.TrimSuffix(m.resourceEditValue, "%")
	}
	return nil
}

func (m *browser) openResourcePlanPicker() tea.Cmd {
	if m.listResourcePlans != nil && len(m.resourcePlans) == 0 {
		if m.resourcePlanErr != "" {
			m.resourceMessage = m.resourcePlanErr
			return nil
		}
		return m.loadResourcePlans()
	}
	m.resourcePlanCursor = findResourcePlan(m.resourcePlans, m.resourceDraft.Plan)
	m.resourcePlanPicker = true
	return nil
}

func (m *browser) resourceEditBlockMessage() string {
	if m.resourceCursor == 4 && m.resourceDraft.Autoscaling {
		return "Replicas are controlled by autoscaling"
	}
	if m.resourceCursor == 3 && m.resourceOriginal.DiskGi == 0 {
		return "A disk cannot be added after app creation"
	}
	if m.resourceCursor >= 6 && !m.resourceDraft.Autoscaling {
		return "Enable autoscaling before editing these settings"
	}
	if m.resourceCursor == 1 || m.resourceCursor == 2 {
		app := m.selectedResourceApp()
		if app.Plan != nil && app.Plan.CostType != "dynamic" && m.resourceDraft.Plan == m.resourceOriginal.Plan {
			return "CPU and RAM are fixed by this plan"
		}
	}
	return ""
}

func (m *browser) handleResourceBrowseAction(key string) tea.Cmd {
	switch key {
	case "s":
		if m.allowWrites && changedAny(resourceConfigChanged(m.resourceOriginal, m.resourceDraft)) {
			if message := m.resourceSaveBlockMessage(); message != "" {
				m.resourceMessage = message
			} else {
				m.resourceReview = true
				m.summaryScroll = 0
			}
		}
	case "x":
		if m.allowWrites {
			m.resourceDraft = m.resourceOriginal
			m.resourceMessage = messageChangesDiscarded
		}
	case "r":
		if !changedAny(resourceConfigChanged(m.resourceOriginal, m.resourceDraft)) {
			m.pvcsState.reset()
			return tea.Batch(m.loadSelectedSummary(), m.loadSelectedUsage(), m.loadUsageHistory(), m.loadSelectedPVCs())
		}
		m.resourceMessage = messageSaveOrDiscardBeforeRefresh
	case "w":
		m.historyRange = (m.historyRange + 1) % len(usageRanges)
		m.historyState.reset()
		m.history = nil
		return m.loadUsageHistory()
	case "m":
		metrics := availableHistoryMetrics(m.selectedResourceApp())
		if len(metrics) > 0 {
			m.historyMetric = (m.historyMetric + 1) % len(metrics)
		}
	case "p":
		m.nextHistoryPod()
	}
	return nil
}

func (m *browser) resourceSaveBlockMessage() string {
	if m.resourceOriginal.DiskGi > 0 && m.resourceDraft.DiskGi < m.resourceOriginal.DiskGi {
		return "Disk cannot be shrunk; choose the current size or a larger size"
	}
	if err := validateResourceDraft(m.resourceDraft); err != nil {
		return err.Error()
	}
	return ""
}

func validateResourceDraft(d ResourceConfig) error {
	if d.Replicas < 0 {
		return errReplicasMustBeZeroOrGreater
	}
	if d.Autoscaling && (d.MinReplicas < 1 || d.MaxReplicas < d.MinReplicas || d.CPUTarget < 1) {
		return errAutoscalingNeeds1MinMaxAndA
	}
	return nil
}

func resourceEditUnit(field int) string {
	switch field {
	case 1:
		return "m"
	case resourceEditUnitValue2:
		return "M"
	case resourceEditUnitValue3:
		return " GiB"
	case resourceEditUnitValue8:
		return "%"
	}
	return ""
}

func resourceDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func (m *browser) stageResourceEdit() {
	v := strings.TrimSpace(m.resourceEditValue)
	n, err := strconv.Atoi(v)
	switch m.resourceCursor {
	case 0:
		if v == "" {
			m.resourceMessage = "Plan cannot be empty"
			return
		}
		m.resourceDraft.Plan = v
	default:
		if !m.stageNumericResourceEdit(v, n, err) {
			return
		}
	}
	m.resourceEditing = false
	m.resourceMessage = ""
}

func (m *browser) stageNumericResourceEdit(value string, number int, parseErr error) bool {
	if m.resourceCursor == 1 {
		if parseErr != nil || number < 1 {
			m.resourceMessage = "CPU request must be a positive number of millicores"
			return false
		}
		m.resourceDraft.CPU = value + "m"
		return true
	}
	if m.resourceCursor == stageResourceEditValue2 {
		if parseErr != nil || number < 1 {
			m.resourceMessage = "RAM limit must be a positive number of megabytes"
			return false
		}
		m.resourceDraft.RAM = value + "M"
		return true
	}
	if parseErr != nil || number < 0 || ((m.resourceCursor == 3 || m.resourceCursor == 8) && number == 0) {
		m.resourceMessage = "Enter a valid non-negative number"
		return false
	}
	switch m.resourceCursor {
	case stageResourceEditValue3:
		if m.resourceOriginal.DiskGi == 0 {
			m.resourceMessage = "A disk cannot be added after app creation"
			return false
		}
		m.resourceDraft.DiskGi = number
	case stageResourceEditValue4:
		m.resourceDraft.Replicas = number
	case stageResourceEditValue6:
		m.resourceDraft.MinReplicas = number
	case stageResourceEditValue7:
		m.resourceDraft.MaxReplicas = number
	case stageResourceEditValue8:
		m.resourceDraft.CPUTarget = number
	}
	return true
}

func (m *browser) saveResources() tea.Cmd {
	if m.updateResources == nil {
		m.resourceBusy = false
		return func() tea.Msg {
			return resourceWriteResult{tenant: m.tenant, appID: m.selectedDetailTarget(), draft: m.resourceDraft}
		}
	}
	draft := m.resourceDraft
	tenant, id := m.tenant, m.selectedDetailTarget()
	m.resourceBusy = true
	return func() tea.Msg {
		err := m.updateResources(m.ctx, tenant, id, draft)
		return resourceWriteResult{tenant: tenant, appID: id, draft: draft, err: err}
	}
}

func (m *browser) handleResourcesWrite(msg resourceWriteResult) tea.Cmd {
	if msg.tenant != m.tenant || msg.appID != m.selectedDetailTarget() {
		return nil
	}
	m.resourceBusy = false
	if msg.err != nil {
		m.resourceMessage = "Save failed · " + safeText(msg.err.Error())
		return nil
	}
	m.resourceOriginal = msg.draft
	m.resourceDraft = msg.draft
	m.resourceMessage = "Resources saved"
	m.resourceAwaitingRefresh = true
	m.detailState.reset()
	return tea.Batch(m.loadSelectedSummary(), m.loadSelectedUsage(), m.loadUsageHistory())
}

func (m *browser) syncResourceDraftFromDetail() {
	if !m.resourceAwaitingRefresh || m.detailApp == nil || m.detailApp.ID != m.selectedDetailTarget() {
		return
	}
	m.resourceOriginal = resourceConfigForApp(*m.detailApp)
	m.resourceDraft = m.resourceOriginal
	m.resourceAwaitingRefresh = false
}
