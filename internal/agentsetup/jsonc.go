package agentsetup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

const jsonIndent = "  "

var errJSONLayout = errors.New("JSON config has a layout this command cannot edit")

// mergeJSONServer sets the darkubectl entry under the object at parent in a
// JSON or JSONC config. The file is edited as text, one value at a time: these
// files belong to the user and to the client itself (~/.claude.json is Claude
// Code's own state), so decoding and re-encoding the whole document would
// reorder every key, drop every comment and turn a one-entry change into a
// diff of the entire file. Keys of an existing entry that are not in entry are
// left alone.
func mergeJSONServer(before string, parent []string, entry map[string]any) (string, error) {
	path := append(slices.Clone(parent), serverName)
	if strings.TrimSpace(before) == "" {
		return renderJSON(nestJSON(path, entry), "") + "\n", nil
	}
	if err := checkJSONObjects(before, path); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(entry))
	for key := range entry {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	text := before
	for _, key := range keys {
		next, err := setJSONValue(text, append(slices.Clone(path), key), entry[key])
		if err != nil {
			return "", err
		}
		text = next
	}
	return text, nil
}

// checkJSONObjects parses the document and requires every existing value along
// path to be an object, so the text editor below only ever sees valid input.
func checkJSONObjects(text string, path []string) error {
	cleaned, err := stripJSONC(text)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(cleaned))
	var root any
	if err := decoder.Decode(&root); err != nil {
		return fmt.Errorf("parse JSON config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("parse JSON config: multiple top-level values")
		}
		return fmt.Errorf("parse JSON config: %w", err)
	}
	current, ok := root.(map[string]any)
	if !ok {
		return errors.New("JSON config root must be an object")
	}
	for _, key := range path {
		value, exists := current[key]
		if !exists {
			return nil
		}
		current, ok = value.(map[string]any)
		if !ok {
			return fmt.Errorf("JSON config key %q must be an object", key)
		}
	}
	return nil
}

type jsonMember struct {
	key                            string
	keyStart, valueStart, valueEnd int
}

type jsonObject struct {
	open, end     int // the braces; end is the index of '}'
	members       []jsonMember
	trailingComma bool
}

// setJSONValue sets the value at path, creating the objects on the way.
func setJSONValue(text string, path []string, value any) (string, error) {
	return setInJSONObject(text, skipJSONSpace(text, 0), path, value)
}

func setInJSONObject(text string, open int, path []string, value any) (string, error) {
	object, err := scanJSONObject(text, open)
	if err != nil {
		return "", err
	}
	for _, member := range object.members {
		if member.key != path[0] {
			continue
		}
		if len(path) > 1 {
			return setInJSONObject(text, member.valueStart, path[1:], value)
		}
		if sameJSON(text[member.valueStart:member.valueEnd], value) {
			return text, nil
		}
		rendered := renderJSON(value, lineIndent(text, member.keyStart))
		return text[:member.valueStart] + rendered + text[member.valueEnd:], nil
	}
	return insertJSONMember(text, object, path[0], nestJSON(path[1:], value)), nil
}

// scanJSONObject lists the members of the object whose '{' is at open.
func scanJSONObject(text string, open int) (jsonObject, error) {
	object := jsonObject{open: open}
	if open >= len(text) || text[open] != '{' {
		return object, errJSONLayout
	}
	i := skipJSONSpace(text, open+1)
	for i < len(text) && text[i] != '}' {
		if text[i] != '"' {
			return object, errJSONLayout
		}
		member := jsonMember{keyStart: i}
		keyEnd := skipJSONString(text, i)
		if err := json.Unmarshal([]byte(text[i:keyEnd]), &member.key); err != nil {
			return object, errJSONLayout
		}
		i = skipJSONSpace(text, keyEnd)
		if i >= len(text) || text[i] != ':' {
			return object, errJSONLayout
		}
		member.valueStart = skipJSONSpace(text, i+1)
		member.valueEnd = skipJSONValue(text, member.valueStart)
		if member.valueEnd == member.valueStart {
			return object, errJSONLayout
		}
		object.members = append(object.members, member)
		i = skipJSONSpace(text, member.valueEnd)
		object.trailingComma = i < len(text) && text[i] == ','
		if object.trailingComma {
			i = skipJSONSpace(text, i+1)
		}
	}
	if i >= len(text) {
		return object, errJSONLayout
	}
	object.end = i
	return object, nil
}

// insertJSONMember adds "key": value as the last member of object, on a line of
// its own, indented like its siblings.
func insertJSONMember(text string, object jsonObject, key string, value any) string {
	closeIndent := lineIndent(text, object.open)
	childIndent := closeIndent + jsonIndent
	if len(object.members) > 0 {
		first := object.members[0].keyStart
		if strings.Contains(text[object.open:first], "\n") {
			childIndent = lineIndent(text, first)
		}
	}
	member := "\n" + childIndent + renderJSON(key, "") + ": " + renderJSON(value, childIndent)

	// Insert before the line break that precedes the closing brace, so a
	// comment after the last member stays with that member.
	at := object.end
	for at > 0 && (text[at-1] == ' ' || text[at-1] == '\t') {
		at--
	}
	ownLine := at > 0 && text[at-1] == '\n'
	if ownLine {
		at--
		if at > 0 && text[at-1] == '\r' {
			at--
		}
	} else {
		at = object.end
		member += "\n" + closeIndent
	}
	if len(object.members) > 0 && !object.trailingComma {
		last := object.members[len(object.members)-1].valueEnd
		return text[:last] + "," + text[last:at] + member + text[at:]
	}
	return text[:at] + member + text[at:]
}

