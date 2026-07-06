package output_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rahacloud/darkubectl/internal/output"
)

func TestFlattenObjectGroupsScalarsThenSections(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"name":      "sql-pad",
		"replicas":  float64(1),
		"enabled":   true,
		"namespace": map[string]any{"id": float64(5), "name": "prod"},
	}
	rows := output.FlattenObject(m)

	// Scalars come first, sorted: enabled, name, replicas.
	if got := rows[0]; got.Key != "enabled" || got.Value != "true" || got.Kind != output.KindBool {
		t.Errorf("row[0] = %+v, want enabled/true/bool", got)
	}
	if got := rows[2]; got.Key != "replicas" || got.Value != "1" || got.Kind != output.KindNumber {
		t.Errorf("row[2] = %+v, want replicas/1/number", got)
	}

	// Then a spacer and the namespace section header, then its sorted children.
	if !rows[3].Spacer {
		t.Errorf("row[3] = %+v, want a spacer", rows[3])
	}
	if got := rows[4]; !got.Header || got.Key != "namespace" {
		t.Errorf("row[4] = %+v, want namespace header", got)
	}
	if got := rows[5]; got.Depth != 1 || got.Key != "id" {
		t.Errorf("row[5] = %+v, want depth-1 id child", got)
	}
}

func TestFlattenObjectScalarKinds(t *testing.T) {
	t.Parallel()

	rows := output.FlattenObject(map[string]any{
		"s": "hi",
		"e": "",
		"n": nil,
		"m": map[string]any{},
		"a": []any{},
	})
	byKey := map[string]output.Row{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	cases := map[string]struct {
		value string
		kind  output.Kind
	}{
		"s": {"hi", output.KindString},
		"e": {`""`, output.KindString},
		"n": {"-", output.KindNull},
		"m": {"{}", output.KindNull},
		"a": {"[]", output.KindNull},
	}
	for key, want := range cases {
		if got := byKey[key]; got.Value != want.value || got.Kind != want.kind {
			t.Errorf("key %q = %q/%d, want %q/%d", key, got.Value, got.Kind, want.value, want.kind)
		}
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	if f, err := output.Parse(""); err != nil || f != output.Table {
		t.Errorf(`Parse("") = %q, %v; want table, nil`, f, err)
	}
	if f, err := output.Parse("json"); err != nil || f != output.JSON {
		t.Errorf(`Parse("json") = %q, %v; want json, nil`, f, err)
	}
	if _, err := output.Parse("xml"); !errors.Is(err, output.ErrUnsupportedFormat) {
		t.Errorf(`Parse("xml") err = %v; want ErrUnsupportedFormat`, err)
	}
}

func TestRenderDescribeIsPlainWhenNotATerminal(t *testing.T) {
	t.Parallel()

	rows := output.FlattenObject(map[string]any{
		"name":      "sql-pad",
		"namespace": map[string]any{"name": "prod"},
	})
	var buf bytes.Buffer
	if err := output.RenderDescribe(&buf, "app/sql-pad", rows); err != nil {
		t.Fatalf("RenderDescribe: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b[") {
		t.Errorf("output to a non-terminal must not contain ANSI escapes:\n%q", out)
	}
	for _, want := range []string{"app/sql-pad", "NAMESPACE", "sql-pad", "prod"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPlainRow(t *testing.T) {
	t.Parallel()

	if got := output.PlainRow(output.Row{Header: true, Key: "namespace"}); got != "NAMESPACE" {
		t.Errorf("header PlainRow = %q, want NAMESPACE", got)
	}
	got := output.PlainRow(output.Row{Depth: 1, Key: "id", Value: "5", Kind: output.KindNumber})
	if !strings.HasPrefix(got, "  id") || !strings.HasSuffix(got, "5") {
		t.Errorf("kv PlainRow = %q, want indented id ... 5", got)
	}
}
