package output

import "charm.land/lipgloss/v2"

// Palette used across the table, describe, and interactive-viewer renderers.
// Colors are chosen to read clearly on both light and dark terminal
// backgrounds (mid-range luminance, avoiding near-white/near-black) rather
// than adapting live to the detected background, since querying the
// terminal for its background color adds latency and can hang on some
// terminals/multiplexers.
var (
	ColorAccent  = lipgloss.Color("#D6336C") // titles, section headers
	ColorBranch  = lipgloss.Color("#228BE6") // nested object/array keys
	ColorKey     = lipgloss.Color("#868E96") // field labels
	ColorBorder  = lipgloss.Color("#5C5F77") // table borders
	ColorSuccess = lipgloss.Color("#40C057") // healthy/true
	ColorDanger  = lipgloss.Color("#E03131") // error/failed
	ColorMuted   = lipgloss.Color("#868E96") // disabled/pending/false
	ColorNumber  = lipgloss.Color("#F08C00") // numeric values

	// The four colors below set both foreground and background for search
	// highlights, so they stay readable regardless of the terminal's own
	// background.

	ColorMatchBG   = lipgloss.Color("#FAB005") // search match background
	ColorMatchFG   = lipgloss.Color("#000000") // search match foreground
	ColorCurrentBG = lipgloss.Color("#E64980") // active match background
	ColorCurrentFG = lipgloss.Color("#FFFFFF") // active match foreground
)
