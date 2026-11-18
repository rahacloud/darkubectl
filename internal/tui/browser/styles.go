package browser

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rahacloud/darkubectl/internal/output"
	"image/color"
	"os"
	"strings"
	"unicode"
)

func fallbackText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return safeText(value)
}

func (m *browser) busy() bool {
	return m.mode == "LIVE" && (m.profileState.active() || m.appsState.active())
}

func (m *browser) liveGlyph() string {
	if m.busy() {
		return paint(m.spinnerGlyph(), output.ColorBranch, true)
	}
	pulse := m.spinnerFrame % 8
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		if pulse < 2 || pulse >= 6 {
			return "○"
		}
		return "●"
	}
	style := lipgloss.NewStyle().Foreground(output.ColorBranch)
	if pulse < 2 || pulse >= 6 {
		style = style.Faint(true)
	}
	return style.Render("●")
}

func (m *browser) spinnerGlyph() string {
	frames := [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[m.spinnerFrame%len(frames)]
}

func (m *browser) loadingMessage(label string) string {
	return paint(m.spinnerGlyph(), output.ColorBranch, true) + " " + paint(label, output.ColorKey, false)
}

func tenantPanelStyle() lipgloss.Style {
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.BorderForeground(output.ColorBorder)
	} else {
		style = style.Border(lipgloss.NormalBorder())
	}
	return style
}

func selectedRowStyle() lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if _, disabled := os.LookupEnv("NO_COLOR"); !disabled {
		style = style.Foreground(output.ColorSelectionFG).Background(output.ColorSelectionBG)
	} else {
		style = style.Reverse(true)
	}
	return style
}

func safeText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(text))
}

func paint(text string, c color.Color, bold bool) string {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return text
	}
	style := lipgloss.NewStyle().Bold(bold)
	if c != nil {
		style = style.Foreground(c)
	}
	return style.Render(text)
}

func stateStyle(state string) (string, color.Color) {
	s := strings.ToLower(strings.TrimSpace(state))
	switch {
	case strings.Contains(s, "unhealthy"), strings.Contains(s, "not ready"), strings.Contains(s, "error"),
		strings.Contains(s, "fail"), strings.Contains(s, "crash"):
		return "●", output.ColorDanger
	case strings.Contains(s, "healthy"), strings.Contains(s, "running"), strings.Contains(s, "ready"):
		return "●", output.ColorSuccess
	case strings.Contains(s, "degraded"), strings.Contains(s, "warning"):
		return "▲", output.ColorNumber
	default:
		return "○", output.ColorMuted
	}
}
