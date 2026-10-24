package brick

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"
)

// libTheme is a project plus a local library "ui/" whose components import
// siblings and data through ~/ (the library root) and which declares a
// global stylesheet component.
func libTheme(extra ...string) fstest.MapFS {
	files := []string{
		"ui/brick-library.yaml", "name: acme-ui\nglobal: [Tokens]\n",
		"ui/components/Tokens.html", "<brick:style>:root { --acme: 1 }</brick:style><template class=\"acme-tokens\"></template>",
		"ui/components/Badge.html", "---\nuse:\n  Dot: ~/components/Dot.html\n  words: ~/data/words.yaml\n---\n<brick:style>.b{}</brick:style><b class=\"b\"><Dot />{{ .Data.words.hi }} {{ children }}</b>",
		"ui/components/Dot.html", "<i>•</i>",
		"ui/components/forms/Field.html", "<label>{{ children }}</label>",
		"ui/data/words.yaml", "hi: hello\n",
	}
	return theme(append(files, extra...)...)
}

// A library's components are used by name, with no import, and ~/ inside
// the library means the library's root: its sibling components and data
// resolve there, and subfolders do not change a name.
func TestLibraryComponentsByName(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: libTheme("content/a.html", `<Badge>new</Badge><Field>x</Field>`), Libraries: []Library{{Dir: "ui"}}})
	if got, want := render(t, e, "content/a.html", nil), `<b class="b"><i>•</i>hello new</b><label>x</label>`; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// The project always wins a name, an earlier library wins over a later
// one, and each shadowing is logged; a prefix keeps both.
func TestLibraryPrecedenceAndPrefix(t *testing.T) {
	t.Parallel()
	fsys := libTheme(
		"components/Badge.html", "<em>project</em>",
		"ui2/brick-library.yaml", "name: other\n",
		"ui2/components/Dot.html", "<i>other</i>",
		"ui2/components/Pill.html", "<span>pill</span>",
		"content/a.html", `<Badge />|<Dot />|<Pill />`,
		"content/b.html", `<AcmeBadge>p</AcmeBadge>|<Badge />`,
	)
	e := newEngine(t, Options{FS: fsys, Libraries: []Library{{Dir: "ui"}, {Dir: "ui2"}}})
	if got, want := render(t, e, "content/a.html", nil), `<em>project</em>|<i>•</i>|<span>pill</span>`; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	var shadowed []string
	for _, w := range e.Warnings() {
		if w.Code == CodeShadow {
			shadowed = append(shadowed, w.Message)
		}
	}
	all := strings.Join(shadowed, "\n")
	if len(shadowed) != 2 || !strings.Contains(all, "<Badge> from library acme-ui is shadowed by ~/components/Badge.html") || !strings.Contains(all, "<Dot> from library other is shadowed") {
		t.Fatalf("shadowing: %q", shadowed)
	}
	e = newEngine(t, Options{FS: fsys, Libraries: []Library{{Dir: "ui", Prefix: "Acme"}}})
	if got, want := render(t, e, "content/b.html", nil), `<b class="b"><i>•</i>hello p</b>|<em>project</em>`; got != want {
		t.Fatalf("prefix: got %q, want %q", got, want)
	}
	// A folder can be listed once, even with a different prefix.
	if _, err := New(Options{FS: fsys, Libraries: []Library{{Dir: "ui"}, {Dir: "ui", Prefix: "Acme"}}}); err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("same folder twice: %v", err)
	}
}

// Only limits a library to the named components; a name it lacks is an
// error, never a silently smaller library.
func TestLibraryOnly(t *testing.T) {
	t.Parallel()
	fsys := libTheme("content/a.html", `<Dot />`, "content/b.html", `<Badge />`)
	e := newEngine(t, Options{FS: fsys, Libraries: []Library{{Dir: "ui", Only: []string{"Dot"}}}})
	if got := render(t, e, "content/a.html", nil); got != "<i>•</i>" {
		t.Fatalf("only: %q", got)
	}
	if err := renderErr(t, e, "content/b.html"); err == nil || !strings.Contains(err.Error(), "<Badge>") {
		t.Fatalf("a component outside only must be unknown: %v", err)
	}
	e = newEngine(t, Options{FS: fsys, Libraries: []Library{{Dir: "ui", Only: []string{"Nope"}}}})
	if err := renderErr(t, e, "content/a.html"); err == nil || !strings.Contains(err.Error(), `only: it has no component "Nope"`) {
		t.Fatalf("unknown only name: %v", err)
	}
}

