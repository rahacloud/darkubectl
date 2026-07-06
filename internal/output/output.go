// Package output renders API results as kubectl-style tables or as JSON/YAML.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// ErrUnsupportedFormat is returned by Parse for an unknown output format.
var ErrUnsupportedFormat = errors.New("unsupported output format")

// Table-writer formatting parameters and structured-output indentation.
const (
	tableMinWidth = 0
	tableTabWidth = 4
	tablePadding  = 3
	yamlIndent    = 2
)

// Format is a supported -o/--output value.
type Format string

// Supported output formats.
const (
	Table Format = "table"
	Wide  Format = "wide"
	JSON  Format = "json"
	YAML  Format = "yaml"
	Name  Format = "name"
)

// Parse validates a user-supplied output value.
func Parse(s string) (Format, error) {
	switch Format(s) {
	case Table, Wide, JSON, YAML, Name:
		return Format(s), nil
	case "":
		return Table, nil
	default:
		return "", fmt.Errorf("%w %q (want: table, wide, json, yaml, name)", ErrUnsupportedFormat, s)
	}
}

// Structured writes v as JSON or YAML. Returns (true, err) if it handled the
// format, or (false, nil) if the caller should render a table itself.
func Structured(w io.Writer, f Format, v any) (bool, error) {
	switch f {
	case JSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return true, enc.Encode(v)
	case YAML:
		enc := yaml.NewEncoder(w)
		enc.SetIndent(yamlIndent)
		if err := enc.Encode(v); err != nil {
			return true, err
		}
		return true, enc.Close()
	case Table, Wide, Name:
		return false, nil
	default:
		return false, nil
	}
}

// WriteTable renders rows with a tab-separated header, kubectl style.
func WriteTable(w io.Writer, header []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, tableMinWidth, tableTabWidth, tablePadding, ' ', 0)
	if _, err := fmt.Fprintln(tw, strings.Join(header, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}
