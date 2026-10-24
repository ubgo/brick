package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick/watch"
)

// The exact pages the site guides show.
const (
	blogPage = `<!doctype html>
<html>
<head><title>Blog · Field Notes</title></head>
<body>
<nav><a href="/">Home</a> <a href="/blog/">Blog</a> <a href="/about/">About</a></nav>
<main><h1>Blog</h1>
<ul>
  <li><a class="post" href="/blog/tide-tables/">Reading tide tables</a> · 1 Oct 2026</li>
  <li><a class="post" href="/blog/first-light/">First light</a> · 1 Sep 2026</li>
</ul>
</main>
</body>
</html>
`
	tidePage = `<!doctype html>
<html>
<head><title>Reading tide tables · Field Notes</title></head>
<body>
<nav class="toc"><a class="h1" href="#reading-tide-tables">Reading tide tables</a>
<a class="h2" href="#spring-tides">Spring tides</a>
<a class="h2" href="#neap-tides">Neap tides</a>
</nav>
<main><h1 id="reading-tide-tables">Reading tide tables</h1>
<p>Two highs a day, give or take.</p>
<h2 id="spring-tides">Spring tides</h2>
<p>Bigger range at full and new moon.</p>
<h2 id="neap-tides">Neap tides</h2>
<p>Smaller range in between.</p>
</main>
</body>
</html>
`
	playPage = `<!doctype html><html><head><meta charset="utf-8"><title>PostLink · playground</title></head><body><a data-brick-loc="components/PostLink.html:6" class="post" href="/x/">Hi</a>`
)

func open(t *testing.T, dev, drafts bool) http.Handler {
	t.Helper()
	s, err := newSite(context.Background(), themeDir, dev, drafts)
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
}

// get serves one request through the site's real handler.
func get(t *testing.T, h http.Handler, url string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code, rec.Body.String()
}

