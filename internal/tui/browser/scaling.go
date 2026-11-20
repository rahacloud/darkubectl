package browser

import (
	"fmt"
	"strconv"

	"github.com/rahacloud/darkubectl/internal/client"
)

func (m *browser) scalingPaneLines() []string {
	lines := []string{}
	if m.namespaceSelected || !m.selectedAppVisible() {
		return []string{summaryField("App", "not selected")}
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	lines = append(lines, sectionHeading("Scaling"))
	if m.detailState.err != nil {
		lines = append(lines, summaryField("Refresh", paint("failed · "+safeText(m.detailState.err.Error()), browserNumber, false)))
	} else if m.detailState.status == requestUnavailable {
		lines = append(lines, summaryField("Detail", "list snapshot"))
	}

	minReplicas, maxReplicas, cpuTarget, autoscaling := scalingValues(a)
	replicas := "-"
	if a.ReplicasPresent {
		replicas = strconv.Itoa(a.Replicas)
	}
	lines = append(lines,
		summaryField("Autoscaling", autoscaling),
		summaryField("Replicas", replicas),
		summaryField("Min replicas", minReplicas),
		summaryField("Max replicas", maxReplicas),
		summaryField("CPU target", cpuTarget))
	return lines
}

func scalingValues(a client.App) (string, string, string, string) {
	autoscaling := "Disabled"
	if a.IsHPAEnabled {
		autoscaling = labelEnabled
	}
	minReplicas, maxReplicas, cpuTarget := "-", "-", "-"
	if a.CustomConfig == nil || a.CustomConfig.HPA == nil {
		return minReplicas, maxReplicas, cpuTarget, autoscaling
	}
	hpa := a.CustomConfig.HPA
	if hpa.Enabled {
		autoscaling = "Enabled"
	}
	if hpa.MinReplicas > 0 {
		minReplicas = strconv.Itoa(hpa.MinReplicas)
	}
	if hpa.MaxReplicas > 0 {
		maxReplicas = strconv.Itoa(hpa.MaxReplicas)
	}
	if hpa.TargetCPUUtilizationPercentage > 0 {
		cpuTarget = fmt.Sprintf("%d%%", hpa.TargetCPUUtilizationPercentage)
	}
	return minReplicas, maxReplicas, cpuTarget, autoscaling
}
