package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"go.starlark.net/starlark"

	"github.com/ubgo/brick"
	markdowngoldmark "github.com/ubgo/brick/contrib/markdown-goldmark"
	sanitizebluemonday "github.com/ubgo/brick/contrib/sanitize-bluemonday"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/examples/ui/demodb"
	"github.com/ubgo/brick/internal/format"
	"github.com/ubgo/brick/kit"
	"github.com/ubgo/brick/site"
	"github.com/ubgo/brick/upload/disk"
)

// fmtStressLineLength is short enough that most tags with two attributes
// wrap, so the render comparison covers wrapping across the whole tree.
const fmtStressLineLength = 40

// fmtStressLayouts are the layouts every example is formatted in: indent
// units that move nearly every line, so the comparison exercises the
// formatter on the whole tree, not only on the few lines the default
// layout changes, one of them with wrapping on (lineLength). Wrapping
// rides on an indent layout rather than a fourth pass because each layout
// renders every page of every example under -race, and a fourth pushed
// the package past go test's 10-minute limit; the inline list (fmt.inline)
// rides on the one-space indent for the same reason. Every layout is also
// checked for idempotence.
var fmtStressLayouts = []struct {
	name       string
	opts       format.Options
	idempotent bool
}{
	{fmt.Sprintf("indent=%q", format.DefaultIndent), format.Options{Indent: format.DefaultIndent}, true},
	{fmt.Sprintf(`indent="    ",lineLength=%d`, fmtStressLineLength), format.Options{Indent: "    ", LineLength: fmtStressLineLength}, true},
	{`indent=" ",inline`, format.Options{Indent: " ", Inline: fmtStressInline}, true},
}

// fmtStressInline is the inline layout's list: the phrasing elements and
// kit components the examples wrap prose in, plus block elements (div,
// section) so whole subtrees lose a level and the rendering check covers
// lines far from any inline element too.
var fmtStressInline = []string{"a", "span", "em", "strong", "b", "i", "code", "label", "button", "div", "section", "Icon", "Badge", "Button"}

// The sites whose every page is rendered before and after formatting. The
// kit is formatted too and offered as brick-kit, so examples/ui (a page per
// kit component) proves the kit's components render the same.
var fmtRenderSites = []string{"../../examples/site", "../../examples/ui", "../../examples/tailwind", "../../examples/engineonly/theme"}

// loadTree reads every file under dir into a MapFS, formatting each .html
// file with opts when raws is non-nil.
func loadTree(t *testing.T, fsys fs.FS, raws *rawSet, opts format.Options) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if raws != nil && strings.HasSuffix(p, fmtExt) {
			opts.IsRaw = raws.forFile(b)
			res, err := format.Format(b, opts)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				return nil
			}
			b = res.Src
		}
		m[p] = &fstest.MapFile{Data: b, Mode: 0o644}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// fmtFixedTime is the clock the stubbed globals report, so a page that
// shows the time renders the same bytes before and after formatting.
var fmtFixedTime = time.Date(2026, time.January, 2, 15, 4, 5, 0, time.UTC)

// exampleGlobals are deterministic stand-ins for the script capabilities
// the example hosts grant (examples/demosite: now; examples/ui: today,
// pause, sanitize_html), so pages that call them render in this test. The
// database (db) is the showcase's own seeded one (renderSite).
func exampleGlobals(policy func(string) string) starlark.StringDict {
	clock := func(layout string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
		return func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
			return starlark.String(fmtFixedTime.Format(layout)), nil
		}
	}
	return starlark.StringDict{
		"now":   starlark.NewBuiltin("now", clock(time.TimeOnly)),
		"today": starlark.NewBuiltin("today", clock(time.DateOnly)),
		// pause stands for slow work; the test has no reason to wait.
		"pause": starlark.NewBuiltin("pause", func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		}),
		"sanitize_html": starlark.NewBuiltin("sanitize_html", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var html string
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &html); err != nil {
				return nil, err
			}
			return starlark.String(policy(html)), nil
		}),
	}
}

// renderFailed stands for a page that does not render, before or after.
const renderFailed = "(render failed)"

