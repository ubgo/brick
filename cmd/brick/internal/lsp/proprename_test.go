package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// propsProject is a site whose Card declares title and body, and a slot.
type propsProject struct{}

func (propsProject) Components(context.Context, string) ([]brick.ComponentDoc, error) {
	return []brick.ComponentDoc{
		{Name: "Badge", Path: "kit/Badge.html", Props: []brick.PropDoc{{Name: "label"}}},
		{Name: "Card", Path: "components/Card.html", Props: []brick.PropDoc{{Name: "title"}, {Name: "body"}}, Slots: []brick.SlotDoc{{Name: "footer"}}},
		{Name: "Plain", Path: "components/Plain.html"},
	}, nil
}

func (propsProject) Check(context.Context, string) ([]brick.Diagnostic, error) { return nil, nil }

// propsCard reads title every way a component can, plus forms that are
// not its title: one under a rebound dot, one in a string, one in a
// comment.
const propsCard = `---
props:
  title: {type: string, default: Untitled}
  body: string
brick: {slots: {footer: {doc: "a title: here"}}}
---
<article class="c {{ .Props.title }}">
  <h2>{{ .Props.title }}</h2>
  {{ if .Props.title }}<em>{{ $.Props.title }}</em>{{ else }}{{ .Props.title }}{{ end }}
  {{ index .Props "title" }} {{ attrs "title" "id" }} {{ .Attrs.title }}
  {{ range .Props.items }}{{ .Props.title }}{{ else }}{{ .Props.title }}{{ end }}
  {{ with .Props.body }}{{ .Props.title }}{{ else with .Props.title }}{{ . }}{{ end }}
  {{ ".Props.title" }}{{/* .Props.title */}}
  <Inner :label=".Props.title" text="x {{ .Props.title }}" />
  {{ if hasNode "footer" }}{{ node "footer" }}{{ end }}
</article>
<brick:script>
def save(state, props):
    return dict(state, t = props["title"], b = props.get('title'))
</brick:script>
`

const propsPage = `<Card title="a" body="b" />
<x-card :title="` + "`dyn`" + `"></x-card>
<Card><brick:prop name="footer">f</brick:prop></Card>
`

