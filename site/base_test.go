package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

func TestNormalizeBase(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"": "", "/": "", "/docs": "/docs", "/docs/": "/docs", "/a/b": "/a/b"} {
		if got, err := normalizeBase(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"docs", "//cdn", "/a/../b", "/a//b", "/a b", "/a?x"} {
		if _, err := normalizeBase(bad); err == nil {
			t.Errorf("%q: accepted", bad)
		}
	}
}

// The rewrite adds the base to root-absolute URLs in URL attributes and
// srcset, and leaves everything else alone.
func TestRebase(t *testing.T) {
	t.Parallel()
	in := `<a href="/blog/">b</a><a href='/x'>x</a><a href="/">home</a><img src="/i.png" srcset="/i-160.png 160w, /i-320.png 320w">` +
		`<form action="/search/"><button formaction="/go/">go</button></form><video poster="/p.jpg"></video>` +
		`<a href="//cdn.test/x">cdn</a><a href="https://e.test/">abs</a><a href="rel/">rel</a><a href="#top">frag</a>` +
		`<a href="/docs/own/">own</a><a href="/engine/">sibling</a><a href="/engine">sibling root</a>` +
		`<pre>href=&#34;/code/&#34;</pre><p>see /plain/ text</p><div data-url="/data/"></div>`
	want := `<a href="/docs/blog/">b</a><a href='/docs/x'>x</a><a href="/docs/">home</a><img src="/docs/i.png" srcset="/docs/i-160.png 160w, /docs/i-320.png 320w">` +
		`<form action="/docs/search/"><button formaction="/docs/go/">go</button></form><video poster="/docs/p.jpg"></video>` +
		`<a href="//cdn.test/x">cdn</a><a href="https://e.test/">abs</a><a href="rel/">rel</a><a href="#top">frag</a>` +
		`<a href="/docs/docs/own/">own</a><a href="/engine/">sibling</a><a href="/engine">sibling root</a>` +
		`<pre>href=&#34;/code/&#34;</pre><p>see /plain/ text</p><div data-url="/data/"></div>`
	if got := string(rebase([]byte(in), "/docs", []string{"/engine/"})); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := string(rebase([]byte(in), "", nil)); got != in {
		t.Fatal("no base must change nothing")
	}
}

// A site under a base path answers only under it, with every link, asset
// and redirect carrying the base, and actions posted and patched there.
func TestSiteUnderBasePath(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["layouts/default.html"] = &fstest.MapFile{Data: []byte("<html><head><title>{{ .Title }}</title></head><body>{{ assets }}<nav><a href=\"/blog/\">Blog</a></nav>{{ children }}</body></html>")}
	fsys["components/Card.html"] = &fstest.MapFile{Data: []byte("<brick:style>.c { color: red }</brick:style><b class=\"c\">{{ .Props.t }}</b>")}
	fsys["components/Toggle.html"] = &fstest.MapFile{Data: []byte("---\nstate:\n  on: false\n---\n<brick:script>toggle</brick:script>\n<div><b>{{ .State.on }}</b><a href=\"/about/\">about</a><button brick:click=\"flip\">x</button></div>")}
	fsys["content/toggle.html"] = &fstest.MapFile{Data: []byte("<Toggle />")}
	s := newSite(t, fsys, Options{BasePath: "/docs/", Engine: brick.Options{Scripting: flipScripting{}}})
	h := s.Handler()
	do := func(method, url, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Brick-Action", "1")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	about := do("GET", "/docs/about/", "")
	if about.Code != 200 {
		t.Fatalf("about: %d %s", about.Code, about.Body)
	}
	body := about.Body.String()
	for _, want := range []string{`<head><meta name="brick-base" content="/docs">`, `<a href="/docs/blog/">Blog</a>`, `<b class="c">about</b>`} {
		if !strings.Contains(body, want) {
			t.Errorf("about lacks %q:\n%s", want, body)
		}
	}
	css := regexp.MustCompile(`href="(/docs/_brick/c/[^"]+\.css)"`).FindStringSubmatch(body)
	if css == nil {
		t.Fatalf("no rebased stylesheet link in %s", body)
	}
	if rec := do("GET", css[1], ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), ".c") {
		t.Errorf("stylesheet under the base: %d", rec.Code)
	}
	if rec := do("GET", "/docs/logo.svg", ""); rec.Code != 200 {
		t.Errorf("static file under the base: %d", rec.Code)
	}
	for url, loc := range map[string]string{"/": "/docs/", "/docs": "/docs/", "/docs/about": "/docs/about/"} {
		if rec := do("GET", url, ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != loc {
			t.Errorf("%s: %d → %q, want %q", url, rec.Code, rec.Header().Get("Location"), loc)
		}
	}
	for _, url := range []string{"/about/", "/_brick/c/x.css", "/docsx/about/"} {
		if rec := do("GET", url, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s outside the base: %d", url, rec.Code)
		}
	}

	page := do("GET", "/docs/toggle/", "").Body.String()
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`).FindStringSubmatch(page)
	if m == nil || !strings.Contains(page, `<a href="/docs/about/">about</a>`) {
		t.Fatalf("toggle: %s", page)
	}
	act := do("POST", "/docs/_brick/action", `{"handle":"`+m[1]+`","token":"`+m[2]+`","action":"flip"}`)
	if act.Code != 200 || !strings.Contains(act.Body.String(), "<b>true</b>") || !strings.Contains(act.Body.String(), `<a href="/docs/about/">about</a>`) {
		t.Fatalf("action under the base: %d %s", act.Code, act.Body)
	}
}

// Mount routes each request to the site with the longest matching base;
// the root site gets the rest.
func TestMount(t *testing.T) {
	t.Parallel()
	mk := func(base, word string) *Site {
		return newSite(t, project("content/index.html", word, "content/page.html", word+"-page"), Options{BasePath: base})
	}
	h, err := Mount(mk("", "root"), mk("/docs", "docs"), mk("/docs/api", "api"))
	if err != nil {
		t.Fatal(err)
	}
	for url, want := range map[string]string{"/": "root", "/page/": "root-page", "/docs/": "docs", "/docs/page/": "docs-page", "/docs/api/": "api", "/docs/api/page/": "api-page"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %q, want %q", url, rec.Code, rec.Body, want)
		}
	}
	if _, err := Mount(mk("/a", "a"), mk("/a/", "b")); err == nil || !strings.Contains(err.Error(), "two sites") {
		t.Errorf("two sites at one base: %v", err)
	}
	h, _ = Mount(mk("/a", "a"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/b/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("outside every base: %d", rec.Code)
	}
}

// An export for a base path has rebased pages and assets beside them, so
// copying it to what a server serves at the base just works.
func TestExportUnderBasePath(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["components/Card.html"] = &fstest.MapFile{Data: []byte("<brick:style>.c { color: red }</brick:style><b class=\"c\">{{ .Props.t }}</b>")}
	fsys["layouts/default.html"] = &fstest.MapFile{Data: []byte("<html><head></head><body>{{ assets }}<a href=\"/blog/\">Blog</a>{{ children }}</body></html>")}
	s := newSite(t, fsys, Options{BasePath: "/docs"})
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := s.Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "about", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	css := regexp.MustCompile(`href="/docs/(_brick/c/[^"]+\.css)"`).FindSubmatch(b)
	if !strings.Contains(string(b), `<a href="/docs/blog/">Blog</a>`) || css == nil {
		t.Fatalf("exported page: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(string(css[1])))); err != nil {
		t.Errorf("the linked asset is not in the export: %v", err)
	}
}