// renderSite renders every route of the site in fsys, kit offered as kitFS.
// A page that fails maps to its error text with positions removed, so a
// page that fails before formatting must fail the same way after.
func renderSite(t *testing.T, fsys, kitFS fs.FS) map[string]string {
	t.Helper()
	ctx := context.Background()
	policy := sanitizebluemonday.New()
	// Every render gets a freshly seeded copy of the showcase's database,
	// as examples/ui grants it (db with exec), so the demo apps and data
	// components render their rows here too; nothing a render writes can
	// reach the render it is compared with.
	db, err := demodb.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	sc, err := scriptstarlark.New(scriptstarlark.Options{Globals: exampleGlobals(policy), DB: db, DBExec: true})
	if err != nil {
		t.Fatal(err)
	}
	// The upload demo needs a sink, as examples/ui gives it; files land in
	// the test's temporary directory.
	uploads, err := disk.New(disk.Options{Dir: t.TempDir(), URLPrefix: "/uploads/"})
	if err != nil {
		t.Fatal(err)
	}
	// The live board demo listens to a topic, so server push is on, as
	// examples/ui and the brick CLI turn it on.
	push, err := brick.NewPush(brick.PushOptions{})
	if err != nil {
		t.Fatal(err)
	}
	opts := brick.Options{Markdown: markdowngoldmark.New(), Sanitizer: policy, Scripting: sc, Uploads: uploads, Push: push}
	if _, err := fs.Stat(fsys, configFile); err == nil {
		cfg, err := brick.LoadConfig(fsys, configFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Apply(&opts, brick.Library{Name: kit.Name, FS: kitFS}); err != nil {
			t.Fatal(err)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	opts.Logger = logger
	s, err := site.New(ctx, site.Options{FS: fsys, Engine: opts, Drafts: true, Logger: logger})
	if err != nil {
		return map[string]string{"(site)": renderFailed}
	}
	out := map[string]string{}
	for _, r := range s.Routes() {
		res, err := s.Render(ctx, r.URL)
		if err != nil {
			out[r.URL] = renderFailed
			continue
		}
		// Fill renders the islands as a request would (a Result without
		// islands is just its bytes).
		b, err := s.Engine().Fill(ctx, res)
		if err != nil {
			out[r.URL] = renderFailed
			continue
		}
		out[r.URL] = string(b)
	}
	return out
}

var (
	wsRun = regexp.MustCompile(`[ \t\n\r\f]+`)
	// wsEq and wsGt are whitespace the formatter may drop inside a tag:
	// around '=' and before '>'.
	wsEq = regexp.MustCompile(` ?= ?`)
	wsGt = regexp.MustCompile(` >`)
)

// instanceIDRE is a component instance id: "b" and 8 hex digits of a hash
// over the calling file's source (instanceID in the engine), so any edit to
// a file, whitespace included, renumbers the ids of what it calls. Kit
// components build element ids on it (bk-f + the id), so it is matched
// inside words too.
var instanceIDRE = regexp.MustCompile(`b[0-9a-f]{8}\b`)

// sealedRE is a value that differs on every render of the same page: a
// stateful island's state handle, its action token and a lazy island's
// ticket are fresh random values each time, so they are replaced before
// comparing. The same handle and token also ride in the hidden fields a
// brick:submit form carries for posting with scripts off. A
// listening root's listen token is sealed afresh on every render too
// .
var sealedRE = regexp.MustCompile(`(data-brick-(?:state|token|lazy|listen-token)|name="brick:(?:state|token)" value)="[^"]*"`)

// collapse is how a browser reads whitespace outside <pre>: every run is
// one space. The formatter only changes the length of runs (and drops
// them inside tags around '=' and before '>'), so collapsed output must
// match exactly. Instance ids are renamed by first appearance, so the
// comparison still sees which elements share an id.
func collapse(s string) string {
	s = sealedRE.ReplaceAllString(s, `$1="…"`)
	ids := map[string]string{}
	s = instanceIDRE.ReplaceAllStringFunc(s, func(id string) string {
		if n, ok := ids[id]; ok {
			return n
		}
		n := fmt.Sprintf("ID%d", len(ids))
		ids[id] = n
		return n
	})
	s = wsRun.ReplaceAllString(s, " ")
	return wsGt.ReplaceAllString(wsEq.ReplaceAllString(s, "="), ">")
}

// TestFmtPreservesRendering is the formatter's guarantee: every page of
// every example site, and every kit component they use, renders the same
// after formatting as before (up to whitespace-run length), and formatting
// is idempotent.
//
// Every site render (one before formatting per site, one per layout per
// site) is its own parallel subtest: each builds its own engine, database,
// upload sink and push hub (renderSite), so they share nothing, and under
// -race the renders dominate the run. A layout's render compares against
// its site's before-render once that is ready (siteRender.done).
func TestFmtPreservesRendering(t *testing.T) {
	if testing.Short() {
		t.Skip("renders every example page once per layout, plus once unformatted")
	}
	raws := newRawSet()
	if err := raws.addFS(kit.FS(), ""); err != nil {
		t.Fatal(err)
	}
	for _, dir := range fmtRenderSites {
		if err := raws.addSite(dir); err != nil {
			t.Fatal(err)
		}
	}
	kitPlain := loadTree(t, kit.FS(), nil, format.Options{})
	befores := map[string]*siteRender{}
	for _, dir := range fmtRenderSites {
		befores[dir] = &siteRender{done: make(chan struct{})}
	}
	tallies := make([]layoutTally, len(fmtStressLayouts))
	// The group returns once every parallel render inside it has finished.
	t.Run("render", func(t *testing.T) {
		for _, dir := range fmtRenderSites {
			t.Run("before/"+filepath.Base(dir), func(t *testing.T) {
				t.Parallel()
				b := befores[dir]
				defer close(b.done)
				b.pages = renderSite(t, loadTree(t, os.DirFS(dir), nil, format.Options{}), kitPlain)
			})
		}
		for i, layout := range fmtStressLayouts {
			kitFmt := loadTree(t, kit.FS(), raws, layout.opts)
			if layout.idempotent {
				checkIdempotent(t, "kit", kitFmt, raws, layout.opts)
			}
			tally := &tallies[i]
			for _, dir := range fmtRenderSites {
				t.Run(filepath.Base(dir)+"/"+layout.name, func(t *testing.T) {
					t.Parallel()
					formatted := loadTree(t, os.DirFS(dir), raws, layout.opts)
					if layout.idempotent {
						checkIdempotent(t, dir, formatted, raws, layout.opts)
					}
					after := renderSite(t, formatted, kitFmt)
					b := befores[dir]
					<-b.done
					if b.pages == nil {
						t.Fatalf("%s did not render before formatting (see before/%s)", dir, filepath.Base(dir))
					}
					before := b.pages
					if len(before) != len(after) {
						t.Fatalf("%d pages before formatting, %d after", len(before), len(after))
					}
					pages, changed, failing := 0, 0, 0
					for url, bp := range before {
						a := after[url]
						pages++
						if a != bp {
							changed++
						}
						if bp == renderFailed {
							failing++
						}
						if collapse(a) != collapse(bp) {
							t.Errorf("%s renders differently after formatting:\n%s", url, firstDiff(collapse(bp), collapse(a)))
						}
					}
					tally.add(pages, changed, failing)
				})
			}
		}
	})
	for i, layout := range fmtStressLayouts {
		tl := &tallies[i]
		t.Logf("%s: %d pages rendered the same, %d of them with different whitespace runs", layout.name, tl.pages, tl.changed)
		// A page that fails before and after formatting proves nothing
		// about the formatter, so every page must render: the showcase's
		// database and the host globals are all provided above.
		if tl.failing > 0 {
			t.Errorf("%s: %d of %d pages fail to render before and after formatting", layout.name, tl.failing, tl.pages)
		}
	}
}

// siteRender is one site's render before formatting, shared by the
// parallel subtests that compare against it: pages is written once by the
// before subtest, which closes done when it ends (nil pages then means it
// failed).
type siteRender struct {
	done  chan struct{}
	pages map[string]string
}

// layoutTally counts one layout's pages across the parallel site
// subtests that add to it.
type layoutTally struct {
	mu                      sync.Mutex
	pages, changed, failing int
}

func (l *layoutTally) add(pages, changed, failing int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pages += pages
	l.changed += changed
	l.failing += failing
}

// checkIdempotent formats every formatted .html file again with the same
// options: nothing moves.
func checkIdempotent(t *testing.T, label string, m fstest.MapFS, raws *rawSet, opts format.Options) {
	t.Helper()
	for p, f := range m {
		if !strings.HasSuffix(p, fmtExt) {
			continue
		}
		opts.IsRaw = raws.forFile(f.Data)
		res, err := format.Format(f.Data, opts)
		if err != nil {
			t.Errorf("%s/%s: second format: %v", label, p, err)
			continue
		}
		if !bytes.Equal(res.Src, f.Data) {
			t.Errorf("%s/%s: formatting is not idempotent:\n%s", label, p, format.Diff(p, f.Data, res.Src))
		}
	}
}

// firstDiff shows where two strings part, with some context.
func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := max(0, i-120)
	return "before: …" + a[lo:min(len(a), i+120)] + "\nafter:  …" + b[lo:min(len(b), i+120)]
}
