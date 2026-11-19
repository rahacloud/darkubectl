package browser

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rahacloud/darkubectl/internal/client"
)

var resourceFields = []string{"Plan", "CPU request", "RAM limit", "Disk size (GiB)", "Replicas", "Autoscaling", "Min replicas", "Max replicas", "CPU target"}

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
		if plan.PlanType == "app" {
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
		m.resourcePlans = append(m.resourcePlans, client.Plan{ID: "plan-standard-demo", Name: "Standard", CodeName: "standard", PlanType: "app", CostType: "fixed", Detail: client.PlanDetail{CPURequest: 500, RAMLimit: 1024}})
	}
}

func (m *browser) resourcesPaneLines() []string {
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{summaryField("App", "not selected")}
	}
	m.ensureResourceDraft()
	a := m.selectedResourceApp()
	d := m.resourceDraft
	lines := []string{sectionHeading("Resources"), ""}
	if m.detailState.err != nil {
		lines = append(lines, summaryField("Refresh", "failed · "+safeText(m.detailState.err.Error())))
	}
	changed := resourceConfigChanged(m.resourceOriginal, d)
	settings := []string{d.Plan, d.CPU, d.RAM, "not configured", strconv.Itoa(d.Replicas), map[bool]string{true: "● enabled", false: "○ disabled"}[d.Autoscaling], strconv.Itoa(d.MinReplicas), strconv.Itoa(d.MaxReplicas), fmt.Sprintf("%d%%", d.CPUTarget)}
	if a.Disk != nil {
		settings[3] = strconv.Itoa(d.DiskGi)
	}
	for i, label := range resourceFields {
		value := settings[i]
		if i == 0 && a.Plan != nil && a.Plan.Name != "" && d.Plan == a.Plan.ID {
			value = a.Plan.Name
		}
		if i == 5 && d.Autoscaling {
			value = paint(value, browserSuccess, true)
		}
		if i == 4 && d.Autoscaling {
			value += paint(" · controlled by autoscaler", browserMuted, false)
		}
		marker := "  "
		label = fmt.Sprintf("%-16s", label+":")
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
	if m.resourceReview {
		lines = []string{sectionHeading("Review resource changes"), ""}
		for i, label := range resourceFields {
			if changed[i] {
				old := resourceValue(m.resourceOriginal, i)
				next := resourceValue(d, i)
				lines = append(lines, fmt.Sprintf("~ %s: %s → %s", label, old, next))
			}
		}
		lines = append(lines, "", paint("Saving may restart the app or change its billed plan.", browserNumber, true))
		if changed[3] {
			lines = append(lines, paint("Disk growth is one-way; shrinking is not supported.", browserNumber, true))
		}
		return lines
	}
	if m.resourcePlanPicker {
		lines := []string{sectionHeading("Choose plan"), ""}
		if m.resourcePlanErr != "" {
			lines = append(lines, paint(m.resourcePlanErr, browserNumber, false))
		}
		if len(m.resourcePlans) == 0 {
			lines = append(lines, summaryField("Plans", "loading or unavailable"))
		}
		for i, plan := range m.resourcePlans {
			marker := "  "
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
			line := marker + name + "  " + paint(description, browserMuted, false)
			if i == m.resourcePlanCursor {
				line = selectedRowStyle().Render(line)
			}
			lines = append(lines, line)
		}
		return lines
	}
	lines = append(lines, "", sectionHeading("Current usage"))
	usage := m.detailUsage
	if m.usageAppID != a.ID {
		usage = nil
	}
	lines = append(lines,
		usageField(" CPU", usageValue(usage, func(u *AppUsage) *float64 { return u.CPUCores }), resourceLimit(a, "cpu"), formatCores, usage != nil && usage.Failed.CPU),
		usageField(" RAM", usageValue(usage, func(u *AppUsage) *float64 { return u.RAMBytes }), resourceLimit(a, "ram"), formatBytes, usage != nil && usage.Failed.RAM))
	if a.Disk != nil && a.Disk.SizeInGi > 0 {
		lines = append(lines, usageField(" Disk", usageValue(usage, func(u *AppUsage) *float64 { return u.DiskBytes }), resourceLimit(a, "disk"), formatBytes, usage != nil && usage.Failed.Disk))
	}
	lines = append(lines, "")
	lines = append(lines, m.historyLines()...)
	if !m.usageState.lastSuccessAt.IsZero() {
		lines = append(lines, "", summaryField("Updated", m.usageState.lastSuccessAt.Local().Format("Jan 2, 3:04 PM MST")))
	}
	lines = append(lines, summaryField("Usage window", "last 5 minutes"))
	return lines
}

