package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
)

// maxDiffValue caps how much of a changed value is printed. Some app fields hold
// base64 blobs measured in kilobytes; a diff that scrolls them off the screen is
// worse than one that says how big they are.
const maxDiffValue = 120

// fieldChange is one top-level field that a pending update would alter.
type fieldChange struct {
	Key    string `json:"key"    yaml:"key"`
	Before string `json:"before" yaml:"before"`
	After  string `json:"after"  yaml:"after"`
}

// diffApps compares two normalized app objects and returns the top-level fields
// that differ, sorted by key.
//
// Top-level is the right granularity: the write is a whole-object PUT, the
// fields callers change are top-level, and descending into nested structures
// would bury the answer to "what is this command about to do?".
func diffApps(before, after map[string]any) []fieldChange {
	keys := map[string]bool{}
	for k := range maps.Keys(before) {
		keys[k] = true
	}
	for k := range maps.Keys(after) {
		keys[k] = true
	}

	var out []fieldChange
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		b, a := renderValue(before[k]), renderValue(after[k])
		if b == a {
			continue
		}
		out = append(out, fieldChange{Key: k, Before: b, After: a})
	}
	return out
}

// renderValue renders a JSON value compactly and deterministically, so that two
// equal values always compare equal as strings.
func renderValue(v any) string {
	if v == nil {
		return "null"
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// truncate shortens a value for display, reporting the full length so a large
// blob is still identifiable.
func truncate(s string) string {
	if len(s) <= maxDiffValue {
		return s
	}
	return fmt.Sprintf("%s… (%d bytes)", s[:maxDiffValue], len(s))
}

// writeDiff prints a field diff in the -/+ style, or says nothing would change.
func writeDiff(w io.Writer, changes []fieldChange) {
	if len(changes) == 0 {
		fmt.Fprintln(w, "no changes: the app already matches")
		return
	}
	for _, c := range changes {
		fmt.Fprintf(w, "  %s:\n", c.Key)
		if lines, ok := namedListDiff(c.Before, c.After); ok {
			for _, l := range lines {
				fmt.Fprintf(w, "    %s\n", l)
			}
			continue
		}
		fmt.Fprintf(w, "    - %s\n", truncate(c.Before))
		fmt.Fprintf(w, "    + %s\n", truncate(c.After))
	}
}

// namedItem is an entry of a {name, value} list, which is how envs are stored.
type namedItem struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// namedListDiff diffs two {name, value} lists entry by entry. Shown whole, an
// app's envs run past the truncation limit and hide the one variable that
// changed; per entry, the diff names it. ok is false when either side is not
// such a list, and the caller prints the whole values instead.
func namedListDiff(before, after string) ([]string, bool) {
	parse := func(s string) ([]namedItem, bool) {
		if s == "null" {
			return nil, true
		}
		var items []namedItem
		if json.Unmarshal([]byte(s), &items) != nil {
			return nil, false
		}
		for _, it := range items {
			if it.Name == "" {
				return nil, false
			}
		}
		return items, true
	}
	b, okB := parse(before)
	a, okA := parse(after)
	if !okB || !okA || (len(a) == 0 && len(b) == 0) {
		return nil, false
	}

	old := make(map[string]string, len(b))
	for _, it := range b {
		old[it.Name] = renderValue(it.Value)
	}
	var lines []string
	for _, it := range a {
		v := renderValue(it.Value)
		prev, had := old[it.Name]
		switch {
		case !had:
			lines = append(lines, "+ "+it.Name+"="+truncate(v))
		case prev != v:
			lines = append(lines, "~ "+it.Name+": "+truncate(prev)+" -> "+truncate(v))
		}
		delete(old, it.Name)
	}
	for _, it := range b {
		if _, gone := old[it.Name]; gone {
			lines = append(lines, "- "+it.Name)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "(reordered)")
	}
	return lines, true
}
