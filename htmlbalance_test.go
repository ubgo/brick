package brick

import (
	"context"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick/internal/lex"
)

// balance runs the balance pass over one body and returns its messages as
// "line:col message".
func balance(t *testing.T, body string) []string {
	t.Helper()
	toks, err := lex.Scan([]byte(body), lex.Options{IsRaw: func(n string, _ []lex.Attr) bool { return n == "brick:script" }})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range checkBalance("f.html", 1, toks) {
		if d.Code != CodeHTMLBalance || d.Severity != SeverityWarning {
			t.Fatalf("diagnostic %+v is not an html-balance warning", d)
		}
		out = append(out, strings.TrimPrefix(d.String(), "f.html:"))
	}
	return out
}

func TestBalanceReportsUnclosedAndStray(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ body, want string }{
		{"<div>\n  <section>x</div>", "2:13: warning: </div> closes <section> (line 2), which is still open"},
		{"<p>a</p>\n<div>x", "2:1: warning: <div> is never closed"},
		{"<p>a</p>\n</span>", "2:1: warning: </span> has no open <span> to close"},
		// A branch that breaks on its own is still reported.
		{"{{ if .X }}<div><b></div>{{ end }}", "1:20: warning: </div> closes <b> (line 1), which is still open"},
		// A range body is its own scope: what opens in it must close in it.
		{"<ul>{{ range .L }}<div>{{ end }}</ul>", "1:19: warning: <div> is never closed"},
		// So is a component body and a block.
		{"<Card><div></Card>", "1:7: warning: <div> is never closed"},
		{`<brick:prop name="h"><b></brick:prop>`, "1:22: warning: <b> is never closed"},
	} {
		got := balance(t, c.body)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.body, got, c.want)
		}
	}
}

func TestBalanceValidMarkupIsQuiet(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		// Void elements, self-closing SVG, optional close tags.
		`<p>a<br>b<img src="x"><input name=a></p><svg><path d="M0"/></svg>`,
		`<ul><li>a<li>b</ul><p>one<p>two`,
		`<table><tr><td>a<td>b<tr><td>c</table>`,
		`<select><option>a<option>b</select><dl><dt>t<dd>d</dl>`,
		// Text that is not markup.
		`<script>if (a < b) { x = "<div>" }</script><style>a>b{}</style>`,
		`<!-- <div> --><!DOCTYPE html><textarea><div></textarea><title><b></title>`,
		`a < b and c <= d`,
		// Actions inside tags and attribute values.
		`<div class="{{ if .A }}x{{ end }}" {{ attrs }}>{{ "</div>" }}</div>`,
		`<td{{ with .A }} class="{{ . }}"{{ end }}>x</td>`,
		`<input {{ if .C }}checked{{ end }}>`,
		// The conditional wrapper: paths that fail are dropped.
		`{{ if .H }}<a href="{{ .H }}">{{ else }}<span>{{ end }}x{{ if .H }}</a>{{ else }}</span>{{ end }}`,
		`{{ if .W }}<div class="w">{{ end }}x{{ if .W }}</div>{{ end }}`,
		`{{ with .X }}<b>{{ . }}</b>{{ else }}<i>none</i>{{ end }}`,
		`{{ range .L }}<tr><td>{{ . }}</td></tr>{{ else }}<tr><td>none</td></tr>{{ end }}`,
		// A computed name, the tag built-in and a component in between.
		`<h{{ .N }}>x</h{{ .N }}>{{ tag "h2" }}y{{ endtag "h2" }}<div><Card>c</Card></div>`,
		"<brick:script>def f():\n  return \"<div>\"\n</brick:script><p>x</p>",
		// A block keyword crossing a tag boundary: the pass gives up quietly.
		`<div {{ if .A }}class="a">{{ else }}>{{ end }}`,
	} {
		if got := balance(t, body); len(got) != 0 {
			t.Errorf("%q: unexpected %q", body, got)
		}
	}
}

// The warning reaches a check, carries the file's own line numbers (after
// frontmatter), fails nothing, and leaves render output unchanged.
func TestBalanceInCompileAll(t *testing.T) {
	t.Parallel()
	page := "---\ntitle: T\n---\n<main>\n  <div>x\n</main>"
	e := newEngine(t, Options{FS: theme("content/a.html", page)})
	r, err := e.CompileAll(context.Background(), "content")
	if err != nil {
		t.Fatal(err)
	}
	if r.Err() != nil {
		t.Fatalf("balance warnings must not fail a check: %v", r.Err())
	}
	var got []string
	for _, d := range r.Diagnostics {
		if d.Code == CodeHTMLBalance {
			got = append(got, d.String())
		}
	}
	want := "content/a.html:6:1: warning: </main> closes <div> (line 5), which is still open"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if out := render(t, e, "content/a.html", nil); !strings.Contains(out, "<main>\n  <div>x\n</main>") {
		t.Fatalf("output changed: %q", out)
	}
}

// The pass must stay quiet on every real theme in the repository: a warning
// people learn to ignore is worse than none. Each tree is loaded as its site
// loads it (the kit as a library, the site's roots), so raw components are
// recognized.
func TestBalanceQuietOnKitAndExamples(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		dir    string
		roots  []string
		prefix string
	}{
		{"kit", nil, ""},
		{"examples/site", nil, "Kit"},
		{"examples/ui", []string{"components", "apps", "demos"}, ""},
		{"examples/engineonly/theme", nil, ""},
		{"examples/tailwind", nil, ""},
	} {
		fsys := fstest.MapFS{}
		loadTree(t, fsys, c.dir, "")
		loadTree(t, fsys, "kit", "kit")
		e := newEngine(t, Options{FS: fsys, Roots: c.roots, Libraries: []Library{{Dir: "kit", Prefix: c.prefix}}})
		files := 0
		err := fs.WalkDir(os.DirFS(c.dir), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, htmlExt) {
				return err
			}
			files++
			for _, w := range e.htmlBalance(p, fsys[p].Data) {
				t.Errorf("%s/%s", c.dir, w)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if files == 0 {
			t.Fatalf("%s: no .html files found", c.dir)
		}
	}
}

// loadTree copies a directory from disk into fsys under prefix.
func loadTree(t *testing.T, fsys fstest.MapFS, dir, prefix string) {
	t.Helper()
	err := fs.WalkDir(os.DirFS(dir), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path.Join(dir, p))
		if err != nil {
			return err
		}
		fsys[path.Join(prefix, p)] = &fstest.MapFile{Data: b}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