// writePropsSite writes the site plus extra files and returns its root.
func writePropsSite(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"brick.yaml":              "",
		"components/Card.html":    propsCard,
		"components/Card.fr.html": "<p>{{ .Props.title }}</p>\n",
		"components/Plain.html":   "<p>{{ .Props.title }}</p>\n",
		"content/index.html":      propsPage,
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// startProps opens Card.html in a server; docChanges is the client's
// documentChanges support.
func startProps(t *testing.T, dir string, docChanges bool) (*client, string) {
	t.Helper()
	c := startServerWith(t, Options{Project: propsProject{}})
	caps := map[string]any{"workspace": map[string]any{"workspaceEdit": map[string]any{"documentChanges": docChanges}}}
	c.call("initialize", map[string]any{"rootUri": pathToURI(dir), "capabilities": caps}, nil)
	uri := pathToURI(filepath.Join(dir, "components", "Card.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "html", "version": 1, "text": propsCard}})
	return c, uri
}

// renameAll applies a documentChanges edit to the files on disk and
// returns each changed file's new text by its site-relative path.
func renameAll(t *testing.T, dir string, we workspaceEdit) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, ch := range we.DocumentChanges {
		p, _ := uriToPath(ch.TextDocument.URI)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = apply(string(b), ch.Edits)
	}
	return out
}

// The scanner tells the component's own .Props.title from the same text
// under a rebound dot, in a string, in a comment and through a variable,
// and tracks else branches back to the outer dot.
func TestComponentUses(t *testing.T) {
	f, ok := scanFile("/x/Card.html", propsCard, nil)
	if !ok {
		t.Fatal("Card does not scan")
	}
	kinds := map[useKind]int{}
	for _, u := range componentUses(f) {
		if u.name != "title" {
			continue
		}
		if got := f.text[u.start:u.end]; got != "title" {
			t.Fatalf("use range covers %q", got)
		}
		kinds[u.kind]++
	}
	// Fields: class, h2, if, $, if-else, .Attrs, range-else, else-with's
	// condition, :label, text="{{ }}". Index: 1. attrs: 1. script: 2.
	// Rebound: inside range, inside with. String: the quoted ".Props.title"
	// is not a name, so none.
	want := map[useKind]int{useField: 10, useIndex: 1, useAttrsArg: 1, useScript: 2, useRebound: 2}
	for k, n := range want {
		if kinds[k] != n {
			t.Fatalf("kind %d: %d uses, want %d (all: %v)", k, kinds[k], n, kinds)
		}
	}
	if kinds[useString] != 0 || kinds[useWhole] != 0 {
		t.Fatalf("unexpected kinds: %v", kinds)
	}

	whole, _ := scanFile("/x/W.html", "{{ $p := .Props }}{{ range .Props.items }}{{ .Props }}{{ end }}{{ $v.Props.title }}{{ dict \"title\" 1 }}", nil)
	got := map[useKind]int{}
	for _, u := range componentUses(whole) {
		got[u.kind]++
	}
	// .Props whole at root blocks; under range it is someone else's.
	if got[useWhole] != 1 || got[useVar] != 1 || got[useString] != 1 {
		t.Fatalf("whole/var/string: %v", got)
	}
}

// Renaming title from its .Props use edits the declaration, every own use
// (including the locale variant), every call-site attribute in both forms,
// and leaves the rebound, string and comment forms alone, reporting the
// rebound ones.
func TestPropRename(t *testing.T) {
	dir := writePropsSite(t, nil)
	c, uri := startProps(t, dir, true)

	var prep struct {
		Range       lspRange
		Placeholder string
	}
	at := pos(uri, propsCard, "<h2>{{ .Props.title", 15)
	c.call("textDocument/prepareRename", at, &prep)
	if prep.Placeholder != "title" {
		t.Fatalf("prepareRename: %+v", prep)
	}

	at["newName"] = "heading"
	var we workspaceEdit
	c.call("textDocument/rename", at, &we)
	got := renameAll(t, dir, we)
	if got["content/index.html"] != strings.NewReplacer(`<Card title=`, `<Card heading=`, `:title=`, `:heading=`).Replace(propsPage) {
		t.Fatalf("page:\n%s", got["content/index.html"])
	}
	if got["components/Card.fr.html"] != "<p>{{ .Props.heading }}</p>\n" {
		t.Fatalf("variant: %q", got["components/Card.fr.html"])
	}
	if _, ok := got["components/Plain.html"]; ok {
		t.Fatal("another component's .Props.title was renamed")
	}
	card := got["components/Card.html"]
	for _, want := range []string{"\n  heading: {type", `{{ .Props.heading }}</h2>`, `{{ $.Props.heading }}`, `{{ index .Props "heading" }}`,
		`attrs "heading" "id"`, `.Attrs.heading`, `{{ range .Props.items }}{{ .Props.title }}{{ else }}{{ .Props.heading }}`,
		`{{ with .Props.body }}{{ .Props.title }}{{ else with .Props.heading }}`, `{{ ".Props.title" }}{{/* .Props.title */}}`,
		`:label=".Props.heading" text="x {{ .Props.heading }}"`, `props["heading"]`, `props.get('heading')`, `doc: "a title: here"`} {
		if !strings.Contains(card, want) {
			t.Fatalf("Card after rename lacks %q:\n%s", want, card)
		}
	}
	var shown []string
	for _, m := range c.notes {
		if m.Method == methodShowMsg {
			shown = append(shown, string(m.Params))
		}
	}
	if len(shown) != 1 || !strings.Contains(shown[0], "skipped 2") || !strings.Contains(shown[0], "where dot is not the component") {
		t.Fatalf("skip report: %v", shown)
	}

	// The same from a call-site attribute and from the declaration.
	page := pathToURI(filepath.Join(dir, "content", "index.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": page, "languageId": "html", "version": 1, "text": propsPage}})
	for _, p := range []map[string]any{pos(page, propsPage, ":title", 2), pos(uri, propsCard, "  title:", 3)} {
		p["newName"] = "heading"
		var again workspaceEdit
		c.call("textDocument/rename", p, &again)
		if g := renameAll(t, dir, again); g["components/Card.html"] != card {
			t.Fatalf("rename from %v differs:\n%s", p["position"], g["components/Card.html"])
		}
	}
}

// References to a prop list its declaration, its own uses and every call.
func TestPropReferences(t *testing.T) {
	dir := writePropsSite(t, nil)
	c, uri := startProps(t, dir, true)
	p := pos(uri, propsCard, "{{ index .Props \"title", 17)
	p["context"] = map[string]any{"includeDeclaration": true}
	var locs []lspLocation
	c.call("textDocument/references", p, &locs)
	got := texts(t, dir, locs)
	n := 0
	for _, s := range got {
		if s != "components/Card.html:title" && s != "components/Card.fr.html:title" && s != "content/index.html:title" {
			t.Fatalf("reference covers %q", s)
		}
		n++
	}
	// Card: decl + 10 fields + index + attrs + 2 script = 15; fr: 1; page: 2.
	if n != 18 {
		t.Fatalf("%d references, want 18: %q", n, got)
	}
	// A slot is a prop too: its declaration, node/hasNode and the block.
	p = pos(uri, propsCard, `node "footer`, 7)
	p["context"] = map[string]any{"includeDeclaration": true}
	c.call("textDocument/references", p, &locs)
	if got := texts(t, dir, locs); !slices.Equal(got, []string{"components/Card.html:footer", "components/Card.html:footer", "components/Card.html:footer", "content/index.html:footer"}) {
		t.Fatalf("slot references: %q", got)
	}
}

// A rename that would clash, or that meets a site brick cannot follow, is
// refused whole with the sites named; nothing is edited.
func TestPropRenameRefusals(t *testing.T) {
	cases := []struct {
		name, newName string
		extra         map[string]string
		want          string
	}{
		{"declared", "body", nil, `already declares "body"`},
		{"slot", "footer", nil, `already declares "footer"`},
		{"passed", "id", map[string]string{"content/x.html": `<Card id="1" />`}, "content/x.html:1:7 already passes id"},
		{"invalid", "data-x", nil, "is not a prop name"},
		{"children", "children", nil, "is not a prop name"},
		{"spread", "heading", map[string]string{"content/x.html": `<Card brick:spread=".Params.p" />`}, "content/x.html:1:7 passes props through brick:spread"},
		{"whole", "heading", map[string]string{"components/Card.fr.html": "{{ template \"x\" .Props }}"}, "Card.fr.html:1:18 hands .Props on whole"},
		{"load", "heading", map[string]string{"components/Card.fr.html": "---\nload: f\n---\n<p></p>"}, "has load:"},
		{"dynamic", "heading", map[string]string{"components/Card.fr.html": "<brick:script>\nx = props[k]\n</brick:script>"}, "reads props with a computed key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePropsSite(t, tc.extra)
			c, uri := startProps(t, dir, true)
			p := pos(uri, propsCard, "<h2>{{ .Props.title", 15)
			p["newName"] = tc.newName
			if msg := c.callErr("textDocument/rename", p); !strings.Contains(msg, tc.want) {
				t.Fatalf("rename to %s: %s, want %q", tc.newName, msg, tc.want)
			}
		})
	}

	// A kit component's prop has no file to rename.
	dir := writePropsSite(t, map[string]string{"content/k.html": `<Badge label="x" />`})
	c, _ := startProps(t, dir, true)
	k := pathToURI(filepath.Join(dir, "content", "k.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": k, "languageId": "html", "version": 1, "text": `<Badge label="x" />`}})
	if msg := c.callErr("textDocument/prepareRename", pos(k, `<Badge label="x" />`, "label", 1)); !strings.Contains(msg, "built into brick") {
		t.Fatalf("kit prop: %s", msg)
	}
}

// A client without documentChanges gets the plain changes map.
func TestPropRenameChangesMap(t *testing.T) {
	dir := writePropsSite(t, nil)
	c, uri := startProps(t, dir, false)
	p := pos(uri, propsCard, "<h2>{{ .Props.title", 15)
	p["newName"] = "heading"
	var we struct {
		Changes         map[string][]json.RawMessage
		DocumentChanges []any `json:"documentChanges"`
	}
	c.call("textDocument/rename", p, &we)
	if we.DocumentChanges != nil || len(we.Changes) != 3 {
		t.Fatalf("changes map: %+v", we)
	}
}
