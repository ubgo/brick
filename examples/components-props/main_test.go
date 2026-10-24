package main

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// want is the exact HTML each page renders; docs/guides/components/props.md
// shows these bytes.
var want = map[string]string{
	"content/index.html": `<h1>Latest posts</h1>
<article class="card card--loud">
  <h3>#0 Hello</h3>
  <p>The first post.</p>
</article><article class="card card--loud">
  <h3>#1 Again</h3>
  <p>More words.</p>
</article>
<article class="card card--plain">
  <h3>#0 Spelled with x-</h3>
  <p>Same file.</p>
</article>
`,
	"content/attrs.html": `<button class="btn wide" id="save" hx-post="/save">Save</button>
<button class="btn primary" hx-post="/buy" id="buy">Buy</button>
`,
	"content/contact.html": `<a href="mailto:ann@example.com">ann@example.com</a> <span>$19.99</span>
`,
}

func TestPages(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderAll(context.Background(), eng)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		if got[p] != want[p] {
			t.Errorf("%s:\n got %q\nwant %q", p, got[p], want[p])
		}
	}
}

// Every call-site mistake the guide's Pitfalls list names is an error
// with the file, line and the exact words below.
func TestPropErrors(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"<Card :post=\"dict `title` `x`\" tone=\"lowd\" />": `content/probe.html:1:32: <Card>: prop "tone": "lowd" is not one of plain, loud`,
		`<Card tone="loud" />`:                              `content/probe.html:1:1: <Card>: <Card> requires the prop "post"`,
		`<Card :post />`:                                    `content/probe.html:1:7: <Card>: :post with no value passes $post, but no $post is declared here; declare it or write :post="…"`,
		`<Contact to="nope" price="1" />`:                   `content/probe.html:1:10: <Contact>: prop "to": "nope" is not an email address`,
		`<Card :post="dict "title" "x"" />`:                 `content/probe.html:1:25: unexpected '"' in <Card> tag`,
	}
	for src, msg := range cases {
		_, err := eng.Render(context.Background(), "content/probe.html", nil, brick.WithSource([]byte(src)))
		if err == nil || err.Error() != msg {
			t.Errorf("%s:\n got %v\nwant %s", src, err, msg)
		}
	}
}

// fields: is checked by brick check (CompileAll) as warnings, and
// strictProps turns an undeclared attribute into an error.
func TestShapeAndStrict(t *testing.T) {
	fsys := fstest.MapFS{
		"components/Author.html": {Data: []byte("---\nprops:\n  author:\n    type: map\n    fields:\n      name: {type: string, required: true}\n      email: string\n---\n<p>{{ .Props.author.emial }}</p>\n")},
		"components/Strict.html": {Data: []byte("---\nstrictProps: true\nprops:\n  title: string\n---\n<p>{{ .Props.title }}</p>\n")},
		"content/a.html":         {Data: []byte("<Author :author=\"dict `email` `a@b.c`\" />\n")},
		"content/b.html":         {Data: []byte("<Strict title=\"x\" tone=\"loud\" />\n")},
	}
	eng, err := brick.New(brick.Options{FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := eng.CompileAll(context.Background(), "content")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, d := range rep.Diagnostics {
		lines = append(lines, d.String())
	}
	got := strings.Join(lines, "\n")
	for _, w := range []string{
		`components/Author.html:9:21: warning: .Props.author.emial: "emial" is not a field of .Props.author (fields: email, name)`,
		`content/a.html:1:9: warning: <Author> author: the required field "name" is missing`,
		`content/b.html:1:19: error: <Strict>: prop "tone" is not declared by <Strict>, which sets strictProps`,
	} {
		if !strings.Contains(got, w) {
			t.Errorf("diagnostics lack %q:\n%s", w, got)
		}
	}
}

// TestOutput compares what `go run .` prints with testdata/output.txt,
// the text the guide's "Try it" section shows.
func TestOutput(t *testing.T) {
	want, err := os.ReadFile("testdata/output.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), &out, "theme"); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A theme that uses a host prop type needs a host that registers it.
func TestPropTypeNeedsHost(t *testing.T) {
	root, err := os.OpenRoot("theme")
	if err != nil {
		t.Fatal(err)
	}
	eng, err := brick.New(brick.Options{FS: root.FS()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/contact.html", nil)
	const want = `components/Contact.html:1: props: to: unknown type "email" (one of string, int, float, bool, list, map, any; the host program adds more with Options.PropTypes)`
	if err == nil || err.Error() != want {
		t.Errorf("got %v\nwant %s", err, want)
	}
}

// A host function that takes int cannot receive an int prop, which
// arrives as int64; this is why money takes int64.
func TestIntPropIsInt64(t *testing.T) {
	root, err := os.OpenRoot("theme")
	if err != nil {
		t.Fatal(err)
	}
	narrow := func(cents int) string { return "never" }
	eng, err := brick.New(brick.Options{FS: root.FS(), Funcs: template.FuncMap{"money": narrow}, PropTypes: map[string]brick.PropType{"email": email}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/contact.html", nil)
	if err == nil || !strings.Contains(err.Error(), "at <.Props.price>: wrong type for value; expected int; got int64") {
		t.Errorf("got %v", err)
	}
}
