package output

import (
	"image/color"
	"io"
	"os"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"
)

// CellColor returns a foreground color for a table cell, or nil for the default.
type CellColor func(col int, value string) color.Color

// StyledTable renders a colorized, bordered table when w is a terminal, and a
// plain tab-separated table otherwise (pipe-safe). cellColor may be nil.
func StyledTable(w io.Writer, header []string, rows [][]string, cellColor CellColor) error {
	if !isTerminal(w) {
		return WriteTable(w, header, rows)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	t := table.New().
		Headers(header...).
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ColorBorder)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if cellColor != nil && row >= 0 && row < len(rows) && col < len(rows[row]) {
				if c := cellColor(col, rows[row][col]); c != nil {
					return cellStyle.Foreground(c)
				}
			}
			return cellStyle
		})

	// Downsamples to the destination's actual color profile, so NO_COLOR and
	// low-color terminals still get the (uncolored) box-drawn table.
	cw := colorprofile.NewWriter(w, os.Environ())
	_, err := io.WriteString(cw, t.Render()+"\n")
	return err
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// IsTerminal reports whether w is a terminal, so callers can decide between
// a styled, grouped presentation and a flat, pipe-safe one.
func IsTerminal(w io.Writer) bool { return isTerminal(w) }

// PrintSectionHeader writes a bold, accent-colored label (e.g. a namespace
// grouping the table printed below it), downsampled to w's color profile.
func PrintSectionHeader(w io.Writer, text string) error {
	style := lipgloss.NewStyle().Bold(true).Foreground(ColorBranch)
	cw := colorprofile.NewWriter(w, os.Environ())
	_, err := io.WriteString(cw, style.Render(text)+"\n")
	return err
}

// StateColor maps a state string to a color: green for healthy, red for
// error/failed, gray for disabled/pending. Returns nil for anything else.
func StateColor(state string) color.Color {
	s := strings.ToLower(state)
	switch {
	case strings.Contains(s, "healthy"), strings.Contains(s, "running"), strings.Contains(s, "ready"):
		return ColorSuccess
	case strings.Contains(s, "error"), strings.Contains(s, "fail"), strings.Contains(s, "crash"):
		return ColorDanger
	case strings.Contains(s, "disabled"), strings.Contains(s, "pending"), strings.Contains(s, "stopped"):
		return ColorMuted
	default:
		return nil
	}
}

// BoolColor colors a "true"/"false" cell green/gray.
func BoolColor(value string) color.Color {
	switch strings.ToLower(value) {
	case "true":
		return ColorSuccess
	case "false":
		return ColorMuted
	default:
		return nil
	}
}

// StatusCells builds a CellColor that colors a state column (pass -1 to skip)
// and any number of boolean columns. Keeps callers free of lipgloss.
func StatusCells(stateCol int, boolCols ...int) CellColor {
	return func(col int, value string) color.Color {
		if col == stateCol {
			return StateColor(value)
		}
		if slices.Contains(boolCols, col) {
			return BoolColor(value)
		}
		return nil
	}
}
