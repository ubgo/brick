package brick

import (
	"context"
	"io"
	"strings"
	"testing"
)

// A kit component merges the caller's class into its own and leaves the
// class out of {{ attrs }}, so the root has one class attribute.
func TestClassMergeHelpers(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Btn.html", "---\nprops:\n  block: bool\n---\n<button class=\"{{ cx `bk-btn` (cond .Props.block `bk-btn--block` ``) .Attrs.class }}\" {{ attrs `class` }}>{{ children }}</button>",
		"content/a.html", `<Btn class="mt-2  extra" id="go" block>Go</Btn>`,
		"content/b.html", `<Btn>Go</Btn>`,
		"content/c.html", `{{ cx "a" nil false "" (list "b" "c") 3 }}|{{ cond 0 "yes" "no" }}|{{ cond "x" "yes" "no" }}`,
	)})
	cases := map[string]string{
		"content/a.html": `<button class="bk-btn bk-btn--block mt-2 extra" id="go">Go</button>`,
		"content/b.html": `<button class="bk-btn" >Go</button>`,
		"content/c.html": `a b c 3|no|yes`,
	}
	for page, want := range cases {
		if got := render(t, e, page, nil); got != want {
			t.Errorf("%s:\n got  %q\n want %q", page, got, want)
		}
	}
}

// Action events and navigation attributes written on a component call pass
// through {{ attrs }} to the element the component renders — inside a
// stateful component only, for events.
func TestBrickAttrsForwardThroughComponents(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/Inp.html", `<input class="in" {{ attrs }}>`,
		"components/Lnk.html", `<a {{ attrs }}>{{ children }}</a>`,
		"components/Form.html", "---\nstate:\n  name: \"\"\n---\n<form brick:submit=\"save\"><Inp brick:model.lazy=\"name\" value=\"{{ .State.name }}\" /><Inp brick:change='pick(1, \"a\")' /></form>",
		"components/Nope.html", `<Inp brick:model="name" />`,
		"components/Bad.html", `<Inp brick:frob="x" />`,
		"content/a.html", `<Form /><Lnk href="/b/" brick:prefetch>b</Lnk>`,
		"content/nope.html", `<Nope />`,
		"content/bad.html", `<Bad />`,
	)})
	got := render(t, e, "content/a.html", nil)
	for _, want := range []string{`<input class="in" brick:model.lazy="name" value="" name="brick:model:name">`, `brick:change="pick(1, &#34;a&#34;)"`, `<a href="/b/" brick:prefetch>b</a>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if err := renderErr(t, e, "content/nope.html"); err == nil || !strings.Contains(err.Error(), "need a component that declares state:") {
		t.Errorf("event outside a stateful component: %v", err)
	}
	if err := renderErr(t, e, "content/bad.html"); err == nil || !strings.Contains(err.Error(), "brick:frob is reserved") {
		t.Errorf("unknown brick: attribute: %v", err)
	}
}

// sortBy orders maps and structs by a key: text without case, numbers as
// numbers, "-" for descending, stable, missing keys first.
func TestSortBy(t *testing.T) {
	t.Parallel()
	type pg struct {
		Title string
		N     int
	}
	e := newEngine(t, Options{FS: theme(
		"content/a.html", `{{ range sortBy "Title" .Props.pages }}{{ .Title }},{{ end }}|{{ range sortBy "-N" .Props.pages }}{{ .N }},{{ end }}|{{ range sortBy "n" .Props.maps }}{{ .n }}{{ .k }},{{ end }}|{{ range sortBy "P.o" .Props.nested }}{{ .k }},{{ end }}`,
	)})
	props := map[string]any{
		"pages":  []pg{{"button", 2}, {"ButtonGroup", 10}, {"Accordion", 1}, {"Badge", 2}},
		"nested": []map[string]any{{"k": "x", "P": map[string]any{"o": 2}}, {"k": "y", "P": map[string]any{"o": 1}}},
		"maps":   []map[string]any{{"n": 3, "k": "a"}, {"k": "none"}, {"n": 1.5, "k": "b"}, {"n": 3, "k": "c"}},
	}
	want := "Accordion,Badge,button,ButtonGroup,|10,2,2,1,|none,1.5b,3a,3c,|y,x,"
	if got := render(t, e, "content/a.html", props); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// C6 warns about open="false" (a truthy text flag) but not about
// attributes whose "false" or "0" is a value the browser reads.
func TestFalseStringWarning(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Box.html", `<div {{ attrs }}></div>`,
		"content/a.html", `<Box open="false" aria-expanded="false" tabindex="0" data-n="0" value="0" />`,
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	var flagged []string
	for _, w := range append(e.Warnings(), r.Warnings...) {
		if w.Code == CodeFalseString {
			flagged = append(flagged, w.Message)
		}
	}
	if len(flagged) != 1 || !strings.HasPrefix(flagged[0], `open="false"`) {
		t.Fatalf("want one warning, for open: %q", flagged)
	}
}

// A page's and a layout's own <brick:style> are linked like a component's.
func TestPageAndLayoutStylesShip(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"layouts/default.html", "<brick:style>.l { color: blue }</brick:style><main>{{ assets }}{{ children }}</main>",
		"content/landing.html", "<brick:style>.p { color: red }</brick:style><p class=\"p\">x</p>",
	)})
	got := render(t, e, "content/landing.html", nil)
	if strings.Count(got, `<link rel="stylesheet" href="/_brick/c/page.`) != 2 || !strings.Contains(got, `<p class="p">x</p>`) {
		t.Errorf("want the page's and the layout's stylesheets in %s", got)
	}
}

