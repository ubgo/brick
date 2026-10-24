package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/internal/lex"
)

// refsProject is a site with a Card used every way a tag can be written,
// plus a kit component that has no file.
type refsProject struct{}

func (refsProject) Components(context.Context, string) ([]brick.ComponentDoc, error) {
	return []brick.ComponentDoc{
		{Name: "Badge", Path: "kit/Badge.html"},
		{Name: "Card", Path: "components/Card.html", Props: []brick.PropDoc{{Name: "title", Type: "string"}}},
		{Name: "Code", Path: "components/Code.html"},
		{Name: "Shell", Path: "components/Shell.html"},
	}, nil
}

func (refsProject) Check(context.Context, string) ([]brick.Diagnostic, error) { return nil, nil }

// refsPage holds a real call of every shape, and tag-shaped text the
// engine does not call: in a {{ }} string, a template comment, an HTML
// comment, a raw component's body and a <brick:script> body.
const refsPage = `---
use:
  Teaser: ~/components/Card.html
---
<Card title="a">x</Card>
<x-card title="b" />
<Teaser title="c" />
{{ "<Card title=\"str\">" }}
{{/* <Card title="comment"> */}}
<!-- <Card title="html"> -->
<Code><Card title="raw" /></Code>
<brick:script>x = "<Card title='script'>"</brick:script>
<Card><brick:prop name="title">t</brick:prop></Card>
`

// writeRefsSite writes the site and returns its root.
func writeRefsSite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"brick.yaml":                "",
		"components/Card.html":      "---\nprops:\n  title: {type: string}\n---\n<div>{{ .Props.title }}</div>\n",
		"components/Card.fr.html":   "<div>fr</div>\n",
		"components/Code.html":      "---\nraw: true\n---\n<pre>{{ .Children }}</pre>\n",
		"components/Shell.html":     "<main><Card title=\"s\" /><Badge /></main>\n",
		"content/index.html":        refsPage,
		"node_modules/x/Other.html": "<Card />\n",
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

// startRefs opens the site's page in a server whose client does (or does
// not) accept file renames.
func startRefs(t *testing.T, dir string, fileRenames bool) (*client, string) {
	t.Helper()
	raws := func(string) func([]byte) RawTest {
		return func([]byte) RawTest { return func(name string, _ []lex.Attr) bool { return name == "Code" } }
	}
	c := startServerWith(t, Options{Project: refsProject{}, RawComponents: raws})
	caps := map[string]any{}
	if fileRenames {
		caps["workspace"] = map[string]any{"workspaceEdit": map[string]any{"documentChanges": true, "resourceOperations": []string{"create", "rename", "delete"}}}
	}
	var init map[string]any
	c.call("initialize", map[string]any{"rootUri": pathToURI(dir), "capabilities": caps}, &init)
	got := init["capabilities"].(map[string]any)
	if got["referencesProvider"] != true || got["renameProvider"] == nil {
		t.Fatalf("references and rename not advertised: %v", got)
	}
	uri := pathToURI(filepath.Join(dir, "content", "index.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "html", "version": 1, "text": refsPage}})
	return c, uri
}

// callErr sends a request that must fail and returns its error message.
func (c *client) callErr(method string, params any) string {
	c.t.Helper()
	c.id++
	b, _ := json.Marshal(params)
	id, _ := json.Marshal(c.id)
	if err := c.conn.write(&message{ID: id, Method: method, Params: b}); err != nil {
		c.t.Fatal(err)
	}
	for m := range c.msgs {
		if string(m.ID) != string(id) {
			c.notes = append(c.notes, m)
			continue
		}
		if m.Error == nil {
			c.t.Fatalf("%s: want an error, got %s", method, m.Result)
		}
		return m.Error.Message
	}
	c.t.Fatalf("%s: stream closed", method)
	return ""
}

type lspRange struct {
	Start struct{ Line, Character int }
	End   struct{ Line, Character int }
}

type lspLocation struct {
	URI   string
	Range lspRange
}

// texts is what each location covers, as "file:text".
func texts(t *testing.T, dir string, locs []lspLocation) []string {
	t.Helper()
	var out []string
	for _, l := range locs {
		p, _ := uriToPath(l.URI)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		start, end := OffsetOf(s, l.Range.Start.Line, l.Range.Start.Character), OffsetOf(s, l.Range.End.Line, l.Range.End.Character)
		rel, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel)+":"+s[start:end])
	}
	sort.Strings(out)
	return out
}

