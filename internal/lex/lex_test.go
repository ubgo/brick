package lex

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// kinds renders tokens compactly: T(text) A(action) O(name) C(name).
func kinds(toks []Token) string {
	var parts []string
	for _, t := range toks {
		switch t.Kind {
		case Text:
			parts = append(parts, "T")
		case Action:
			parts = append(parts, "A")
		case Open:
			if t.SelfClosing {
				parts = append(parts, "O/"+t.Name)
			} else {
				parts = append(parts, "O:"+t.Name)
			}
		case Close:
			parts = append(parts, "C:"+t.Name)
		}
	}
	return strings.Join(parts, " ")
}

func rawIs(t *testing.T, src string, toks []Token) {
	t.Helper()
	var b bytes.Buffer
	for _, tok := range toks {
		b.Write(tok.Raw)
	}
	if b.String() != src {
		t.Fatalf("round trip lost bytes:\n got  %q\n want %q", b.String(), src)
	}
}

func TestScanShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"plain html", `<div class="a">hi</div>`, "T"},
		{"component", `<Card title="Hi">body</Card>`, "O:Card T C:Card"},
		{"self closing", `<Card />`, "O/Card"},
		{"hyphen form", `<x-card>b</x-card>`, "O:x-card T C:x-card"},
		{"engine tag", `<brick:prop name="h">x</brick:prop>`, "O:brick:prop T C:brick:prop"},
		{"action", `<p>{{ .Title }}</p>`, "T A T"},
		{"range around component", `{{ range .P }}<Card :p="." />{{ end }}`, "A O/Card A"},
		{"component in string literal", `{{ "<Card/>" }}`, "A"},
		{"component in template comment", `{{/* <Card> */}}`, "A"},
		{"closing braces in string", `{{ "}}" }}x`, "A T"},
		{"component in html comment", `<!-- <Card>old</Card> -->`, "T"},
		{"component in script", `<script>let x = "<Card>";</script>`, "T"},
		{"action in script", `<script>let x = {{ .X }};</script>`, "T A T"},
		{"component in quoted attr", `<div data-x='<Card>'>y</div>`, "T"},
		{"action in plain attr", `<div class="{{ .C }}">y</div>`, "T A T"},
		{"lowercase is html", `<card>x</card>`, "T"},
		{"uppercase html is a component tag", `<DIV>x</DIV>`, "O:DIV T C:DIV"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			toks, err := Scan([]byte(tc.src), Options{})
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if got := kinds(toks); got != tc.want {
				t.Fatalf("kinds = %q, want %q", got, tc.want)
			}
			rawIs(t, tc.src, toks)
		})
	}
}

func TestActionClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src   string
		act   ActKind
		decls string
		refs  string
	}{
		{`{{ if .X }}`, ActIf, "", ""},
		{`{{- range $i, $p := .Posts -}}`, ActRange, "i,p", "i,p"},
		{`{{ else if .Y }}`, ActElse, "", ""},
		{`{{ end }}`, ActEnd, "", ""},
		{`{{ $x := 1 }}`, ActOther, "x", "x"},
		{`{{ $x = 2 }}`, ActOther, "", "x"},
		{`{{ $.Params.a }}{{ $p.Title }}`, ActOther, "", ""},
		{`{{/* note */}}`, ActComment, "", ""},
		{`{{ break }}`, ActBreak, "", ""},
		{`{{ "$notvar" }}`, ActOther, "", ""},
	} {
		toks, err := Scan([]byte(tc.src), Options{})
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		a := toks[0]
		if a.Act != tc.act || strings.Join(a.Decls, ",") != tc.decls {
			t.Fatalf("%s: act=%v decls=%v", tc.src, a.Act, a.Decls)
		}
		if tc.refs != "" && strings.Join(a.Refs, ",") != tc.refs {
			t.Fatalf("%s: refs=%v", tc.src, a.Refs)
		}
	}
	// $ refs of the second action are counted separately.
	toks, _ := Scan([]byte(`{{ $.A }}{{ $p.T }}`), Options{})
	if strings.Join(toks[0].Refs, ",") != "" || strings.Join(toks[1].Refs, ",") != "p" {
		t.Fatalf("refs = %v %v", toks[0].Refs, toks[1].Refs)
	}
}