// nestJSON wraps value in one object per path element, outermost first.
func nestJSON(path []string, value any) any {
	for _, key := range slices.Backward(path) {
		value = map[string]any{key: value}
	}
	return value
}

// renderJSON encodes value for insertion at a line indented by indent: the
// first line carries no indent, the following ones do. Nested levels indent by
// a tab in a tab-indented file and by two spaces otherwise.
func renderJSON(value any, indent string) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	unit := jsonIndent
	if strings.HasPrefix(indent, "\t") {
		unit = "\t"
	}
	encoder.SetIndent(indent, unit)
	if err := encoder.Encode(value); err != nil {
		// Only strings, string slices and maps of them are ever passed in.
		panic("agentsetup: encode JSON value: " + err.Error())
	}
	return strings.TrimRight(buf.String(), "\n")
}

// sameJSON reports whether raw, a value as written in the file, already means
// value, so an entry the user formatted their own way is not rewritten.
func sameJSON(raw string, value any) bool {
	cleaned, err := stripJSONC(raw)
	if err != nil {
		return false
	}
	var existing any
	if err := json.Unmarshal([]byte(cleaned), &existing); err != nil {
		return false
	}
	have, errHave := json.Marshal(existing)
	want, errWant := json.Marshal(value)
	return errHave == nil && errWant == nil && bytes.Equal(have, want)
}

// lineIndent returns the leading whitespace of the line that holds pos.
func lineIndent(text string, pos int) string {
	start := strings.LastIndexByte(text[:pos], '\n') + 1
	end := start
	for end < pos && (text[end] == ' ' || text[end] == '\t') {
		end++
	}
	return text[start:end]
}

// skipJSONSpace returns the index of the first byte at or after i that is
// neither whitespace nor part of a comment.
func skipJSONSpace(text string, i int) int {
	for i < len(text) {
		switch {
		case text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r':
			i++
		case strings.HasPrefix(text[i:], "//"), strings.HasPrefix(text[i:], "/*"):
			i = skipJSONComment(text, i)
		default:
			return i
		}
	}
	return i
}

// skipJSONComment returns the index after the comment starting at i. A line
// comment ends before its newline; an unterminated block comment ends the text.
func skipJSONComment(text string, i int) int {
	if strings.HasPrefix(text[i:], "//") {
		if end := strings.IndexByte(text[i:], '\n'); end >= 0 {
			return i + end
		}
		return len(text)
	}
	if end := strings.Index(text[i+len("/*"):], "*/"); end >= 0 {
		return i + len("/*") + end + len("*/")
	}
	return len(text)
}

// skipJSONString returns the index after the string whose opening quote is at i.
func skipJSONString(text string, i int) int {
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return len(text)
}

// skipJSONValue returns the index after the value starting at i.
func skipJSONValue(text string, i int) int {
	if i >= len(text) {
		return i
	}
	switch text[i] {
	case '"':
		return skipJSONString(text, i)
	case '{', '[':
		return skipJSONContainer(text, i)
	}
	// A number or literal runs to the next separator, space or comment.
	for i < len(text) && !strings.ContainsRune(",}] \t\r\n/", rune(text[i])) {
		i++
	}
	return i
}

// skipJSONContainer returns the index after the object or array opening at i.
func skipJSONContainer(text string, i int) int {
	depth := 0
	for i < len(text) {
		switch ch := text[i]; {
		case ch == '"':
			i = skipJSONString(text, i)
		case strings.HasPrefix(text[i:], "//"), strings.HasPrefix(text[i:], "/*"):
			i = skipJSONComment(text, i)
		case ch == '{' || ch == '[':
			depth++
			i++
		case ch == '}' || ch == ']':
			depth--
			i++
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return i
}

// stripJSONC removes comments and trailing commas, leaving plain JSON with the
// same line structure.
func stripJSONC(text string) (string, error) {
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		switch {
		case text[i] == '"':
			end := skipJSONString(text, i)
			if end == len(text) && (end-i < len(`""`) || text[end-1] != '"') {
				return "", errors.New("unterminated JSON string")
			}
			b.WriteString(text[i:end])
			i = end
		case strings.HasPrefix(text[i:], "/*"):
			end := skipJSONComment(text, i)
			if !strings.HasSuffix(text[i:end], "*/") || end-i < len("/**/") {
				return "", errors.New("unterminated JSONC comment")
			}
			b.WriteString(strings.Repeat("\n", strings.Count(text[i:end], "\n")))
			i = end
		case strings.HasPrefix(text[i:], "//"):
			i = skipJSONComment(text, i)
		case text[i] == ',' && closesJSONContainer(text, i+1):
			i++
		default:
			b.WriteByte(text[i])
			i++
		}
	}
	return b.String(), nil
}

// closesJSONContainer reports whether the next token at or after i is a
// closing brace or bracket, which makes the comma before it a trailing one.
func closesJSONContainer(text string, i int) bool {
	i = skipJSONSpace(text, i)
	return i < len(text) && (text[i] == '}' || text[i] == ']')
}