// The base meta goes right after <head> (matched in any case), or first
// when a page has no head; with no base nothing changes.
func TestInjectBase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, base, want string }{
		{"<html><head><title>t</title></head></html>", "/docs", `<html><head><meta name="brick-base" content="/docs"><title>t</title></head></html>`},
		{"<HTML><HEAD></HEAD></HTML>", "/docs", `<HTML><HEAD><meta name="brick-base" content="/docs"></HEAD></HTML>`},
		{"<p>fragment</p>", "/docs", `<meta name="brick-base" content="/docs"><p>fragment</p>`},
		{`<!doctype html><html><head lang="en" data-x><title>t</title></head></html>`, "/docs", `<!doctype html><html><head lang="en" data-x><meta name="brick-base" content="/docs"><title>t</title></head></html>`},
		{"<body><header>top</header></body>", "/docs", `<meta name="brick-base" content="/docs"><body><header>top</header></body>`},
		{"<html><head></head></html>", "", "<html><head></head></html>"},
	} {
		if got := string(injectBase([]byte(tc.in), tc.base)); got != tc.want {
			t.Errorf("injectBase(%q, %q):\n got  %s\n want %s", tc.in, tc.base, got, tc.want)
		}
	}
}

// rebaseWriter rewrites an HTML response and drops its now-wrong
// Content-Length; any other response passes through with its status.
func TestRebaseWriter(t *testing.T) {
	t.Parallel()
	s := &Site{opts: Options{BasePath: "/docs"}}
	serve := func(contentType string, status int, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		rw := &rebaseWriter{ResponseWriter: rec}
		rw.Header().Set("Content-Type", contentType)
		rw.Header().Set("Content-Length", "999")
		if status != 0 {
			rw.WriteHeader(status)
		}
		_, _ = rw.Write([]byte(body))
		rw.flush(s)
		return rec
	}
	html := serve(htmlContentType, 0, `<a href="/x/">x</a>`)
	if html.Code != http.StatusOK || html.Body.String() != `<a href="/docs/x/">x</a>` || html.Header().Get("Content-Length") != "" {
		t.Errorf("html: %d %q %q", html.Code, html.Body, html.Header().Get("Content-Length"))
	}
	text := serve("text/plain; charset=utf-8", http.StatusGone, `<a href="/x/">x</a>`)
	if text.Code != http.StatusGone || text.Body.String() != `<a href="/x/">x</a>` || text.Header().Get("Content-Length") != "999" {
		t.Errorf("non-HTML must pass through: %d %q %q", text.Code, text.Body, text.Header().Get("Content-Length"))
	}
}

// ListenAndServeHandler shuts down cleanly when its context ends, returns
// the listener's error when it cannot listen, and ListenAndServeAll
// refuses an empty site list before listening at all.
func TestListenAndServeHandler(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ListenAndServeHandler(ctx, "127.0.0.1:0", http.NotFoundHandler()) }()
	cancel()
	if err := <-done; err != nil {
		t.Errorf("shutdown after the context ended: %v", err)
	}
	if err := ListenAndServeHandler(context.Background(), "127.0.0.1:-1", http.NotFoundHandler()); err == nil {
		t.Error("an address that cannot be listened on must be an error")
	}
	if err := ListenAndServeAll(context.Background(), "127.0.0.1:0"); err == nil || !strings.Contains(err.Error(), "at least one site") {
		t.Errorf("no sites: %v", err)
	}
}
