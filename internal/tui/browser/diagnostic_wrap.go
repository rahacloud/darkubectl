package browser

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	diagnosticMinimumWrapWidth     = 12
	diagnosticContinuationIndent   = 2
	diagnosticEscapeSequenceOffset = 2
)

func wrapDiagnosticLines(lines []string, width int) []string {
	if width < diagnosticMinimumWrapWidth {
		return lines
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		if !isDiagnosticLine(ansi.Strip(line)) || ansi.StringWidth(line) <= width {
			wrapped = append(wrapped, line)
			continue
		}
		parts := strings.Split(ansi.Wrap(line, width-diagnosticContinuationIndent, ""), "\n")
		wrapped = append(wrapped, parts[0])
		style := diagnosticStyle(line)
		for _, part := range parts[1:] {
			continuation := "  " + ansi.Strip(part)
			if style != "" {
				continuation = style + continuation + "\x1b[0m"
			}
			wrapped = append(wrapped, continuation)
		}
	}
	return wrapped
}

func isDiagnosticLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return diagnosticMarker(lower) != ""
}

func diagnosticMarker(lower string) string {
	for _, marker := range []string{
		"could not load", "refresh failed", "save failed", "copy failed", "request failed",
		"failed ·", "failed:", "stale ·", statusWarning, "error:", "error ", "unavailable",
		"access denied", "forbidden", "timed out", "timeout", "rejected", "unable to",
	} {
		if strings.Contains(lower, marker) {
			return marker
		}
	}
	return ""
}

func diagnosticStyle(line string) string {
	plain := ansi.Strip(line)
	marker := diagnosticMarker(strings.ToLower(plain))
	if marker == "" {
		return ""
	}
	target := strings.Index(strings.ToLower(plain), marker)
	visibleBytes := 0
	style := ""
	for i := 0; i < len(line); {
		sequence, end, ok := ansiSequenceAt(line, i)
		if ok {
			if strings.HasSuffix(sequence, "m") {
				if sgrReset(sequence) {
					style = ""
				} else {
					style += sequence
				}
			}
			i = end
			continue
		}
		if visibleBytes >= target {
			break
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		visibleBytes += size
		i += size
	}
	return style
}

func ansiSequenceAt(line string, start int) (string, int, bool) {
	if start+1 >= len(line) || line[start] != '\x1b' || line[start+1] != '[' {
		return "", start, false
	}
	end := start + diagnosticEscapeSequenceOffset
	for end < len(line) && (line[end] < '@' || line[end] > '~') {
		end++
	}
	if end >= len(line) {
		return "", start, false
	}
	return line[start : end+1], end + 1, true
}

func sgrReset(sequence string) bool {
	parameters := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m")
	first, _, _ := strings.Cut(parameters, ";")
	return first == "" || first == "0"
}
