package site

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// tagFeedSite is a blog with a dynamic output page per tag:
// content/tags/[slug]/feed.xml.html, its value list from posts.slugs, and
// an HTML page at /tags/[slug]/ beside it.
func tagFeedSite() []string {
	front := "---\nuse: {posts: ~/scripts/posts.star}\nbrick: {paths: posts.slugs}\n---\n"
	return []string{
		"scripts/posts.star", "# fake",
		"layouts/default.html", "<html>{{ children }}</html>",
		"site.yaml", "baseURL: https://example.com\n",
		"content/index.html", "home",
		"content/tags/[slug].html", front + "<h1>{{ .Page.Route.slug }}</h1>",
		"content/tags/[slug]/feed.xml.html", front + "<?xml version=\"1.0\"?>\n<rss><title>{{ .Page.Route.slug }} &amp; more</title></rss>",
	}
}

// A dynamic output page (G13) answers each value of its paths: list at a
// file URL, with its format's Content-Type, its prolog and no layout; the
// slash spelling, a value off the list and a [param] in the file name are
// not pages. Routes lists the pattern as a file URL.
func TestDynamicOutputServe(t *testing.T) {
	s := newSite(t, project(tagFeedSite()...), Options{Engine: brick.Options{Scripting: newPosts()}, Sitemap: &Sitemap{}})
	h := s.Handler()
	rec, body := fetch(h, "/tags/hello/feed.xml")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != outputTypes["xml"] || body != "<?xml version=\"1.0\"?>\n<rss><title>hello &amp; more</title></rss>" {
		t.Fatalf("feed: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), body)
	}
	if rec, body := fetch(h, "/tags/hello/"); rec.Code != http.StatusOK || !strings.Contains(body, "<html><h1>hello</h1>") {
		t.Fatalf("html page beside the feed: %d %q", rec.Code, body)
	}
	for _, u := range []string{"/tags/nope/feed.xml", "/tags/hello/feed.xml/", "/tags/hello/other.xml"} {
		if rec, _ := fetch(h, u); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", u, rec.Code)
		}
	}
	var pats []string
	for _, r := range s.Patterns() {
		pats = append(pats, r.URL+"|"+r.Output)
	}
	if !slices.Equal(pats, []string{"/tags/[slug]/|", "/tags/[slug]/feed.xml|xml"}) {
		t.Fatalf("patterns: %v", pats)
	}
	// The sitemap lists pages, not feeds.
	if _, sm := fetch(h, "/sitemap.xml"); strings.Contains(sm, "feed.xml") || !strings.Contains(sm, "/tags/hello/</loc>") {
		t.Fatalf("sitemap:\n%s", sm)
	}
}

