package browser

import (
	"fmt"

	"github.com/rahacloud/darkubectl/internal/output"
)

func (m *browser) scalingPaneLines() []string {
	lines := []string{m.appViewTabs()}
	if m.namespaceSelected || !m.selectedAppVisible() {
		return append(lines, paint("No app selected. Move to an app to view its scaling settings.", output.ColorMuted, false))
	}
	a := m.apps[m.selected]
	if m.detailApp != nil && m.detailApp.ID == a.ID {
		a = *m.detailApp
	}
	lines = append(lines, sectionHeading("Scaling"))
	if m.detailState.err != nil {
		lines = append(lines, paint("Detail refresh failed: "+safeText(m.detailState.err.Error()), output.ColorNumber, false))
	} else if m.detailState.status == requestUnavailable {
		lines = append(lines, paint("Scaling settings are unavailable; showing the list snapshot.", output.ColorMuted, false))
	}

	autoscaling := "Disabled"
	if a.IsHPAEnabled {
		autoscaling = "Enabled"
	}
	minReplicas, maxReplicas, cpuTarget := "-", "-", "-"
	if a.CustomConfig != nil && a.CustomConfig.HPA != nil {
		hpa := a.CustomConfig.HPA
		if hpa.Enabled {
			autoscaling = "Enabled"
		} else if a.IsHPAEnabled {
			autoscaling = "Enabled"
		}
		if hpa.MinReplicas > 0 {
			minReplicas = fmt.Sprintf("%d", hpa.MinReplicas)
		}
		if hpa.MaxReplicas > 0 {
			maxReplicas = fmt.Sprintf("%d", hpa.MaxReplicas)
		}
		if hpa.TargetCPUUtilizationPercentage > 0 {
			cpuTarget = fmt.Sprintf("%d%%", hpa.TargetCPUUtilizationPercentage)
		}
	}
	replicas := "-"
	if a.ReplicasPresent {
		replicas = fmt.Sprintf("%d", a.Replicas)
	}
	lines = append(lines,
		summaryField("Autoscaling", autoscaling),
		summaryField("Replicas", replicas),
		summaryField("Min replicas", minReplicas),
		summaryField("Max replicas", maxReplicas),
		summaryField("CPU target", cpuTarget))
	return lines
}
