package site

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// fragmentSite has two pages on one layout (a sidebar that marks the
// current page server-side, as a brick:current link) and one page on
// another layout.
func fragmentSite() fstest.MapFS {
	link := func(u string) string {
		return `<a href="` + u + `"{{ if eq .Page.URL "` + u + `" }} aria-current="page"{{ end }} brick:current.exact>` + u + `</a>`
	}
	return fstest.MapFS{
		"site.yaml":            {Data: []byte("title: T\n")},
		"layouts/default.html": {Data: []byte(`<html><head><title>{{ .Title }}</title><meta name="description" content="{{ .Title }} page"></head><body>{{ assets }}<nav>` + link("/a/") + link("/b/") + `<svg><title>icon</title></svg></nav><main>{{ children }}</main></body></html>`)},
		"layouts/plain.html":   {Data: []byte(`<html><head><title>{{ .Title }}</title></head><body>{{ assets }}<p>plain</p>{{ children }}</body></html>`)},
		"content/a.html":       {Data: []byte("---\ntitle: A\n---\n<h1>page a</h1>")},
		"content/b.html":       {Data: []byte("---\ntitle: B\n---\n<h1>page b</h1><a href=\"/a/\">to a</a>")},
		"content/c.html":       {Data: []byte("---\ntitle: C\nlayout: plain\n---\n<h1>page c</h1>")},
	}
}

// navFetch GETs url from h, with the Brick-Nav header when nav is set.
func navFetch(h http.Handler, url, nav string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if nav != "" {
		req.Header.Set(headerNav, nav)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A navigation that shows the same layout gets only the page, its title,
// description and assets; another layout, or a stale fingerprint, gets
// the full page. The sidebar's server-side aria-current on a
// brick:current link does not make each page a layout of its own.
func TestFragmentNavigation(t *testing.T) {
	t.Parallel()
	s := newSite(t, fragmentSite(), Options{Engine: brick.Options{Navigation: true}})
	h := s.Handler()
	a := navFetch(h, "/a/", "")
	fp := a.Header().Get(headerLayout)
	if fp == "" || !strings.Contains(a.Body.String(), `<meta name="brick-layout" content="`+fp+`">`) {
		t.Fatalf("a page names its layout in the header and the head:\n%v\n%s", a.Header(), a.Body)
	}
	if !strings.Contains(a.Body.String(), brick.PageStartMark+"<h1>page a</h1>"+brick.PageEndMark) {
		t.Fatalf("the page is marked inside its layout: %s", a.Body)
	}
	if !strings.Contains(a.Header().Get("Vary"), headerNav) {
		t.Fatalf("Vary: %q", a.Header().Get("Vary"))
	}
	b := navFetch(h, "/b/", fp)
	body := b.Body.String()
	if b.Header().Get(headerFragment) != "1" || b.Header().Get(headerLayout) != fp {
		t.Fatalf("same layout: want a fragment, got %v\n%s", b.Header(), body)
	}
	for _, want := range []string{"<title>B</title>", `<meta name="description" content="B page">`, `</brick-assets><h1>page b</h1><a href="/a/">to a</a></body>`} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment lacks %q:\n%s", want, body)
		}
	}
	for _, not := range []string{"<nav>", "<main>", brick.PageStartMark, "<title>icon</title>"} {
		if strings.Contains(body, not) {
			t.Errorf("fragment carries layout part %q:\n%s", not, body)
		}
	}
	c := navFetch(h, "/c/", fp)
	if c.Header().Get(headerFragment) != "" || !strings.Contains(c.Body.String(), "<p>plain</p>") || c.Header().Get(headerLayout) == fp {
		t.Fatalf("another layout: want the full page with its own fingerprint, got %v\n%s", c.Header(), c.Body)
	}
	if stale := navFetch(h, "/b/", "0123"); stale.Header().Get(headerFragment) != "" || !strings.Contains(stale.Body.String(), "<nav>") {
		t.Fatalf("a stale fingerprint gets the full page: %s", stale.Body)
	}
}

// Without nav.js nothing changes: no marks, no fingerprint, no header, and
// the bytes are Site.Render's (which Export uses and never marks).
func TestFragmentOffWithoutNavigation(t *testing.T) {
	t.Parallel()
	s := newSite(t, fragmentSite(), Options{})
	rec := navFetch(s.Handler(), "/a/", "anything")
	if rec.Header().Get(headerLayout) != "" || rec.Header().Get(headerFragment) != "" || strings.Contains(rec.Body.String(), "brick:page") || strings.Contains(rec.Body.String(), layoutMetaName) {
		t.Fatalf("a page without nav.js is unchanged: %v\n%s", rec.Header(), rec.Body)
	}
	if want := page(t, s, "/a/"); rec.Body.String() != want {
		t.Fatalf("served bytes differ from Render:\n%s\n%s", rec.Body, want)
	}
	nav := newSite(t, fragmentSite(), Options{Engine: brick.Options{Navigation: true}})
	if strings.Contains(page(t, nav, "/a/"), "brick:page") {
		t.Fatal("Site.Render (and so Export) never carries the marks")
	}
}

// A fragment's URLs are rewritten under the base path like a full page's.
func TestFragmentBasePath(t *testing.T) {
	t.Parallel()
	s := newSite(t, fragmentSite(), Options{BasePath: "/docs", Engine: brick.Options{Navigation: true}})
	h := s.Handler()
	fp := navFetch(h, "/docs/a/", "").Header().Get(headerLayout)
	b := navFetch(h, "/docs/b/", fp)
	if b.Header().Get(headerFragment) != "1" || !strings.Contains(b.Body.String(), `<a href="/docs/a/">to a</a>`) {
		t.Fatalf("fragment under a base path: %v\n%s", b.Header(), b.Body)
	}
}

// pageRegion cuts only a page with exactly one pair of marks in order.
func TestPageRegion(t *testing.T) {
	t.Parallel()
	s, e := brick.PageStartMark, brick.PageEndMark
	cases := map[string]bool{
		"<b>" + s + "x" + e + "</b>": true,
		"<b>x</b>":                   false,
		s + "x" + e + s + "y" + e:    false,
		e + "x" + s:                  false,
		"<b>" + s + "x</b>":          false,
	}
	for in, want := range cases {
		if _, _, ok := pageRegion([]byte(in)); ok != want {
			t.Errorf("pageRegion(%q) = %v, want %v", in, ok, want)
		}
	}
}