func TestIdents(t *testing.T) {
	t.Parallel()
	toks, err := Scan([]byte(`{{ if hasNode "f" }}{{ node "x" . | upper }}{{ helpers.slugify .Title }}{{ .children }}`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tok := range toks {
		for _, id := range tok.Idents {
			names = append(names, id.Name)
			if got := string(tok.Raw[id.Start:id.End]); got != id.Name {
				t.Fatalf("ident offsets wrong: %q vs %q", got, id.Name)
			}
		}
	}
	if strings.Join(names, ",") != "hasNode,node,upper,helpers.slugify" {
		t.Fatalf("idents = %v", names)
	}
}

func TestAttributes(t *testing.T) {
	t.Parallel()
	toks, err := Scan([]byte(`<Card title="Hi {{ .N }}" :count="5" open x-bind:class='a' :f="eq .S `+"`blog`"+`" data-id=7 />`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := toks[0].Attrs
	if len(a) != 6 {
		t.Fatalf("attrs = %+v", a)
	}
	if a[0].Name != "title" || a[0].Value != "Hi {{ .N }}" || a[0].Quote != '"' {
		t.Fatalf("attr0 = %+v", a[0])
	}
	if a[2].Name != "open" || a[2].HasValue {
		t.Fatalf("bare attr = %+v", a[2])
	}
	if a[3].Name != "x-bind:class" || a[3].Quote != '\'' {
		t.Fatalf("attr3 = %+v", a[3])
	}
	if a[4].Value != "eq .S `blog`" {
		t.Fatalf("backtick expr = %q", a[4].Value)
	}
	if a[5].Value != "7" || a[5].Quote != 0 {
		t.Fatalf("unquoted = %+v", a[5])
	}
}

func TestRawBody(t *testing.T) {
	t.Parallel()
	src := "<CodeBlock lang=\"go\">if x < 10 { {{ not }} }</CodeBlock>after"
	toks, err := Scan([]byte(src), Options{IsRaw: func(name string, _ []Attr) bool { return name == "CodeBlock" }})
	if err != nil {
		t.Fatal(err)
	}
	if kinds(toks) != "O:CodeBlock T C:CodeBlock T" || !toks[1].RawBody || string(toks[1].Raw) != "if x < 10 { {{ not }} }" {
		t.Fatalf("raw body: %s %+v", kinds(toks), toks[1])
	}
	rawIs(t, src, toks)
}

func TestScanErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, msg string }{
		{`<Card {{ if .X }}open{{ end }}>`, "between attributes"},
		{`<PostList :f="eq .S \"blog\"" />`, "backslash escapes"},
		{`{{ .A `, "unclosed {{"},
		{`{{ "abc }}`, "unterminated string"},
		{`<Card title="x`, "unclosed quote"},
		{`<CodeBlock>no end`, "raw body has no closing"},
		{`<Card :$post />`, "write :post, not :$post"},
	} {
		_, err := Scan([]byte(tc.src), Options{IsRaw: func(n string, _ []Attr) bool { return n == "CodeBlock" }})
		var le *Error
		if !errors.As(err, &le) || !strings.Contains(le.Msg, tc.msg) {
			t.Fatalf("%s: err = %v, want %q", tc.src, err, tc.msg)
		}
	}
}

func TestPositions(t *testing.T) {
	t.Parallel()
	toks, err := Scan([]byte("a\n  <Card />\n{{ .X }}"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if toks[1].Pos.Line != 2 || toks[1].Pos.Col != 3 || toks[3].Pos.Line != 3 || toks[3].Pos.Col != 1 {
		t.Fatalf("positions: %+v %+v", toks[1].Pos, toks[3].Pos)
	}
}
