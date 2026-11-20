package browser

import "time"

// formatLocalTime keeps timestamps in the user's configured local timezone for
// the interactive TUI.
func formatLocalWallTime(value time.Time,
	layout string) string {
	return value.Local().Format(layout) //nolint:gosmopolitan // The browser intentionally displays local wall-clock time.
}