// refsAt asks for the references at the marker in the page.
func refsAt(c *client, uri, marker string, delta int, decl bool) []lspLocation {
	p := pos(uri, refsPage, marker, delta)
	p["context"] = map[string]any{"includeDeclaration": decl}
	var locs []lspLocation
	c.call("textDocument/references", p, &locs)
	return locs
}

// References to a component list every call in the site (open and close
// tags, both forms, a use: alias), and nothing the engine reads as text:
// the tag-shaped strings in {{ }}, comments, a raw body and a script.
func TestReferences(t *testing.T) {
	dir := writeRefsSite(t)
	c, uri := startRefs(t, dir, true)

	got := texts(t, dir, refsAt(c, uri, "<x-card", 3, false))
	want := []string{
		"components/Shell.html:Card",
		"content/index.html:Card", "content/index.html:Card", "content/index.html:Card", "content/index.html:Card",
		"content/index.html:Teaser", "content/index.html:x-card",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("references:\n got %q\nwant %q", got, want)
	}
	// From a close tag, with the declaration: the same calls plus the file.
	if locs := refsAt(c, uri, "</Card>", 3, true); len(locs) != len(want)+1 || locs[0].URI != pathToURI(filepath.Join(dir, "components", "Card.html")) {
		t.Fatalf("from a close tag with the declaration: %+v", locs)
	}

	// A prop: every attribute and <brick:prop name> block passing it to a
	// Card, its declaration and its .Props.title use, never the raw body's.
	got = texts(t, dir, refsAt(c, uri, `title="a"`, 1, true))
	want = []string{
		"components/Card.html:title", "components/Card.html:title", "components/Shell.html:title",
		"content/index.html:title", "content/index.html:title", "content/index.html:title", "content/index.html:title",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("prop references:\n got %q\nwant %q", got, want)
	}

	// Anywhere in the component's own file lists its calls.
	cardURI := pathToURI(filepath.Join(dir, "components", "Card.html"))
	cardText := "---\nprops:\n  title: {type: string}\n---\n<div>{{ .Props.title }}</div>\n"
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": cardURI, "languageId": "html", "version": 1, "text": cardText}})
	var locs []lspLocation
	p := pos(cardURI, cardText, "<div>", 1)
	p["context"] = map[string]any{"includeDeclaration": false}
	c.call("textDocument/references", p, &locs)
	if len(locs) != 7 {
		t.Fatalf("from the definition file: %d references, want 7", len(locs))
	}

	// Tag-shaped text is not a reference: the cursor on one finds nothing.
	for _, marker := range []string{`{{ "<Card`, "{{/* <Card", "<!-- <Card", "<Code><Card", `"<Card title='script`} {
		off := strings.Index(refsPage, marker) + strings.Index(marker, "<Card") + 2
		l, ch := PositionOf(refsPage, off)
		var none []lspLocation
		c.call("textDocument/references", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": map[string]int{"line": l, "character": ch}, "context": map[string]any{"includeDeclaration": false}}, &none)
		if len(none) != 0 {
			t.Fatalf("%s: tag-shaped text answered references %+v", marker, none)
		}
	}
}

// workspaceEdit is the part of a WorkspaceEdit a rename answers.
type workspaceEdit struct {
	DocumentChanges []struct {
		Kind         string
		OldURI       string `json:"oldUri"`
		NewURI       string `json:"newUri"`
		TextDocument struct{ URI string }
		Edits        []struct {
			Range   lspRange
			NewText string
		}
	}
}

// apply applies one document's edits to its text, last first.
func apply(text string, edits []struct {
	Range   lspRange
	NewText string
}) string {
	sorted := slices.Clone(edits)
	slices.SortFunc(sorted, func(a, b struct {
		Range   lspRange
		NewText string
	}) int {
		return OffsetOf(text, b.Range.Start.Line, b.Range.Start.Character) - OffsetOf(text, a.Range.Start.Line, a.Range.Start.Character)
	})
	for _, e := range sorted {
		start, end := OffsetOf(text, e.Range.Start.Line, e.Range.Start.Character), OffsetOf(text, e.Range.End.Line, e.Range.End.Character)
		text = text[:start] + e.NewText + text[end:]
	}
	return text
}

// Renaming a component rewrites every call in its written form, the use:
// path, and renames the file and its locale variant; an alias tag and the
// tag-shaped text the engine does not call stay as they are.
func TestRename(t *testing.T) {
	dir := writeRefsSite(t)
	c, uri := startRefs(t, dir, true)

	var prep struct {
		Range       lspRange
		Placeholder string
	}
	c.call("textDocument/prepareRename", pos(uri, refsPage, "<x-card", 3), &prep)
	if prep.Placeholder != "x-card" {
		t.Fatalf("prepareRename: %+v", prep)
	}

	p := pos(uri, refsPage, "<x-card", 3)
	p["newName"] = "x-media-card"
	var we workspaceEdit
	c.call("textDocument/rename", p, &we)
	var renames []string
	results := map[string]string{}
	for _, ch := range we.DocumentChanges {
		if ch.Kind == resourceRename {
			if len(results) != 2 {
				t.Fatal("a file rename comes before the text edits that name its old URI")
			}
			renames = append(renames, filepath.Base(ch.OldURI)+"→"+filepath.Base(ch.NewURI))
			continue
		}
		pth, _ := uriToPath(ch.TextDocument.URI)
		b, _ := os.ReadFile(pth)
		results[filepath.Base(pth)] = apply(string(b), ch.Edits)
	}
	if want := []string{"Card.fr.html→MediaCard.fr.html", "Card.html→MediaCard.html"}; !slices.Equal(renames, want) {
		t.Fatalf("file renames %q, want %q", renames, want)
	}
	wantPage := strings.NewReplacer(
		"~/components/Card.html", "~/components/MediaCard.html",
		`<Card title="a">x</Card>`, `<MediaCard title="a">x</MediaCard>`,
		"<x-card", "<x-media-card",
		`<Card><brick:prop name="title">t</brick:prop></Card>`, `<MediaCard><brick:prop name="title">t</brick:prop></MediaCard>`,
	).Replace(refsPage)
	if results["index.html"] != wantPage {
		t.Fatalf("page after rename:\n%s\nwant:\n%s", results["index.html"], wantPage)
	}
	if results["Shell.html"] != "<main><MediaCard title=\"s\" /><Badge /></main>\n" {
		t.Fatalf("component after rename: %q", results["Shell.html"])
	}

	// Refusals: a kit component, a name that is not one, a taken name.
	badge := pathToURI(filepath.Join(dir, "components", "Shell.html"))
	shell := "<main><Card title=\"s\" /><Badge /></main>\n"
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": badge, "languageId": "html", "version": 1, "text": shell}})
	if msg := c.callErr("textDocument/prepareRename", pos(badge, shell, "<Badge", 2)); !strings.Contains(msg, "built into brick") {
		t.Fatalf("kit prepareRename: %s", msg)
	}
	for newName, want := range map[string]string{"card-x": "not a component name", "Shell": "<Shell> already exists", "x-shell": "<Shell> already exists"} {
		p["newName"] = newName
		if msg := c.callErr("textDocument/rename", p); !strings.Contains(msg, want) {
			t.Fatalf("rename to %s: %s, want %q", newName, msg, want)
		}
	}

	// A client that cannot rename files is refused, not given tag edits
	// that would leave every call pointing at a missing file.
	plain, uri2 := startRefs(t, dir, false)
	p2 := pos(uri2, refsPage, "<x-card", 3)
	p2["newName"] = "MediaCard"
	if msg := plain.callErr("textDocument/rename", p2); !strings.Contains(msg, "resourceOperations") {
		t.Fatalf("no file renames: %s", msg)
	}
}
