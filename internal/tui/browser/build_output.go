package browser

import (
	"strings"
	"unicode"
)

// formatBuildOutput preserves source order and maps each source line to its first visible row.
func formatBuildOutput(output string, width int, raw bool) ([]string, []int) {
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	offsets := make([]int, 0, len(lines))
	previous := ""
	phase := false
	for _, line := range lines {
		if !raw && strings.HasPrefix(line, "--- ") {
			result = buildOutputBreak(result)
			offsets = append(offsets, len(result))
			result = appendBuildHeading(result, "◆ "+strings.TrimPrefix(line, "--- "), width)
			previous, phase = "", true
			continue
		}
		step, text := buildStepLine(line)
		if raw || step == "" {
			offsets = append(offsets, len(result))
			prefix := ""
			if !raw && phase && line != "" && width > 2 {
				prefix = "  "
			}
			for _, row := range wrapBuildOutput(line, max(1, width-len(prefix))) {
				result = append(result, prefix+row)
			}
			if strings.TrimSpace(line) != "" {
				previous = ""
			}
			continue
		}
		if step != previous {
			result = buildOutputBreak(result)
			offsets = append(offsets, len(result))
			result = appendBuildHeading(result, step+" "+text, width)
			previous = step
			continue
		}
		offsets = append(offsets, len(result))
		prefix := "    "
		if width <= len(prefix) {
			prefix = ""
		}
		for _, row := range wrapBuildOutput(text, max(1, width-len(prefix))) {
			switch {
			case strings.HasPrefix(text, "DONE ") || text == "DONE":
				row = paint(row, browserSuccess, false)
			case strings.HasPrefix(text, "ERROR") || strings.HasPrefix(text, "FAILED"):
				row = paint(row, browserDanger, false)
			}
			result = append(result, prefix+row)
		}
	}
	return result, offsets
}

func buildOutputBreak(lines []string) []string {
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		return append(lines, "")
	}
	return lines
}

func appendBuildHeading(lines []string, text string, width int) []string {
	for _, row := range wrapBuildOutput(text, width) {
		lines = append(lines, paint(row, browserAccent, true))
	}
	return lines
}

func buildStepLine(line string) (string, string) {
	if len(line) < 3 || line[0] != '#' {
		return "", line
	}
	i := 1
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 1 || i == len(line) || !unicode.IsSpace(rune(line[i])) {
		return "", line
	}
	return line[:i], strings.TrimLeft(line[i:], " \t")
}
