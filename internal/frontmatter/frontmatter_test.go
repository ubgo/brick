package frontmatter

import (
	"errors"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		in       string
		body     string
		bodyLine int
		params   map[string]any
		hasFM    bool
		warn     bool
	}{
		{name: "no frontmatter", in: "<h1>Hi</h1>", body: "<h1>Hi</h1>", bodyLine: 1, params: map[string]any{}},
		{name: "simple", in: "---\ntitle: About\n---\n<h1>x</h1>", body: "<h1>x</h1>", bodyLine: 4, params: map[string]any{"title": "About"}, hasFM: true},
		{name: "crlf", in: "---\r\ntitle: A\r\n---\r\nbody", body: "body", bodyLine: 4, params: map[string]any{"title": "A"}, hasFM: true},
		{name: "bom", in: "\uFEFF---\ntitle: A\n---\nbody", body: "body", bodyLine: 4, params: map[string]any{"title": "A"}, hasFM: true},
		{name: "trailing spaces on delimiter", in: "---  \nn: 1\n--- \nbody", body: "body", bodyLine: 4, params: map[string]any{"n": int64(1)}, hasFM: true},
		{name: "frontmatter only", in: "---\nn: 1\n---", body: "", bodyLine: 4, params: map[string]any{"n": int64(1)}, hasFM: true},
		{name: "empty frontmatter", in: "---\n---\nbody", body: "body", bodyLine: 3, params: map[string]any{}, hasFM: true},
		{name: "only one newline consumed", in: "---\na: b\n---\n\nbody", body: "\nbody", bodyLine: 4, params: map[string]any{"a": "b"}, hasFM: true},
		{name: "blank line before delimiter warns", in: "\n---\na: b\n---\nx", body: "\n---\na: b\n---\nx", bodyLine: 1, params: map[string]any{}, warn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, err := Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if string(d.Body) != tc.body || d.BodyLine != tc.bodyLine || d.HasFrontmatter != tc.hasFM {
				t.Fatalf("body=%q line=%d fm=%v; want %q %d %v", d.Body, d.BodyLine, d.HasFrontmatter, tc.body, tc.bodyLine, tc.hasFM)
			}
			if len(d.Params) != len(tc.params) {
				t.Fatalf("params = %v, want %v", d.Params, tc.params)
			}
			for k, v := range tc.params {
				if d.Params[k] != v {
					t.Fatalf("params[%s] = %#v, want %#v", k, d.Params[k], v)
				}
			}
			if (len(d.Warnings) > 0) != tc.warn {
				t.Fatalf("warnings = %v, want warning=%v", d.Warnings, tc.warn)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   string
		want error
		line int
	}{
		{"unclosed", "---\na: b\nbody", ErrUnclosed, 1},
		{"invalid utf8", "ok\n\xff", ErrInvalidUTF8, 2},
		{"not a map", "---\n- a\n- b\n---\n", ErrNotMapping, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tc.in))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var pe *PosError
			if !errors.As(err, &pe) || pe.Line != tc.line {
				t.Fatalf("line = %+v, want %d", pe, tc.line)
			}
		})
	}
	// Duplicate keys are an error, reported on the file's line.
	_, err := Parse([]byte("---\na: 1\na: 2\n---\n"))
	var pe *PosError
	if err == nil || !errors.As(err, &pe) || pe.Line < 2 {
		t.Fatalf("duplicate key: %v", err)
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	d, err := Parse([]byte("---\nn: 5\nf: 1.5\nd: 2026-09-12\nnested:\n  1: one\nlist: [1, a]\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Params["n"] != int64(5) || d.Params["f"] != 1.5 {
		t.Fatalf("numbers: %#v %#v", d.Params["n"], d.Params["f"])
	}
	if _, ok := d.Params["d"].(time.Time); !ok {
		t.Fatalf("date is %T, want time.Time", d.Params["d"])
	}
	nested, ok := d.Params["nested"].(map[string]any)
	if !ok || nested["1"] != "one" {
		t.Fatalf("nested: %#v", d.Params["nested"])
	}
	list, ok := d.Params["list"].([]any)
	if !ok || list[0] != int64(1) || list[1] != "a" {
		t.Fatalf("list: %#v", d.Params["list"])
	}
}
