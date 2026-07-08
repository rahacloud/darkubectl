package output

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Kind classifies a described value for type-aware coloring.
type Kind int

// Value kinds surfaced by the describe flattener.
const (
	KindBranch Kind = iota // a nested object/array header (no scalar value)
	KindString
	KindNumber
	KindBool
	KindNull
)

// keyColumnWidth is the padded width of a key before its value.
const keyColumnWidth = 24

// Row is one flattened line of a described object.
type Row struct {
	Depth  int
	Key    string
	Value  string
	Kind   Kind
	Header bool // top-level section header (an object/array), rendered uppercase
	Spacer bool // a blank separator line
}

// FlattenObject turns a decoded JSON object into an ordered, depth-tagged list
// of rows: top-level scalars first (a summary block), then each nested object or
// array as its own section. Suitable for both the static renderer and the viewer.
func FlattenObject(m map[string]any) []Row {
	var scalarKeys, branchKeys []string
	for k, v := range m {
		if isBranch(v) {
			branchKeys = append(branchKeys, k)
		} else {
			scalarKeys = append(scalarKeys, k)
		}
	}
	sort.Strings(scalarKeys)
	sort.Strings(branchKeys)

	rows := make([]Row, 0, len(scalarKeys)+2*len(branchKeys))
	for _, k := range scalarKeys {
		value, kind := formatScalar(m[k])
		rows = append(rows, Row{Key: k, Value: value, Kind: kind})
	}
	for _, k := range branchKeys {
		rows = append(rows, Row{Spacer: true})
		rows = append(rows, Row{Key: k, Kind: KindBranch, Header: true})
		flattenChildren(&rows, 1, m[k])
	}
	return rows
}

func isBranch(v any) bool {
	switch val := v.(type) {
	case map[string]any:
		return len(val) > 0
	case []any:
		return len(val) > 0
	default:
		return false
	}
}

func flattenChildren(rows *[]Row, depth int, v any) {
	switch val := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(val) {
			flattenValue(rows, depth, k, val[k])
		}
	case []any:
		for i, item := range val {
			flattenValue(rows, depth, "["+strconv.Itoa(i)+"]", item)
		}
	}
}

func flattenValue(rows *[]Row, depth int, key string, v any) {
	switch val := v.(type) {
	case map[string]any:
		if len(val) == 0 {
			*rows = append(*rows, Row{Depth: depth, Key: key, Value: "{}", Kind: KindNull})
			return
		}
		*rows = append(*rows, Row{Depth: depth, Key: key, Kind: KindBranch})
		flattenChildren(rows, depth+1, val)
	case []any:
		if len(val) == 0 {
			*rows = append(*rows, Row{Depth: depth, Key: key, Value: "[]", Kind: KindNull})
			return
		}
		*rows = append(*rows, Row{Depth: depth, Key: key, Kind: KindBranch})
		flattenChildren(rows, depth+1, val)
	default:
		value, kind := formatScalar(val)
		*rows = append(*rows, Row{Depth: depth, Key: key, Value: value, Kind: kind})
	}
}

func formatScalar(v any) (string, Kind) {
	switch val := v.(type) {
	case nil:
		return "-", KindNull
	case bool:
		return strconv.FormatBool(val), KindBool
	case float64:
		// JSON numbers decode to float64; print integers without a decimal point.
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10), KindNumber
		}
		return strconv.FormatFloat(val, 'f', -1, 64), KindNumber
	case string:
		if val == "" {
			return `""`, KindString
		}
		return val, KindString
	case map[string]any:
		return "{}", KindNull // only reached for empty maps
	case []any:
		return "[]", KindNull // only reached for empty slices
	default:
		return fmt.Sprintf("%v", val), KindString
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DescribeStyles holds the lipgloss styles for a describe rendering.
type DescribeStyles struct {
	title   lipgloss.Style
	section lipgloss.Style
	key     lipgloss.Style
	branch  lipgloss.Style
	str     lipgloss.Style
	num     lipgloss.Style
	boolean lipgloss.Style
	null    lipgloss.Style
}

func newDescribeStyles() DescribeStyles {
	return DescribeStyles{
		title:   lipgloss.NewStyle().Bold(true).Foreground(ColorAccent),
		section: lipgloss.NewStyle().Bold(true).Foreground(ColorAccent),
		key:     lipgloss.NewStyle().Foreground(ColorKey),
		branch:  lipgloss.NewStyle().Bold(true).Foreground(ColorBranch),
		str:     lipgloss.NewStyle(), // inherit the terminal's default foreground
		num:     lipgloss.NewStyle().Foreground(ColorNumber),
		boolean: lipgloss.NewStyle().Foreground(ColorSuccess),
		null:    lipgloss.NewStyle().Foreground(ColorMuted).Faint(true),
	}
}

func (s DescribeStyles) value(kind Kind, text string) string {
	switch kind {
	case KindNumber:
		return s.num.Render(text)
	case KindBool:
		return s.boolean.Render(text)
	case KindNull:
		return s.null.Render(text)
	case KindString, KindBranch:
		return s.str.Render(text)
	default:
		return s.str.Render(text)
	}
}

// RenderRow formats a single row (without a trailing newline) using the styles.
// It is shared by the static renderer and the interactive viewer.
func RenderRow(s DescribeStyles, row Row) string {
	if row.Spacer {
		return ""
	}
	indent := strings.Repeat("  ", row.Depth)
	switch {
	case row.Header:
		return s.section.Render(strings.ToUpper(row.Key))
	case row.Kind == KindBranch:
		return indent + s.branch.Render(row.Key)
	default:
		return indent + s.key.Render(padRight(row.Key, keyColumnWidth-len(indent))) + s.value(row.Kind, row.Value)
	}
}

// PlainRow renders a row without any styling, used by the interactive viewer to
// index lines for case-insensitive search.
func PlainRow(row Row) string {
	if row.Spacer {
		return ""
	}
	indent := strings.Repeat("  ", row.Depth)
	switch {
	case row.Header:
		return strings.ToUpper(row.Key)
	case row.Kind == KindBranch:
		return indent + row.Key
	default:
		return indent + padRight(row.Key, keyColumnWidth-len(indent)) + row.Value
	}
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-len(s))
}

// RenderDescribe writes a colorized static view of rows to w. Color is
// downsampled (or stripped entirely) to match w's actual color profile, so
// pipes, redirected files, and NO_COLOR all fall back to plain text.
func RenderDescribe(w io.Writer, title string, rows []Row) error {
	s := newDescribeStyles()

	var b strings.Builder
	b.WriteString(s.title.Render(title))
	b.WriteString("\n\n")
	for _, row := range rows {
		b.WriteString(RenderRow(s, row))
		b.WriteString("\n")
	}
	cw := colorprofile.NewWriter(w, os.Environ())
	_, err := io.WriteString(cw, b.String())
	return err
}

// NewStyles exposes describe styles for the interactive viewer, which always
// targets a real terminal.
func NewStyles() DescribeStyles { return newDescribeStyles() }