// A folder without a manifest, or a manifest with an unknown key, is an
// error naming the library.
func TestLibraryManifestErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		fsys fstest.MapFS
		want string
	}{
		"no manifest": {theme("ui/components/A.html", "a", "content/a.html", "<A />"), "no brick-library.yaml in ~/ui"},
		"unknown key": {theme("ui/brick-library.yaml", "name: x\nglobals: [T]\n", "content/a.html", "<A />"), "field globals not found"},
		"bad global":  {theme("ui/brick-library.yaml", "global: [lower]\n", "content/a.html", "<A />"), `global: "lower" is not a component name`},
	} {
		e := newEngine(t, Options{FS: tc.fsys, Libraries: []Library{{Dir: "ui"}}})
		if err := renderErr(t, e, "content/a.html"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	for _, tc := range []struct {
		lib  Library
		want string
	}{
		{Library{}, "set Dir or FS"},
		{Library{Dir: "ui", FS: fstest.MapFS{}}, "not both"},
		{Library{FS: fstest.MapFS{}}, "needs a Name"},
		{Library{Name: "../x", FS: fstest.MapFS{}}, "needs a Name"},
		{Library{Dir: "../x"}, "Options.Libraries[0]"},
		{Library{Dir: "ui", Prefix: "acme"}, "Prefix"},
	} {
		if _, err := New(Options{FS: theme(), Libraries: []Library{tc.lib}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v, want %q", tc.lib, err, tc.want)
		}
	}
}

// ~/ inside a library is the library root, so a library cannot read the
// project that uses it.
func TestLibraryCannotReachProject(t *testing.T) {
	t.Parallel()
	fsys := libTheme(
		"ui/components/Peek.html", "---\nuse:\n  secret: ~/data/secret.yaml\n---\n{{ .Data.secret.v }}",
		"data/secret.yaml", "v: project-secret\n",
		"content/a.html", `<Peek />`,
	)
	e := newEngine(t, Options{FS: fsys, Libraries: []Library{{Dir: "ui"}}})
	err := renderErr(t, e, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "ui/data/secret.yaml") {
		t.Fatalf("~/ in a library must resolve inside it: %v", err)
	}
}

// A page using a library component links the library's global
// components' assets (no <Theme /> in the layout); a page that uses none
// does not; the global's template is never rendered.
func TestLibraryGlobalAssets(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: libTheme(
		"content/a.html", `{{ assets }}<Badge>x</Badge>`,
		"content/b.html", `{{ assets }}<p>plain</p>`,
	), Libraries: []Library{{Dir: "ui"}}})
	a := render(t, e, "content/a.html", nil)
	if !strings.Contains(a, "/_brick/c/Tokens.") || !strings.Contains(a, "/_brick/c/Badge.") || strings.Contains(a, "acme-tokens") {
		t.Fatalf("a: %s", a)
	}
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil || !strings.Contains(strings.Join(r.Files, " "), "ui/components/Tokens.html") {
		t.Fatalf("the global is a dependency of the page: %v %v", r.Files, err)
	}
	if b := render(t, e, "content/b.html", nil); strings.Contains(b, "Tokens") {
		t.Fatalf("b: %s", b)
	}
}

// An embedded library (a Go package's FS) is mounted at brick_libs/<name>/
// and works exactly like a folder one, globals included.
func TestLibraryEmbedded(t *testing.T) {
	t.Parallel()
	kit := fstest.MapFS{
		"brick-library.yaml":      {Data: []byte("name: tiny-kit\nglobal: [Base]\n")},
		"components/Base.html":    {Data: []byte("<brick:style>:root{--t:1}</brick:style><template></template>")},
		"components/Chip.html":    {Data: []byte("---\nuse:\n  Dot: ~/components/Dot.html\n---\n<span><Dot />{{ children }}</span>")},
		"components/Dot.html":     {Data: []byte("<i>.</i>")},
		"components/sub/Tag.html": {Data: []byte("<em>{{ children }}</em>")},
	}
	e := newEngine(t, Options{FS: theme("content/a.html", `{{ assets }}<Chip>c</Chip><Tag>t</Tag>`), Libraries: []Library{{Name: "tiny-kit", FS: kit}}})
	got := render(t, e, "content/a.html", nil)
	if !strings.Contains(got, `<span><i>.</i>c</span><em>t</em>`) || !strings.Contains(got, "/_brick/c/Base.") {
		t.Fatalf("got %s", got)
	}
	// Only embedded libraries and no project FS: still works.
	e = newEngine(t, Options{Libraries: []Library{{Name: "tiny-kit", FS: kit}}})
	out, err := e.Render(context.Background(), "content/x.html", nil, WithSource([]byte(`<Chip>z</Chip>`)))
	if err != nil || string(out) != `<span><i>.</i>z</span>` {
		t.Fatalf("no project FS: %q %v", out, err)
	}
	if _, err := New(Options{Libraries: []Library{{Name: "k", FS: kit}, {Name: "k", FS: kit}}}); err == nil || !strings.Contains(err.Error(), `two embedded libraries are named "k"`) {
		t.Fatalf("duplicate mount: %v", err)
	}
}