func resourceConfigChanged(a, b ResourceConfig) []bool {
	return []bool{a.Plan != b.Plan, a.CPU != b.CPU, a.RAM != b.RAM, a.DiskGi != b.DiskGi, a.Replicas != b.Replicas, a.Autoscaling != b.Autoscaling, a.MinReplicas != b.MinReplicas, a.MaxReplicas != b.MaxReplicas, a.CPUTarget != b.CPUTarget}
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
	case 2:
		return d.RAM
	case 3:
		return strconv.Itoa(d.DiskGi)
	case 4:
		return strconv.Itoa(d.Replicas)
	case 5:
		if d.Autoscaling {
			return "enabled"
		}
		return "disabled"
	case 6:
		return strconv.Itoa(d.MinReplicas)
	case 7:
		return strconv.Itoa(d.MaxReplicas)
	case 8:
		return fmt.Sprintf("%d%%", d.CPUTarget)
	}
	return ""
}

func (m *browser) handleResourcesKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.resourceBusy {
		return nil
	}
	if m.resourceReview {
		switch msg.String() {
		case "enter", "y":
			m.resourceReview = false
			return m.saveResources()
		case "esc", "n":
			m.resourceReview = false
			m.resourceMessage = "Save cancelled · staged changes remain"
		}
		return nil
	}
	if m.resourcePlanPicker {
		switch msg.String() {
		case "esc":
			m.resourcePlanPicker = false
		case "up", "k":
			m.resourcePlanCursor = max(0, m.resourcePlanCursor-1)
		case "down", "j":
			m.resourcePlanCursor = min(len(m.resourcePlans)-1, m.resourcePlanCursor+1)
		case "enter":
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
		return nil
	}
	if m.resourceEditing {
		switch msg.String() {
		case "esc":
			m.resourceEditing = false
		case "enter":
			m.stageResourceEdit()
		case "ctrl+u":
			m.resourceEditValue = ""
		case "backspace", "ctrl+h":
			r := []rune(m.resourceEditValue)
			if len(r) > 0 {
				m.resourceEditValue = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" && resourceDigits(msg.Text) {
				m.resourceEditValue += msg.Text
			}
		}
		return nil
	}
	switch msg.String() {
	case "up", "k":
		m.resourceCursor = max(0, m.resourceCursor-1)
	case "down", "j":
		m.resourceCursor = min(len(resourceFields)-1, m.resourceCursor+1)
	case "e":
		if !m.allowWrites {
			return nil
		}
		if m.resourceCursor == 5 {
			m.resourceDraft.Autoscaling = !m.resourceDraft.Autoscaling
			return nil
		}
		if m.resourceCursor == 0 {
			if m.listResourcePlans != nil && len(m.resourcePlans) == 0 {
				if m.resourcePlanErr != "" {
					m.resourceMessage = m.resourcePlanErr
					return nil
				}
				return m.loadResourcePlans()
			}
			m.resourcePlanCursor = 0
			for i, p := range m.resourcePlans {
				if p.ID == m.resourceDraft.Plan {
					m.resourcePlanCursor = i
					break
				}
			}
			m.resourcePlanPicker = true
			return nil
		}
		if m.resourceCursor == 4 && m.resourceDraft.Autoscaling {
			m.resourceMessage = "Replicas are controlled by autoscaling"
			return nil
		}
		if m.resourceCursor == 3 && m.resourceOriginal.DiskGi == 0 {
			m.resourceMessage = "A disk cannot be added after app creation"
			return nil
		}
		if m.resourceCursor >= 6 && !m.resourceDraft.Autoscaling {
			m.resourceMessage = "Enable autoscaling before editing these settings"
			return nil
		}
		if m.resourceCursor == 1 || m.resourceCursor == 2 {
			if a := m.selectedResourceApp(); a.Plan != nil && a.Plan.CostType != "dynamic" && m.resourceDraft.Plan == m.resourceOriginal.Plan {
				m.resourceMessage = "CPU and RAM are fixed by this plan"
				return nil
			}
		}
		m.resourceEditing = true
		m.resourceEditValue = resourceValue(m.resourceDraft, m.resourceCursor)
		if m.resourceCursor == 1 || m.resourceCursor == 2 {
			m.resourceEditValue = strings.TrimSuffix(m.resourceEditValue, resourceEditUnit(m.resourceCursor))
		}
		if m.resourceCursor == 8 {
			m.resourceEditValue = strings.TrimSuffix(m.resourceEditValue, "%")
		}
	case "s":
		if m.allowWrites && changedAny(resourceConfigChanged(m.resourceOriginal, m.resourceDraft)) {
			if m.resourceOriginal.DiskGi > 0 && m.resourceDraft.DiskGi < m.resourceOriginal.DiskGi {
				m.resourceMessage = "Disk cannot be shrunk; choose the current size or a larger size"
			} else if err := validateResourceDraft(m.resourceDraft); err != nil {
				m.resourceMessage = err.Error()
			} else {
				m.resourceReview = true
				m.summaryScroll = 0
			}
		}
	case "x":
		if m.allowWrites {
			m.resourceDraft = m.resourceOriginal
			m.resourceMessage = "Staged changes discarded"
		}
	case "r":
		if !changedAny(resourceConfigChanged(m.resourceOriginal, m.resourceDraft)) {
			return tea.Batch(m.loadSelectedSummary(), m.loadSelectedUsage(), m.loadUsageHistory())
		}
		m.resourceMessage = "Save or discard staged changes before refreshing"
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

func validateResourceDraft(d ResourceConfig) error {
	if d.Replicas < 0 {
		return fmt.Errorf("replicas must be zero or greater")
	}
	if d.Autoscaling && (d.MinReplicas < 1 || d.MaxReplicas < d.MinReplicas || d.CPUTarget < 1) {
		return fmt.Errorf("autoscaling needs 1 ≤ min ≤ max and a positive CPU target")
	}
	return nil
}

func resourceEditUnit(field int) string {
	switch field {
	case 1:
		return "m"
	case 2:
		return "M"
	case 3:
		return " GiB"
	case 8:
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
	case 1:
		if err != nil || n < 1 {
			m.resourceMessage = "CPU request must be a positive number of millicores"
			return
		}
		m.resourceDraft.CPU = v + "m"
	case 2:
		if err != nil || n < 1 {
			m.resourceMessage = "RAM limit must be a positive number of megabytes"
			return
		}
		m.resourceDraft.RAM = v + "M"
	case 3, 4, 6, 7, 8:
		if err != nil || n < 0 || ((m.resourceCursor == 3 || m.resourceCursor == 8) && n == 0) {
			m.resourceMessage = "Enter a valid non-negative number"
			return
		}
		switch m.resourceCursor {
		case 3:
			if m.resourceOriginal.DiskGi == 0 {
				m.resourceMessage = "A disk cannot be added after app creation"
				return
			}
			m.resourceDraft.DiskGi = n
		case 4:
			m.resourceDraft.Replicas = n
		case 6:
			m.resourceDraft.MinReplicas = n
		case 7:
			m.resourceDraft.MaxReplicas = n
		case 8:
			m.resourceDraft.CPUTarget = n
		}
	}
	m.resourceEditing = false
	m.resourceMessage = ""
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
