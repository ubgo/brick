package site

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/watch"
)

// fakeMD stands in for a markdown converter: it wraps paragraphs in <p>.
type fakeMD struct{}

func (fakeMD) Convert(src []byte, w io.Writer) error {
	_, err := io.WriteString(w, "<p>"+strings.TrimSpace(string(src))+"</p>")
	return err
}

func project(files ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for i := 0; i+1 < len(files); i += 2 {
		fsys[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return fsys
}

// blog is a small but complete project used by most tests.
func blog() fstest.MapFS {
	return project(
		"site.yaml", "title: Blog\n",
		"layouts/default.html", "<html>{{ .Title }}|{{ children }}</html>",
		"components/Card.html", "<b>{{ .Props.t }}</b>",
		"content/index.html", "---\ntitle: Home\n---\n{{ range .Site.Pages }}{{ if eq .Section `blog` }}<a href=\"{{ .URL }}\">{{ .Title }}</a>{{ end }}{{ end }}",
		"content/about.html", "---\ntitle: About\n---\n<Card t=\"about\" />",
		"content/about.fr.html", "---\ntitle: À propos\n---\nbonjour",
		"content/blog/index.html", "---\ntitle: Blog\n---\nlist",
		"content/blog/first.md", "---\ntitle: First\ndate: 2026-01-01\n---\nhello {{ .Title }}",
		"content/blog/second.html", "---\ntitle: Second\ndate: 2026-02-01\n---\ntwo",
		"content/blog/draft.html", "---\ntitle: Draft\ndraft: true\n---\nd",
		"content/blog/_notes.html", "not a page",
		"content/legal.html", "---\ntitle: Legal\nbrick:\n  url: /terms/\n---\nterms",
		"content/404.html", "---\ntitle: Missing\n---\nnot here",
		"static/logo.svg", "<svg/>",
	)
}

func newSite(t *testing.T, fsys fstest.MapFS, opts Options) *Site {
	t.Helper()
	opts.FS = fsys
	if opts.Engine.Markdown == nil {
		opts.Engine.Markdown = fakeMD{}
	}
	s, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func page(t *testing.T, s *Site, url string) string {
	t.Helper()
	res, err := s.Render(context.Background(), url)
	if err != nil {
		t.Fatalf("%s: %v", url, err)
	}
	b, err := res.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIndexAndRoutes(t *testing.T) {
	t.Parallel()
	s := newSite(t, blog(), Options{})
	var urls []string
	for _, r := range s.Routes() {
		urls = append(urls, r.URL+"="+r.Path)
	}
	want := "/=content/index.html /about/=content/about.html /blog/=content/blog/index.html /blog/first/=content/blog/first.md /blog/second/=content/blog/second.html /terms/=content/legal.html"
	if got := strings.Join(urls, " "); got != want {
		t.Fatalf("routes:\n got  %s\n want %s", got, want)
	}
	pages := s.Pages()
	if pages[0].Title != "Second" || pages[1].Title != "First" || pages[0].Section != "blog" {
		t.Fatalf("order: %+v", pages[:2])
	}
	if got := page(t, s, "/"); got != `<html>Home|<a href="/blog/second/">Second</a><a href="/blog/first/">First</a><a href="/blog/">Blog</a></html>` {
		t.Fatalf("home: %s", got)
	}
	drafts := newSite(t, blog(), Options{Drafts: true})
	if len(drafts.Routes()) != len(s.Routes())+1 {
		t.Fatal("Drafts did not add the draft")
	}
}

func TestMarkdownPage(t *testing.T) {
	t.Parallel()
	s := newSite(t, blog(), Options{})
	if got := page(t, s, "/blog/first/"); got != "<html>First|<p>hello {{ .Title }}</p></html>" {
		t.Fatalf("markdown: %q", got)
	}
	noMD, err := New(context.Background(), Options{FS: blog()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noMD.Render(context.Background(), "/blog/first/"); err != ErrNotFound {
		t.Fatalf("a .md page without a converter: %v", err)
	}
	if !strings.Contains(joinDiags(noMD.Diagnostics()), "needs brick.Options.Markdown") {
		t.Fatal("no warning for a skipped .md page")
	}
}

func TestLocales(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["content/only.fr.html"] = &fstest.MapFile{Data: []byte("seul")}
	s := newSite(t, fsys, Options{Locales: []string{"en", "fr"}})
	if got := page(t, s, "/fr/about/"); got != "<html>À propos|bonjour</html>" {
		t.Fatalf("fr variant: %q", got)
	}
	if got := page(t, s, "/about/"); got != "<html>About|<b>about</b></html>" {
		t.Fatalf("en: %q", got)
	}
	if got := page(t, s, "/fr/only/"); got != "<html>|seul</html>" {
		t.Fatalf("variant-only: %q", got)
	}
	if _, err := s.Render(context.Background(), "/only/"); err != ErrNotFound {
		t.Fatal("a variant-only page got a default-locale URL")
	}
}

func TestRouteErrors(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["content/clash.html"] = &fstest.MapFile{Data: []byte("---\nbrick:\n  url: /about\n---\nx")}
	_, err := New(context.Background(), Options{FS: fsys})
	if err == nil || !strings.Contains(err.Error(), "both have the URL /about/") {
		t.Fatalf("collision: %v", err)
	}
	var be *brick.Error
	if !errors.As(err, &be) || be.Code != CodeRoute {
		t.Fatalf("collision is not a *brick.Error with Code %q: %#v", CodeRoute, err)
	}
	bad := blog()
	bad["content/bad.html"] = &fstest.MapFile{Data: []byte("---\nbrick:\n  url: ../x\n---\nx")}
	if _, err := New(context.Background(), Options{FS: bad}); err == nil {
		t.Fatal("a bad url override was accepted")
	}
}

func TestCollections(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["content/blog/_collection.yaml"] = &fstest.MapFile{Data: []byte("fields:\n  title: {type: string, required: true}\n  date: {type: any, required: true}\n")}
	s := newSite(t, fsys, Options{})
	d := joinDiags(s.Diagnostics())
	if strings.Contains(d, "content/blog/index.html") {
		t.Fatalf("the section's index page is not a member of its collection: %s", d)
	}
	fsys["content/blog/undated.html"] = &fstest.MapFile{Data: []byte("---\ntitle: Undated\n---\nx")}
	d = joinDiags(newSite(t, fsys, Options{}).Diagnostics())
	if !strings.Contains(d, "content/blog/undated.html:1: error: frontmatter breaks the blog collection: date is required") {
		t.Fatalf("collection: %s", d)
	}
	if strings.Contains(d, "second.html") {
		t.Fatalf("a valid page reported: %s", d)
	}
}

func TestCacheAndInvalidation(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["components/Tagged.html"] = &fstest.MapFile{Data: []byte("---\ncache:\n  tags: [posts]\n---\nT")}
	fsys["content/tagged.html"] = &fstest.MapFile{Data: []byte("<Tagged />")}
	s := newSite(t, fsys, Options{})
	ctx := context.Background()
	a1, _ := s.Render(ctx, "/about/")
	a2, _ := s.Render(ctx, "/about/")
	if a1 != a2 {
		t.Fatal("second render missed the cache")
	}
	s.Render(ctx, "/blog/second/")
	s.Render(ctx, "/tagged/")

	// Editing a component evicts only the pages that read it.
	fsys["components/Card.html"].Data = []byte("<i>{{ .Props.t }}</i>")
	dropped, err := s.Apply(ctx, []watch.Change{{Path: "components/Card.html", Op: watch.Modified}})
	if err != nil || strings.Join(dropped, ",") != "/about/" {
		t.Fatalf("evict by file: %v %v", dropped, err)
	}
	if got := page(t, s, "/about/"); got != "<html>About|<i>about</i></html>" {
		t.Fatalf("after edit: %q", got)
	}
	if b, _ := s.Render(ctx, "/blog/second/"); b == nil {
		t.Fatal("unrelated page lost")
	}

	// Tags.
	if got := s.InvalidateTags("posts"); strings.Join(got, ",") != "/tagged/" {
		t.Fatalf("tags: %v", got)
	}

	// A body-only page edit keeps the index, so only that page goes.
	s.Render(ctx, "/about/")
	fsys["content/blog/second.html"].Data = []byte("---\ntitle: Second\ndate: 2026-02-01\n---\nTWO")
	dropped, _ = s.Apply(ctx, []watch.Change{{Path: "content/blog/second.html", Op: watch.Modified}})
	if strings.Join(dropped, ",") != "/blog/second/" {
		t.Fatalf("body edit: %v", dropped)
	}
	// A title edit changes the index, so everything goes.
	fsys["content/blog/second.html"].Data = []byte("---\ntitle: Renamed\ndate: 2026-02-01\n---\nTWO")
	dropped, _ = s.Apply(ctx, []watch.Change{{Path: "content/blog/second.html", Op: watch.Modified}})
	if strings.Join(dropped, ",") != "*" || !strings.Contains(page(t, s, "/"), "Renamed") {
		t.Fatalf("title edit: %v", dropped)
	}
	// A new page appears.
	fsys["content/new.html"] = &fstest.MapFile{Data: []byte("new")}
	if dropped, _ = s.Apply(ctx, []watch.Change{{Path: "content/new.html", Op: watch.Created}}); dropped[0] != "*" || !strings.Contains(page(t, s, "/new/"), "new") {
		t.Fatalf("create: %v", dropped)
	}
}

func TestHandler(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["components/Js.html"] = &fstest.MapFile{Data: []byte("<brick:client>x</brick:client><i></i>")}
	fsys["content/js.html"] = &fstest.MapFile{Data: []byte("<Js />")}
	s := newSite(t, fsys, Options{Dev: true})
	h := s.Handler()
	get := func(method, url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, url, nil))
		return rec
	}
	if r := get("GET", "/about/"); r.Code != 200 || !strings.Contains(r.Body.String(), `<b data-brick-loc="components/Card.html:1">about</b>`) || !strings.Contains(r.Body.String(), "EventSource") {
		t.Fatalf("page: %d %s", r.Code, r.Body)
	}
	if r := get("GET", "/about"); r.Code != 301 || r.Header().Get("Location") != "/about/" {
		t.Fatalf("redirect: %d %v", r.Code, r.Header())
	}
	if r := get("GET", "/logo.svg"); r.Code != 200 || r.Body.String() != "<svg/>" {
		t.Fatalf("static: %d %s", r.Code, r.Body)
	}
	if r := get("GET", "/nope/"); r.Code != 404 || !strings.Contains(r.Body.String(), "not here") {
		t.Fatalf("404: %d %s", r.Code, r.Body)
	}
	if r := get("GET", "/_brick/play/Card?t=played"); r.Code != 200 || !strings.Contains(r.Body.String(), `<b data-brick-loc="components/Card.html:1">played</b>`) {
		t.Fatalf("playground: %d %s", r.Code, r.Body)
	}
	if r := get("POST", "/about/"); r.Code != 405 {
		t.Fatalf("POST: %d", r.Code)
	}
	get("GET", "/js/")
	var asset string
	for _, f := range listFiles(t, s.Engine()) {
		asset = f
	}
	if r := get("GET", "/_brick/"+asset); r.Code != 200 || !strings.Contains(r.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset %s: %d", asset, r.Code)
	}
}

func TestLiveReload(t *testing.T) {
	t.Parallel()
	s := newSite(t, blog(), Options{Dev: true})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/_brick/live")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf) // ": connected"
	if !strings.Contains(string(buf[:n]), "connected") {
		t.Fatalf("stream: %q", buf[:n])
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.live.publish([]string{"/about/"})
	}()
	n, _ = resp.Body.Read(buf)
	if got := string(buf[:n]); got != "data: [\"/about/\"]\n\n" {
		t.Fatalf("event: %q", got)
	}
}

func TestExport(t *testing.T) {
	t.Parallel()
	s := newSite(t, blog(), Options{})
	dir := filepath.Join(t.TempDir(), "out")
	rep, err := s.Export(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pages != 6 || rep.Static != 1 {
		t.Fatalf("report: %+v", rep)
	}
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if read("about/index.html") != "<html>About|<b>about</b></html>" || read("index.html") == "" || read("terms/index.html") == "" || read("404.html") == "" || read("logo.svg") != "<svg/>" {
		t.Fatal("missing output")
	}
	first := read("blog/second/index.html")
	if _, err := s.Export(context.Background(), dir); err != nil {
		t.Fatalf("re-export: %v", err)
	}
	if read("blog/second/index.html") != first {
		t.Fatal("export is not deterministic")
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Export(context.Background(), other); err == nil {
		t.Fatal("export overwrote a directory it did not create")
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["content/broken.html"] = &fstest.MapFile{Data: []byte("{{ index .Params.x 1 }}")}
	fsys["content/typo.html"] = &fstest.MapFile{Data: []byte("<Crad />")}
	s := newSite(t, fsys, Options{})
	d, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	all := joinDiags(d)
	if !strings.Contains(all, "content/broken.html") || !strings.Contains(all, "unknown component <Crad>") {
		t.Fatalf("check:\n%s", all)
	}
}

// TestWatchApplies runs the watcher on a real directory: an edit shows up
// in the next render.
func TestWatchApplies(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(p, s string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("components/Card.html", "<b>one</b>")
	write("content/index.html", "<Card />")
	s, err := New(context.Background(), Options{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if got := page(t, s, "/"); got != "<b>one</b>" {
		t.Fatalf("before: %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Watch(ctx, watch.Options{Interval: 10 * time.Millisecond}) }()
	time.Sleep(50 * time.Millisecond)
	write("components/Card.html", "<b>two</b>")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if page(t, s, "/") == "<b>two</b>" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the edit never showed")
}

// A library passed as a changeable FS (a dev server's os.DirFS over a kit
// it is developing) is watched too: an edit to one of its components reloads
// the page that uses it, as an edit to the project's own components does.
func TestWatchAppliesLibraryEdits(t *testing.T) {
	t.Parallel()
	lib := t.TempDir()
	write := func(p, s string) {
		full := filepath.Join(lib, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(brick.LibraryManifest, "name: ui\n")
	write("components/Card.html", "<b>one</b>")
	s, err := New(context.Background(), Options{
		FS:     fstest.MapFS{"content/index.html": {Data: []byte("<Card />")}},
		Engine: brick.Options{Libraries: []brick.Library{{Name: "ui", FS: os.DirFS(lib)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := page(t, s, "/"); got != "<b>one</b>" {
		t.Fatalf("before: %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Watch(ctx, watch.Options{Interval: 10 * time.Millisecond}) }()
	time.Sleep(50 * time.Millisecond)
	write("components/Card.html", "<b>two</b>")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if page(t, s, "/") == "<b>two</b>" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the library edit never showed")
}

func joinDiags(ds []brick.Diagnostic) string {
	var parts []string
	for _, d := range ds {
		parts = append(parts, d.String())
	}
	return strings.Join(parts, "\n")
}

func listFiles(t *testing.T, e *brick.Engine) []string {
	t.Helper()
	var out []string
	entries, _ := e.Assets().(interface {
		ReadDir(string) ([]os.DirEntry, error)
	}).ReadDir("c")
	for _, en := range entries {
		out = append(out, "c/"+en.Name())
	}
	return out
}

func TestServeLifecycleAndDevErrors(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["content/broken.html"] = &fstest.MapFile{Data: []byte("<p>ok</p>{{ index .Params.x 1 }}")}
	s := newSite(t, fsys, Options{Dev: true})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/broken/", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `class="brick-error"`) || !strings.Contains(rec.Body.String(), "EventSource") {
		t.Fatalf("dev error: %d %s", rec.Code, rec.Body)
	}
	prod := newSite(t, fsys, Options{})
	rec = httptest.NewRecorder()
	prod.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/broken/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "brick-error") {
		t.Fatalf("prod error leaked: %d %s", rec.Code, rec.Body)
	}
	if got := string(injectLive([]byte("<p>no body</p>"), "")); !strings.HasPrefix(got, "<p>no body</p><script>") {
		t.Fatalf("inject: %s", got)
	}
	if statusOf(ErrNotFound) != 404 || statusOf(io.EOF) != 500 {
		t.Fatal("statusOf")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.ListenAndServe(ctx, "127.0.0.1:0") }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not stop")
	}
}

func TestIslandsThroughTheSite(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["components/Who.html"] = &fstest.MapFile{Data: []byte("---\nbrick:\n  island: true\n---\n<b>{{ with .Ctx.user }}{{ . }}{{ else }}guest{{ end }}</b>")}
	fsys["content/who.html"] = &fstest.MapFile{Data: []byte("<Who />")}
	s := newSite(t, fsys, Options{Request: func(r *http.Request) []brick.RenderOption {
		return []brick.RenderOption{brick.Provide("user", r.URL.Query().Get("u"))}
	}})
	h := s.Handler()
	for u, want := range map[string]string{"/who/?u=sara": "<b>sara</b>", "/who/?u=ali": "<b>ali</b>"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s: %s", u, rec.Body)
		}
	}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := s.Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "who", "index.html"))
	if !strings.Contains(string(b), "<b>guest</b>") {
		t.Fatalf("export: %s", b)
	}
}

func TestActionsThroughTheSite(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["components/Toggle.html"] = &fstest.MapFile{Data: []byte("---\nstate:\n  on: false\n---\n<brick:script>toggle</brick:script>\n<div><b>{{ .State.on }}</b><button brick:click=\"flip\">x</button></div>")}
	fsys["content/toggle.html"] = &fstest.MapFile{Data: []byte("<Toggle />")}
	s := newSite(t, fsys, Options{Engine: brick.Options{Scripting: flipScripting{}}})
	h := s.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/toggle/", nil))
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	if m == nil || !strings.Contains(rec.Body.String(), "<b>false</b>") {
		t.Fatalf("page: %s", rec.Body)
	}
	req := httptest.NewRequest("POST", "/_brick/action", strings.NewReader(`{"handle":"`+m[1]+`","token":"`+m[2]+`","action":"flip"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<b>true</b>") {
		t.Fatalf("action: %d %s", rec.Code, rec.Body)
	}
}

// flipScripting has one module with one action, flip.
type flipScripting struct{}

func (flipScripting) Compile(context.Context, brick.ScriptSource) (brick.ScriptModule, error) {
	return flipModule{}, nil
}

type flipModule struct{}

func (flipModule) Functions() []string { return []string{"flip"} }
func (flipModule) Call(_ context.Context, _ string, args ...any) (any, error) {
	st, _ := args[0].(map[string]any)
	on, _ := st["on"].(bool)
	return map[string]any{"on": !on}, nil
}

// A page kept in the cache across a rebuild still links working assets:
// the new engine serves the files the old one registered.
func TestCachedPageAssetsSurviveRebuild(t *testing.T) {
	fsys := project(
		"layouts/default.html", "<html>{{ assets }}{{ children }}</html>",
		"components/Box.html", "<brick:style>.box { color: red }</brick:style><b class=\"box\">b</b>",
		"content/a.html", "<brick:style>.a { color: blue }</brick:style><Box />",
		"content/b.html", "<p>b</p>",
	)
	s := newSite(t, fsys, Options{Dev: true})
	h := s.Handler()
	a := page(t, s, "/a/")
	links := regexp.MustCompile(`href="(/_brick/c/[^"]+)"`).FindAllStringSubmatch(a, -1)
	if len(links) != 2 {
		t.Fatalf("want the page's and Box's stylesheets: %s", a)
	}
	fsys["content/b.html"] = &fstest.MapFile{Data: []byte("<p>b2</p>")}
	if _, err := s.Apply(context.Background(), []watch.Change{{Path: "content/b.html", Op: watch.Modified}}); err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, l[1], nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s after a rebuild: %d", l[1], rec.Code)
		}
	}
}

// In Dev the live-reload stream is closed on pagehide and reopened from the
// back/forward cache, so cached pages never hold dev-server connections.
func TestLiveScriptReleasesItsStream(t *testing.T) {
	s := newSite(t, blog(), Options{Dev: true})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/about/", nil))
	for _, want := range []string{`new EventSource(b+"/` + liveEndpoint + `")`, `addEventListener("pagehide",()=>es.close())`, `if(e.persisted)open()`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("live script lacks %q", want)
		}
	}
}

// TestCheckReportsEachErrorOnce pins that a page error found by both the
// compile pass and the render pass is one diagnostic: the render copy must
// carry the bare message, not "file:line:col: message" a second time.
func TestCheckReportsEachErrorOnce(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["content/typo.html"] = &fstest.MapFile{Data: []byte("<Crad />")}
	s := newSite(t, fsys, Options{})
	d, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range d {
		if x.File == "content/typo.html" && strings.Contains(x.Message, "unknown component <Crad>") {
			n++
			if strings.Contains(x.Message, "content/typo.html") {
				t.Fatalf("message repeats its position: %s", x)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want 1 diagnostic, got %d:\n%s", n, joinDiags(d))
	}
}