// Inside a vendored module, ~/ means the nearest folder holding a
// brick-library.yaml — so a single component imported from a library
// resolves its siblings — and the module root when there is none (files
// vendored before libraries existed keep working).
func TestVendoredLibraryRoot(t *testing.T) {
	t.Parallel()
	mod := "brick_modules/github.com/acme/mono@v1.0.0/"
	e := newEngine(t, Options{FS: theme(
		mod+"ui/brick-library.yaml", "name: acme\n",
		mod+"ui/components/Card.html", "---\nuse:\n  Dot: ~/components/Dot.html\n---\n<div><Dot />{{ children }}</div>",
		mod+"ui/components/Dot.html", "<i>ui</i>",
		mod+"old/Legacy.html", "---\nuse:\n  Dot: ~/old/Dot.html\n---\n<Dot />",
		mod+"old/Dot.html", "<i>old</i>",
		"content/a.html", "---\nuse:\n  Card: github.com/acme/mono/ui/components/Card.html@v1.0.0\n  Legacy: github.com/acme/mono/old/Legacy.html@v1.0.0\n---\n<Card>c</Card><Legacy />",
	)})
	if got, want := render(t, e, "content/a.html", nil), `<div><i>ui</i>c</div><i>old</i>`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// brick.yaml's libraries: a ~/ folder, a remote library (its vendored
// folder), and a bare name picked among the libraries the host offers;
// prefix and only apply; an unknown bare name is an error listing the
// offered ones.
func TestConfigLibraries(t *testing.T) {
	t.Parallel()
	var c Config
	src := "libraries:\n  - ~/ui\n  - github.com/acme/ui@v1.2.0\n  - {from: github.com/acme/mono/kit@v2.0.0, prefix: Acme, only: [Button]}\n  - tiny-kit\n"
	if err := yaml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	var opts Options
	offered := Library{Name: "tiny-kit", FS: fstest.MapFS{}}
	if err := c.Apply(&opts, offered); err != nil {
		t.Fatal(err)
	}
	got := opts.Libraries
	if len(got) != 4 || got[0].Dir != "ui" || got[1].Dir != "brick_modules/github.com/acme/ui@v1.2.0" || got[2].Dir != "brick_modules/github.com/acme/mono@v2.0.0/kit" || got[2].Prefix != "Acme" || len(got[2].Only) != 1 || got[3].Name != "tiny-kit" || got[3].FS == nil {
		t.Fatalf("libraries: %+v", got)
	}
	c = Config{Libraries: []LibrarySpec{{From: "nope"}}}
	if err := c.Apply(&Options{}, offered); err == nil || !strings.Contains(err.Error(), `"nope" is not`) || !strings.Contains(err.Error(), "tiny-kit") {
		t.Fatalf("unknown bare name: %v", err)
	}
}

// New leaves the caller's Options.Libraries as they were, so one Options
// builds any number of engines: the site layer rebuilds from the same
// Options on every change, and an embedded library must still be an FS the
// second time.
func TestLibraryOptionsReusable(t *testing.T) {
	t.Parallel()
	lib := fstest.MapFS{
		LibraryManifest:       {Data: []byte("name: ui\n")},
		"components/Dot.html": {Data: []byte("<i>•</i>")},
	}
	opts := Options{FS: theme("content/a.html", `<Dot />`), Libraries: []Library{{Name: "ui", FS: lib}}}
	for i := range 2 {
		e := newEngine(t, opts)
		if got := render(t, e, "content/a.html", nil); got != "<i>•</i>" {
			t.Fatalf("engine %d: %q", i+1, got)
		}
	}
	if opts.Libraries[0].FS == nil || opts.Libraries[0].Dir != "" {
		t.Fatalf("New rewrote the caller's library: %+v", opts.Libraries[0])
	}
}
