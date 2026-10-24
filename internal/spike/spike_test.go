// Package spike holds the transpiler spike: executable proofs of the five
// design rules that depend on html/template internals.
//
// Why it exists: the whole design rests on
// behaviour of the standard library that no document can promise. Each test
// here pins one behaviour against the real html/template, so an upgrade of Go
// that changes it fails loudly instead of silently breaking brick.
package spike

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"strings"
	"testing"
)

// scope is the value a generated body is executed with. It mirrors what the
// engine will pass: the caller's root ($), the caller's dot (.), and the
// caller's $variables by name.
type scope struct {
	Root any
	Dot  any
	Vars map[string]any
}

type post struct{ Title string }

// T2 + T3 — a body written inside `{{ range $p := .Posts }}` can see $p, dot
// and $ once a prologue re-declares them. Without the prologue Go refuses the
// template ("undefined variable"), which is the hole the rule exists to close.
func TestT2VariablesReachBodies(t *testing.T) {
	t.Parallel()

	// Proof of the problem: a define cannot see the caller's variables.
	_, err := template.New("p").Parse(`{{ range $p := .Posts }}{{ template "b" }}{{ end }}{{ define "b" }}{{ $p.Title }}{{ end }}`)
	if err == nil || !strings.Contains(err.Error(), `undefined variable "$p"`) {
		t.Fatalf("expected undefined variable error, got %v", err)
	}

	// The fix: the page calls a function that executes the body with a scope
	// value; the body's prologue rebinds $, $p and dot before the author text.
	var set *template.Template
	funcs := template.FuncMap{
		"__children": func(name string, root, dot any, vars map[string]any) (template.HTML, error) {
			var buf bytes.Buffer
			if err := set.ExecuteTemplate(&buf, name, scope{Root: root, Dot: dot, Vars: vars}); err != nil {
				return "", err
			}
			return template.HTML(buf.String()), nil // #nosec: body output was escaped by html/template
		},
		"__vars": func(kv ...any) map[string]any {
			m := make(map[string]any, len(kv)/2)
			for i := 0; i+1 < len(kv); i += 2 {
				m[kv[i].(string)] = kv[i+1]
			}
			return m
		},
		"__once": func(v any) []any { return []any{v} },
	}
	src := `{{ range $i, $p := .Posts }}<li>{{ __children "body" $ . (__vars "i" $i "p" $p) }}</li>{{ end }}` +
		`{{ define "body" }}{{ $p := index .Vars "p" }}{{ $i := index .Vars "i" }}{{ $ := .Root }}` +
		`{{ range __once .Dot }}#{{ $i }} {{ $p.Title }} / dot={{ .Title }} / root={{ $.Site }}{{ end }}{{ end }}`
	var err2 error
	set, err2 = template.New("page").Funcs(funcs).Parse(src)
	if err2 != nil {
		t.Fatalf("parse: %v", err2)
	}
	var out bytes.Buffer
	data := map[string]any{"Site": "brick", "Posts": []post{{"A<b>"}, {"B"}}}
	if err := set.Execute(&out, data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `<li>#0 A&lt;b&gt; / dot=A&lt;b&gt; / root=brick</li><li>#1 B / dot=B / root=brick</li>`
	if out.String() != want {
		t.Fatalf("got  %s\nwant %s", out.String(), want)
	}
}

// T7 — `{{ helpers.slugify x }}` is not callable in Go templates; the rewritten
// per-file function name is. The rewrite is textual, done by the transpiler's
// action lexer before Parse, so no parse-tree surgery is needed.
func TestT7NamespaceCallRewrite(t *testing.T) {
	t.Parallel()
	helpers := map[string]any{"slugify": func(s string) string { return strings.ToLower(s) }}
	tm := template.Must(template.New("x").Funcs(template.FuncMap{"helpers": func() map[string]any { return helpers }}).Parse(`{{ helpers.slugify .T }}`))
	err := tm.Execute(io.Discard, map[string]string{"T": "Hi"})
	if err == nil {
		t.Fatalf("expected the namespaced call to fail as written")
	}

	rewritten := template.Must(template.New("x").Funcs(template.FuncMap{
		"__call_helpers_slugify": func(s string) string { return strings.ToLower(s) },
	}).Parse(`{{ __call_helpers_slugify .T }}`))
	var out bytes.Buffer
	if err := rewritten.Execute(&out, map[string]string{"T": "Hi"}); err != nil || out.String() != "hi" {
		t.Fatalf("rewritten call: %q, %v", out.String(), err)
	}
}

// chunk is either static bytes or a marker, the shape the engine's Result uses.
type chunk struct {
	marker string
	bytes  []byte
}

// builder is an io.Writer that also accepts markers; it proves text/template
// writes to the writer in order with no internal buffering, so a marker
// function can split the stream at the exact point it was called.
type builder struct {
	chunks []chunk
	cur    bytes.Buffer
}

func (b *builder) Write(p []byte) (int, error) { return b.cur.Write(p) }

func (b *builder) mark(name string) {
	if b.cur.Len() > 0 {
		b.chunks = append(b.chunks, chunk{bytes: bytes.Clone(b.cur.Bytes())})
		b.cur.Reset()
	}
	b.chunks = append(b.chunks, chunk{marker: name})
}

func (b *builder) flush() {
	if b.cur.Len() > 0 {
		b.chunks = append(b.chunks, chunk{bytes: bytes.Clone(b.cur.Bytes())})
		b.cur.Reset()
	}
}

// T9 — markers are carried outside the byte stream. Data that happens to
// contain any sentinel-looking text is output unchanged, because no sentinel
// exists: the marker is a separate chunk.
func TestT9MarkersOutsideTheStream(t *testing.T) {
	t.Parallel()
	b := &builder{}
	tm := template.Must(template.New("x").Funcs(template.FuncMap{
		"__marker": func(st *builder, name string) string { st.mark(name); return "" },
	}).Parse(`<head>{{ __marker .St "assets" }}</head><p>{{ .Bio }}</p>{{ __marker .St "nonce" }}<i>end</i>`))
	forged := `\x00brick:assets\x00 <!--brick-marker-->`
	if err := tm.Execute(b, map[string]any{"St": b, "Bio": forged}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	b.flush()

	var kinds []string
	for _, c := range b.chunks {
		if c.marker != "" {
			kinds = append(kinds, "M:"+c.marker)
		} else {
			kinds = append(kinds, "B:"+string(c.bytes))
		}
	}
	got := strings.Join(kinds, " | ")
	want := `B:<head> | M:assets | B:</head><p>\x00brick:assets\x00 &lt;!--brick-marker--&gt;</p> | M:nonce | B:<i>end</i>`
	if got != want {
		t.Fatalf("chunks:\n got  %s\n want %s", got, want)
	}
}

// T10 — errors carry positions against the generated source, and escaper
// errors surface only at Execute. A dry run with nil data surfaces the escaper
// error as *template.Error, distinguishable from ordinary execution errors.
func TestT10ErrorPositionsAndDryRun(t *testing.T) {
	t.Parallel()

	// Parse errors: "template: NAME:LINE: ..." — the name is ours to choose, so
	// naming generated templates after their origin makes them mappable.
	_, err := template.New("content/pricing.html@12").Parse("line1\n{{ .A ")
	if err == nil || !strings.Contains(err.Error(), "content/pricing.html@12:2") {
		t.Fatalf("parse error lacks position: %v", err)
	}

	// Escaper error: a body ending inside an attribute only fails at Execute.
	tm := template.Must(template.New("bad").Parse(`<div class="{{ .X }}`))
	err = tm.Execute(io.Discard, nil)
	var he *template.Error
	if !errors.As(err, &he) {
		t.Fatalf("expected *template.Error from dry run, got %T %v", err, err)
	}

	// A correct template dry-run with nil data produces no error at all, so a
	// dry run in CompileAll reports only real escaping problems.
	ok := template.Must(template.New("ok").Parse(`<div class="{{ .X }}">{{ .Y.Z }}{{ range .L }}{{ . }}{{ end }}</div>`))
	if err := ok.Execute(io.Discard, nil); err != nil {
		t.Fatalf("dry run of a valid template must be silent, got %v", err)
	}
}

// jsonValue is what toJSON returns: raw JSON that html/template marshals with
// its own escaping in script context and prints as text elsewhere.
type jsonValue []byte

func (j jsonValue) MarshalJSON() ([]byte, error) { return j, nil }
func (j jsonValue) String() string               { return string(j) }

// D13 — toJSON output is correct in script, attribute and body contexts, and
// a </script> inside the data cannot break out of the script element.
func TestD13ToJSONContexts(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(map[string]string{"a": "</script><b>x"})
	if err != nil {
		t.Fatal(err)
	}
	v := jsonValue(raw)
	tm := template.Must(template.New("x").Parse(
		`<script type="application/json">{{ .V }}</script><div data-x="{{ .V }}">{{ .V }}</div>`))
	var out bytes.Buffer
	if err := tm.Execute(&out, map[string]any{"V": v}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "</script><b>") {
		t.Fatalf("script context not escaped: %s", got)
	}
	if !strings.Contains(got, "<script type=\"application/json\">{\"a\":\"\\u003c/script\\u003e\\u003cb\\u003ex\"}</script>") {
		t.Fatalf("script context wrong: %q", got)
	}
	if !strings.Contains(got, "data-x=\"{&#34;a&#34;:&#34;\\u003c/script\\u003e\\u003cb\\u003ex&#34;}\"") {
		t.Fatalf("attribute context wrong: %s", got)
	}
}
