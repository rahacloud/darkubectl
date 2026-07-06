package output

import (
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

// CellColor returns a foreground color for a table cell, or nil for the default.
type CellColor func(col int, value string) lipgloss.TerminalColor

// StyledTable renders a colorized, bordered table when w is a terminal, and a
// plain tab-separated table otherwise (pipe-safe). cellColor may be nil.
func StyledTable(w io.Writer, header []string, rows [][]string, cellColor CellColor) error {
	if !isTerminal(w) {
		return WriteTable(w, header, rows)
	}

	r := lipgloss.NewRenderer(w)
	headerStyle := r.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1)
	cellStyle := r.NewStyle().Padding(0, 1)

	t := table.New().
		Headers(header...).
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(r.NewStyle().Foreground(lipgloss.Color("240"))).
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

	_, err := io.WriteString(w, t.Render()+"\n")
	return err
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// Palette for status-aware coloring.
var (
	colorGreen = lipgloss.Color("42")
	colorRed   = lipgloss.Color("196")
	colorGray  = lipgloss.Color("244")
)

// StateColor maps a state string to a color: green for healthy, red for
// error/failed, gray for disabled/pending. Returns nil for anything else.
func StateColor(state string) lipgloss.TerminalColor {
	s := strings.ToLower(state)
	switch {
	case strings.Contains(s, "healthy"), strings.Contains(s, "running"), strings.Contains(s, "ready"):
		return colorGreen
	case strings.Contains(s, "error"), strings.Contains(s, "fail"), strings.Contains(s, "crash"):
		return colorRed
	case strings.Contains(s, "disabled"), strings.Contains(s, "pending"), strings.Contains(s, "stopped"):
		return colorGray
	default:
		return nil
	}
}

// BoolColor colors a "true"/"false" cell green/gray.
func BoolColor(value string) lipgloss.TerminalColor {
	switch strings.ToLower(value) {
	case "true":
		return colorGreen
	case "false":
		return colorGray
	default:
		return nil
	}
}

// StatusCells builds a CellColor that colors a state column (pass -1 to skip)
// and any number of boolean columns. Keeps callers free of lipgloss.
func StatusCells(stateCol int, boolCols ...int) CellColor {
	return func(col int, value string) lipgloss.TerminalColor {
		if col == stateCol {
			return StateColor(value)
		}
		if slices.Contains(boolCols, col) {
			return BoolColor(value)
		}
		return nil
	}
}