// Export writes one file per paths: entry at the output page's file name.
func TestDynamicOutputExport(t *testing.T) {
	s := newSite(t, project(tagFeedSite()...), Options{Engine: brick.Options{Scripting: newPosts()}, BasePath: "/docs"})
	dir := filepath.Join(t.TempDir(), "out")
	rep, err := s.Export(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pages != 5 {
		t.Fatalf("pages %d, want 5 (home, two tag pages, two feeds)", rep.Pages)
	}
	for _, slug := range []string{"hello", "second"} {
		b, err := os.ReadFile(filepath.Join(dir, "tags", slug, "feed.xml"))
		if err != nil || string(b) != "<?xml version=\"1.0\"?>\n<rss><title>"+slug+" &amp; more</title></rss>" {
			t.Fatalf("%s feed: %q %v", slug, b, err)
		}
	}
}

// meSite is a per-request page beside a cached one, both reading the
// visitor from .Ctx, plus per-request pages whose loader redirects or sets
// its own Cache-Control.
func meSite() []string {
	return []string{
		"s/g.star", "# fake",
		"content/me.html", "---\nbrick: {per_request: true}\n---\nme {{ or .Ctx.user \"anonymous\" }}",
		"content/shared.html", "shared {{ or .Ctx.user \"anonymous\" }}",
		"content/out.html", "---\ndo: moved\nuse: {g: ~/s/g.star}\nload: g.one\nbrick: {per_request: true}\n---\nout",
		"content/mine.html", "---\ndo: cached\nuse: {g: ~/s/g.star}\nload: g.one\nbrick: {per_request: true}\n---\nmine",
		"content/users/[slug].html", "---\nbrick: {per_request: true}\n---\n{{ .Page.Route.slug }} for {{ or .Ctx.user \"anonymous\" }}",
	}
}

// A per-request page (G14) renders for each visitor with Options.Request's
// values and is never cached; the shared page beside it still is, and its
// body never sees a visitor.
func TestPerRequestPage(t *testing.T) {
	s := newSite(t, project(meSite()...), Options{Engine: brick.Options{Scripting: gateScripting{}}, Request: cookieUser})
	h := s.Handler()
	for _, user := range []string{"ann", "bob", ""} {
		want := user
		if want == "" {
			want = "anonymous"
		}
		rec := getAs(h, "/me/", user)
		if rec.Body.String() != "me "+want || rec.Header().Get("Cache-Control") != PerRequestCacheControl {
			t.Fatalf("%q: %q %v", user, rec.Body.String(), rec.Header())
		}
		if rec := getAs(h, "/users/x/", user); rec.Body.String() != "x for "+want {
			t.Fatalf("dynamic per-request %q: %q", user, rec.Body.String())
		}
		if rec := getAs(h, "/shared/", user); rec.Body.String() != "shared anonymous" || rec.Header().Get("Cache-Control") != "" {
			t.Fatalf("shared %q: %q %v", user, rec.Body.String(), rec.Header())
		}
	}
	s.cacheMu.Lock()
	keys := make([]string, 0, len(s.cache))
	for k := range s.cache {
		keys = append(keys, k.url)
	}
	s.cacheMu.Unlock()
	if !slices.Equal(keys, []string{"/shared/"}) {
		t.Fatalf("cache holds %v, want only /shared/", keys)
	}
	for _, r := range s.Routes() {
		if r.PerRequest != (r.URL == "/me/" || r.URL == "/out/" || r.URL == "/mine/") {
			t.Fatalf("route %s PerRequest %v", r.URL, r.PerRequest)
		}
	}
}

// A per-request page's own loader saw the visitor, so its redirect is sent
// with no-store, as an island's is; a Cache-Control the loader sets wins
// over the default.
func TestPerRequestHeaders(t *testing.T) {
	files := append(meSite(),
		"content/lost.html", "---\ndo: missing\nuse: {g: ~/s/g.star}\nload: g.one\nbrick: {per_request: true}\n---\nlost",
		"content/gone.html", "---\ndo: missing\nuse: {g: ~/s/g.star}\nload: g.one\n---\ngone",
		"content/404.html", "no such page",
	)
	h := newSite(t, project(files...), Options{Engine: brick.Options{Scripting: gateScripting{}}}).Handler()
	if rec := getAs(h, "/out/", ""); rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Cache-Control") != noStore {
		t.Fatalf("redirect: %d %v", rec.Code, rec.Header())
	}
	if rec := getAs(h, "/mine/", ""); rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("loader's own Cache-Control: %v", rec.Header())
	}
	// Its loader's not-found is per visitor too: the 404 page is sent
	// no-store. The same not-found from a shared page is not marked.
	if rec := getAs(h, "/lost/", ""); rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != noStore || rec.Body.String() != "no such page" {
		t.Fatalf("per-request not found: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if rec := getAs(h, "/gone/", ""); rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") == noStore {
		t.Fatalf("shared not found: %d %v", rec.Code, rec.Header())
	}
}

// Export skips a per-request page, static or dynamic, and says so in the
// report; the rest of the site is written.
func TestPerRequestExport(t *testing.T) {
	files := []string{
		"content/index.html", "home",
		"content/me.html", "---\nbrick: {per_request: true}\n---\nme",
		"content/users/[slug].html", "---\nbrick: {per_request: true}\n---\nno paths: needed",
	}
	s := newSite(t, project(files...), Options{})
	dir := filepath.Join(t.TempDir(), "out")
	rep, err := s.Export(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pages != 1 || !slices.Equal(rep.Skipped, []string{"/me/", "/users/[slug]/"}) {
		t.Fatalf("report %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, "me", "index.html")); !os.IsNotExist(err) {
		t.Fatalf("per-request page written: %v", err)
	}
	if _, err := New(context.Background(), Options{FS: project("content/a.html", "---\nbrick: {per_request: yes please}\n---\na")}); err == nil || !strings.Contains(err.Error(), "per_request") {
		t.Fatalf("non-bool per_request: %v", err)
	}
}