// A component's output loses the whitespace at its edges (T11), so an
// inline component in a sentence adds no stray spaces; whitespace inside
// it, a page's own and a layout's are kept.
func TestComponentOutputTrimmed(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Kbd.html", "---\nprops:\n  k: string\n---\n<brick:style>.k{}</brick:style>\n\n<kbd>{{ .Props.k }}</kbd>\n\n",
		"components/Pre.html", "\n<pre>\n  a\n</pre>\n",
		"components/Empty.html", "\n  \n",
		"content/a.html", "\nPress <Kbd k=\"⌘\" />+<Kbd k=\"K\" />.<Empty/>|<Pre />\n",
	)})
	if got, want := render(t, e, "content/a.html", nil), "\nPress <kbd>⌘</kbd>+<kbd>K</kbd>.|<pre>\n  a\n</pre>\n"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// A literal attribute on a component call is HTML attribute text: its
// character references are decoded once, and {{ attrs }} escapes once.
func TestLiteralAttrEntities(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/T.html", "---\nprops:\n  title: string\n---\n<b {{ attrs }}>{{ .Props.title }}|{{ len .Props.title }}</b>",
		"content/a.html", `<T title="Tom &amp; Jerry" data-x="f(&quot;x&quot;)" />`,
	)})
	if got, want := render(t, e, "content/a.html", nil), `<b data-x="f(&#34;x&#34;)">Tom &amp; Jerry|11</b>`; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// C7's warning about a child tag inside {{ if }} fires only for a parent
// that reads .Children; for any other parent the child is just markup.
func TestChildrenIfWarningOnlyForReaders(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Tabs.html", `{{ range .Children }}{{ .Name }}{{ end }}{{ children }}`,
		"components/Card.html", "{{/* Card never reads .Children; this comment says so. */}}<div>{{ children }}</div>",
		"components/Tab.html", `{{ children }}`,
		"content/a.html", `<Tabs>{{ if .Props.x }}<Tab>a</Tab>{{ end }}</Tabs><Card>{{ if .Props.x }}<Tab>b</Tab>{{ end }}</Card>`,
	)})
	render(t, e, "content/a.html", nil)
	var got []string
	for _, w := range e.Warnings() {
		if w.Code == CodeChildrenIf {
			got = append(got, w.Message)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0], "<Tabs>'s .Children") {
		t.Fatalf("want one warning, for Tabs: %q", got)
	}
}

// An action call the browser could not parse is a compile error, on a
// plain element and on a component call; JSON arguments and {{ }} values
// pass.
func TestActionCallArgsChecked(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/B.html", `<button {{ attrs }}>{{ children }}</button>`,
		"components/Ok.html", "---\nstate:\n  n: 0\n---\n<div><button brick:click='add(1, \"a b\", [true])'>+</button><button brick:click=\"reset\">0</button><button brick:click=\"set({{ .State.n }})\">s</button><B brick:click='f(\"x\")'>b</B></div>",
		"components/Plain.html", "---\nstate:\n  n: 0\n---\n<div><button brick:click=\"mode('x')\">m</button></div>",
		"components/Comp.html", "---\nstate:\n  n: 0\n---\n<div><B brick:click=\"go(x)\">b</B></div>",
		"content/ok.html", `<Ok />`,
		"content/plain.html", `<Plain />`,
		"content/comp.html", `<Comp />`,
	)})
	render(t, e, "content/ok.html", nil)
	for _, page := range []string{"content/plain.html", "content/comp.html"} {
		if err := renderErr(t, e, page); err == nil || !strings.Contains(err.Error(), "arguments must be JSON") {
			t.Errorf("%s: %v", page, err)
		}
	}
}

// An import quoted in a comment is not an import: it is neither linked nor
// reported (a module whose doc comment shows its own import line used to
// fail as a cycle), and the comment text is kept. A "//" inside a string is
// not a comment.
func TestClientImportsIgnoreComments(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Modal.html", "<brick:client>\n// Use it with: import { open } from \"brick:Modal\"\n/* or import \"brick:Nope\" */\nexport const open = () => \"http://x\"\n</brick:client><dialog></dialog>",
		"components/Opener.html", "<brick:client>\nimport { open } from \"brick:Modal\" // not \"brick:Nope\"\nconst u = \"a//b\"; brick.mount(\"Opener\", () => open())\n</brick:client><button>o</button>",
		"content/a.html", "<Opener />{{ assets }}",
	)})
	render(t, e, "content/a.html", nil)
	var modal, opener string
	for _, p := range listAssetFiles(t, e) {
		f, _ := e.Assets().Open(p)
		b, _ := io.ReadAll(f)
		switch {
		case strings.HasPrefix(p, "c/Modal."):
			modal = string(b)
		case strings.HasPrefix(p, "c/Opener."):
			opener = string(b)
		}
	}
	if !strings.Contains(modal, `// Use it with: import { open } from "brick:Modal"`) || !strings.Contains(modal, `/* or import "brick:Nope" */`) {
		t.Errorf("comments rewritten or lost:\n%s", modal)
	}
	if !strings.Contains(opener, `import { open } from "./Modal.`) || !strings.Contains(opener, `// not "brick:Nope"`) || !strings.Contains(opener, `"a//b"`) {
		t.Errorf("opener:\n%s", opener)
	}
}