// Every file in content/ is a page at the URL its path names, and
// brick: {url:} moves one.
func TestRoutes(t *testing.T) {
	s, err := newSite(context.Background(), themeDir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range s.Routes() {
		got = append(got, r.URL+" "+r.Path)
	}
	want := []string{
		"/ content/index.html",
		"/about/ content/about.html",
		"/blog/ content/blog/index.html",
		"/blog/first-light/ content/blog/first-light.html",
		"/blog/tide-tables/ content/blog/tide-tables.md",
		"/terms/ content/legal.html",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("routes:\n%s", strings.Join(got, "\n"))
	}
}

func TestBlogIndex(t *testing.T) {
	code, body := get(t, open(t, false, false), "/blog/")
	if code != http.StatusOK || body != blogPage {
		t.Fatalf("%d\n%s", code, body)
	}
}

func TestMarkdownTableOfContents(t *testing.T) {
	code, body := get(t, open(t, false, false), "/blog/tide-tables/")
	if code != http.StatusOK || body != tidePage {
		t.Fatalf("%d\n%s", code, body)
	}
}

// An unknown URL answers 404 with content/404.html in the layout.
func TestNotFound(t *testing.T) {
	code, body := get(t, open(t, false, false), "/nope/")
	if code != http.StatusNotFound || !strings.Contains(body, "<h1>Nothing here</h1>") || !strings.Contains(body, "<title>Not found · Field Notes</title>") {
		t.Fatalf("%d\n%s", code, body)
	}
}

// A draft is a page only when Drafts is on (brick serve), never in an export.
func TestDrafts(t *testing.T) {
	if _, body := get(t, open(t, false, false), "/blog/"); strings.Contains(body, "Half written") {
		t.Fatal("a draft is listed without Drafts")
	}
	if code, _ := get(t, open(t, false, false), "/blog/half-written/"); code != http.StatusNotFound {
		t.Fatalf("draft page without Drafts: %d", code)
	}
	if _, body := get(t, open(t, false, true), "/blog/"); !strings.Contains(body, `<li><a class="post" href="/blog/half-written/">Half written</a> · 10 Oct 2026</li>`) {
		t.Fatalf("draft missing with Drafts:\n%s", body)
	}
}

// themeFS copies the theme into memory, so a test can add a file.
func themeFS(t *testing.T) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	err := fs.WalkDir(os.DirFS(themeDir), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(filepath.Join(themeDir, p))
		m[p] = &fstest.MapFile{Data: b}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A post that breaks blog/_collection.yaml is a diagnostic naming the file
// and the field; the site still builds.
func TestCollectionSchema(t *testing.T) {
	ctx := context.Background()
	s, err := newSiteFS(ctx, themeFS(t), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := s.Diagnostics(); len(d) != 0 {
		t.Fatalf("the example itself has diagnostics: %v", d)
	}
	m := themeFS(t)
	m["content/blog/untitled.html"] = &fstest.MapFile{Data: []byte("---\ndate: 2026-10-12\n---\n<p>No title.</p>\n")}
	s, err = newSiteFS(ctx, m, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range s.Diagnostics() {
		got = append(got, d.File+": "+d.Message)
	}
	want := []string{"content/blog/untitled.html: frontmatter breaks the blog collection: title is required"}
	if !slices.Equal(got, want) {
		t.Fatalf("diagnostics: %q", got)
	}
}

// Export writes every route as <url>/index.html plus 404.html; drafts stay out.
func TestExport(t *testing.T) {
	ctx := context.Background()
	s, err := newSite(ctx, themeDir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rep, err := s.Export(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".brick-export", "404.html", "about/index.html", "blog/first-light/index.html", "blog/index.html", "blog/tide-tables/index.html", "index.html", "refresh.js", "terms/index.html"}
	if !slices.Equal(files, want) || rep.Pages != 6 {
		t.Fatalf("report %+v, files:\n%s", rep, strings.Join(files, "\n"))
	}
	b, err := os.ReadFile(filepath.Join(dir, "blog/index.html"))
	if err != nil || string(b) != blogPage {
		t.Fatalf("exported blog/index.html:\n%s", b)
	}
	// Exporting again into the same folder is allowed (it holds the marker).
	if _, err := s.Export(ctx, dir); err != nil {
		t.Fatal(err)
	}
}

// The page cache: an edit evicts only the pages that read the file, and a
// cache tag lets data outside the project evict the pages that use it.
func TestRenderCache(t *testing.T) {
	ctx := context.Background()
	s, err := newSite(ctx, themeDir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"/blog/", "/about/"} {
		if _, err := s.Render(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := s.Render(ctx, "/blog/")
	b, _ := s.Render(ctx, "/blog/")
	if a != b {
		t.Fatal("a second render of the same URL did not come from the cache")
	}
	dropped, err := s.Apply(ctx, []watch.Change{{Path: "components/PostLink.html", Op: watch.Modified}})
	if err != nil || !slices.Equal(dropped, []string{"/blog/"}) {
		t.Fatalf("Apply dropped %v, %v", dropped, err)
	}
	if _, err := s.Render(ctx, "/blog/"); err != nil {
		t.Fatal(err)
	}
	if got := s.InvalidateTags("posts"); !slices.Equal(got, []string{"/blog/"}) {
		t.Fatalf("InvalidateTags dropped %v", got)
	}
}

// Dev adds the toolbar, live reload and the component playground.
func TestDevServer(t *testing.T) {
	h := open(t, true, false)
	_, body := get(t, h, "/blog/")
	for _, want := range []string{`<details id="brick-devbar" data-brick-devbar>`, `/_brick/live`, `<a href="/_brick/play/PostLink"`, `data-brick-loc="components/PostLink.html:6"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("dev page lacks %q:\n%s", want, body)
		}
	}
	code, body := get(t, h, "/_brick/play/PostLink?title=Hi&url=/x/")
	if code != http.StatusOK || !strings.HasPrefix(body, playPage) {
		t.Fatalf("playground %d:\n%s", code, body)
	}
	if code, _ := get(t, open(t, false, false), "/_brick/play/PostLink?title=Hi&url=/x/"); code != http.StatusNotFound {
		t.Fatalf("playground without Dev: %d", code)
	}
}

// A render error is shown in the page in Dev, and only as a bare 500 otherwise.
func TestDevInlineError(t *testing.T) {
	const want = `<pre class="brick-error" style="background:#fff1f0;color:#a8071a;border:1px solid #ffa39e;padding:12px;white-space:pre-wrap;font:13px/1.5 monospace">content/broken.html:1:7: at &lt;len 3&gt;: error calling len: len of type int</pre>`
	for _, dev := range []bool{true, false} {
		m := themeFS(t)
		m["content/broken.html"] = &fstest.MapFile{Data: []byte("<p>{{ len 3 }}</p>\n")}
		s, err := newSiteFS(context.Background(), m, dev, false)
		if err != nil {
			t.Fatal(err)
		}
		code, body := get(t, s.Handler(), "/broken/")
		if code != http.StatusInternalServerError || strings.Contains(body, want) != dev {
			t.Fatalf("dev=%v: %d\n%s", dev, code, body)
		}
		if !dev && body != "Internal Server Error\n" {
			t.Fatalf("production error body %q", body)
		}
	}
}

// Route.Meta carries the frontmatter, so a menu is built from an ordinary
// key; weight orders it and a page without the key is left out.
func TestMenuLinks(t *testing.T) {
	s, err := newSite(context.Background(), themeDir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := menuLinks(s, "main")
	want := []menuLink{{"Home", "/"}, {"Blog", "/blog/"}, {"About", "/about/"}}
	if !slices.Equal(got, want) {
		t.Fatalf("menu: %v", got)
	}
	if none := menuLinks(s, "footer"); len(none) != 0 {
		t.Fatalf("footer menu: %v", none)
	}
	// Routes returns copies: editing one changes nothing in the site.
	rs := s.Routes()
	rs[0].Meta["menu"] = "elsewhere"
	if again := menuLinks(s, "main"); !slices.Equal(again, want) {
		t.Fatalf("Meta edit leaked into the site: %v", again)
	}
}

// watchSite applies a file edit to a site served with Handler, with no Dev.
func TestWatchSite(t *testing.T) {
	dir := t.TempDir()
	for p, f := range themeFS(t) {
		dst := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := newSite(ctx, dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- watchSite(ctx, s, 10*time.Millisecond) }()
	h := s.Handler()
	if _, body := get(t, h, "/about/"); !strings.Contains(body, "One person, one notebook.") {
		t.Fatalf("before the edit:\n%s", body)
	}
	about := filepath.Join(dir, "content/about.html")
	// The watcher takes its first snapshot on its own goroutine, so an edit
	// written before that snapshot becomes the baseline and is never seen.
	// Rewriting with fresh content until the page shows it makes the test
	// independent of that ordering, which only loses under heavy load.
	edit := func(n int) {
		page := fmt.Sprintf("---\ntitle: About\nmenu: main\nweight: 3\n---\n<h1>About</h1>\n<p>Two people.</p>\n<!-- edit %d -->\n", n)
		if err := os.WriteFile(about, []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	edit(0)
	deadline := time.Now().Add(30 * time.Second)
	for n := 1; ; n++ {
		if _, body := get(t, h, "/about/"); strings.Contains(body, "Two people.") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the edit never reached the served page")
		}
		time.Sleep(20 * time.Millisecond)
		if n%25 == 0 {
			edit(n)
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("watchSite ended with %v", err)
	}
}
